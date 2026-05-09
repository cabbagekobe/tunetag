// Package flac reads and writes FLAC metadata blocks. The audio
// data is preserved untouched; only the metadata region between the
// "fLaC" marker and the first audio frame is rewritten.
//
// Only VORBIS_COMMENT and PICTURE blocks are typed; the rest
// (STREAMINFO, SEEKTABLE, CUESHEET, APPLICATION, PADDING, unknown)
// round-trip as raw bytes via RawBlock so that callers don't need
// the package to understand every block type to mutate metadata.
//
// A *File is not safe for concurrent use.
package flac

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Magic is the four-byte stream marker at the start of every FLAC
// stream.
var Magic = [4]byte{'f', 'L', 'a', 'C'}

// ErrNoFLAC is returned by Read when the input does not begin with
// the "fLaC" marker.
var ErrNoFLAC = errors.New("flac: missing fLaC marker")

// File is the parsed metadata region of a FLAC file. Blocks holds
// every metadata block in stream order; Blocks[0] is always
// STREAMINFO. The audioOffset records where the first audio frame
// begins in the source file (used by WriteFile only).
type File struct {
	Blocks      []Block
	audioOffset int64
}

// Read parses the metadata region of rs. The audio frames are not
// consumed.
func Read(rs io.ReadSeeker) (*File, error) {
	var marker [4]byte
	if _, err := io.ReadFull(rs, marker[:]); err != nil {
		return nil, err
	}
	if marker != Magic {
		return nil, ErrNoFLAC
	}
	var blocks []Block
	streamInfoSeen := false
	for {
		blockType, last, size, err := readBlockHeader(rs)
		if err != nil {
			return nil, fmt.Errorf("flac: read block header: %w", err)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(rs, body); err != nil {
			return nil, fmt.Errorf("flac: read block body (type %d): %w", blockType, err)
		}
		if !streamInfoSeen {
			if blockType != BlockStreamInfo {
				return nil, fmt.Errorf("flac: first metadata block must be STREAMINFO, got type %d", blockType)
			}
			streamInfoSeen = true
		}
		var b Block
		switch blockType {
		case BlockVorbisComment:
			vc, err := parseVorbisComment(body)
			if err != nil {
				return nil, err
			}
			b = vc
		case BlockPicture:
			p, err := parsePicture(body)
			if err != nil {
				return nil, err
			}
			b = p
		case BlockPadding:
			b = &PaddingBlock{Size: int(size)}
		default:
			b = &RawBlock{BlockType: blockType, Body: body}
		}
		blocks = append(blocks, b)
		if last {
			break
		}
	}
	off, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	return &File{Blocks: blocks, audioOffset: off}, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// VorbisComment returns the first VORBIS_COMMENT block, creating one
// (and inserting it after STREAMINFO) if absent. The returned block
// is the one stored inside f, so mutations are persisted across
// subsequent WriteFile calls.
func (f *File) VorbisComment() *VorbisComment {
	for _, b := range f.Blocks {
		if vc, ok := b.(*VorbisComment); ok {
			return vc
		}
	}
	vc := &VorbisComment{Vendor: VendorString}
	// Insert directly after STREAMINFO (Blocks[0]).
	f.Blocks = append(f.Blocks[:1], append([]Block{vc}, f.Blocks[1:]...)...)
	return vc
}

// Pictures returns every PICTURE block in stream order.
func (f *File) Pictures() []*Picture {
	var out []*Picture
	for _, b := range f.Blocks {
		if p, ok := b.(*Picture); ok {
			out = append(out, p)
		}
	}
	return out
}

// AddPicture appends a PICTURE block.
func (f *File) AddPicture(p *Picture) {
	f.Blocks = append(f.Blocks, p)
}

// RemovePictures deletes every PICTURE block.
func (f *File) RemovePictures() {
	out := f.Blocks[:0]
	for _, b := range f.Blocks {
		if _, ok := b.(*Picture); !ok {
			out = append(out, b)
		}
	}
	f.Blocks = out
}

// encodeMetadata returns the bytes that go between the "fLaC" marker
// and the first audio frame: every block plus its 4-byte header,
// with the last block's last-flag set.
func (f *File) encodeMetadata() ([]byte, error) {
	if len(f.Blocks) == 0 {
		return nil, errors.New("flac: cannot encode file with zero blocks")
	}
	if f.Blocks[0].Type() != BlockStreamInfo {
		return nil, errors.New("flac: first block must be STREAMINFO")
	}
	var buf bytes.Buffer
	for i, b := range f.Blocks {
		body, err := b.Encode()
		if err != nil {
			return nil, fmt.Errorf("flac: encode block %d (type %d): %w", i, b.Type(), err)
		}
		last := i == len(f.Blocks)-1
		if err := writeBlockHeader(&buf, b.Type(), last, uint32(len(body))); err != nil {
			return nil, err
		}
		buf.Write(body)
	}
	return buf.Bytes(), nil
}

// WriteFile writes f back to path. When the new non-padding blocks
// fit within the bytes occupied by the previous metadata region the
// difference is absorbed by a single trailing PADDING block so the
// audio bytes stay in place. Otherwise the file is rewritten via a
// temporary file and atomic rename.
func (f *File) WriteFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			src.Close()
		}
	}()

	existing, err := scanMetadataSize(src)
	if err != nil {
		return err
	}

	blocksNoPad := stripPadding(f.Blocks)
	sizeNoPad, err := encodedBlocksSize(blocksNoPad)
	if err != nil {
		return err
	}
	available := int(existing) - 4 // bytes available for blocks excluding "fLaC"

	switch {
	case sizeNoPad == available:
		// Exact fit; no padding block needed.
		closed = true
		src.Close()
		return f.writeMetadataInPlace(path, blocksNoPad)
	case sizeNoPad+4 <= available:
		// Room for an explicit PADDING block to absorb the slack.
		padBody := available - sizeNoPad - 4
		padded := append(append([]Block(nil), blocksNoPad...), &PaddingBlock{Size: padBody})
		closed = true
		src.Close()
		return f.writeMetadataInPlace(path, padded)
	}

	// Full rewrite using the user's block layout (with their padding).
	if _, err := src.Seek(int64(existing), io.SeekStart); err != nil {
		return err
	}
	encoded, err := f.encodeMetadata()
	if err != nil {
		return err
	}
	if err := f.rewriteAtomic(path, encoded, src); err != nil {
		return err
	}
	closed = true
	return src.Close()
}

func (f *File) writeMetadataInPlace(path string, blocks []Block) error {
	tmp := &File{Blocks: blocks}
	body, err := tmp.encodeMetadata()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := out.Write(Magic[:]); err != nil {
		return err
	}
	_, err = out.Write(body)
	return err
}

func stripPadding(blocks []Block) []Block {
	out := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if _, ok := b.(*PaddingBlock); ok {
			continue
		}
		out = append(out, b)
	}
	return out
}

func encodedBlocksSize(blocks []Block) (int, error) {
	total := 0
	for _, b := range blocks {
		body, err := b.Encode()
		if err != nil {
			return 0, err
		}
		total += 4 + len(body) // 4-byte block header per block
	}
	return total, nil
}

func (f *File) rewriteAtomic(path string, encoded []byte, body io.Reader) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-flac-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(Magic[:]); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(encoded); err != nil {
		cleanup()
		return err
	}
	if _, err := io.Copy(tmp, body); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func scanMetadataSize(f *os.File) (uint32, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	var marker [4]byte
	if _, err := io.ReadFull(f, marker[:]); err != nil {
		return 0, err
	}
	if marker != Magic {
		return 0, ErrNoFLAC
	}
	total := uint32(4)
	for {
		_, last, size, err := readBlockHeader(f)
		if err != nil {
			return 0, err
		}
		total += 4 + size
		if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
			return 0, err
		}
		if last {
			return total, nil
		}
	}
}

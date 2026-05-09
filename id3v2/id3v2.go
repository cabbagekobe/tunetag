// Package id3v2 reads and writes ID3v2.2, v2.3, and v2.4 tags
// (the variable-length block at the start of an MP3 file marked
// with "ID3").
//
// Reading
//
// Read parses any of the three supported revisions; v2.2 frame IDs
// are normalised to their canonical 4-character v2.3/2.4 equivalents
// when known, so callers always look frames up by the modern names
// (e.g. "TIT2", "APIC"). Tag-level unsynchronisation is undone
// transparently. The extended header (v2.3 / v2.4) is skipped on
// read and not preserved on write.
//
// Writing
//
// Encode emits a tag at t.Version, which must be V23 or V24 — v2.2
// is read-only because re-encoding to v2.2 would require demoting
// every frame body and is rarely useful. The default Padding is
// 1024 bytes; raise it to leave room for in-place edits in the
// surrounding file. Tag-level unsynchronisation and the extended
// header are never emitted.
//
// A *Tag is not safe for concurrent use.
package id3v2

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// DefaultPadding is the number of zero bytes Read attaches to a tag
// so that callers can re-encode in place without growing the file.
const DefaultPadding = 1024

// Tag is the in-memory representation of an ID3v2 tag.
type Tag struct {
	Version Version
	Flags   Flags
	Frames  []Frame // preserved in the order seen on disk
	Padding int     // target padding bytes for the next Encode
}

// Read parses an ID3v2 tag from the start of r. ErrNoTag is returned
// when r does not begin with the ID3v2 magic bytes.
func Read(r io.Reader) (*Tag, error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}
	body := make([]byte, h.Size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("id3v2: short body: %w", err)
	}
	if h.Flags&FlagUnsync != 0 {
		body = unsyncDecode(body)
	}
	if h.Flags&FlagExtended != 0 {
		body, err = stripExtendedHeader(h.Version, body)
		if err != nil {
			return nil, err
		}
	}
	frames, err := readFrames(h.Version, body)
	if err != nil {
		return nil, err
	}
	return &Tag{
		Version: h.Version,
		Flags:   h.Flags &^ FlagExtended,
		Frames:  frames,
		Padding: DefaultPadding,
	}, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*Tag, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Encode writes the tag (header + frames + padding) to w.
func (t *Tag) Encode(w io.Writer) error {
	return t.encodeWithPadding(w, t.Padding)
}

// encodeWithPadding is identical to Encode except the padding length
// is supplied explicitly. WriteFile uses this to grow the in-place
// padding so that the encoded tag exactly fills the bytes occupied
// by the previous tag.
func (t *Tag) encodeWithPadding(w io.Writer, pad int) error {
	if t.Version != V23 && t.Version != V24 {
		return fmt.Errorf("id3v2: writing %s is not supported (use V23 or V24)", t.Version)
	}
	var framesBuf bytes.Buffer
	for _, f := range t.Frames {
		if err := f.Encode(t.Version, &framesBuf); err != nil {
			return fmt.Errorf("id3v2: encode frame %q: %w", f.ID(), err)
		}
	}
	flags := t.Flags &^ (FlagExtended | FlagUnsync)
	if pad < 0 {
		pad = 0
	}
	if flags&FlagFooter != 0 {
		return errors.New("id3v2: footer (v2.4) encoding is not yet implemented")
	}
	payloadSize := uint32(framesBuf.Len() + pad)
	h := Header{Version: t.Version, Flags: flags, Size: payloadSize}
	if err := h.writeTo(w); err != nil {
		return err
	}
	if _, err := w.Write(framesBuf.Bytes()); err != nil {
		return err
	}
	if pad > 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

// framesEncodedSize returns the length of the encoded frames (no
// header, no padding) for sizing decisions in WriteFile.
func (t *Tag) framesEncodedSize() (uint32, error) {
	if t.Version != V23 && t.Version != V24 {
		return 0, fmt.Errorf("id3v2: writing %s is not supported", t.Version)
	}
	var buf bytes.Buffer
	for _, f := range t.Frames {
		if err := f.Encode(t.Version, &buf); err != nil {
			return 0, err
		}
	}
	return uint32(buf.Len()), nil
}

func stripExtendedHeader(v Version, body []byte) ([]byte, error) {
	if len(body) < 4 {
		return nil, errors.New("id3v2: extended header truncated")
	}
	if v == V24 {
		extSize := decodeSynchsafe(body[:4])
		if int(extSize) > len(body) || extSize < 4 {
			return nil, errors.New("id3v2: extended header size out of range")
		}
		return body[extSize:], nil
	}
	extSize := uint32(body[0])<<24 | uint32(body[1])<<16 | uint32(body[2])<<8 | uint32(body[3])
	if 4+int(extSize) > len(body) {
		return nil, errors.New("id3v2: extended header size out of range")
	}
	return body[4+extSize:], nil
}

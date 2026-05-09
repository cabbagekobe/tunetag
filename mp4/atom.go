// Package mp4 reads and writes iTunes-style metadata from MP4 /
// M4A containers.
//
// Scope (v1):
//   - Top-level boxes: ftyp, moov, mdat, free, skip, wide, uuid (raw).
//   - Inside moov: udta/meta/ilst is parsed; everything else is held
//     as raw bytes so writes do not perturb tracks.
//   - Both standard 4-character keys (©nam, ©ART, …) and freeform
//     "----" keys are supported.
//   - Fragmented MP4 (mvex/moof) is detected and rejected on write.
//
// Concurrency: a *File is not safe for concurrent use.
package mp4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FourCC is a four-character box / atom type code.
type FourCC [4]byte

func (f FourCC) String() string { return string(f[:]) }

// Equal compares against a 4-byte string literal.
func (f FourCC) Equal(s string) bool {
	if len(s) != 4 {
		return false
	}
	return f[0] == s[0] && f[1] == s[1] && f[2] == s[2] && f[3] == s[3]
}

// fourCC builds a FourCC from a 4-byte string. Panics if s is not
// exactly 4 bytes long; meant for internal use with literal keys.
func fourCC(s string) FourCC {
	if len(s) != 4 {
		panic("mp4: fourCC requires a 4-byte string, got " + s)
	}
	var f FourCC
	copy(f[:], s)
	return f
}

// Box is the parsed header of an ISO BMFF box.
type Box struct {
	Type       FourCC
	Size       uint64 // total size (header + body); 0 means "extends to EOF"
	HeaderSize int    // 8 or 16
	BodyOffset int64  // absolute offset of the body
	BodyLen    int64  // body length in bytes
}

// readBoxHeader reads the box header at off in r. fileSize is used
// only to interpret the special size==0 ("extends to EOF") case.
func readBoxHeader(r io.ReaderAt, off, fileSize int64) (Box, error) {
	var hdr [16]byte
	n, err := r.ReadAt(hdr[:8], off)
	if err != nil && n < 8 {
		return Box{}, fmt.Errorf("mp4: read box header at %d: %w", off, err)
	}
	rawSize := binary.BigEndian.Uint32(hdr[0:4])
	var typ FourCC
	copy(typ[:], hdr[4:8])
	headerSize := 8
	var totalSize uint64
	switch {
	case rawSize == 0:
		// Box extends to end of file.
		if off >= fileSize {
			return Box{}, fmt.Errorf("mp4: box at %d: size=0 with no remaining data", off)
		}
		totalSize = uint64(fileSize - off)
	case rawSize == 1:
		// 64-bit largesize follows.
		if _, err := r.ReadAt(hdr[8:16], off+8); err != nil {
			return Box{}, fmt.Errorf("mp4: read largesize at %d: %w", off+8, err)
		}
		totalSize = binary.BigEndian.Uint64(hdr[8:16])
		headerSize = 16
		if totalSize < 16 {
			return Box{}, fmt.Errorf("mp4: box at %d: largesize %d < 16", off, totalSize)
		}
	default:
		totalSize = uint64(rawSize)
		if totalSize < 8 {
			return Box{}, fmt.Errorf("mp4: box at %d: size %d < 8", off, totalSize)
		}
	}
	return Box{
		Type:       typ,
		Size:       totalSize,
		HeaderSize: headerSize,
		BodyOffset: off + int64(headerSize),
		BodyLen:    int64(totalSize) - int64(headerSize),
	}, nil
}

// scanTopLevel returns every top-level box in r.
func scanTopLevel(r io.ReaderAt, fileSize int64) ([]Box, error) {
	var boxes []Box
	off := int64(0)
	for off < fileSize {
		b, err := readBoxHeader(r, off, fileSize)
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, b)
		off += int64(b.Size)
	}
	return boxes, nil
}

// scanChildren reads child boxes within a parent body.
func scanChildren(r io.ReaderAt, parentBody, parentLen int64) ([]Box, error) {
	end := parentBody + parentLen
	var boxes []Box
	off := parentBody
	for off < end {
		b, err := readBoxHeader(r, off, end)
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, b)
		off += int64(b.Size)
	}
	return boxes, nil
}

// readBoxBody returns the body bytes of b.
func readBoxBody(r io.ReaderAt, b Box) ([]byte, error) {
	body := make([]byte, b.BodyLen)
	if _, err := r.ReadAt(body, b.BodyOffset); err != nil {
		return nil, err
	}
	return body, nil
}

// writeBoxHeader writes a 32-bit-size box header for size <= 2^32-1.
// For larger boxes the caller must use writeBoxHeaderLarge.
func writeBoxHeader(w io.Writer, typ FourCC, totalSize uint32) error {
	if totalSize < 8 {
		return fmt.Errorf("mp4: box %s total size %d < 8", typ, totalSize)
	}
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], totalSize)
	copy(hdr[4:8], typ[:])
	_, err := w.Write(hdr[:])
	return err
}

// writeBox writes a complete 32-bit-size box: header + body.
func writeBox(w io.Writer, typ FourCC, body []byte) error {
	total := uint64(8 + len(body))
	if total > 1<<32-1 {
		return errors.New("mp4: writeBox: payload too large; use writeBoxLarge")
	}
	if err := writeBoxHeader(w, typ, uint32(total)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

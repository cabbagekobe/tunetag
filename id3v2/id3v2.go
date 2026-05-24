// Package id3v2 reads and writes ID3v2.2, v2.3, and v2.4 tags
// (the variable-length block at the start of an MP3 file marked
// with "ID3").
//
// # Reading
//
// Read parses any of the three supported revisions; v2.2 frame IDs
// are normalised to their canonical 4-character v2.3/2.4 equivalents
// when known, so callers always look frames up by the modern names
// (e.g. "TIT2", "APIC"). Tag-level unsynchronisation is undone
// transparently. The extended header (v2.3 / v2.4) is skipped on
// read and not preserved on write.
//
// # Writing
//
// Encode emits a tag at t.Version. V23 and V24 are first-class; V22
// is also supported but the writer errors out on any frame whose
// canonical 4-character ID has no v2.2 equivalent (PRIV, TSO2 and
// other v2.4-only fields). The default Padding is 1024 bytes; raise
// it to leave room for in-place edits in the surrounding file.
//
// The v2.4 footer flag is honoured: setting Flags=FlagFooter on a
// V24 tag emits a 10-byte "3DI" trailer after the frames and forces
// padding to 0 (the spec requires footer and padding be exclusive).
// Tag-level unsynchronisation and the extended header are never
// emitted on output.
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
	defer func() { _ = f.Close() }()
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
//
// When the footer flag is set (v2.4 only), padding is forced to 0
// per the spec — the two are mutually exclusive — and a 10-byte
// "3DI" footer is appended after the frames. The footer carries the
// same flags and a synchsafe payload size identical to the header's.
func (t *Tag) encodeWithPadding(w io.Writer, pad int) error {
	switch t.Version {
	case V22, V23, V24:
	default:
		return fmt.Errorf("id3v2: writing %s is not supported", t.Version)
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
	hasFooter := flags&FlagFooter != 0
	if hasFooter {
		if t.Version != V24 {
			return fmt.Errorf("id3v2: footer flag requires v2.4, got %s", t.Version)
		}
		pad = 0
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
	if hasFooter {
		if err := writeFooter(w, t.Version, flags, payloadSize); err != nil {
			return err
		}
	}
	return nil
}

// writeFooter writes a 10-byte ID3v2.4 footer ("3DI" + version + 0
// + flags + synchsafe size). The footer's payload size matches the
// header's so readers can locate the tag from either end.
func writeFooter(w io.Writer, v Version, flags Flags, payloadSize uint32) error {
	var b [HeaderSize]byte
	b[0], b[1], b[2] = '3', 'D', 'I'
	b[3] = byte(v)
	b[4] = 0
	b[5] = byte(flags)
	sz, err := encodeSynchsafe(payloadSize)
	if err != nil {
		return err
	}
	copy(b[6:10], sz[:])
	_, err = w.Write(b[:])
	return err
}

// framesEncodedSize returns the length of the encoded frames (no
// header, no padding) for sizing decisions in WriteFile.
func (t *Tag) framesEncodedSize() (uint32, error) {
	switch t.Version {
	case V22, V23, V24:
	default:
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

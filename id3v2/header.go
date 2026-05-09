package id3v2

import (
	"errors"
	"fmt"
	"io"
)

// HeaderSize is the on-disk size of the ID3v2 tag header in bytes.
const HeaderSize = 10

// Flags are the bit flags carried in the tag-level header byte at
// offset 5. Not every flag is valid in every version; see the ID3v2
// specifications for details.
type Flags uint8

const (
	FlagUnsync       Flags = 1 << 7 // valid in v2.2/v2.3/v2.4
	FlagExtended     Flags = 1 << 6 // v2.3/v2.4
	FlagExperimental Flags = 1 << 5 // v2.3/v2.4
	FlagFooter       Flags = 1 << 4 // v2.4 only
)

// Header is the parsed 10-byte ID3v2 tag header.
type Header struct {
	Version Version
	Flags   Flags
	// Size is the byte length of the payload that follows the header
	// (frames + padding, plus extended header when present). It does
	// not include the 10-byte header itself, nor the 10-byte v2.4
	// footer.
	Size uint32
}

// ErrNoTag is returned by Read when the input does not begin with
// the "ID3" magic bytes.
var ErrNoTag = errors.New("id3v2: no tag found")

// ErrUnsupportedVersion is returned when the major revision is not
// 2, 3, or 4.
var ErrUnsupportedVersion = errors.New("id3v2: unsupported tag version")

func readHeader(r io.Reader) (Header, error) {
	var b [HeaderSize]byte
	n, err := io.ReadFull(r, b[:])
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if n == 0 {
				return Header{}, ErrNoTag
			}
		}
		return Header{}, err
	}
	if b[0] != 'I' || b[1] != 'D' || b[2] != '3' {
		return Header{}, ErrNoTag
	}
	if b[3] < 2 || b[3] > 4 {
		return Header{}, fmt.Errorf("%w: %d.%d", ErrUnsupportedVersion, b[3], b[4])
	}
	// A synchsafe size byte with the top bit set is malformed.
	for i := 6; i < 10; i++ {
		if b[i]&0x80 != 0 {
			return Header{}, fmt.Errorf("id3v2: malformed synchsafe size byte at %d", i)
		}
	}
	return Header{
		Version: Version(b[3]),
		Flags:   Flags(b[5]),
		Size:    decodeSynchsafe(b[6:10]),
	}, nil
}

func (h Header) writeTo(w io.Writer) error {
	if h.Version != V22 && h.Version != V23 && h.Version != V24 {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, h.Version)
	}
	var b [HeaderSize]byte
	b[0], b[1], b[2] = 'I', 'D', '3'
	b[3] = byte(h.Version)
	b[4] = 0
	b[5] = byte(h.Flags)
	sz, err := encodeSynchsafe(h.Size)
	if err != nil {
		return err
	}
	copy(b[6:10], sz[:])
	_, err = w.Write(b[:])
	return err
}

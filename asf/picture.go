package asf

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Picture is one cover-art entry from a WM/Picture descriptor.
// The on-disk format is:
//
//	1 byte   picture type (matches ID3v2 APIC values)
//	4 bytes  data length (LE uint32)
//	N bytes  MIME type (UTF-16LE, NUL-terminated)
//	N bytes  description (UTF-16LE, NUL-terminated)
//	N bytes  image data
type Picture struct {
	Type        uint8
	MIME        string
	Description string
	Data        []byte
}

// DecodePicture decodes the body of a WM/Picture descriptor
// (Type=TypeBinary) into a Picture.
func DecodePicture(raw []byte) (*Picture, error) {
	if len(raw) < 5 {
		return nil, fmt.Errorf("asf: WM/Picture body too short (%d bytes)", len(raw))
	}
	p := &Picture{Type: raw[0]}
	dataLen := binary.LittleEndian.Uint32(raw[1:5])
	pos := 5
	mime, n, err := readUTF16NULString(raw[pos:])
	if err != nil {
		return nil, fmt.Errorf("asf: WM/Picture MIME: %w", err)
	}
	p.MIME = mime
	pos += n
	descr, n, err := readUTF16NULString(raw[pos:])
	if err != nil {
		return nil, fmt.Errorf("asf: WM/Picture description: %w", err)
	}
	p.Description = descr
	pos += n
	if pos+int(dataLen) > len(raw) {
		return nil, fmt.Errorf("asf: WM/Picture data length %d overflows", dataLen)
	}
	p.Data = make([]byte, dataLen)
	copy(p.Data, raw[pos:pos+int(dataLen)])
	return p, nil
}

// Encode produces the descriptor value bytes for a WM/Picture
// entry. Caller is responsible for wrapping the result in a
// Descriptor with Type=TypeBinary.
func (p *Picture) Encode() []byte {
	mime := encodeUTF16NUL(p.MIME)
	if len(mime) == 0 {
		mime = []byte{0, 0} // empty MIME still needs a NUL
	}
	descr := encodeUTF16NUL(p.Description)
	if len(descr) == 0 {
		descr = []byte{0, 0}
	}
	var buf bytes.Buffer
	buf.WriteByte(p.Type)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(p.Data)))
	buf.Write(mime)
	buf.Write(descr)
	buf.Write(p.Data)
	return buf.Bytes()
}

// Pictures returns every WM/Picture descriptor decoded as a
// Picture. Decoding errors are silently skipped; the raw bytes
// remain accessible via File.Extended.
func (f *File) Pictures() []*Picture {
	var out []*Picture
	for _, d := range f.Extended {
		if d.Name != NamePicture || d.Type != TypeBinary {
			continue
		}
		p, err := DecodePicture(d.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// AddPicture appends a WM/Picture descriptor.
func (f *File) AddPicture(p *Picture) {
	f.Extended = append(f.Extended, Descriptor{
		Name: NamePicture, Type: TypeBinary, Value: p.Encode(),
	})
}

// RemovePictures deletes every WM/Picture descriptor.
func (f *File) RemovePictures() {
	out := f.Extended[:0]
	for _, d := range f.Extended {
		if d.Name == NamePicture {
			continue
		}
		out = append(out, d)
	}
	f.Extended = out
}

// readUTF16NULString reads a UTF-16LE NUL-terminated string from
// the start of b. Returns the decoded string and the number of
// bytes consumed (including the terminating NUL).
func readUTF16NULString(b []byte) (string, int, error) {
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			return decodeUTF16NUL(b[:i+2]), i + 2, nil
		}
	}
	return "", 0, fmt.Errorf("UTF-16LE string is not NUL-terminated within %d bytes", len(b))
}

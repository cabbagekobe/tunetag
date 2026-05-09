package id3v2

import (
	"bytes"
	"testing"
)

// FuzzReadID3v2 feeds arbitrary bytes into the tag reader and
// asserts that no input causes a panic during read or encode.
func FuzzReadID3v2(f *testing.F) {
	// Seed with one valid v2.3 tag and a couple of non-tag buffers.
	f.Add(seedV23Tag())
	f.Add([]byte{})
	f.Add([]byte("not_an_id3_tag_at_all"))

	f.Fuzz(func(t *testing.T, data []byte) {
		tag, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}
		var buf bytes.Buffer
		_ = tag.Encode(&buf)
	})
}

// seedV23Tag returns a tiny but well-formed ID3v2.3 tag with a
// single TIT2 frame.
func seedV23Tag() []byte {
	body := []byte{0x00, 'O', 'k'}
	var fbuf bytes.Buffer
	fbuf.WriteString("TIT2")
	fbuf.Write([]byte{0x00, 0x00, 0x00, byte(len(body))}) // size
	fbuf.Write([]byte{0x00, 0x00})                        // flags
	fbuf.Write(body)
	h := Header{Version: V23, Size: uint32(fbuf.Len())}
	var out bytes.Buffer
	_ = h.writeTo(&out)
	out.Write(fbuf.Bytes())
	return out.Bytes()
}

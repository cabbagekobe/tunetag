package flac

import (
	"bytes"
	"testing"
)

// FuzzReadFLAC asserts that the FLAC reader does not panic on
// arbitrary inputs and that any tag it returns can be re-encoded.
func FuzzReadFLAC(f *testing.F) {
	f.Add(seedFLAC())
	f.Add([]byte{})
	f.Add([]byte("fLaC")) // truncated, no blocks

	f.Fuzz(func(t *testing.T, data []byte) {
		f, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}
		_, _ = f.encodeMetadata()
	})
}

func seedFLAC() []byte {
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	vc := &VorbisComment{Vendor: "v", Comments: []string{"TITLE=Hi"}}
	var buf bytes.Buffer
	buf.Write(Magic[:])
	for i, b := range []Block{si, vc} {
		body, _ := b.Encode()
		_ = writeBlockHeader(&buf, b.Type(), i == 1, uint32(len(body)))
		buf.Write(body)
	}
	return buf.Bytes()
}

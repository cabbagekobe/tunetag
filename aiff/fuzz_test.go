package aiff

import (
	"bytes"
	"io"
	"testing"
)

// FuzzReadAIFF feeds arbitrary bytes through the AIFF reader. The
// reader must not panic; any successfully-parsed file is also
// re-encoded — streaming raw chunks back out of the same reader —
// to ensure the writer is panic-free too.
func FuzzReadAIFF(f *testing.F) {
	f.Add(seedAIFF())
	f.Add([]byte{})
	f.Add([]byte("FORM\x00\x00\x00\x04AIFF")) // header only, no chunks

	f.Fuzz(func(t *testing.T, data []byte) {
		rs := bytes.NewReader(data)
		a, err := Read(rs)
		if err != nil {
			return
		}
		_ = a.encodeTo(io.Discard, rs)
	})
}

func seedAIFF() []byte {
	var pay bytes.Buffer
	putChunk(&pay, ChunkNAME, []byte("seed"))
	putChunk(&pay, ChunkANNO, []byte("note"))
	putChunk(&pay, "SSND", []byte("seed_audio"))
	return buildAIFF("AIFF", pay.Bytes())
}

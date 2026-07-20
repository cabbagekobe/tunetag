package wav

import (
	"bytes"
	"io"
	"testing"
)

// FuzzReadWAV feeds arbitrary bytes through the WAV reader. The
// reader must not panic; any successfully-parsed file is also
// re-encoded — streaming raw chunks back out of the same reader —
// to ensure the writer is panic-free too.
func FuzzReadWAV(f *testing.F) {
	f.Add(seedWAV())
	f.Add([]byte{})
	f.Add([]byte("RIFF\x04\x00\x00\x00WAVE")) // header only, no chunks

	f.Fuzz(func(t *testing.T, data []byte) {
		rs := bytes.NewReader(data)
		w, err := Read(rs)
		if err != nil {
			return
		}
		_ = w.encodeTo(io.Discard, rs)
	})
}

func seedWAV() []byte {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0x01}, 16))
	putChunk(&pay, ChunkLIST, infoBody([2]string{InfoTitle, "seed"}))
	putChunk(&pay, "data", []byte("seed_audio"))
	return buildWAV(pay.Bytes())
}

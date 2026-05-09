package mp4

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/testutil"
)

// FuzzReadMP4 feeds arbitrary bytes through the MP4 reader. The
// reader must not panic; any successfully-parsed file is also
// re-encoded to ensure ilst encoding is panic-free.
func FuzzReadMP4(f *testing.F) {
	f.Add(testutil.BuildMinimal(testutil.MinimalOptions{Title: "seed"}))
	f.Add([]byte("not an mp4"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "fuzz.mp4")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Skip(err)
		}
		f, err := Read(p)
		if err != nil {
			return
		}
		// We only verify that ilst encode doesn't panic; the result
		// itself need not round-trip on these arbitrary inputs.
		_, _ = f.Tag.encode()
		_ = bytes.Equal(nil, nil) // keep "bytes" import if shrunk later
	})
}

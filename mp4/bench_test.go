package mp4

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/testutil"
)

func benchTempMP4(b *testing.B, opt testutil.MinimalOptions) string {
	b.Helper()
	dir := b.TempDir()
	p := filepath.Join(dir, "bench.m4a")
	body := testutil.BuildMinimal(opt)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		b.Fatal(err)
	}
	return p
}

func BenchmarkRead_MinimalMP4(b *testing.B) {
	p := benchTempMP4(b, testutil.MinimalOptions{Title: "T", Artist: "A", Album: "Al"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Read(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteInPlace_IdenticalSize(b *testing.B) {
	p := benchTempMP4(b, testutil.MinimalOptions{Title: "ABCD"})
	f, err := Read(p)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Same-size title alternation keeps the path on the in-place
		// (delta == 0) branch.
		if i%2 == 0 {
			f.Tag.SetTitle("WXYZ")
		} else {
			f.Tag.SetTitle("ABCD")
		}
		if err := f.WriteFile(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteWithFreeAbsorb(b *testing.B) {
	p := benchTempMP4(b, testutil.MinimalOptions{Title: "T", FreeBytes: 128})
	f, err := Read(p)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Toggling title length forces the free atom absorption path.
		if i%2 == 0 {
			f.Tag.SetTitle("X")
		} else {
			f.Tag.SetTitle("longer title here")
		}
		if err := f.WriteFile(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanTopLevel(b *testing.B) {
	body := testutil.BuildMinimal(testutil.MinimalOptions{
		Title: "T", Artist: "A", Album: "Al",
	})
	rd := byteReaderAt(body)
	size := int64(len(body))
	b.SetBytes(size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := scanTopLevel(rd, size); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodedMoovBytes(b *testing.B) {
	p := benchTempMP4(b, testutil.MinimalOptions{Title: "T", Artist: "A"})
	f, err := Read(p)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.EncodedMoovBytes(); err != nil {
			b.Fatal(err)
		}
	}
}

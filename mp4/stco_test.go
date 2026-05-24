package mp4

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/mp4test"
)

// readSTCOOffsets walks the file at p and returns the chunk offsets
// of the first stco box found inside the first trak.
func readSTCOOffsets(t *testing.T, p string) []uint32 {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, _ := f.Stat()
	tops, err := scanTopLevel(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range tops {
		if !b.Type.Equal("moov") {
			continue
		}
		body, err := readBoxBody(f, b)
		if err != nil {
			t.Fatal(err)
		}
		offsets, ok := findFirstSTCO(body)
		if ok {
			return offsets
		}
	}
	t.Fatal("no stco found")
	return nil
}

// findFirstSTCO recursively walks moov body to locate the first stco.
func findFirstSTCO(body []byte) ([]uint32, bool) {
	pos := 0
	for pos < len(body) {
		if pos+8 > len(body) {
			return nil, false
		}
		size := binary.BigEndian.Uint32(body[pos : pos+4])
		typ := string(body[pos+4 : pos+8])
		if size < 8 || int(size) > len(body)-pos {
			return nil, false
		}
		child := body[pos+8 : pos+int(size)]
		switch typ {
		case "stco":
			count := binary.BigEndian.Uint32(child[4:8])
			out := make([]uint32, count)
			for i := uint32(0); i < count; i++ {
				out[i] = binary.BigEndian.Uint32(child[8+4*i : 12+4*i])
			}
			return out, true
		case "trak", "mdia", "minf", "stbl":
			if vals, ok := findFirstSTCO(child); ok {
				return vals, true
			}
		}
		pos += int(size)
	}
	return nil, false
}

func TestPatchSTCO_OffsetsShiftedByDelta(t *testing.T) {
	originalOffsets := []uint32{1024, 2048, 4096}
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:    "tiny",
		WithStco: originalOffsets,
	})
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	// Force a tag change that exceeds in-place capacity (no free
	// atom in this fixture) so Tier 2 rewrite engages.
	longTitle := bytes.Repeat([]byte("x"), 256)
	f.Tag.SetTitle(string(longTitle))

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() <= originalSize {
		t.Errorf("expected file growth (%d -> %d)", originalSize, info.Size())
	}
	delta := info.Size() - originalSize

	got := readSTCOOffsets(t, p)
	for i, want := range originalOffsets {
		if got[i] != want+uint32(delta) {
			t.Errorf("stco[%d] = %d, want %d (+delta=%d)", i, got[i], want+uint32(delta), delta)
		}
	}

	// Re-read should still yield the new title.
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Tag.Title() != string(longTitle) {
		t.Errorf("Title not preserved")
	}
}

func TestPatchSTCO_MdatBeforeMoov_NoPatchNeeded(t *testing.T) {
	originalOffsets := []uint32{500, 1000, 2000}
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:      "tiny",
		WithStco:   originalOffsets,
		MdatBefore: true,
	})
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Tag.SetTitle(string(bytes.Repeat([]byte("y"), 256)))

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := readSTCOOffsets(t, p)
	for i, want := range originalOffsets {
		if got[i] != want {
			t.Errorf("stco[%d] = %d, want %d (no patch expected when mdat is before moov)", i, got[i], want)
		}
	}
}

func TestPatchSTCO_OverflowAutoPromotesToCo64(t *testing.T) {
	// 0xFFFFFFFF is the maximum legal stco entry; any positive delta
	// forces a co64 promotion.
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:    "tiny",
		WithStco: []uint32{0xFFFFFFFF, 0xFFFFFFFE},
	})
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	longTitle := string(bytes.Repeat([]byte("z"), 256))
	f.Tag.SetTitle(longTitle)
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// After auto-promotion, the file should contain co64 (not stco)
	// holding the original values shifted by the moov delta.
	values, foundCo64 := readFirstChunkOffsets(t, p)
	if !foundCo64 {
		t.Fatalf("expected co64 box after auto-promotion")
	}
	if len(values) != 2 {
		t.Fatalf("entries = %d, want 2", len(values))
	}
	if values[0] <= 0xFFFFFFFF {
		t.Errorf("entry 0 = %d, expected > 32-bit max after promotion", values[0])
	}

	// Title round-trip preserved.
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Tag.Title() != longTitle {
		t.Errorf("Title not preserved")
	}
}

// readFirstChunkOffsets walks the moov box and returns either stco
// (32-bit) or co64 (64-bit) values for the first track. The bool
// return indicates whether the box was co64.
func readFirstChunkOffsets(t *testing.T, p string) ([]uint64, bool) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, _ := f.Stat()
	tops, err := scanTopLevel(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range tops {
		if !b.Type.Equal("moov") {
			continue
		}
		body, err := readBoxBody(f, b)
		if err != nil {
			t.Fatal(err)
		}
		vals, isCo64, ok := findFirstChunkOffsetBox(body)
		if ok {
			return vals, isCo64
		}
	}
	return nil, false
}

func findFirstChunkOffsetBox(body []byte) ([]uint64, bool, bool) {
	pos := 0
	for pos < len(body) {
		if pos+8 > len(body) {
			return nil, false, false
		}
		size := binary.BigEndian.Uint32(body[pos : pos+4])
		typ := string(body[pos+4 : pos+8])
		if size < 8 || int(size) > len(body)-pos {
			return nil, false, false
		}
		child := body[pos+8 : pos+int(size)]
		switch typ {
		case "stco":
			count := binary.BigEndian.Uint32(child[4:8])
			out := make([]uint64, count)
			for i := uint32(0); i < count; i++ {
				out[i] = uint64(binary.BigEndian.Uint32(child[8+4*i : 12+4*i]))
			}
			return out, false, true
		case "co64":
			count := binary.BigEndian.Uint32(child[4:8])
			out := make([]uint64, count)
			for i := uint32(0); i < count; i++ {
				out[i] = binary.BigEndian.Uint64(child[8+8*i : 16+8*i])
			}
			return out, true, true
		case "trak", "mdia", "minf", "stbl":
			if vals, co64, ok := findFirstChunkOffsetBox(child); ok {
				return vals, co64, true
			}
		}
		pos += int(size)
	}
	return nil, false, false
}

func TestPatchCO64_RoundTrip(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}
	body = append(body,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00,
	)
	out, err := patchCO64(body, 1234)
	if err != nil {
		t.Fatal(err)
	}
	v0 := binary.BigEndian.Uint64(out[8:16])
	v1 := binary.BigEndian.Uint64(out[16:24])
	if v0 != 0x1000+1234 || v1 != 0x2000+1234 {
		t.Errorf("co64 patched = %d, %d", v0, v1)
	}
}

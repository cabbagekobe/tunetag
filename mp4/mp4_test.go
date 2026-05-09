package mp4

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/testutil"
)

func writeTempMP4(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead_Minimal(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{
		Title: "Hello", Artist: "Alice", Album: "First",
	})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Tag.Title(); got != "Hello" {
		t.Errorf("Title = %q", got)
	}
	if got := f.Tag.Artist(); got != "Alice" {
		t.Errorf("Artist = %q", got)
	}
	if got := f.Tag.Album(); got != "First" {
		t.Errorf("Album = %q", got)
	}
}

func TestRead_NotMP4(t *testing.T) {
	p := writeTempMP4(t, []byte("not an mp4 file"))
	if _, err := Read(p); err == nil {
		t.Fatal("expected error for non-MP4 input")
	}
}

func TestWriteFile_InPlaceExact(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{Title: "Same"})
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, raw) {
		t.Errorf("byte-for-byte equal expected on identity write")
	}
}

func TestWriteFile_AbsorbsIntoSiblingFree(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{
		Title:     "A",
		FreeBytes: 256, // generous reserve
	})
	p := writeTempMP4(t, raw)
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff < 0 || f.freeLen != 256 {
		t.Fatalf("expected sibling free of 256, got off=%d len=%d", f.freeOff, f.freeLen)
	}
	// Replace title with a noticeably longer string; the delta must
	// fit in the 256-byte free atom (header included).
	f.Tag.SetTitle("a much longer title to exercise free absorption")

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() != originalSize {
		t.Errorf("file size changed %d -> %d (in-place expected)", originalSize, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Tag.Title(); got != "a much longer title to exercise free absorption" {
		t.Errorf("Title = %q", got)
	}
}

func TestWriteFile_ShrinksByInsertingFree(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{
		Title:  "a much longer title to be replaced by a short one",
		Artist: "and another field",
	})
	p := writeTempMP4(t, raw)
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff >= 0 {
		t.Fatalf("expected no sibling free in fixture")
	}

	f.Tag.SetTitle("X")
	f.Tag.SetArtist("Y")

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() != originalSize {
		t.Errorf("file size changed %d -> %d (insert-free expected)", originalSize, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatalf("re-read after shrink: %v", err)
	}
	if got := out.Tag.Title(); got != "X" {
		t.Errorf("Title = %q", got)
	}
	if got := out.Tag.Artist(); got != "Y" {
		t.Errorf("Artist = %q", got)
	}
}

func TestWriteFile_FullRewriteWhenGrowing(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{Title: "tiny"})
	p := writeTempMP4(t, raw)
	original := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	longTitle := "tiny title made very long with extra padding " +
		"to overflow any in-place reserve by hundreds of bytes that simply cannot fit"
	f.Tag.SetTitle(longTitle)
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() <= original {
		t.Errorf("expected file growth on Tier 2 rewrite (was %d, now %d)", original, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Tag.Title(); got != longTitle {
		t.Errorf("Title = %q", got)
	}
}

func TestPicture_AddCover_DetectsJPEG(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x", FreeBytes: 4096})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'J', 'F', 'I', 'F', 0x00}
	f.Tag.AddCover(jpeg)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := out.Tag.Pictures()
	if len(pics) != 1 {
		t.Fatalf("pictures = %d, want 1", len(pics))
	}
	if pics[0].TypeCode != DataTypeJPEG {
		t.Errorf("TypeCode = %d, want JPEG (%d)", pics[0].TypeCode, DataTypeJPEG)
	}
	if !bytes.Equal(pics[0].Payload, jpeg) {
		t.Errorf("payload mismatch")
	}
}

func TestTrack_RoundTrip(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x", FreeBytes: 256})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Tag.SetTrack(3, 12)
	f.Tag.SetDisc(1, 2)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n, total := out.Tag.Track(); n != 3 || total != 12 {
		t.Errorf("track = %d/%d", n, total)
	}
	if n, total := out.Tag.Disc(); n != 1 || total != 2 {
		t.Errorf("disc = %d/%d", n, total)
	}
}

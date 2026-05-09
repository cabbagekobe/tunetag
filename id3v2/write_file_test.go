package id3v2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// helper: write a tagless file containing only audio bytes.
func writeAudioFile(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// helper: build a tag with a TIT2 frame of the given title.
func makeTag(version Version, title string) *Tag {
	tag := &Tag{Version: version, Padding: DefaultPadding}
	tag.SetTitle(title)
	return tag
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func readBody(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWriteFile_PrependsToTaglessFile(t *testing.T) {
	audio := []byte("FAKE_MP3_AUDIO_BODY_BYTES_HERE")
	p := writeAudioFile(t, audio)

	tag := makeTag(V24, "Hello")
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "Hello" {
		t.Errorf("Title = %q", out.Title())
	}

	// Audio body must still be present, untouched, after the tag.
	data := readBody(t, p)
	if !bytes.HasSuffix(data, audio) {
		t.Errorf("audio body lost after WriteFile")
	}
}

func TestWriteFile_ReplacesExistingTagInPlaceWhenItFits(t *testing.T) {
	audio := []byte("AAAABBBBCCCCDDDDEEEEFFFFGGGG")
	p := writeAudioFile(t, audio)

	// Round 1: large padding so a smaller subsequent tag fits.
	tag1 := &Tag{Version: V24, Padding: 4096}
	tag1.SetTitle("First")
	if err := tag1.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	sizeAfter1 := fileSize(t, p)

	// Round 2: same shape but different title; should fit easily in
	// the existing 4 KiB padding and stay in-place.
	tag2 := &Tag{Version: V24, Padding: 1024}
	tag2.SetTitle("Second")
	if err := tag2.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	sizeAfter2 := fileSize(t, p)

	if sizeAfter1 != sizeAfter2 {
		t.Errorf("size changed %d -> %d (in-place expected)", sizeAfter1, sizeAfter2)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "Second" {
		t.Errorf("Title = %q", out.Title())
	}

	data := readBody(t, p)
	if !bytes.HasSuffix(data, audio) {
		t.Errorf("audio body lost after in-place rewrite")
	}
}

func TestWriteFile_GrowsFileWhenTagExceedsSlot(t *testing.T) {
	audio := []byte("AUDIO")
	p := writeAudioFile(t, audio)

	tag1 := &Tag{Version: V24, Padding: 0}
	tag1.SetTitle("X")
	if err := tag1.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	size1 := fileSize(t, p)

	// Round 2: significantly larger. Slot in round 1 = ~21 bytes,
	// so this must trigger a full rewrite.
	tag2 := &Tag{Version: V24, Padding: 4096}
	tag2.SetTitle("A much longer title that needs more space")
	tag2.SetArtist("Likewise a long artist name")
	tag2.SetAlbum("And an album for good measure")
	if err := tag2.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	size2 := fileSize(t, p)

	if size2 <= size1 {
		t.Errorf("expected grown file, got %d -> %d", size1, size2)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "A much longer title that needs more space" {
		t.Errorf("Title = %q", out.Title())
	}
	if out.Artist() != "Likewise a long artist name" {
		t.Errorf("Artist = %q", out.Artist())
	}

	data := readBody(t, p)
	if !bytes.HasSuffix(data, audio) {
		t.Errorf("audio body lost after rewrite")
	}
}

func TestWriteFile_RepeatedSmallEdits_StayInPlace(t *testing.T) {
	audio := []byte("PAYLOAD" + string(make([]byte, 4096)))
	p := writeAudioFile(t, audio)

	tag := makeTag(V24, "Initial")
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	baseSize := fileSize(t, p)

	for i, title := range []string{"a", "ab", "abc", "abcd", "longer title here"} {
		fresh, err := ReadFile(p)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		fresh.SetTitle(title)
		if err := fresh.WriteFile(p); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if got := fileSize(t, p); got != baseSize {
			t.Errorf("iter %d: size %d, want %d (in-place expected)", i, got, baseSize)
		}
	}

	final, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if final.Title() != "longer title here" {
		t.Errorf("final Title = %q", final.Title())
	}
	if !bytes.HasSuffix(readBody(t, p), audio) {
		t.Errorf("audio body lost during repeated in-place edits")
	}
}

func TestWriteFile_FailedRewriteLeavesNoOrphan(t *testing.T) {
	// Use a directory we can pre-populate, then make the temp dir
	// unwritable to force failure on tmp file creation. This is
	// platform-dependent (unix); skip on non-unix.
	dir := t.TempDir()
	p := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(p, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	tag := makeTag(V24, "X")
	if err := tag.WriteFile(p); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// After successful writes there must be no temp file remnants in dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("orphan temp file: %s", e.Name())
		}
	}
}

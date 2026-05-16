package ape

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// buildAPETag builds a minimal APEv2 byte blob from a list of
// (key, value) pairs.
func buildAPETag(t *testing.T, hasHeader bool, pairs ...[2]string) []byte {
	t.Helper()
	tag := &Tag{HasHeader: hasHeader}
	for _, p := range pairs {
		if err := tag.Set(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	body, err := tag.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func writeTemp(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.ape")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead_EmptyFile(t *testing.T) {
	_, err := Read(bytes.NewReader(nil))
	if !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
}

func TestRead_RandomBytes(t *testing.T) {
	body := bytes.Repeat([]byte{0xCC}, 256)
	if _, err := Read(bytes.NewReader(body)); !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
}

func TestEncodeAndRead_RoundTrip(t *testing.T) {
	tag := &Tag{HasHeader: true}
	tag.Set("Title", "Hello")
	tag.Set("Artist", "Alice")
	tag.Set("Album", "Songs")
	tag.Set("Year", "2026")
	tag.Set("Track", "3/12")
	body, err := tag.Encode()
	if err != nil {
		t.Fatal(err)
	}
	full := append([]byte("AUDIO_BODY_GOES_HERE"), body...)

	got, err := Read(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "Hello" || got.Artist() != "Alice" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
	if got.Year() != 2026 {
		t.Errorf("Year = %d", got.Year())
	}
	if n, total := got.TrackNumber(); n != 3 || total != 12 {
		t.Errorf("Track = %d/%d", n, total)
	}
}

func TestEncode_NoHeader(t *testing.T) {
	tag := &Tag{}
	tag.Set("Title", "x")
	body, _ := tag.Encode()
	// Without header: only footer's 32 bytes appended.
	if !bytes.Equal(body[len(body)-32:len(body)-32+8], Preamble[:]) {
		t.Errorf("footer preamble missing at offset %d", len(body)-32)
	}
}

func TestRead_PrefersAPEOverID3v1(t *testing.T) {
	tag := &Tag{HasHeader: true}
	tag.Set("Title", "From APE")
	body, _ := tag.Encode()
	// Append a 128-byte ID3v1 trailer so the file looks like
	// audio + APEv2 + ID3v1.
	id3v1 := make([]byte, 128)
	copy(id3v1[0:3], []byte("TAG"))
	full := append([]byte("audio"), body...)
	full = append(full, id3v1...)

	got, err := Read(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "From APE" {
		t.Errorf("Title = %q", got.Title())
	}
}

func TestSet_RemovesOnEmptyValue(t *testing.T) {
	tag := &Tag{}
	tag.Set("Title", "x")
	if err := tag.Set("Title", ""); err != nil {
		t.Fatal(err)
	}
	if len(tag.Items) != 0 {
		t.Errorf("expected items removed, got %+v", tag.Items)
	}
}

func TestSet_RejectsInvalidKey(t *testing.T) {
	tag := &Tag{}
	cases := []string{"", "X", "ID3", "TAG", "with\ttab", "key\x00null"}
	for _, k := range cases {
		if err := tag.Set(k, "v"); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Set(%q) = %v, want ErrInvalidKey", k, err)
		}
	}
}

func TestSet_CaseInsensitiveLookup(t *testing.T) {
	tag := &Tag{}
	tag.Set("Title", "first")
	tag.Set("TITLE", "second") // should update, not add
	if len(tag.Items) != 1 {
		t.Errorf("Items = %d, want 1 (case-insensitive update)", len(tag.Items))
	}
	if tag.Get("title") != "second" {
		t.Errorf("Get = %q", tag.Get("title"))
	}
}

func TestRead_ReportsAPEv1AsUnsupported(t *testing.T) {
	// Build a minimal footer with version=1000.
	var footer bytes.Buffer
	footer.Write(Preamble[:])
	binary.Write(&footer, binary.LittleEndian, uint32(1000)) // APEv1
	binary.Write(&footer, binary.LittleEndian, uint32(32))   // size
	binary.Write(&footer, binary.LittleEndian, uint32(0))    // count
	binary.Write(&footer, binary.LittleEndian, uint32(0))    // flags
	footer.Write(make([]byte, 8))                            // reserved
	body := append([]byte("audio"), footer.Bytes()...)
	if _, err := Read(bytes.NewReader(body)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("got %v, want ErrUnsupportedVersion", err)
	}
}

func TestWriteFile_PreservesAudioAndID3v1(t *testing.T) {
	// Build initial file: audio + APEv2 + ID3v1.
	audio := []byte("THIS_IS_AUDIO_BODY")
	body := buildAPETag(t, true, [2]string{"Title", "old"})
	id3v1 := make([]byte, 128)
	copy(id3v1[0:3], []byte("TAG"))
	copy(id3v1[3:3+30], []byte("V1_TITLE"))
	full := append(append([]byte{}, audio...), body...)
	full = append(full, id3v1...)
	p := writeTemp(t, full)

	tag, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tag.Set("Title", "new")
	tag.Set("Artist", "added")
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, audio) {
		t.Errorf("audio body not preserved at start")
	}
	if !bytes.HasSuffix(got, id3v1) {
		t.Errorf("ID3v1 trailer not preserved at end")
	}
	// And the tag should round-trip.
	reread, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Title() != "new" || reread.Artist() != "added" {
		t.Errorf("Title=%q Artist=%q", reread.Title(), reread.Artist())
	}
}

func TestWriteFile_AppendsTagWhenAbsent(t *testing.T) {
	audio := []byte("PURE_AUDIO_NO_TAG_AT_ALL")
	p := writeTemp(t, audio)
	tag := &Tag{HasHeader: true}
	tag.Set("Title", "Brand New")
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, audio) {
		t.Errorf("audio body damaged when appending tag")
	}
	reread, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Title() != "Brand New" {
		t.Errorf("Title = %q", reread.Title())
	}
}

func TestRemove_DeletesAllMatching(t *testing.T) {
	tag := &Tag{Items: []Item{
		{Key: "Title", Value: []byte("a")},
		{Key: "title", Value: []byte("b")},
		{Key: "Album", Value: []byte("c")},
	}}
	if n := tag.Remove("title"); n != 2 {
		t.Errorf("Remove returned %d, want 2", n)
	}
	if len(tag.Items) != 1 || tag.Items[0].Key != "Album" {
		t.Errorf("Items = %+v", tag.Items)
	}
}

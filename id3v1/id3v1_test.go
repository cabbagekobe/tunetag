package id3v1

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRead_TooShort(t *testing.T) {
	rs := bytes.NewReader(make([]byte, 50))
	if _, err := Read(rs); err != ErrNoTag {
		t.Fatalf("got %v, want ErrNoTag", err)
	}
}

func TestRead_NoMagic(t *testing.T) {
	rs := bytes.NewReader(make([]byte, 200))
	if _, err := Read(rs); err != ErrNoTag {
		t.Fatalf("got %v, want ErrNoTag", err)
	}
}

func TestRead_v10(t *testing.T) {
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	copy(buf[3:33], "Title")
	copy(buf[33:63], "Artist")
	copy(buf[63:93], "Album")
	copy(buf[93:97], "2026")
	copy(buf[97:127], "Comment")
	buf[127] = 17 // Rock

	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	want := Tag{
		Title: "Title", Artist: "Artist", Album: "Album",
		Year: "2026", Comment: "Comment", Track: 0, Genre: 17,
	}
	if *tag != want {
		t.Errorf("got %+v\nwant %+v", *tag, want)
	}
	if got := tag.GenreName(); got != "Rock" {
		t.Errorf("GenreName = %q, want Rock", got)
	}
}

func TestRead_v11(t *testing.T) {
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	copy(buf[3:33], "Title")
	copy(buf[33:63], "Artist")
	copy(buf[63:93], "Album")
	copy(buf[93:97], "2026")
	copy(buf[97:125], "Comment") // 28 bytes
	buf[125] = 0
	buf[126] = 7
	buf[127] = 17

	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Track != 7 {
		t.Errorf("Track = %d, want 7", tag.Track)
	}
	if tag.Comment != "Comment" {
		t.Errorf("Comment = %q", tag.Comment)
	}
}

func TestRead_v11_BoundaryCommentNotMistakenForTrack(t *testing.T) {
	// A 30-char comment ending in two non-zero bytes must NOT be
	// reinterpreted as a v1.1 layout.
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	for i := 97; i < 127; i++ {
		buf[i] = 'X'
	}
	buf[127] = GenreNone

	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Track != 0 {
		t.Errorf("Track = %d, want 0 (v1.0 layout)", tag.Track)
	}
	if len(tag.Comment) != 30 {
		t.Errorf("Comment len = %d, want 30", len(tag.Comment))
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []*Tag{
		{Title: "A", Artist: "B", Album: "C", Year: "2026", Comment: "D", Genre: GenreNone},
		{Title: "Title", Artist: "Artist", Album: "Album", Year: "2026", Comment: "Cmt", Track: 5, Genre: 17},
		{Genre: GenreNone}, // all-empty
	}
	for i, in := range cases {
		var buf bytes.Buffer
		if err := in.Encode(&buf); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if buf.Len() != TagSize {
			t.Fatalf("case %d: encoded size %d, want %d", i, buf.Len(), TagSize)
		}
		out, err := Read(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if *out != *in {
			t.Errorf("case %d:\n got %+v\nwant %+v", i, *out, *in)
		}
	}
}

func TestEncode_TruncatesLongFields(t *testing.T) {
	long := string(bytes.Repeat([]byte{'X'}, 50))
	in := &Tag{Title: long, Genre: GenreNone}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Title) != 30 {
		t.Errorf("Title len = %d, want 30", len(out.Title))
	}
}

func TestWriteFile_AppendsToFileWithoutTag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	audio := []byte("AUDIO_DATA_HERE")
	if err := os.WriteFile(p, audio, 0o644); err != nil {
		t.Fatal(err)
	}

	tag := &Tag{Title: "T", Artist: "A", Genre: GenreNone}
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(audio)+TagSize {
		t.Fatalf("size = %d, want %d", len(data), len(audio)+TagSize)
	}
	if !bytes.Equal(data[:len(audio)], audio) {
		t.Errorf("audio body corrupted")
	}
	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "T" || out.Artist != "A" {
		t.Errorf("read back %+v", out)
	}
}

func TestWriteFile_ReplacesExistingTagInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	audio := []byte("AUDIO_DATA_HERE")

	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := (&Tag{Title: "OLD", Genre: GenreNone}).Encode(f); err != nil {
		t.Fatal(err)
	}
	f.Close()

	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	if err := (&Tag{Title: "NEW", Genre: GenreNone}).WriteFile(p); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() {
		t.Errorf("size changed %d -> %d (must replace in place)", before.Size(), after.Size())
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "NEW" {
		t.Errorf("Title = %q, want NEW", out.Title)
	}

	body := make([]byte, len(audio))
	rf, _ := os.Open(p)
	defer rf.Close()
	if _, err := rf.Read(body); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, audio) {
		t.Errorf("audio body corrupted by in-place write")
	}
}

func TestStripFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	audio := []byte("AUDIO")

	f, _ := os.Create(p)
	f.Write(audio)
	(&Tag{Title: "X", Genre: GenreNone}).Encode(f)
	f.Close()

	if err := StripFile(p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !bytes.Equal(data, audio) {
		t.Errorf("after strip got %v, want %v", data, audio)
	}

	if err := StripFile(p); err != nil {
		t.Fatalf("strip on tagless file should be no-op: %v", err)
	}
}

func TestGenreName(t *testing.T) {
	cases := map[uint8]string{
		0:   "Blues",
		17:  "Rock",
		79:  "Hard Rock",
		80:  "Folk",
		191: "Psybient",
		192: "",
		255: "",
	}
	for code, want := range cases {
		tag := &Tag{Genre: code}
		if got := tag.GenreName(); got != want {
			t.Errorf("Genre %d: got %q, want %q", code, got, want)
		}
	}
}

func TestGenresTableLength(t *testing.T) {
	if len(Genres) != 192 {
		t.Errorf("len(Genres) = %d, want 192 (80 standard + 112 Winamp)", len(Genres))
	}
}

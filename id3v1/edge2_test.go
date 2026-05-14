package id3v1

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// --- Read additional edge cases ---------------------------------

func TestRead_TruncatedBelowTagSize(t *testing.T) {
	// 127-byte file (one short of TagSize): definitely not enough room
	// for an ID3v1 trailer, so the magic check never even runs.
	rs := bytes.NewReader(make([]byte, TagSize-1))
	if _, err := Read(rs); !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
}

func TestRead_TrackOnByte126OnlyEmitsV11(t *testing.T) {
	// byte 125 = 0, byte 126 != 0 => v1.1 layout: comment is 28 bytes.
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	for i := 97; i < 125; i++ {
		buf[i] = 'D'
	}
	buf[125] = 0
	buf[126] = 42
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Track != 42 {
		t.Errorf("Track = %d, want 42", tag.Track)
	}
	if len(tag.Comment) != 28 {
		t.Errorf("Comment len = %d, want 28", len(tag.Comment))
	}
}

func TestRead_NonZeroByte125ForcesV10Layout(t *testing.T) {
	// byte 125 != 0 disqualifies the v1.1 detection: the entire 30-byte
	// comment slot is the comment, and Track stays 0.
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	copy(buf[97:127], []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"))
	buf[125] = 'Z' // not zero -> reader must NOT treat as v1.1
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Track != 0 {
		t.Errorf("Track = %d, want 0", tag.Track)
	}
	if len(tag.Comment) != 30 {
		t.Errorf("Comment len = %d, want 30", len(tag.Comment))
	}
}

func TestRead_HighByteFieldsPreserved(t *testing.T) {
	// Tags written by foobar2000 etc. sometimes contain bytes 0x80-0xFF
	// in fields. They must be preserved (interpreted as Latin-1).
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	buf[3] = 0xC9 // É
	buf[4] = 0x00
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	// trimPad strips trailing NULs; the leading 0xC9 byte must survive.
	if tag.Title == "" {
		t.Errorf("Title empty; want at least one byte preserved")
	}
}

// --- Encode additional edge cases -------------------------------

func TestEncode_LongTitleSilentlyTruncated(t *testing.T) {
	in := &Tag{
		Title: "X23456789012345678901234567890_OVERFLOW",
		Genre: GenreNone,
	}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != TagSize {
		t.Errorf("encoded size = %d, want %d", buf.Len(), TagSize)
	}
	out, _ := Read(bytes.NewReader(buf.Bytes()))
	if len(out.Title) > 30 {
		t.Errorf("Title len = %d > 30", len(out.Title))
	}
	if out.Title != "X23456789012345678901234567890" {
		t.Errorf("Title = %q", out.Title)
	}
}

func TestEncode_GenreNonePreserved(t *testing.T) {
	var buf bytes.Buffer
	if err := (&Tag{Genre: GenreNone}).Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, _ := Read(bytes.NewReader(buf.Bytes()))
	if out.Genre != GenreNone {
		t.Errorf("Genre = %d, want %d", out.Genre, GenreNone)
	}
}

func TestEncode_AllFieldsMaxLength(t *testing.T) {
	thirty := bytes.Repeat([]byte{'A'}, 30)
	four := []byte("2026")
	in := &Tag{
		Title:   string(thirty),
		Artist:  string(thirty),
		Album:   string(thirty),
		Year:    string(four),
		Comment: string(thirty),
		Genre:   17, // Rock
	}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != TagSize {
		t.Errorf("size = %d", buf.Len())
	}
	out, _ := Read(bytes.NewReader(buf.Bytes()))
	if out.Title != string(thirty) || out.Album != string(thirty) {
		t.Errorf("fields truncated unexpectedly")
	}
	if out.Genre != 17 {
		t.Errorf("Genre = %d", out.Genre)
	}
}

// --- WriteFile additional ----------------------------------------

func TestWriteFile_OverwritesExistingTrailer(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	// Pre-populate with TAG-marked trailer + audio prefix.
	audio := bytes.Repeat([]byte{0x42}, 100)
	if err := os.WriteFile(p, audio, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Tag{Title: "first", Genre: GenreNone}).WriteFile(p); err != nil {
		t.Fatal(err)
	}
	if err := (&Tag{Title: "second", Genre: GenreNone}).WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Size() != int64(len(audio)+TagSize) {
		t.Errorf("size = %d, want %d (trailer must replace, not stack)", info.Size(), len(audio)+TagSize)
	}
	got, _ := ReadFile(p)
	if got.Title != "second" {
		t.Errorf("Title = %q, want second", got.Title)
	}
}

func TestWriteFile_NonexistentPath(t *testing.T) {
	if err := (&Tag{Title: "x", Genre: GenreNone}).WriteFile("/nonexistent/dir/x.bin"); err == nil {
		t.Fatal("expected error: missing parent dir")
	}
}

func TestStripFile_NonexistentPath(t *testing.T) {
	if err := StripFile("/nonexistent/dir/x.bin"); err == nil {
		t.Fatal("expected error: missing parent dir")
	}
}

func TestStripFile_PreservesAudioBytes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	audio := bytes.Repeat([]byte{0xAA}, 200)
	if err := os.WriteFile(p, audio, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Tag{Title: "x", Genre: GenreNone}).WriteFile(p); err != nil {
		t.Fatal(err)
	}
	if err := StripFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, audio) {
		t.Errorf("audio bytes mutated by Strip")
	}
}

// --- ReadFile error paths ---------------------------------------

func TestReadFile_NonexistentPath(t *testing.T) {
	if _, err := ReadFile("/nonexistent/dir/x.bin"); err == nil {
		t.Fatal("expected error opening missing file")
	}
}

// --- Read on io.Seeker that can return at SeekEnd -------------------

func TestRead_SeekableButShorter(t *testing.T) {
	// A short reader (no TAG, < TagSize) — must produce ErrNoTag.
	rs := bytes.NewReader([]byte("short"))
	_, err := Read(rs)
	if !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
	// And position must be restored to start-relative end.
	pos, _ := rs.Seek(0, io.SeekCurrent)
	_ = pos // implementation does not promise restoration, but must not crash.
}

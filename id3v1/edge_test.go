package id3v1

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRead_EmptyStream(t *testing.T) {
	if _, err := Read(bytes.NewReader(nil)); !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
}

func TestRead_ExactlyTagSize_NoMagic(t *testing.T) {
	// 128 bytes that don't start with "TAG" must be ErrNoTag, not a
	// false-positive parse of all-zero fields.
	rs := bytes.NewReader(make([]byte, TagSize))
	if _, err := Read(rs); !errors.Is(err, ErrNoTag) {
		t.Errorf("got %v, want ErrNoTag", err)
	}
}

func TestRead_ExactlyTagSize_WithMagic(t *testing.T) {
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Title != "" || tag.Artist != "" || tag.Album != "" {
		t.Errorf("expected empty fields, got %+v", tag)
	}
}

func TestRead_SpacePaddedYear(t *testing.T) {
	// Some encoders pad short fields with 0x20 instead of 0x00.
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	copy(buf[93:97], "99  ") // year "99" followed by two spaces
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Year != "99" {
		t.Errorf("Year = %q, want %q", tag.Year, "99")
	}
}

func TestRead_FullLengthFields(t *testing.T) {
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	for i := 3; i < 127; i++ {
		buf[i] = 'X'
	}
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Title) != 30 {
		t.Errorf("Title len = %d, want 30", len(tag.Title))
	}
	if len(tag.Year) != 4 {
		t.Errorf("Year len = %d, want 4", len(tag.Year))
	}
	if len(tag.Comment) != 30 {
		t.Errorf("Comment len = %d, want 30 (v1.0 layout)", len(tag.Comment))
	}
	if tag.Track != 0 {
		t.Errorf("Track = %d, want 0", tag.Track)
	}
}

func TestRead_V11_TrackZero(t *testing.T) {
	// Track=0 in the v1.1 slot should be treated as v1.0 (no track),
	// not as "track 0". Otherwise round-trip becomes ambiguous.
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	for i := 97; i < 125; i++ {
		buf[i] = 'C'
	}
	buf[125] = 0
	buf[126] = 0 // track = 0 → not v1.1
	buf[127] = GenreNone
	tag, err := Read(bytes.NewReader(buf[:]))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Track != 0 {
		t.Errorf("Track = %d, want 0 (v1.0 layout)", tag.Track)
	}
	// trimPad strips trailing NULs from the v1.0 30-byte comment
	// field, so the actual length here is 28 (the 'C' run).
	if tag.Comment != "CCCCCCCCCCCCCCCCCCCCCCCCCCCC" {
		t.Errorf("Comment = %q", tag.Comment)
	}
}

func TestEncode_TruncatesYearTo4Bytes(t *testing.T) {
	in := &Tag{Year: "20260101", Genre: GenreNone}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if out.Year != "2026" {
		t.Errorf("Year = %q, want 2026", out.Year)
	}
}

func TestEncode_UTF8_TruncatesAtByteBoundary(t *testing.T) {
	// ID3v1 has no encoding byte. tunetag stores the 30-byte field
	// as raw bytes; on read, Go's string conversion preserves them
	// as UTF-8. Each Japanese rune is 3 bytes, so 30 bytes ÷ 3 = 10
	// runes — provided the truncation happens to land on a UTF-8
	// boundary, which it does for repetitions of 3-byte runes.
	long := "日本語タイトル日本語タイトル日本語タイトル日本語タイトル日本語"
	in := &Tag{Title: long, Genre: GenreNone}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(out.Title)); n != 10 {
		t.Errorf("Title rune count = %d, want 10 (UTF-8 preserved)", n)
	}
}

func TestEncode_TrackNonZeroEmitsV11(t *testing.T) {
	// Track != 0 must reserve byte 125 = 0 and byte 126 = track,
	// shrinking the comment field from 30 to 28.
	in := &Tag{Comment: "ABCDEFGHIJKLMNOPQRSTUVWXYZ12", Track: 7, Genre: GenreNone}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if raw[125] != 0 {
		t.Errorf("byte 125 = %d, want 0", raw[125])
	}
	if raw[126] != 7 {
		t.Errorf("byte 126 = %d, want 7", raw[126])
	}
	out, _ := Read(bytes.NewReader(raw))
	if out.Track != 7 {
		t.Errorf("Track = %d", out.Track)
	}
	if len(out.Comment) != 28 {
		t.Errorf("Comment len = %d, want 28", len(out.Comment))
	}
}

func TestWriteFile_Empty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tag := &Tag{Title: "X", Genre: GenreNone}
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Size() != TagSize {
		t.Errorf("size = %d, want %d", info.Size(), TagSize)
	}
}

func TestWriteFile_ShortFileBelowTagSize(t *testing.T) {
	// File smaller than TagSize: should be treated as untagged so
	// the new tag is appended, not detected as an existing trailer.
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	body := bytes.Repeat([]byte{0xAB}, 50)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Tag{Title: "Y", Genre: GenreNone}).WriteFile(p); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if len(out) != 50+TagSize {
		t.Errorf("size = %d, want %d", len(out), 50+TagSize)
	}
	if !bytes.Equal(out[:50], body) {
		t.Errorf("audio prefix corrupted")
	}
}

func TestStripFile_OnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StripFile(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}

func TestStripFile_OnTagSizeFileWithoutMagic(t *testing.T) {
	// A file exactly TagSize long without "TAG" magic must be left
	// alone (no false-positive truncation).
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	body := bytes.Repeat([]byte{0xCD}, TagSize)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StripFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, body) {
		t.Errorf("file modified despite no TAG marker")
	}
}

func TestGenreName_AllStandardCodes(t *testing.T) {
	// Every valid index returns a non-empty name; out-of-range
	// returns empty.
	for code := 0; code < len(Genres); code++ {
		tag := &Tag{Genre: uint8(code)}
		if tag.GenreName() == "" {
			t.Errorf("Genre %d returned empty name", code)
		}
	}
	for _, code := range []uint8{192, 200, 254, 255} {
		tag := &Tag{Genre: code}
		if tag.GenreName() != "" {
			t.Errorf("Genre %d name = %q, want empty", code, tag.GenreName())
		}
	}
}

package aac

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
)

func writeTemp(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.aac")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIsADTS(t *testing.T) {
	cases := []struct {
		b    []byte
		want bool
	}{
		{[]byte{0xFF, 0xF1}, true},  // MPEG-4, no CRC, layer 0
		{[]byte{0xFF, 0xF0}, true},  // MPEG-4, CRC
		{[]byte{0xFF, 0xF9}, true},  // MPEG-2
		{[]byte{0xFF, 0xFB}, false}, // MP3 (layer III)
		{[]byte{0xFF, 0xE0}, false}, // MPEG-2.5 MP3
		{[]byte{0x00, 0x00}, false}, // not sync
		{[]byte{0xFF}, false},       // too short
		{nil, false},
	}
	for _, c := range cases {
		if got := IsADTS(c.b); got != c.want {
			t.Errorf("IsADTS(% X) = %v, want %v", c.b, got, c.want)
		}
	}
}

func TestRead_RawADTS_NoTags(t *testing.T) {
	// Pure ADTS with no tags should parse successfully (no error)
	// and return a File with V2 == V1 == nil.
	body := append([]byte{0xFF, 0xF1, 0x50, 0x80}, make([]byte, 8)...)
	f, err := Read(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if f.V2 != nil || f.V1 != nil {
		t.Errorf("expected no tags, got V2=%v V1=%v", f.V2 != nil, f.V1 != nil)
	}
	if f.Title() != "" {
		t.Errorf("Title = %q, want empty", f.Title())
	}
}

func TestRead_LeadingID3v2(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("AACtitle")
	tag.SetArtist("AACartist")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	// Append fake audio bytes (ADTS-shaped).
	buf.Write([]byte{0xFF, 0xF1, 0x50, 0x80})
	buf.Write(make([]byte, 32))

	f, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if f.V2 == nil || f.Title() != "AACtitle" {
		t.Errorf("Title = %q V2 nil? %v", f.Title(), f.V2 == nil)
	}
}

func TestRead_TrailingID3v1(t *testing.T) {
	var buf bytes.Buffer
	// Fake audio
	buf.Write([]byte{0xFF, 0xF1, 0x50, 0x80})
	buf.Write(make([]byte, 32))
	v1 := &id3v1.Tag{Title: "old school", Genre: id3v1.GenreNone}
	if err := v1.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	f, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if f.V1 == nil || f.V1.Title != "old school" {
		t.Errorf("V1 = %+v", f.V1)
	}
}

func TestRead_RejectsGarbage(t *testing.T) {
	body := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	if _, err := Read(bytes.NewReader(body)); !errors.Is(err, ErrNotAAC) {
		t.Errorf("got %v, want ErrNotAAC", err)
	}
}

func TestWriteFile_RoundTripsID3v2(t *testing.T) {
	// File: ID3v2 prefix + ADTS-shaped body.
	v2 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	v2.SetTitle("Old")
	var buf bytes.Buffer
	v2.Encode(&buf)
	audio := append([]byte{0xFF, 0xF1, 0x50, 0x80}, make([]byte, 64)...)
	buf.Write(audio)
	p := writeTemp(t, buf.Bytes())

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.V2.SetTitle("New")
	f.V2.SetArtist("Whoever")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "New" || got.Artist() != "Whoever" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
	// Audio body bytes still intact at the right offset.
	raw, _ := os.ReadFile(p)
	if !bytes.Contains(raw, audio) {
		t.Errorf("audio body not preserved verbatim")
	}
}

func TestWriteFile_AddsID3v2WhenAbsent(t *testing.T) {
	audio := append([]byte{0xFF, 0xF1, 0x50, 0x80}, make([]byte, 32)...)
	p := writeTemp(t, audio)
	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.V2 != nil {
		t.Fatal("starting file had no V2 tag")
	}
	f.V2 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	f.V2.SetTitle("Brand New")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "Brand New" {
		t.Errorf("Title = %q", got.Title())
	}
}

package id3v2

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEncode_V22_TextFrame round-trips a TextFrame through v2.2.
// On disk the frame must use the 3-char "TT2" id; in memory the
// canonical "TIT2" must be preserved on re-read.
func TestEncode_V22_TextFrame(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0}
	in.SetTitle("V22 Title")
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Verify the 3-char id appears at the right offset (after the
	// 10-byte tag header).
	if !bytes.Contains(buf.Bytes()[HeaderSize:], []byte("TT2")) {
		t.Errorf("encoded bytes do not contain v2.2 TT2 frame id")
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != V22 {
		t.Errorf("Version = %s, want V22", out.Version)
	}
	if out.Title() != "V22 Title" {
		t.Errorf("Title = %q", out.Title())
	}
}

func TestEncode_V22_UserTextFrame(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&UserTextFrame{Encoding: EncISO88591, Description: "MOOD", Value: "Calm"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d", len(out.Frames))
	}
	utf, ok := out.Frames[0].(*UserTextFrame)
	if !ok {
		t.Fatalf("got %T, want *UserTextFrame", out.Frames[0])
	}
	if utf.Description != "MOOD" || utf.Value != "Calm" {
		t.Errorf("got %+v", utf)
	}
}

func TestEncode_V22_CommentFrame(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&CommentFrame{Encoding: EncISO88591, Language: "eng", Description: "", Text: "hi"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	cf, ok := out.Frames[0].(*CommentFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if cf.Language != "eng" || cf.Text != "hi" {
		t.Errorf("got %+v", cf)
	}
}

func TestEncode_V22_USLT(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&UnsynchronisedLyricsFrame{Encoding: EncISO88591, Language: "eng", Text: "lyrics"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	uf, ok := out.Frames[0].(*UnsynchronisedLyricsFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if uf.Text != "lyrics" {
		t.Errorf("got %+v", uf)
	}
}

func TestEncode_V22_PictureFrame_JPEG(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&PictureFrame{
			Encoding:    EncISO88591,
			MIME:        "image/jpeg",
			PictureType: 3,
			Description: "Cover",
			Data:        []byte{0xFF, 0xD8, 0xFF, 0xE0},
		},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	// The 3-char "JPG" image format code must appear in the encoded
	// frame body (right after the encoding byte).
	if !bytes.Contains(buf.Bytes(), []byte("JPG")) {
		t.Errorf("encoded bytes missing JPG image format code")
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	pf, ok := out.Frames[0].(*PictureFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if pf.MIME != "image/jpeg" {
		t.Errorf("MIME = %q, want image/jpeg", pf.MIME)
	}
	if pf.PictureType != 3 || pf.Description != "Cover" {
		t.Errorf("got %+v", pf)
	}
	if !bytes.Equal(pf.Data, []byte{0xFF, 0xD8, 0xFF, 0xE0}) {
		t.Errorf("Data = % X", pf.Data)
	}
}

func TestEncode_V22_PictureFrame_UnknownMIME(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&PictureFrame{MIME: "image/webp", PictureType: 3, Data: []byte{0x00}},
	}}
	err := in.Encode(&bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for unsupported MIME in v2.2")
	}
	if !strings.Contains(err.Error(), "image/webp") {
		t.Errorf("error %q should mention the MIME type", err)
	}
}

func TestEncode_V22_PRIV_RejectedNoEquivalent(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&PrivFrame{Owner: "WM/ContentID", Data: []byte("xyz")},
	}}
	err := in.Encode(&bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error: PRIV has no v2.2 representation")
	}
}

func TestEncode_V22_UFID_RoundTrip(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&UFIDFrame{Owner: "https://musicbrainz.org", Identifier: []byte("1234-5678")},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	uf, ok := out.Frames[0].(*UFIDFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if uf.Owner != "https://musicbrainz.org" || !bytes.Equal(uf.Identifier, []byte("1234-5678")) {
		t.Errorf("got %+v", uf)
	}
}

// TestEncode_V22_RejectsUnmappableTextFrame uses TSO2 (album-artist
// sort order, introduced in v2.4) which has no v2.2 representation.
func TestEncode_V22_RejectsUnmappableTextFrame(t *testing.T) {
	in := &Tag{Version: V22, Padding: 0, Frames: []Frame{
		&TextFrame{FrameID: "TSO2", Encoding: EncISO88591, Text: []string{"X"}},
	}}
	err := in.Encode(&bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error: TSO2 has no v2.2 representation")
	}
}

// TestWriteFile_V22_PrependsAndRoundTrips writes a fresh v2.2 tag
// to a tagless file, then re-reads it. Exercises framesEncodedSize's
// V22 path, which previously rejected the version and broke
// WriteFile entirely.
func TestWriteFile_V22_PrependsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	audio := []byte("FAKE_AUDIO_BODY")
	if err := os.WriteFile(p, audio, 0o644); err != nil {
		t.Fatal(err)
	}

	tag := &Tag{Version: V22, Padding: 0}
	tag.SetTitle("V22 WriteFile Test")
	tag.SetArtist("Tester")
	if err := tag.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != V22 {
		t.Errorf("Version = %s", out.Version)
	}
	if out.Title() != "V22 WriteFile Test" {
		t.Errorf("Title = %q", out.Title())
	}
	if out.Artist() != "Tester" {
		t.Errorf("Artist = %q", out.Artist())
	}

	// Audio body must remain intact after the tag.
	body, _ := os.ReadFile(p)
	if !bytes.HasSuffix(body, audio) {
		t.Errorf("audio body lost after V22 WriteFile")
	}
}

// TestWriteFile_V22_InPlaceReplace verifies the in-place path works
// for V22: a second SetTitle call must not change the file size as
// long as the new tag fits in the existing slot.
func TestWriteFile_V22_InPlaceReplace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	audio := []byte("AUDIO")
	if err := os.WriteFile(p, audio, 0o644); err != nil {
		t.Fatal(err)
	}

	tag := &Tag{Version: V22, Padding: 256}
	tag.SetTitle("First")
	if err := tag.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(p)

	tag2, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tag2.SetTitle("Second")
	if err := tag2.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(p)
	if info1.Size() != info2.Size() {
		t.Errorf("size changed %d -> %d (in-place expected)", info1.Size(), info2.Size())
	}
	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "Second" {
		t.Errorf("Title = %q", out.Title())
	}
}

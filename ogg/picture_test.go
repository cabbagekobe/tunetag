package ogg

import (
	"bytes"
	"testing"

	"github.com/cabbagekobe/tunetag/flac"
)

func TestPicture_RoundTrip(t *testing.T) {
	// Build an Ogg Vorbis file with no embedded picture, add
	// one via AddPicture, write, re-read, and verify.
	ident := buildVorbisIdent()
	comment := buildVorbisComment("v", [2]string{"TITLE", "Cover Test"})
	stream := buildPage(11, 0, 0x02, ident)
	stream = append(stream, buildPage(11, 1, 0, comment)...)
	stream = append(stream, buildPage(11, 2, 0, []byte("\x00\x00\x00\x00audio"))...)
	p := writeOggTemp(t, stream)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if pics := f.Pictures(); len(pics) != 0 {
		t.Fatalf("starting Pictures = %d, want 0", len(pics))
	}
	pic := &flac.Picture{
		PictureType: 3, // CoverFront
		MIME:        "image/jpeg",
		Description: "front",
		Data:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x42, 0x42},
	}
	if err := f.AddPicture(pic); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := g.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures after write = %d, want 1", len(pics))
	}
	got := pics[0]
	if got.MIME != pic.MIME || got.Description != pic.Description || !bytes.Equal(got.Data, pic.Data) {
		t.Errorf("Picture mismatch: %+v vs %+v", got, pic)
	}
}

func TestRemovePictures(t *testing.T) {
	ident := buildVorbisIdent()
	comment := buildVorbisComment("v", [2]string{"TITLE", "x"})
	stream := buildPage(13, 0, 0x02, ident)
	stream = append(stream, buildPage(13, 1, 0, comment)...)
	stream = append(stream, buildPage(13, 2, 0, []byte("\x00\x00\x00\x00audio"))...)
	p := writeOggTemp(t, stream)

	f, _ := ReadFile(p)
	_ = f.AddPicture(&flac.Picture{PictureType: 3, MIME: "image/png", Data: []byte{0x89, 0x50}})
	f.RemovePictures()
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, _ := ReadFile(p)
	if len(g.Pictures()) != 0 {
		t.Errorf("Pictures not removed")
	}
}

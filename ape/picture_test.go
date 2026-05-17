package ape

import (
	"bytes"
	"testing"
)

func TestPicture_RoundTrip(t *testing.T) {
	tag := &Tag{HasHeader: true}
	tag.Set("Title", "x")
	pic := &Picture{
		Filename: "cover.jpg",
		Data:     []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x42, 0x42},
	}
	if err := tag.AddPicture(pic); err != nil {
		t.Fatal(err)
	}
	body, err := tag.Encode()
	if err != nil {
		t.Fatal(err)
	}
	full := append([]byte("AUDIO"), body...)

	got, err := Read(bytes.NewReader(full))
	if err != nil {
		t.Fatal(err)
	}
	pics := got.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures = %d, want 1", len(pics))
	}
	if pics[0].Filename != "cover.jpg" || !bytes.Equal(pics[0].Data, pic.Data) {
		t.Errorf("Picture mismatch: %+v", pics[0])
	}
}

func TestRemovePictures(t *testing.T) {
	tag := &Tag{HasHeader: true}
	tag.Set("Title", "keep me")
	_ = tag.AddPicture(&Picture{Filename: "a.jpg", Data: []byte{0xFF, 0xD8}})
	_ = tag.AddPictureAs(KeyCoverArtBack, &Picture{Filename: "b.jpg", Data: []byte{0xFF, 0xD8}})
	tag.RemovePictures()
	if got := tag.Pictures(); len(got) != 0 {
		t.Errorf("Pictures after Remove = %d, want 0", len(got))
	}
	if tag.Get("Title") != "keep me" {
		t.Errorf("non-cover items were deleted")
	}
}

func TestDecodePicture_MissingNULSeparator(t *testing.T) {
	if _, err := DecodePicture([]byte("no-nul-here")); err == nil {
		t.Error("expected error for missing NUL separator")
	}
}

func TestAddPicture_FilenameEmpty(t *testing.T) {
	tag := &Tag{}
	pic := &Picture{Data: []byte{0xAB, 0xCD}}
	if err := tag.AddPicture(pic); err != nil {
		t.Fatal(err)
	}
	// On-disk value must still have the NUL separator at offset 0.
	it := tag.Items[0]
	if it.Type != ItemBinary {
		t.Errorf("Type = %v, want ItemBinary", it.Type)
	}
	if len(it.Value) != 3 || it.Value[0] != 0 {
		t.Errorf("Value = % X, want 00 AB CD", it.Value)
	}
}

package flac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPicture_RoundTrip(t *testing.T) {
	in := &Picture{
		PictureType:   3,
		MIME:          "image/jpeg",
		Description:   "Front cover 表紙",
		Width:         500,
		Height:        500,
		Depth:         24,
		IndexedColors: 0,
		Data:          []byte{0xFF, 0xD8, 'J', 'F', 'I', 'F'},
	}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parsePicture(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.PictureType != 3 || out.MIME != "image/jpeg" || out.Description != "Front cover 表紙" ||
		out.Width != 500 || out.Height != 500 || out.Depth != 24 ||
		!bytes.Equal(out.Data, in.Data) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

func TestPicture_RoundTripAllFields(t *testing.T) {
	in := &Picture{
		PictureType:   3,
		MIME:          "image/png",
		Description:   "front cover",
		Width:         800,
		Height:        600,
		Depth:         24,
		IndexedColors: 0,
		Data:          []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01},
	}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parsePicture(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.PictureType != in.PictureType || out.MIME != in.MIME ||
		out.Description != in.Description || out.Width != in.Width ||
		out.Height != in.Height || out.Depth != in.Depth ||
		out.IndexedColors != in.IndexedColors {
		t.Errorf("metadata mismatch: %+v vs %+v", in, out)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Errorf("Data mismatch")
	}
}

func TestRead_PictureWithEmptyData(t *testing.T) {
	in := &Picture{
		PictureType: 3,
		MIME:        "image/png",
		Description: "",
		Data:        nil,
	}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parsePicture(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 0 {
		t.Errorf("Data len = %d, want 0", len(out.Data))
	}
}

func TestRead_PictureTruncated(t *testing.T) {
	if _, err := parsePicture([]byte{0, 0, 0, 1}); err == nil {
		t.Fatal("expected error: PICTURE block too short")
	}
}

func TestPicture_DataLengthLies(t *testing.T) {
	var body bytes.Buffer
	be := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		body.Write(b[:])
	}
	be(3)
	be(9)
	body.Write([]byte("image/png"))
	be(0)
	be(800)
	be(600)
	be(24)
	be(0)
	be(1_000_000)
	body.Write([]byte{0xAB, 0xCD})
	if _, err := parsePicture(body.Bytes()); err == nil {
		t.Fatal("expected error: PICTURE data len exceeds body")
	}
}

func TestPicture_OversizedRejected(t *testing.T) {
	p := &Picture{MIME: string(bytes.Repeat([]byte{'a'}, MaxBlockSize+1))}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: PICTURE block too large")
	}
}

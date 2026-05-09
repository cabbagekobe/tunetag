package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- Read-side defensive parsing ---------------------------------

func TestRead_EmptyStream(t *testing.T) {
	if _, err := Read(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error on empty stream")
	}
}

func TestRead_TruncatedAfterMagic(t *testing.T) {
	// "fLaC" only — no STREAMINFO header follows.
	if _, err := Read(bytes.NewReader([]byte("fLaC"))); err == nil {
		t.Fatal("expected error: truncated after magic")
	}
}

func TestRead_BlockBodyExceedsStream(t *testing.T) {
	// Block header claims 1 KiB body but only 4 bytes follow.
	var buf bytes.Buffer
	buf.Write(Magic[:])
	if err := writeBlockHeader(&buf, BlockStreamInfo, true, 1024); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00})
	if _, err := Read(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected error: block body exceeds stream")
	}
}

func TestRead_TruncatedWithoutLastFlag(t *testing.T) {
	// STREAMINFO has its last-flag CLEAR, but no further block follows.
	var buf bytes.Buffer
	buf.Write(Magic[:])
	if err := writeBlockHeader(&buf, BlockStreamInfo, false, 34); err != nil {
		t.Fatal(err)
	}
	buf.Write(make([]byte, 34))
	if _, err := Read(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected error: stream ends without last-block flag")
	}
}

func TestRead_VorbisCommentCountTooLarge(t *testing.T) {
	// Comment count claims 1M entries but body is short. Parser must
	// reject without trying to allocate / read past the body.
	var body bytes.Buffer
	binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len
	binary.Write(&body, binary.LittleEndian, uint32(1_000_000))
	// no actual comment data
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: comment count exceeds body")
	}
}

func TestRead_VorbisCommentStringLenOverflow(t *testing.T) {
	var body bytes.Buffer
	binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len
	binary.Write(&body, binary.LittleEndian, uint32(1)) // 1 comment
	binary.Write(&body, binary.LittleEndian, uint32(0xFFFFFFFF))
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: comment length exceeds body")
	}
}

func TestRead_VorbisCommentNoEqualsSign(t *testing.T) {
	// Comments without '=' are stored verbatim. splitComment treats
	// the whole string as the "key" with an empty value, so a lookup
	// by that exact key returns one empty string; unrelated keys
	// return nil.
	in := &VorbisComment{Vendor: "v", Comments: []string{"NotAValidEntry"}}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Comments) != 1 {
		t.Fatalf("Comments = %d, want 1", len(out.Comments))
	}
	if got := out.Get("NotAValidEntry"); len(got) != 1 || got[0] != "" {
		t.Errorf("Get(\"NotAValidEntry\") = %#v, want [\"\"]", got)
	}
	if out.Get("TITLE") != nil {
		t.Errorf("unrelated key should return nil")
	}
}

func TestRead_MultipleVorbisCommentBlocks(t *testing.T) {
	// Spec says only one VORBIS_COMMENT block per stream, but we
	// shouldn't panic when given two: the helper VorbisComment()
	// returns the first.
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	v1 := &VorbisComment{Vendor: "first", Comments: []string{"TITLE=A"}}
	v2 := &VorbisComment{Vendor: "second", Comments: []string{"TITLE=B"}}
	var buf bytes.Buffer
	buf.Write(Magic[:])
	for i, b := range []Block{si, v1, v2} {
		body, _ := b.Encode()
		writeBlockHeader(&buf, b.Type(), i == 2, uint32(len(body)))
		buf.Write(body)
	}
	f, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	vc := f.VorbisComment()
	if vc.Vendor != "first" {
		t.Errorf("VorbisComment helper returned %q, want first", vc.Vendor)
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
	// Just a few bytes — definitely shorter than the 32-byte minimum.
	if _, err := parsePicture([]byte{0, 0, 0, 1}); err == nil {
		t.Fatal("expected error: PICTURE block too short")
	}
}

// --- VorbisComment helpers ---------------------------------------

func TestVorbisComment_RemoveAllOccurrences(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"ARTIST=A", "Artist=B", "artist=C", "TITLE=X",
	}}
	vc.Remove("ARTIST")
	if vc.Get("artist") != nil {
		t.Errorf("Remove left artist values: %v", vc.Comments)
	}
	if vc.First("TITLE") != "X" {
		t.Errorf("Remove dropped unrelated entry: %v", vc.Comments)
	}
}

func TestVorbisComment_SetEmptyClearsKey(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"TITLE=X"}}
	vc.Set("TITLE", "")
	if len(vc.Comments) != 0 {
		t.Errorf("Set with empty value should clear: %v", vc.Comments)
	}
}

func TestVorbisComment_VendorEmptyDefaults(t *testing.T) {
	vc := &VorbisComment{}
	body, err := vc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Vendor != VendorString {
		t.Errorf("Vendor = %q, want %q", out.Vendor, VendorString)
	}
}

// --- Block-level invariants --------------------------------------

func TestBlock_OversizedPaddingRejected(t *testing.T) {
	p := &PaddingBlock{Size: MaxBlockSize + 1}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: padding exceeds 24-bit max")
	}
}

func TestBlock_NegativePaddingRejected(t *testing.T) {
	p := &PaddingBlock{Size: -1}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: negative padding")
	}
}

func TestWriteBlockHeader_TypeOutOfRange(t *testing.T) {
	if err := writeBlockHeader(&bytes.Buffer{}, 200, false, 0); err == nil {
		t.Fatal("expected error: block type > 127")
	}
}

func TestWriteBlockHeader_SizeOutOfRange(t *testing.T) {
	if err := writeBlockHeader(&bytes.Buffer{}, BlockStreamInfo, false, MaxBlockSize+1); err == nil {
		t.Fatal("expected error: size exceeds 24-bit max")
	}
}

// --- File-level WriteFile ---------------------------------------

func TestWriteFile_RejectsZeroBlocks(t *testing.T) {
	f := &File{}
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	// Create a placeholder file the writer can open.
	body := []byte("fLaC")
	body = append(body, []byte{0x80, 0, 0, 0}...) // last STREAMINFO header, size=0
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err == nil {
		t.Fatal("expected error: zero blocks")
	}
}

func TestWriteFile_RejectsNonStreamInfoFirstBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	body := []byte("fLaC")
	body = append(body, []byte{0x80, 0, 0, 0}...)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &File{Blocks: []Block{
		&VorbisComment{Vendor: "x"}, // first block is NOT STREAMINFO
	}}
	if err := f.WriteFile(p); err == nil {
		t.Fatal("expected error: first block must be STREAMINFO")
	}
}

func TestWriteFile_NonexistentPath(t *testing.T) {
	f := &File{Blocks: []Block{
		&RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)},
	}}
	if err := f.WriteFile("/dev/null/missing/x.flac"); err == nil {
		t.Fatal("expected error for nonexistent parent")
	}
}

func TestRead_NotFLAC(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("OggS......"))); !errors.Is(err, ErrNoFLAC) {
		t.Errorf("got %v, want ErrNoFLAC", err)
	}
}

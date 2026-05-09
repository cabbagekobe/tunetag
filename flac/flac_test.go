package flac

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// helper: build a minimal valid FLAC file in memory.
//
//	"fLaC" + STREAMINFO(34 bytes, last=true unless more blocks) + ...
func buildFLAC(t *testing.T, blocks []Block, audio []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write(Magic[:])
	for i, b := range blocks {
		body, err := b.Encode()
		if err != nil {
			t.Fatalf("encode block %d: %v", i, err)
		}
		last := i == len(blocks)-1
		if err := writeBlockHeader(&buf, b.Type(), last, uint32(len(body))); err != nil {
			t.Fatal(err)
		}
		buf.Write(body)
	}
	buf.Write(audio)
	return buf.Bytes()
}

// helper: synth a 34-byte STREAMINFO body. Contents are arbitrary;
// we only care that the byte count is correct.
func dummyStreamInfo() *RawBlock {
	return &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
}

func TestRead_NoMagic(t *testing.T) {
	rs := bytes.NewReader([]byte("XYZW___not_flac"))
	if _, err := Read(rs); !errors.Is(err, ErrNoFLAC) {
		t.Fatalf("got %v, want ErrNoFLAC", err)
	}
}

func TestRead_StreamInfoOnly(t *testing.T) {
	raw := buildFLAC(t, []Block{dummyStreamInfo()}, []byte{0xDE, 0xAD, 0xBE, 0xEF})
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(f.Blocks))
	}
	if f.Blocks[0].Type() != BlockStreamInfo {
		t.Errorf("first block type = %d", f.Blocks[0].Type())
	}
}

func TestRead_RejectsMissingStreamInfo(t *testing.T) {
	// Build a file whose first block is VORBIS_COMMENT (not allowed).
	bad := buildFLAC(t, []Block{&VorbisComment{Vendor: "x"}}, nil)
	if _, err := Read(bytes.NewReader(bad)); err == nil {
		t.Fatal("expected error")
	}
}

func TestVorbisComment_RoundTrip(t *testing.T) {
	in := &VorbisComment{
		Vendor:   "tunetag-test",
		Comments: []string{"TITLE=Hello", "ARTIST=Alice", "ARTIST=Bob"},
	}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Vendor != in.Vendor {
		t.Errorf("Vendor = %q", out.Vendor)
	}
	if len(out.Comments) != 3 {
		t.Errorf("Comments len = %d", len(out.Comments))
	}
	for i := range in.Comments {
		if in.Comments[i] != out.Comments[i] {
			t.Errorf("Comments[%d] = %q, want %q", i, out.Comments[i], in.Comments[i])
		}
	}
}

func TestVorbisComment_GetSetRemove(t *testing.T) {
	vc := &VorbisComment{Vendor: "x"}
	vc.Set("title", "First Title")
	if got := vc.First("TITLE"); got != "First Title" {
		t.Errorf("case-insensitive lookup failed: %q", got)
	}
	vc.Set("Title", "Second Title")
	if vals := vc.Get("title"); len(vals) != 1 || vals[0] != "Second Title" {
		t.Errorf("Set should replace: %v", vals)
	}
	vc.Add("ARTIST", "A")
	vc.Add("artist", "B")
	if vals := vc.Get("Artist"); len(vals) != 2 {
		t.Errorf("Add multi-value: %v", vals)
	}
	vc.Remove("ARTIST")
	if vals := vc.Get("artist"); len(vals) != 0 {
		t.Errorf("Remove failed: %v", vals)
	}
}

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

func TestRead_FullFile_VorbisCommentAndPicture(t *testing.T) {
	vc := &VorbisComment{Vendor: "v", Comments: []string{"TITLE=X", "ARTIST=Y"}}
	pic := &Picture{PictureType: 3, MIME: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}
	raw := buildFLAC(t, []Block{dummyStreamInfo(), vc, pic}, []byte("AUDIO"))

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Blocks) != 3 {
		t.Fatalf("blocks = %d", len(f.Blocks))
	}
	gotVC := f.VorbisComment()
	if gotVC.First("TITLE") != "X" {
		t.Errorf("Title via VorbisComment helper: %q", gotVC.First("TITLE"))
	}
	if pics := f.Pictures(); len(pics) != 1 || pics[0].MIME != "image/png" {
		t.Errorf("Pictures = %+v", pics)
	}
}

func TestVorbisComment_Helper_CreatesOnDemand(t *testing.T) {
	raw := buildFLAC(t, []Block{dummyStreamInfo()}, nil)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(f.Blocks))
	}
	vc := f.VorbisComment()
	vc.Set("TITLE", "auto")
	if len(f.Blocks) != 2 {
		t.Errorf("expected helper to insert VC block, got %d blocks", len(f.Blocks))
	}
	if f.Blocks[1] != vc {
		t.Errorf("helper did not insert the same block")
	}
}

func TestWriteFile_InPlace_Exact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	vc := &VorbisComment{Vendor: "v", Comments: []string{"TITLE=Same"}}
	raw := buildFLAC(t, []Block{dummyStreamInfo(), vc}, []byte("AUDIO12345"))
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, raw) {
		t.Errorf("byte-for-byte equal expected after no-op write")
	}
}

func TestWriteFile_InPlace_AddsPadding(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	// Initial: STREAMINFO + PADDING(2 KiB) + audio.
	raw := buildFLAC(t, []Block{dummyStreamInfo(), &PaddingBlock{Size: 2048}}, []byte("AUDIO"))
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	vc := f.VorbisComment()
	vc.Set("TITLE", "After Edit")

	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	if got := fileSize(t, p); int(got) != len(raw) {
		t.Errorf("size = %d, want %d (in-place expected)", got, len(raw))
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.VorbisComment().First("TITLE"); got != "After Edit" {
		t.Errorf("Title = %q", got)
	}

	body, _ := os.ReadFile(p)
	if !bytes.HasSuffix(body, []byte("AUDIO")) {
		t.Errorf("audio body lost")
	}
}

func TestWriteFile_FullRewrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	// Initial: STREAMINFO + small VC, no padding.
	vc := &VorbisComment{Vendor: "v", Comments: []string{"T=x"}}
	raw := buildFLAC(t, []Block{dummyStreamInfo(), vc}, []byte("AUDIO_BODY_HERE"))
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	gotVC := f.VorbisComment()
	gotVC.Set("DESCRIPTION", "a much longer description that will definitely not fit in the existing tiny VORBIS_COMMENT block")
	gotVC.Add("ARTIST", "Alice")
	gotVC.Add("ARTIST", "Bob")

	before := fileSize(t, p)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	after := fileSize(t, p)
	if after <= before {
		t.Errorf("expected file growth, got %d -> %d", before, after)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.VorbisComment().First("DESCRIPTION"); !bytes.Contains([]byte(got), []byte("longer description")) {
		t.Errorf("DESCRIPTION = %q", got)
	}
	body, _ := os.ReadFile(p)
	if !bytes.HasSuffix(body, []byte("AUDIO_BODY_HERE")) {
		t.Errorf("audio body lost")
	}
}

func TestWriteFile_PreservesUnknownBlocks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	app := &RawBlock{BlockType: BlockApplication, Body: []byte("APP_DATA_OPAQUE")}
	seek := &RawBlock{BlockType: BlockSeekTable, Body: bytes.Repeat([]byte{0x12}, 64)}
	raw := buildFLAC(t,
		[]Block{dummyStreamInfo(), app, seek, &PaddingBlock{Size: 256}},
		[]byte("AUDIO"))
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	vc := f.VorbisComment()
	vc.Set("TITLE", "preserve test")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}

	out, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var foundApp, foundSeek bool
	for _, b := range out.Blocks {
		if r, ok := b.(*RawBlock); ok {
			switch r.BlockType {
			case BlockApplication:
				if bytes.Equal(r.Body, app.Body) {
					foundApp = true
				}
			case BlockSeekTable:
				if bytes.Equal(r.Body, seek.Body) {
					foundSeek = true
				}
			}
		}
	}
	if !foundApp {
		t.Errorf("APPLICATION block not preserved")
	}
	if !foundSeek {
		t.Errorf("SEEKTABLE block not preserved")
	}
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

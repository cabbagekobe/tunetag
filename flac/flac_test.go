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

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// --- Read defensive ---------------------------------------------

func TestRead_EmptyStream(t *testing.T) {
	if _, err := Read(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error on empty stream")
	}
}

func TestRead_NoMagic(t *testing.T) {
	rs := bytes.NewReader([]byte("XYZW___not_flac"))
	if _, err := Read(rs); !errors.Is(err, ErrNoFLAC) {
		t.Fatalf("got %v, want ErrNoFLAC", err)
	}
}

func TestRead_NotFLAC(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("OggS......"))); !errors.Is(err, ErrNoFLAC) {
		t.Errorf("got %v, want ErrNoFLAC", err)
	}
}

func TestRead_TruncatedAfterMagic(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("fLaC"))); err == nil {
		t.Fatal("expected error: truncated after magic")
	}
}

func TestRead_BlockBodyExceedsStream(t *testing.T) {
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
	bad := buildFLAC(t, []Block{&VorbisComment{Vendor: "x"}}, nil)
	if _, err := Read(bytes.NewReader(bad)); err == nil {
		t.Fatal("expected error")
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
		_ = writeBlockHeader(&buf, b.Type(), i == 2, uint32(len(body)))
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

func TestRead_FromFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	vc := &VorbisComment{Vendor: "v", Comments: []string{"TITLE=hello"}}
	pad := &PaddingBlock{Size: 1024}
	f := &File{Blocks: []Block{si, vc, pad}}
	body, err := f.encodeMetadata()
	if err != nil {
		t.Fatal(err)
	}
	full := append(Magic[:], body...)
	full = append(full, []byte("audio")...)
	if err := os.WriteFile(p, full, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.VorbisComment().First("TITLE") != "hello" {
		t.Errorf("TITLE = %q", got.VorbisComment().First("TITLE"))
	}
}

// --- File helpers ----------------------------------------------

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

func TestFile_VorbisCommentCreatesIfAbsent(t *testing.T) {
	f := &File{Blocks: []Block{
		&RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)},
	}}
	vc := f.VorbisComment()
	if vc == nil {
		t.Fatal("returned nil")
	}
	if len(f.Blocks) != 2 {
		t.Errorf("Blocks = %d, want 2 (STREAMINFO + new VC)", len(f.Blocks))
	}
	if _, ok := f.Blocks[1].(*VorbisComment); !ok {
		t.Errorf("Blocks[1] = %T, want *VorbisComment", f.Blocks[1])
	}
}

func TestFile_VorbisCommentMutationsPersist(t *testing.T) {
	f := &File{Blocks: []Block{
		&RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)},
	}}
	f.VorbisComment().Set("TITLE", "first")
	if v := f.VorbisComment().First("TITLE"); v != "first" {
		t.Errorf("First TITLE = %q", v)
	}
	f.VorbisComment().Set("TITLE", "second")
	if v := f.VorbisComment().First("TITLE"); v != "second" {
		t.Errorf("First TITLE = %q", v)
	}
}

func TestFile_RemovePicturesNoPicture(t *testing.T) {
	f := &File{Blocks: []Block{
		&RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)},
	}}
	f.RemovePictures()
	if len(f.Blocks) != 1 {
		t.Errorf("Blocks = %d, want 1", len(f.Blocks))
	}
}

func TestFile_AddPicturesMultiple(t *testing.T) {
	f := &File{Blocks: []Block{
		&RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)},
	}}
	f.AddPicture(&Picture{PictureType: 3, MIME: "image/png"})
	f.AddPicture(&Picture{PictureType: 4, MIME: "image/jpeg"})
	if len(f.Pictures()) != 2 {
		t.Errorf("Pictures = %d, want 2", len(f.Pictures()))
	}
	f.RemovePictures()
	if len(f.Pictures()) != 0 {
		t.Errorf("after RemovePictures: %d, want 0", len(f.Pictures()))
	}
}

// --- WriteFile -------------------------------------------------

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

func TestWriteFile_AbsorbingPaddingShrinksSafely(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	vc := &VorbisComment{Vendor: "v", Comments: []string{"TITLE=" + string(bytes.Repeat([]byte{'a'}, 200))}}
	pad := &PaddingBlock{Size: 1024}
	f := &File{Blocks: []Block{si, vc, pad}}
	body, err := f.encodeMetadata()
	if err != nil {
		t.Fatal(err)
	}
	full := append(Magic[:], body...)
	full = append(full, []byte("audio")...)
	if err := os.WriteFile(p, full, 0o644); err != nil {
		t.Fatal(err)
	}
	origSize := int64(len(full))

	f2, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f2.VorbisComment().Set("TITLE", "x")
	if err := f2.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Size() != origSize {
		t.Errorf("size = %d, want %d (padding should absorb)", info.Size(), origSize)
	}
	got, _ := os.ReadFile(p)
	if !bytes.HasSuffix(got, []byte("audio")) {
		t.Errorf("audio bytes corrupted")
	}
}

func TestWriteFile_RejectsZeroBlocks(t *testing.T) {
	f := &File{}
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	body := []byte("fLaC")
	body = append(body, []byte{0x80, 0, 0, 0}...)
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
		&VorbisComment{Vendor: "x"},
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

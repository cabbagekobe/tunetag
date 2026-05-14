package flac

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// --- VorbisComment ------------------------------------------------

func TestVC_Set_RemovesAllMatchingCaseInsensitive(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"TITLE=A", "Title=B", "title=C", "Artist=X",
	}}
	vc.Set("title", "Z")
	titles := vc.Get("TITLE")
	if len(titles) != 1 || titles[0] != "Z" {
		t.Errorf("Get TITLE = %v, want [Z]", titles)
	}
	if vc.First("Artist") != "X" {
		t.Errorf("Artist dropped accidentally")
	}
}

func TestVC_Add_AppendsDuplicates(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"GENRE=Rock"}}
	vc.Add("GENRE", "Jazz")
	vc.Add("GENRE", "Blues")
	got := vc.Get("GENRE")
	if len(got) != 3 {
		t.Errorf("GENRE count = %d, want 3", len(got))
	}
}

func TestVC_First_EmptyOnMissing(t *testing.T) {
	vc := &VorbisComment{}
	if got := vc.First("NOPE"); got != "" {
		t.Errorf("First on missing key = %q, want empty", got)
	}
}

func TestVC_Get_PreservesValueOrder(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"ARTIST=A", "TITLE=T", "ARTIST=B", "ARTIST=C",
	}}
	got := vc.Get("ARTIST")
	want := []string{"A", "B", "C"}
	if len(got) != 3 {
		t.Fatalf("Get count = %d", len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Get[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVC_RemoveCaseInsensitive(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"Date=2026", "DATE=2025"}}
	vc.Remove("date")
	if vc.Get("DATE") != nil {
		t.Errorf("Remove left entries: %v", vc.Comments)
	}
}

func TestVC_RoundTripPreservesCase(t *testing.T) {
	in := &VorbisComment{Vendor: "vendor", Comments: []string{"TiTlE=case-preserved"}}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Comments) != 1 || out.Comments[0] != "TiTlE=case-preserved" {
		t.Errorf("comment = %v, want unchanged case", out.Comments)
	}
	// Lookup must be case-insensitive.
	if v := out.First("title"); v != "case-preserved" {
		t.Errorf("First = %q", v)
	}
}

func TestParseVorbisComment_TruncatedVendorLen(t *testing.T) {
	// Body too short to even hold the 4-byte vendor length.
	if _, err := parseVorbisComment([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error: truncated vendor length")
	}
}

func TestParseVorbisComment_VendorLenExceedsBody(t *testing.T) {
	var body bytes.Buffer
	binary.Write(&body, binary.LittleEndian, uint32(100)) // claim 100-byte vendor
	body.WriteString("x")                                  // only 1 byte present
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: vendor length exceeds body")
	}
}

func TestParseVorbisComment_TruncatedBetweenVendorAndCount(t *testing.T) {
	var body bytes.Buffer
	binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len = 0
	body.WriteByte(0)                                    // first byte of comment count missing
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: truncated before comment count")
	}
}

func TestParseVorbisComment_TruncatedAtCommentI(t *testing.T) {
	var body bytes.Buffer
	binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor
	binary.Write(&body, binary.LittleEndian, uint32(2)) // 2 comments
	binary.Write(&body, binary.LittleEndian, uint32(1))
	body.WriteByte('A')
	// Second comment header truncated.
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: truncated at comment 1")
	}
}

// --- Picture round-trip / encode -------------------------------

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

func TestPicture_DataLengthLies(t *testing.T) {
	// Build a body whose declared data length exceeds the actual bytes
	// supplied. parsePicture must reject rather than panic.
	var body bytes.Buffer
	be := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		body.Write(b[:])
	}
	be(3)         // picture type
	be(9)         // mime len = 9
	body.Write([]byte("image/png"))
	be(0)         // description len = 0
	be(800)       // width
	be(600)       // height
	be(24)        // depth
	be(0)         // indexed
	be(1_000_000) // data len lies
	body.Write([]byte{0xAB, 0xCD})
	if _, err := parsePicture(body.Bytes()); err == nil {
		t.Fatal("expected error: PICTURE data len exceeds body")
	}
}

func TestPicture_OversizedRejected(t *testing.T) {
	// Synthesize a picture whose encoded size would clearly exceed
	// MaxBlockSize. We don't actually allocate that much data; instead
	// we set an absurdly long MIME string.
	p := &Picture{MIME: string(bytes.Repeat([]byte{'a'}, MaxBlockSize+1))}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: PICTURE block too large")
	}
}

// --- File-level helpers ----------------------------------------

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
	// No-op on a file without any picture blocks.
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

// --- Read/Write file end-to-end ---------------------------------

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

// --- block.go encoder defenses -----------------------------------

func TestRawBlock_TypeAndEncode(t *testing.T) {
	r := &RawBlock{BlockType: 99, Body: []byte("hello")}
	if r.Type() != 99 {
		t.Errorf("Type = %d", r.Type())
	}
	b, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, r.Body) {
		t.Errorf("Encode mismatch")
	}
}

func TestWriteBlockHeader_LastFlagSetCorrectly(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBlockHeader(&buf, BlockStreamInfo, true, 34); err != nil {
		t.Fatal(err)
	}
	hdr := buf.Bytes()
	if hdr[0]&0x80 == 0 {
		t.Errorf("last flag not set: % X", hdr)
	}
	if hdr[0]&0x7F != BlockStreamInfo {
		t.Errorf("block type = %d, want %d", hdr[0]&0x7F, BlockStreamInfo)
	}
}

// --- writeFile fallthrough --------------------------------------

func TestWriteFile_AbsorbingPaddingShrinksSafely(t *testing.T) {
	// Build a FLAC with TITLE=long, write to disk, then shrink the title
	// and write back. The file size must stay constant (padding absorbs
	// the diff).
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
	// Audio bytes must still be there.
	got, _ := os.ReadFile(p)
	if !bytes.HasSuffix(got, []byte("audio")) {
		t.Errorf("audio bytes corrupted")
	}
}

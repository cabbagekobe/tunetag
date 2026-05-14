package tunetag

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/internal/testutil"
)

// --- Detect on each format -----------------------------------

func TestDetect_DetectsFLAC(t *testing.T) {
	body := []byte("fLaC")
	body = append(body, 0x80, 0, 0, 0)
	got, err := Detect(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatFLAC {
		t.Errorf("got %s, want FLAC", got)
	}
}

func TestDetect_DetectsMP4(t *testing.T) {
	body := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x"})
	got, err := Detect(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatMP4 {
		t.Errorf("got %s, want MP4", got)
	}
}

func TestDetect_DetectsID3v2(t *testing.T) {
	var buf bytes.Buffer
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("hello")
	tag.Encode(&buf)
	got, err := Detect(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatID3v2 {
		t.Errorf("got %s, want ID3v2", got)
	}
}

func TestDetect_DetectsID3v1OnlyTrailer(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 200))
	(&id3v1.Tag{Title: "x", Genre: id3v1.GenreNone}).Encode(&buf)
	got, err := Detect(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatID3v1 {
		t.Errorf("got %s, want ID3v1", got)
	}
}

// --- Open: per-format wrappers --------------------------------

func TestOpen_FLAC_FormatAndTitle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	body := buildFLAC(t, "Tune of the Day")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Format() != FormatFLAC {
		t.Errorf("Format = %s", tag.Format())
	}
	if tag.Title() != "Tune of the Day" {
		t.Errorf("Title = %q", tag.Title())
	}
}

func TestOpen_MP4_AllTextFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	body := testutil.BuildMinimal(testutil.MinimalOptions{
		Title:  "T",
		Artist: "A",
		Album:  "Al",
	})
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Format() != FormatMP4 {
		t.Errorf("Format = %s", tag.Format())
	}
	if tag.Title() != "T" || tag.Artist() != "A" || tag.Album() != "Al" {
		t.Errorf("fields wrong: %+v", tag)
	}
}

func TestOpen_FallsBackToID3v1(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	var buf bytes.Buffer
	buf.Write(make([]byte, 100))
	(&id3v1.Tag{Title: "only v1", Genre: id3v1.GenreNone}).Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Format() != FormatID3v1 {
		t.Errorf("Format = %s", tag.Format())
	}
	if tag.Title() != "only v1" {
		t.Errorf("Title = %q", tag.Title())
	}
}

// --- OpenFLAC / OpenMP4 ---------------------------------------

func TestOpenFLAC_NonexistentPath(t *testing.T) {
	if _, err := OpenFLAC("/nonexistent/x.flac"); err == nil {
		t.Fatal("expected error opening missing FLAC")
	}
}

func TestOpenMP4_NonexistentPath(t *testing.T) {
	if _, err := OpenMP4("/nonexistent/x.m4a"); err == nil {
		t.Fatal("expected error opening missing MP4")
	}
}

func TestOpenFLAC_ReturnsParsedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	body := buildFLAC(t, "x")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := OpenFLAC(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.VorbisComment().First("TITLE") != "x" {
		t.Errorf("TITLE = %q", got.VorbisComment().First("TITLE"))
	}
}

// --- Strip on each format -------------------------------------

func TestStrip_ID3v1_PreservesAudio(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	audio := bytes.Repeat([]byte{0xAB}, 200)
	var buf bytes.Buffer
	buf.Write(audio)
	(&id3v1.Tag{Title: "x", Genre: id3v1.GenreNone}).Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, audio) {
		t.Errorf("audio bytes mutated by Strip")
	}
}

func TestStrip_ID3v2(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("X")
	var buf bytes.Buffer
	tag.Encode(&buf)
	buf.Write([]byte("AUDIO"))
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	// After strip, the tag header still exists but frames are gone.
	got, err := id3v2.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Frames) != 0 {
		t.Errorf("frames after Strip = %d, want 0", len(got.Frames))
	}
}

func TestStrip_FLAC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.flac")
	body := buildFLAC(t, "title-to-remove")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := flac.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Only STREAMINFO survives.
	for _, b := range got.Blocks {
		if _, ok := b.(*flac.VorbisComment); ok {
			t.Errorf("VorbisComment block survived Strip")
		}
	}
}

func TestStrip_MP4_ClearsItems(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	body := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x"})
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	// After Strip the ilst items should be empty.
	got, err := OpenMP4(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tag.Items) != 0 {
		t.Errorf("Items after Strip = %d, want 0", len(got.Tag.Items))
	}
}

// --- Tag accessors via mp3Tag --------------------------------

func TestMP3Tag_V1FieldsExposedWhenV2Absent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	var buf bytes.Buffer
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{
		Title:  "the title",
		Artist: "the artist",
		Album:  "the album",
		Year:   "2026",
		Track:  9,
		Genre:  17,
	}).Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Title() != "the title" {
		t.Errorf("Title = %q", tag.Title())
	}
	if tag.Artist() != "the artist" {
		t.Errorf("Artist = %q", tag.Artist())
	}
	if tag.Album() != "the album" {
		t.Errorf("Album = %q", tag.Album())
	}
	if tag.Year() != 2026 {
		t.Errorf("Year = %d", tag.Year())
	}
	if n, _ := tag.TrackNumber(); n != 9 {
		t.Errorf("Track = %d", n)
	}
	if tag.Genre() != "Rock" {
		t.Errorf("Genre = %q", tag.Genre())
	}
}

// --- Picture safety: copies, not shared slices ---------------

func TestPicturesAreSafelyDecoupledFromV2Tag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	picData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0, Frames: []id3v2.Frame{
		&id3v2.PictureFrame{Encoding: id3v2.EncUTF8, MIME: "image/jpeg", PictureType: 3, Data: picData},
	}}
	var buf bytes.Buffer
	tag.Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := got.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures = %d, want 1", len(pics))
	}
	if !bytes.Equal(pics[0].Data, picData) {
		t.Errorf("Picture data mismatch")
	}
	// Mutating the returned slice must not panic.
	pics[0].Data[0] = 0x00
}

// --- ErrUnknownFormat propagation ----------------------------

func TestDetect_PositionRestoredOnUnknown(t *testing.T) {
	body := bytes.Repeat([]byte{0xDD}, 64)
	rs := bytes.NewReader(body)
	rs.Seek(20, io.SeekStart)
	_, err := Detect(rs)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
	pos, _ := rs.Seek(0, io.SeekCurrent)
	if pos != 20 {
		t.Errorf("pos = %d, want 20", pos)
	}
}

// --- Helpers -------------------------------------------------

func buildFLAC(t *testing.T, title string) []byte {
	t.Helper()
	si := &flac.RawBlock{BlockType: flac.BlockStreamInfo, Body: make([]byte, 34)}
	vc := &flac.VorbisComment{Vendor: "v", Comments: []string{"TITLE=" + title}}
	pad := &flac.PaddingBlock{Size: 64}
	f := &flac.File{Blocks: []flac.Block{si, vc, pad}}
	body, err := encodeMetadataExported(t, f)
	if err != nil {
		t.Fatal(err)
	}
	full := append(flac.Magic[:], body...)
	full = append(full, []byte("audiooo")...)
	return full
}

// encodeMetadataExported reproduces flac.File.encodeMetadata using
// the public Block.Encode and writeBlockHeader path. Necessary because
// encodeMetadata is unexported.
func encodeMetadataExported(t *testing.T, f *flac.File) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	for i, b := range f.Blocks {
		body, err := b.Encode()
		if err != nil {
			return nil, err
		}
		last := byte(0)
		if i == len(f.Blocks)-1 {
			last = 0x80
		}
		size := uint32(len(body))
		buf.WriteByte(last | (b.Type() & 0x7F))
		buf.WriteByte(byte(size >> 16))
		buf.WriteByte(byte(size >> 8))
		buf.WriteByte(byte(size))
		buf.Write(body)
	}
	return buf.Bytes(), nil
}

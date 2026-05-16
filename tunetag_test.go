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
	"github.com/cabbagekobe/tunetag/internal/mp4test"
)

// --- helpers ---------------------------------------------------

func writeFile(t *testing.T, name string, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeFLACBlockHdr writes a 4-byte FLAC block header. Used by
// FLAC tests that build files by hand instead of going through the
// flac package's unexported encodeMetadata.
func writeFLACBlockHdr(buf *bytes.Buffer, blockType uint8, last bool, size uint32) {
	var b [4]byte
	b[0] = blockType & 0x7F
	if last {
		b[0] |= 0x80
	}
	b[1] = byte(size >> 16)
	b[2] = byte(size >> 8)
	b[3] = byte(size)
	buf.Write(b[:])
}

// buildFLACFile builds a complete FLAC byte slice with the given
// blocks plus a short audio body, suitable for testing Open().
func buildFLACFile(t *testing.T, blocks []flac.Block, audio []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write(flac.Magic[:])
	for i, b := range blocks {
		body, err := b.Encode()
		if err != nil {
			t.Fatal(err)
		}
		writeFLACBlockHdr(&buf, b.Type(), i == len(blocks)-1, uint32(len(body)))
		buf.Write(body)
	}
	buf.Write(audio)
	return buf.Bytes()
}

// --- Detect ----------------------------------------------------

func TestDetect_KnownFormats(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want Format
	}{
		{"id3v2", []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, FormatID3v2},
		{"flac", []byte{'f', 'L', 'a', 'C', 0, 0, 0, 4, 'd', 'a', 't', 'a'}, FormatFLAC},
		{"mp4", []byte{0, 0, 0, 8, 'f', 't', 'y', 'p'}, FormatMP4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Detect(bytes.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Detect = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDetect_FLACFromMagic(t *testing.T) {
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

func TestDetect_MP4FromTestutil(t *testing.T) {
	body := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "x"})
	got, err := Detect(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatMP4 {
		t.Errorf("got %s, want MP4", got)
	}
}

func TestDetect_ID3v2FromEncode(t *testing.T) {
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

func TestDetect_ID3v1Trailer(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "X", Genre: id3v1.GenreNone}).Encode(&buf)
	got, err := Detect(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != FormatID3v1 {
		t.Errorf("got %s, want ID3v1", got)
	}
}

func TestDetect_Unknown(t *testing.T) {
	if _, err := Detect(bytes.NewReader([]byte("nothing here at all"))); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_EmptyStream(t *testing.T) {
	_, err := Detect(bytes.NewReader(nil))
	if !errors.Is(err, ErrEmptyFile) {
		t.Errorf("got %v, want ErrEmptyFile", err)
	}
	// Refines ErrUnknownFormat, so existing callers that branch on
	// the older sentinel must keep working.
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("ErrEmptyFile should also match ErrUnknownFormat, got %v", err)
	}
}

func TestDetect_TooSmall(t *testing.T) {
	for size := 1; size < 12; size++ {
		body := bytes.Repeat([]byte{0xAB}, size)
		_, err := Detect(bytes.NewReader(body))
		if !errors.Is(err, ErrFileTooSmall) {
			t.Errorf("size=%d: got %v, want ErrFileTooSmall", size, err)
		}
		if !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("size=%d: ErrFileTooSmall should also match ErrUnknownFormat, got %v", size, err)
		}
	}
	// Exactly the threshold (12 bytes) of garbage should still fall
	// through to the generic ErrUnknownFormat, not ErrFileTooSmall.
	_, err := Detect(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 12)))
	if errors.Is(err, ErrFileTooSmall) {
		t.Errorf("12-byte garbage should be ErrUnknownFormat, not ErrFileTooSmall (got %v)", err)
	}
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("12-byte garbage: got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_GarbageBytes(t *testing.T) {
	body := bytes.Repeat([]byte{0xCD}, 256)
	if _, err := Detect(bytes.NewReader(body)); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_FtypNotAtFront(t *testing.T) {
	// MP4 detection requires "ftyp" at exactly offset 4 (the box's
	// type field). The same 4 bytes anywhere else must NOT match.
	body := []byte("XXXX" + "FOOO" + "ftyp" + "M4A more bytes")
	if _, err := Detect(bytes.NewReader(body)); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_RestoresStreamPosition(t *testing.T) {
	body := []byte("PADBYTEID3\x04\x00\x00\x00\x00\x00\x00")
	rs := bytes.NewReader(body)
	rs.Seek(5, io.SeekStart)
	_, _ = Detect(rs) // outcome irrelevant; we only care about position
	pos, _ := rs.Seek(0, io.SeekCurrent)
	if pos != 5 {
		t.Errorf("position after Detect = %d, want 5", pos)
	}
}

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

// --- Open ------------------------------------------------------

func TestOpen_ID3v2(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("Open Test")
	tag.SetArtist("Artist")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte("AUDIO"))
	p := writeFile(t, "x.mp3", buf.Bytes())

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatID3v2 {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Open Test" || got.Artist() != "Artist" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
}

func TestOpen_ID3v1(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "OldSchool", Artist: "Pioneer", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatID3v1 {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "OldSchool" {
		t.Errorf("Title = %q", got.Title())
	}
}

func TestOpen_FLAC(t *testing.T) {
	si := &flac.RawBlock{BlockType: flac.BlockStreamInfo, Body: make([]byte, 34)}
	vc := &flac.VorbisComment{Vendor: "v", Comments: []string{"TITLE=Hello", "ARTIST=Alice"}}
	raw := buildFLACFile(t, []flac.Block{si, vc}, []byte("AUDIO"))
	p := writeFile(t, "x.flac", raw)

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatFLAC {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Hello" || got.Artist() != "Alice" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
}

func TestOpen_MP4(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title: "Cosmic", Artist: "Carl", Album: "Stars",
	})
	p := writeFile(t, "x.m4a", raw)

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatMP4 {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Cosmic" {
		t.Errorf("Title = %q", got.Title())
	}
	if got.Album() != "Stars" {
		t.Errorf("Album = %q", got.Album())
	}
}

func TestOpen_FallsBackToID3v1(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 100))
	(&id3v1.Tag{Title: "only v1", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

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

func TestOpen_PreferenceID3v2OverID3v1(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("V2")
	var buf bytes.Buffer
	tag.Encode(&buf)
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "V1", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatID3v2 {
		t.Errorf("Format = %s, want ID3v2", got.Format())
	}
	if got.Title() != "V2" {
		t.Errorf("Title = %q, want V2", got.Title())
	}
}

func TestOpen_NonexistentPath(t *testing.T) {
	if _, err := Open("/nonexistent/tunetag/edge.mp3"); err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestOpen_GarbageFile(t *testing.T) {
	p := writeFile(t, "garbage", []byte("not a known container"))
	if _, err := Open(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestOpenFLAC_NonexistentPath(t *testing.T) {
	if _, err := OpenFLAC("/nonexistent/x.flac"); err == nil {
		t.Fatal("expected error opening missing FLAC")
	}
}

func TestOpenFLAC_ReturnsParsedFile(t *testing.T) {
	si := &flac.RawBlock{BlockType: flac.BlockStreamInfo, Body: make([]byte, 34)}
	vc := &flac.VorbisComment{Vendor: "v", Comments: []string{"TITLE=x"}}
	raw := buildFLACFile(t, []flac.Block{si, vc}, []byte("AUDIO"))
	p := writeFile(t, "x.flac", raw)
	got, err := OpenFLAC(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.VorbisComment().First("TITLE") != "x" {
		t.Errorf("TITLE = %q", got.VorbisComment().First("TITLE"))
	}
}

func TestOpenMP4_NonexistentPath(t *testing.T) {
	if _, err := OpenMP4("/nonexistent/x.m4a"); err == nil {
		t.Fatal("expected error opening missing MP4")
	}
}

// --- OpenMP3 ---------------------------------------------------

func TestOpenMP3_PrefersV2(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("V2 Title")
	var buf bytes.Buffer
	tag.Encode(&buf)
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "V1 Title", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

	mp3, err := OpenMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if mp3.V2 == nil || mp3.V1 == nil {
		t.Fatalf("missing tag: V2=%v V1=%v", mp3.V2 != nil, mp3.V1 != nil)
	}
	if mp3.V2.Title() != "V2 Title" {
		t.Errorf("V2 Title = %q", mp3.V2.Title())
	}
	if mp3.V1.Title != "V1 Title" {
		t.Errorf("V1 Title = %q", mp3.V1.Title)
	}
}

func TestOpenMP3_NeitherFound(t *testing.T) {
	p := writeFile(t, "x.mp3", []byte("AUDIO"))
	if _, err := OpenMP3(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestOpenMP3_OnlyV1(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "Only V1", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

	mp3, err := OpenMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if mp3.V2 != nil {
		t.Errorf("V2 should be nil")
	}
	if mp3.V1 == nil || mp3.V1.Title != "Only V1" {
		t.Errorf("V1 = %+v", mp3.V1)
	}
}

// --- Strip -----------------------------------------------------

func TestStrip_ID3v1(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("AUDIO_ONLY_BODY"))
	(&id3v1.Tag{Title: "x", Genre: id3v1.GenreNone}).Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !bytes.Equal(data, []byte("AUDIO_ONLY_BODY")) {
		t.Errorf("after strip = %q", data)
	}
}

func TestStrip_ID3v2(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("X")
	var buf bytes.Buffer
	tag.Encode(&buf)
	buf.Write([]byte("AUDIO"))
	p := writeFile(t, "x.mp3", buf.Bytes())

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := id3v2.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Frames) != 0 {
		t.Errorf("frames after Strip = %d, want 0", len(got.Frames))
	}
}

func TestStrip_FLAC(t *testing.T) {
	si := &flac.RawBlock{BlockType: flac.BlockStreamInfo, Body: make([]byte, 34)}
	vc := &flac.VorbisComment{Vendor: "v", Comments: []string{"TITLE=title-to-remove"}}
	pad := &flac.PaddingBlock{Size: 64}
	raw := buildFLACFile(t, []flac.Block{si, vc, pad}, []byte("AUDIO"))
	p := writeFile(t, "x.flac", raw)

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := flac.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range got.Blocks {
		if _, ok := b.(*flac.VorbisComment); ok {
			t.Errorf("VorbisComment block survived Strip")
		}
	}
}

func TestStrip_MP4(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title: "removeme", FreeBytes: 256,
	})
	p := writeFile(t, "x.m4a", raw)

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := OpenMP4(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tag.Items) != 0 {
		t.Errorf("Items after Strip = %d, want 0", len(got.Tag.Items))
	}
}

func TestStrip_GarbageFile(t *testing.T) {
	body := []byte("not a known container")
	p := writeFile(t, "garbage", body)
	if err := Strip(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, body) {
		t.Errorf("Strip on unknown format must not mutate the file")
	}
}

// --- mp3Tag wrapper / Picture safety ---------------------------

func TestMP3Tag_V1FieldsExposedWhenV2Absent(t *testing.T) {
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
	p := writeFile(t, "x.mp3", buf.Bytes())

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

func TestPicturesAreSafelyDecoupledFromV2Tag(t *testing.T) {
	picData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0, Frames: []id3v2.Frame{
		&id3v2.PictureFrame{Encoding: id3v2.EncUTF8, MIME: "image/jpeg", PictureType: 3, Data: picData},
	}}
	var buf bytes.Buffer
	tag.Encode(&buf)
	p := writeFile(t, "x.mp3", buf.Bytes())

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

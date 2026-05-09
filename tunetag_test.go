package tunetag

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/internal/testutil"
)

func writeFile(t *testing.T, name string, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetect(t *testing.T) {
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

func TestDetect_ID3v1Trailer(t *testing.T) {
	// 50 bytes of audio + an ID3v1 trailer.
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
	var buf bytes.Buffer
	buf.Write(flac.Magic[:])
	for i, b := range []flac.Block{si, vc} {
		body, _ := b.Encode()
		writeFLACBlockHdr(&buf, b.Type(), i == 1, uint32(len(body)))
		buf.Write(body)
	}
	buf.Write([]byte("AUDIO"))
	p := writeFile(t, "x.flac", buf.Bytes())

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
	raw := testutil.BuildMinimal(testutil.MinimalOptions{
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
	if mp3.V2 == nil {
		t.Fatalf("V2 missing")
	}
	if mp3.V1 == nil {
		t.Fatalf("V1 missing")
	}
	if mp3.V2.Title() != "V2 Title" {
		t.Errorf("V2 Title = %q", mp3.V2.Title())
	}
	if mp3.V1.Title != "V1 Title" {
		t.Errorf("V1 Title = %q", mp3.V1.Title)
	}
}

func TestStrip_MP4(t *testing.T) {
	raw := testutil.BuildMinimal(testutil.MinimalOptions{
		Title: "removeme", FreeBytes: 256,
	})
	p := writeFile(t, "x.m4a", raw)

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "" {
		t.Errorf("Title still %q after Strip", got.Title())
	}
}

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

// helper for FLAC test (writeFLACBlockHdr).
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

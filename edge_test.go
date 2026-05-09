package tunetag

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
)

func TestDetect_EmptyStream(t *testing.T) {
	if _, err := Detect(bytes.NewReader(nil)); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_GarbageBytes(t *testing.T) {
	body := bytes.Repeat([]byte{0xCD}, 256)
	if _, err := Detect(bytes.NewReader(body)); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestDetect_RestoresStreamPosition(t *testing.T) {
	// Detect must leave rs's offset where it was on entry, regardless
	// of whether the format was recognised.
	body := []byte("PADBYTEID3\x04\x00\x00\x00\x00\x00\x00")
	rs := bytes.NewReader(body)
	rs.Seek(5, 0)
	_, _ = Detect(rs) // outcome irrelevant; we only care about position
	pos, _ := rs.Seek(0, 1)
	if pos != 5 {
		t.Errorf("position after Detect = %d, want 5", pos)
	}
}

func TestDetect_FtypNotAtFront(t *testing.T) {
	// MP4 detection requires "ftyp" at exactly offset 4 (the box's
	// type field). The same 4 bytes anywhere else must NOT match.
	// Here "ftyp" sits at offset 8 with garbage at offset 4-7.
	body := []byte("XXXX" + "FOOO" + "ftyp" + "M4A more bytes")
	if _, err := Detect(bytes.NewReader(body)); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestOpen_NonexistentPath(t *testing.T) {
	if _, err := Open("/nonexistent/tunetag/edge.mp3"); err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestOpen_GarbageFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage")
	if err := os.WriteFile(p, []byte("not a known container"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestStrip_GarbageFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage")
	body := []byte("not a known container")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, body) {
		t.Errorf("Strip on unknown format must not mutate the file")
	}
}

func TestOpenMP3_BothV1AndV2_ReturnsBoth(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")

	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("V2 Title")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "V1 Title", Genre: id3v1.GenreNone}).Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	mp3, err := OpenMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if mp3.V1 == nil || mp3.V2 == nil {
		t.Fatalf("expected both V1 and V2: V1=%v V2=%v", mp3.V1 != nil, mp3.V2 != nil)
	}
}

func TestOpenMP3_NeitherFound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	if err := os.WriteFile(p, []byte("AUDIO"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMP3(p); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("got %v, want ErrUnknownFormat", err)
	}
}

func TestOpenMP3_OnlyV1(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")
	var buf bytes.Buffer
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "Only V1", Genre: id3v1.GenreNone}).Encode(&buf)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

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

func TestFormat_String(t *testing.T) {
	cases := map[Format]string{
		FormatUnknown: "Unknown",
		FormatID3v1:   "ID3v1",
		FormatID3v2:   "ID3v2",
		FormatFLAC:    "FLAC",
		FormatMP4:     "MP4",
		Format(99):    "Unknown",
	}
	for f, want := range cases {
		if got := f.String(); got != want {
			t.Errorf("Format(%d).String() = %q, want %q", f, got, want)
		}
	}
}

func TestPictureType_AllAdvertisedRange(t *testing.T) {
	// The package promises 21 picture types (0..20). Verify the
	// extreme constants compile and have the expected values.
	if PictureOther != 0 {
		t.Errorf("PictureOther = %d, want 0", PictureOther)
	}
	if PicturePublisherLogo != 20 {
		t.Errorf("PicturePublisherLogo = %d, want 20", PicturePublisherLogo)
	}
}

// TestOpen_PreferenceID3v2OverID3v1 verifies that when both tags
// exist, Open returns ID3v2 (the richer one).
func TestOpen_PreferenceID3v2OverID3v1(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.mp3")

	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("V2")
	var buf bytes.Buffer
	tag.Encode(&buf)
	buf.Write(make([]byte, 50))
	(&id3v1.Tag{Title: "V1", Genre: id3v1.GenreNone}).Encode(&buf)
	os.WriteFile(p, buf.Bytes(), 0o644)

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

package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/id3v2"
)

// --- builders --------------------------------------------------

// putChunk appends one top-level RIFF chunk (id + size + body +
// optional pad) to buf.
func putChunk(buf *bytes.Buffer, id string, body []byte) {
	if len(id) != 4 {
		panic("chunk id must be 4 bytes")
	}
	buf.WriteString(id)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	if len(body)%2 == 1 {
		buf.WriteByte(0)
	}
}

// buildWAV wraps payload bytes (no "WAVE" prefix; we add it) in a
// RIFF header.
func buildWAV(payload []byte) []byte {
	var out bytes.Buffer
	out.WriteString(chunkRIFF)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(payload)+4))
	out.WriteString(waveType)
	out.Write(payload)
	return out.Bytes()
}

// infoBody encodes a sequence of (id, value) pairs as the body of
// a LIST/INFO chunk (i.e. the bytes that follow the "INFO" tag).
func infoBody(pairs ...[2]string) []byte {
	var buf bytes.Buffer
	buf.WriteString(ChunkINFO)
	for _, p := range pairs {
		v := append([]byte(p[1]), 0)
		buf.WriteString(p[0])
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(v)))
		buf.Write(v)
		if len(v)%2 == 1 {
			buf.WriteByte(0)
		}
	}
	return buf.Bytes()
}

func writeTemp(t *testing.T, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- Read ------------------------------------------------------

func TestRead_RejectsNonRIFF(t *testing.T) {
	_, err := Read(bytes.NewReader([]byte("nope-not-riff-at-all")))
	if !errors.Is(err, ErrNoWAV) {
		t.Errorf("got %v, want ErrNoWAV", err)
	}
}

func TestRead_RejectsRF64(t *testing.T) {
	body := append([]byte("RF64"), bytes.Repeat([]byte{0}, 4)...)
	body = append(body, []byte("WAVE")...)
	_, err := Read(bytes.NewReader(body))
	if !errors.Is(err, ErrRF64Unsupported) {
		t.Errorf("got %v, want ErrRF64Unsupported", err)
	}
}

func TestRead_RejectsRIFFOtherType(t *testing.T) {
	// RIFF/AVI is a thing; we must not accept it as WAV.
	body := append([]byte("RIFF"), bytes.Repeat([]byte{0}, 4)...)
	body = append(body, []byte("AVI ")...)
	_, err := Read(bytes.NewReader(body))
	if !errors.Is(err, ErrNoWAV) {
		t.Errorf("got %v, want ErrNoWAV", err)
	}
}

func TestRead_NoMetadata(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0x11}, 16))
	putChunk(&pay, "data", bytes.Repeat([]byte{0x22}, 32))
	raw := buildWAV(pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Info) != 0 {
		t.Errorf("Info = %d entries, want 0", len(f.Info))
	}
	if f.ID3 != nil {
		t.Errorf("ID3 = %+v, want nil", f.ID3)
	}
	if len(f.chunks) != 2 {
		t.Errorf("chunks = %d, want 2", len(f.chunks))
	}
}

func TestRead_LISTINFO(t *testing.T) {
	body := infoBody(
		[2]string{InfoTitle, "Hello"},
		[2]string{InfoArtist, "Alice"},
		[2]string{InfoDate, "2026-05-16"},
		[2]string{InfoTrack, "3/12"},
	)
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putChunk(&pay, ChunkLIST, body)
	putChunk(&pay, "data", []byte("audio"))
	raw := buildWAV(pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "Hello" || f.Artist() != "Alice" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
	if f.Year() != 2026 {
		t.Errorf("Year = %d", f.Year())
	}
	if n, total := f.TrackNumber(); n != 3 || total != 12 {
		t.Errorf("Track = %d/%d", n, total)
	}
}

func TestRead_OddSizedInfoEntry(t *testing.T) {
	// Value "ab" has length 2 + NUL = 3 → odd, needs a pad byte.
	// The parser must skip the pad so the next sub-chunk header
	// is read at the correct offset.
	body := infoBody(
		[2]string{InfoTitle, "ab"},     // size 3, padded
		[2]string{InfoArtist, "Alice"}, // size 6, no pad
	)
	var pay bytes.Buffer
	putChunk(&pay, ChunkLIST, body)
	raw := buildWAV(pay.Bytes())
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "ab" || f.Artist() != "Alice" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
}

func TestRead_ID3Chunk(t *testing.T) {
	id3 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	id3.SetTitle("From ID3")
	id3.SetArtist("Carl")
	var id3Body bytes.Buffer
	if err := id3.Encode(&id3Body); err != nil {
		t.Fatal(err)
	}
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putChunk(&pay, "data", []byte("audio"))
	putChunk(&pay, ChunkID3, id3Body.Bytes())
	raw := buildWAV(pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID3 == nil {
		t.Fatal("ID3 is nil")
	}
	if f.Title() != "From ID3" || f.Artist() != "Carl" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
}

func TestRead_ID3PreferredOverLISTINFO(t *testing.T) {
	id3 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	id3.SetTitle("id3-wins")
	var id3Body bytes.Buffer
	id3.Encode(&id3Body)

	var pay bytes.Buffer
	putChunk(&pay, ChunkLIST, infoBody([2]string{InfoTitle, "list-loses"}))
	putChunk(&pay, ChunkID3, id3Body.Bytes())
	raw := buildWAV(pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "id3-wins" {
		t.Errorf("Title = %q", f.Title())
	}
}

func TestRead_StripsTrailingNULs(t *testing.T) {
	// Some writers pad values with multiple NULs; the parser
	// must strip them all from the visible string.
	var body bytes.Buffer
	body.WriteString(ChunkINFO)
	val := []byte("title\x00\x00\x00\x00") // 5 chars + 4 NULs = 9 bytes
	body.WriteString(InfoTitle)
	binary.Write(&body, binary.LittleEndian, uint32(len(val)))
	body.Write(val)
	body.WriteByte(0) // odd length pad

	var pay bytes.Buffer
	putChunk(&pay, ChunkLIST, body.Bytes())
	raw := buildWAV(pay.Bytes())
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "title" {
		t.Errorf("Title = %q", f.Title())
	}
}

func TestRead_TruncatedTrailingChunkIsTolerated(t *testing.T) {
	// Some recorders close the file mid-chunk. We should parse
	// every complete chunk and stop cleanly at the truncation
	// instead of failing the whole read.
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	// Cut off a chunk header partway:
	pay.Write([]byte{'d', 'a', 't'})
	raw := buildWAV(pay.Bytes())
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("expected tolerant parse, got %v", err)
	}
	if len(f.chunks) != 1 {
		t.Errorf("kept chunks = %d, want 1", len(f.chunks))
	}
}

// --- Write -----------------------------------------------------

func TestWriteFile_RoundTripsLISTINFO(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0x11}, 16))
	putChunk(&pay, ChunkLIST, infoBody([2]string{InfoTitle, "old"}))
	putChunk(&pay, "data", []byte("audio_bytes"))
	raw := buildWAV(pay.Bytes())
	p := writeTemp(t, "x.wav", raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.SetInfo(InfoTitle, "new title")
	f.SetInfo(InfoArtist, "new artist")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.InfoValue(InfoTitle) != "new title" || g.InfoValue(InfoArtist) != "new artist" {
		t.Errorf("after write: title=%q artist=%q", g.Title(), g.Artist())
	}
	// The "data" chunk's contents must be unchanged.
	found := false
	for _, c := range g.chunks {
		if c.id == "data" {
			if string(c.body) != "audio_bytes" {
				t.Errorf("data body = %q", c.body)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("data chunk lost on round-trip")
	}
}

func TestWriteFile_AddsID3WhenAbsent(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putChunk(&pay, "data", []byte("audio"))
	raw := buildWAV(pay.Bytes())
	p := writeTemp(t, "x.wav", raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID3 != nil {
		t.Fatal("starting file should have no ID3")
	}
	f.ID3 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	f.ID3.SetTitle("Tagged")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.ID3 == nil || g.ID3.Title() != "Tagged" {
		t.Errorf("ID3 not restored: %+v", g.ID3)
	}
}

func TestWriteFile_DropsEmptyMetadata(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putChunk(&pay, ChunkLIST, infoBody([2]string{InfoTitle, "byebye"}))
	putChunk(&pay, "data", []byte("audio"))
	raw := buildWAV(pay.Bytes())
	p := writeTemp(t, "x.wav", raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Info = nil
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Info) != 0 {
		t.Errorf("Info should be empty, got %+v", g.Info)
	}
	for _, c := range g.chunks {
		if c.id == ChunkLIST {
			t.Errorf("LIST chunk should have been dropped")
		}
	}
}

func TestWriteFile_RIFFSizeIsCorrect(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putChunk(&pay, "data", []byte("audio"))
	raw := buildWAV(pay.Bytes())
	p := writeTemp(t, "x.wav", raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.SetInfo(InfoTitle, "x")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:4]) != chunkRIFF {
		t.Fatalf("not RIFF: %q", got[:4])
	}
	size := binary.LittleEndian.Uint32(got[4:8])
	if int(size)+8 != len(got) {
		t.Errorf("RIFF size %d + 8 != file len %d", size, len(got))
	}
}

// --- accessors -------------------------------------------------

func TestSetInfo_EmptyValueRemoves(t *testing.T) {
	f := &File{Info: []InfoItem{{ID: InfoTitle, Value: "x"}}}
	f.SetInfo(InfoTitle, "")
	if len(f.Info) != 0 {
		t.Errorf("Info = %+v", f.Info)
	}
}

func TestSetInfo_IgnoresBadID(t *testing.T) {
	f := &File{}
	f.SetInfo("toolong", "x")
	f.SetInfo("ab", "y")
	if len(f.Info) != 0 {
		t.Errorf("invalid IDs were stored: %+v", f.Info)
	}
}

func TestYear_FromInfoCRD(t *testing.T) {
	f := &File{Info: []InfoItem{{ID: InfoDate, Value: "2026-05-16"}}}
	if f.Year() != 2026 {
		t.Errorf("Year = %d", f.Year())
	}
}

func TestYear_NonNumeric(t *testing.T) {
	f := &File{Info: []InfoItem{{ID: InfoDate, Value: "spring"}}}
	if y := f.Year(); y != 0 {
		t.Errorf("Year = %d, want 0", y)
	}
}

func TestTrackNumber_FromInfoITRK(t *testing.T) {
	f := &File{Info: []InfoItem{{ID: InfoTrack, Value: "7/15"}}}
	n, total := f.TrackNumber()
	if n != 7 || total != 15 {
		t.Errorf("Track = %d/%d", n, total)
	}
}

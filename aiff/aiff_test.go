package aiff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/id3v2"
)

func putChunk(buf *bytes.Buffer, id string, body []byte) {
	if len(id) != 4 {
		panic("chunk id must be 4 bytes")
	}
	buf.WriteString(id)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(body)))
	buf.Write(body)
	if len(body)%2 == 1 {
		buf.WriteByte(0)
	}
}

func buildAIFF(form string, payload []byte) []byte {
	var out bytes.Buffer
	out.WriteString("FORM")
	_ = binary.Write(&out, binary.BigEndian, uint32(len(payload)+4))
	out.WriteString(form)
	out.Write(payload)
	return out.Bytes()
}

func writeTemp(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.aiff")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRead_RejectsOversizedChunk(t *testing.T) {
	// FORM/AIFF whose first chunk declares size = 4 GiB but the
	// file is tiny. Must error with a sane message rather than
	// attempt a multi-GiB allocation.
	var pay bytes.Buffer
	pay.WriteString("COMM")
	_ = binary.Write(&pay, binary.BigEndian, uint32(0xFFFFFFFF))
	pay.Write(bytes.Repeat([]byte{0xAB}, 32))
	raw := buildAIFF("AIFF", pay.Bytes())
	_, err := Read(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected error for oversized chunk, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("exceeds remaining")) {
		t.Errorf("err = %v, want one mentioning 'exceeds remaining'", err)
	}
}

func TestRead_RejectsNonFORM(t *testing.T) {
	_, err := Read(bytes.NewReader([]byte("notFORMatall")))
	if !errors.Is(err, ErrNoAIFF) {
		t.Errorf("got %v, want ErrNoAIFF", err)
	}
}

func TestRead_RejectsWrongFormType(t *testing.T) {
	// FORM with type "MOV " should not be accepted.
	body := append([]byte("FORM"), bytes.Repeat([]byte{0}, 4)...)
	body = append(body, []byte("MOV ")...)
	if _, err := Read(bytes.NewReader(body)); !errors.Is(err, ErrNoAIFF) {
		t.Errorf("got %v, want ErrNoAIFF", err)
	}
}

func TestRead_TextChunks(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "COMM", bytes.Repeat([]byte{0}, 18))
	putChunk(&pay, "NAME", []byte("My Title"))
	putChunk(&pay, "AUTH", []byte("Me"))
	putChunk(&pay, "(c) ", []byte("2026"))
	putChunk(&pay, "ANNO", []byte("first note"))
	putChunk(&pay, "ANNO", []byte("second note"))
	putChunk(&pay, "SSND", []byte("audio_payload"))
	raw := buildAIFF("AIFF", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "My Title" || f.Artist() != "Me" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
	if f.Text["(c) "] != "2026" {
		t.Errorf("(c) = %q", f.Text["(c) "])
	}
	if len(f.Annotations) != 2 {
		t.Fatalf("Annotations = %d, want 2", len(f.Annotations))
	}
	if f.Comment() != "first note\nsecond note" {
		t.Errorf("Comment = %q", f.Comment())
	}
}

func TestRead_AcceptsAIFC(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "NAME", []byte("compressed"))
	raw := buildAIFF("AIFC", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.FormType != "AIFC" {
		t.Errorf("FormType = %q", f.FormType)
	}
}

func TestRead_ID3Chunk(t *testing.T) {
	id3 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	id3.SetTitle("from ID3")
	var b bytes.Buffer
	if err := id3.Encode(&b); err != nil {
		t.Fatal(err)
	}
	var pay bytes.Buffer
	putChunk(&pay, "ID3 ", b.Bytes())
	raw := buildAIFF("AIFF", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID3 == nil || f.Title() != "from ID3" {
		t.Errorf("Title=%q ID3=%v", f.Title(), f.ID3 != nil)
	}
}

func TestRead_ID3PreferredOverNAME(t *testing.T) {
	id3 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	id3.SetTitle("id3-wins")
	var b bytes.Buffer
	_ = id3.Encode(&b)
	var pay bytes.Buffer
	putChunk(&pay, "NAME", []byte("name-loses"))
	putChunk(&pay, "ID3 ", b.Bytes())
	raw := buildAIFF("AIFF", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "id3-wins" {
		t.Errorf("Title = %q", f.Title())
	}
}

func TestRead_OddSizedChunkAlignment(t *testing.T) {
	// "abc" has length 3 → 1 pad byte required. After it, the
	// next chunk must still parse correctly.
	var pay bytes.Buffer
	putChunk(&pay, "NAME", []byte("abc"))
	putChunk(&pay, "AUTH", []byte("ok"))
	raw := buildAIFF("AIFF", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "abc" || f.Artist() != "ok" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
}

func TestWriteFile_RoundTrips(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "COMM", bytes.Repeat([]byte{0}, 18))
	putChunk(&pay, "NAME", []byte("old"))
	putChunk(&pay, "SSND", []byte("audio"))
	raw := buildAIFF("AIFF", pay.Bytes())
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.SetTitle("new title")
	f.SetAuthor("new author")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title() != "new title" || g.Artist() != "new author" {
		t.Errorf("after write: title=%q author=%q", g.Title(), g.Artist())
	}
	// Audio chunk preserved verbatim. Raw chunks are no longer
	// buffered in memory, so check the bytes on disk.
	if got := rawChunkBody(t, p, "SSND"); string(got) != "audio" {
		t.Errorf("SSND body = %q", got)
	}
}

// rawChunkBody re-parses the FORM layout of the file at path and
// returns the body bytes of the first chunk with the given id.
// Fails the test when the chunk is absent.
func rawChunkBody(t *testing.T, path, id string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 12 {
		t.Fatalf("file too short: %d bytes", len(raw))
	}
	for i := 12; i+8 <= len(raw); {
		cid := string(raw[i : i+4])
		size := int(binary.BigEndian.Uint32(raw[i+4 : i+8]))
		i += 8
		if i+size > len(raw) {
			t.Fatalf("chunk %q size %d overflows file", cid, size)
		}
		if cid == id {
			return raw[i : i+size]
		}
		i += size + size%2
	}
	t.Fatalf("chunk %q not found in %s", id, path)
	return nil
}

func TestWriteFile_DropsEmptyMetadata(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "NAME", []byte("byebye"))
	putChunk(&pay, "SSND", []byte("audio"))
	raw := buildAIFF("AIFF", pay.Bytes())
	p := writeTemp(t, raw)

	f, _ := ReadFile(p)
	f.Text = nil
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title() != "" {
		t.Errorf("Title should be empty, got %q", g.Title())
	}
	for _, c := range g.chunks {
		if c.id == "NAME" {
			t.Errorf("NAME chunk should have been dropped")
		}
	}
}

func TestWriteFile_AddsID3WhenAbsent(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "SSND", []byte("audio"))
	raw := buildAIFF("AIFF", pay.Bytes())
	p := writeTemp(t, raw)

	f, _ := ReadFile(p)
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
		t.Errorf("ID3 not restored")
	}
}

func TestWriteFile_FORMSizeIsCorrect(t *testing.T) {
	var pay bytes.Buffer
	putChunk(&pay, "SSND", []byte("audio"))
	raw := buildAIFF("AIFF", pay.Bytes())
	p := writeTemp(t, raw)

	f, _ := ReadFile(p)
	f.SetTitle("x")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got[:4]) != "FORM" {
		t.Fatalf("not FORM: %q", got[:4])
	}
	size := binary.BigEndian.Uint32(got[4:8])
	if int(size)+8 != len(got) {
		t.Errorf("FORM size %d + 8 != file len %d", size, len(got))
	}
}

// --- Streaming (audio bytes must stay out of memory) -----------

// countingReadSeeker wraps a ReadSeeker and tallies the bytes
// delivered through Read, so a test can prove how much of the
// stream was actually consumed (Seeks are free).
type countingReadSeeker struct {
	rs   io.ReadSeeker
	read int64
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	n, err := c.rs.Read(p)
	c.read += int64(n)
	return n, err
}

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return c.rs.Seek(offset, whence)
}

func TestRead_SkipsAudioBytes(t *testing.T) {
	// An 8 MiB SSND chunk: Read must skip it via Seek rather than
	// buffering it.
	audio := bytes.Repeat([]byte{0xA5}, 8<<20)
	var pay bytes.Buffer
	putChunk(&pay, "NAME", []byte("big"))
	putChunk(&pay, "SSND", audio)
	raw := buildAIFF("AIFF", pay.Bytes())

	crs := &countingReadSeeker{rs: bytes.NewReader(raw)}
	f, err := Read(crs)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "big" {
		t.Errorf("Title = %q", f.Title())
	}
	if crs.read > 4<<10 {
		t.Errorf("Read consumed %d bytes; audio chunk was not skipped", crs.read)
	}
}

func TestWriteFile_FromReaderSource(t *testing.T) {
	// Read from an in-memory stream (not ReadFile): WriteFile must
	// stream the audio bytes from that same reader.
	var pay bytes.Buffer
	putChunk(&pay, "SSND", []byte("reader_audio"))
	raw := buildAIFF("AIFF", pay.Bytes())

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	f.SetTitle("via reader")
	p := filepath.Join(t.TempDir(), "out.aiff")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title() != "via reader" {
		t.Errorf("title = %q", g.Title())
	}
	if got := rawChunkBody(t, p, "SSND"); string(got) != "reader_audio" {
		t.Errorf("SSND body = %q", got)
	}
}

func TestWriteFile_SourceFileGone(t *testing.T) {
	// ReadFile remembers the path instead of buffering audio; if
	// the file vanishes before WriteFile, the write must fail
	// cleanly rather than emit a corrupt file.
	var pay bytes.Buffer
	putChunk(&pay, "SSND", []byte("gone_audio"))
	raw := buildAIFF("AIFF", pay.Bytes())
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err == nil {
		t.Fatal("expected error when source file is gone, got nil")
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file was written despite the failure: stat err = %v", err)
	}
}

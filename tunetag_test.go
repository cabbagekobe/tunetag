package tunetag

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/asf"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/internal/mp4test"
	"github.com/cabbagekobe/tunetag/wav"
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

// buildWAVFile composes a minimal RIFF/WAVE file with the given
// inner payload (chunks after the "WAVE" type tag).
func buildWAVFile(payload []byte) []byte {
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(payload)+4))
	out.WriteString("WAVE")
	out.Write(payload)
	return out.Bytes()
}

// putWAVChunk appends one RIFF chunk to buf.
func putWAVChunk(buf *bytes.Buffer, id string, body []byte) {
	buf.WriteString(id)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	if len(body)%2 == 1 {
		buf.WriteByte(0)
	}
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
		{"wav", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'}, FormatWAV},
		{"aiff", []byte{'F', 'O', 'R', 'M', 0, 0, 0, 0, 'A', 'I', 'F', 'F'}, FormatAIFF},
		{"aifc", []byte{'F', 'O', 'R', 'M', 0, 0, 0, 0, 'A', 'I', 'F', 'C'}, FormatAIFF},
		{"ogg", []byte{'O', 'g', 'g', 'S', 0, 0, 0, 0, 0, 0, 0, 0}, FormatOgg},
		{"adts", []byte{0xFF, 0xF1, 0x50, 0x80, 0, 0, 0, 0, 0, 0, 0, 0}, FormatAAC},
		{"asf", []byte{
			0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11,
			0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C,
		}, FormatASF},
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
	_ = tag.Encode(&buf)
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
	_ = (&id3v1.Tag{Title: "X", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_, _ = rs.Seek(5, io.SeekStart)
	_, _ = Detect(rs) // outcome irrelevant; we only care about position
	pos, _ := rs.Seek(0, io.SeekCurrent)
	if pos != 5 {
		t.Errorf("position after Detect = %d, want 5", pos)
	}
}

func TestDetect_PositionRestoredOnUnknown(t *testing.T) {
	body := bytes.Repeat([]byte{0xDD}, 64)
	rs := bytes.NewReader(body)
	_, _ = rs.Seek(20, io.SeekStart)
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
	_ = (&id3v1.Tag{Title: "OldSchool", Artist: "Pioneer", Genre: id3v1.GenreNone}).Encode(&buf)
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

func TestOpen_WAV_LISTINFO(t *testing.T) {
	// Body of a LIST chunk: "INFO" + sub-chunks.
	var info bytes.Buffer
	info.WriteString("INFO")
	// INAM = "Wave Title"\0
	v := append([]byte("Wave Title"), 0)
	info.WriteString("INAM")
	_ = binary.Write(&info, binary.LittleEndian, uint32(len(v)))
	info.Write(v)
	if len(v)%2 == 1 {
		info.WriteByte(0)
	}
	var pay bytes.Buffer
	putWAVChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putWAVChunk(&pay, "LIST", info.Bytes())
	putWAVChunk(&pay, "data", []byte("audio"))
	p := writeFile(t, "x.wav", buildWAVFile(pay.Bytes()))

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatWAV {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Wave Title" {
		t.Errorf("Title = %q", got.Title())
	}
}

func TestOpen_WAV_NoMetadataIsStillReadable(t *testing.T) {
	// The user's primary complaint: WAV files with no tags at all
	// were rejected as "unsupported". They must now Open
	// successfully and return an empty tag instead.
	var pay bytes.Buffer
	putWAVChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putWAVChunk(&pay, "data", []byte("audio"))
	p := writeFile(t, "bare.wav", buildWAVFile(pay.Bytes()))

	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Format() != FormatWAV {
		t.Errorf("Format = %s", tag.Format())
	}
	if tag.Title() != "" || tag.Artist() != "" {
		t.Errorf("expected empty fields, got title=%q artist=%q", tag.Title(), tag.Artist())
	}
}

func TestOpen_WAV_PrefersID3OverLISTINFO(t *testing.T) {
	// Build ID3v2 body.
	id3 := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	id3.SetTitle("id3-wins")
	var id3Body bytes.Buffer
	_ = id3.Encode(&id3Body)
	// Build INFO body.
	var info bytes.Buffer
	info.WriteString("INFO")
	v := append([]byte("list-loses"), 0)
	info.WriteString("INAM")
	_ = binary.Write(&info, binary.LittleEndian, uint32(len(v)))
	info.Write(v)

	var pay bytes.Buffer
	putWAVChunk(&pay, "LIST", info.Bytes())
	putWAVChunk(&pay, "id3 ", id3Body.Bytes())
	p := writeFile(t, "x.wav", buildWAVFile(pay.Bytes()))

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "id3-wins" {
		t.Errorf("Title = %q, want id3-wins", got.Title())
	}
}

func TestStrip_WAV(t *testing.T) {
	var info bytes.Buffer
	info.WriteString("INFO")
	v := append([]byte("removeme"), 0)
	info.WriteString("INAM")
	_ = binary.Write(&info, binary.LittleEndian, uint32(len(v)))
	info.Write(v)

	var pay bytes.Buffer
	putWAVChunk(&pay, "fmt ", bytes.Repeat([]byte{0}, 16))
	putWAVChunk(&pay, "LIST", info.Bytes())
	putWAVChunk(&pay, "data", []byte("audio"))
	p := writeFile(t, "x.wav", buildWAVFile(pay.Bytes()))

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := wav.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Info) != 0 {
		t.Errorf("Info after Strip = %+v, want empty", got.Info)
	}
	if got.ID3 != nil {
		t.Errorf("ID3 after Strip = %+v, want nil", got.ID3)
	}
}

func TestOpenWAV_NonexistentPath(t *testing.T) {
	if _, err := OpenWAV("/nonexistent/x.wav"); err == nil {
		t.Fatal("expected error opening missing WAV")
	}
}

// --- AIFF ------------------------------------------------------

// buildAIFFRaw composes a minimal FORM/AIFF blob with the given
// inner chunk payload (chunks after the "AIFF" form-type tag).
func buildAIFFRaw(formType string, payload []byte) []byte {
	var out bytes.Buffer
	out.WriteString("FORM")
	_ = binary.Write(&out, binary.BigEndian, uint32(len(payload)+4))
	out.WriteString(formType)
	out.Write(payload)
	return out.Bytes()
}

func putAIFFChunk(buf *bytes.Buffer, id string, body []byte) {
	buf.WriteString(id)
	_ = binary.Write(buf, binary.BigEndian, uint32(len(body)))
	buf.Write(body)
	if len(body)%2 == 1 {
		buf.WriteByte(0)
	}
}

func TestOpen_AIFF_TextChunks(t *testing.T) {
	var pay bytes.Buffer
	putAIFFChunk(&pay, "NAME", []byte("Aiff Title"))
	putAIFFChunk(&pay, "AUTH", []byte("Aiff Author"))
	p := writeFile(t, "x.aif", buildAIFFRaw("AIFF", pay.Bytes()))

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatAIFF {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Aiff Title" || got.Artist() != "Aiff Author" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
}

func TestOpen_AIFF_EmptyIsStillReadable(t *testing.T) {
	p := writeFile(t, "bare.aif", buildAIFFRaw("AIFF", nil))
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatAIFF {
		t.Errorf("Format = %s", got.Format())
	}
}

func TestStrip_AIFF(t *testing.T) {
	var pay bytes.Buffer
	putAIFFChunk(&pay, "NAME", []byte("byebye"))
	putAIFFChunk(&pay, "SSND", []byte("audio"))
	p := writeFile(t, "x.aif", buildAIFFRaw("AIFF", pay.Bytes()))
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title() != "" {
		t.Errorf("Title = %q, want empty after strip", got.Title())
	}
}

// --- Ogg -------------------------------------------------------

func TestOpen_Ogg_Vorbis(t *testing.T) {
	// We rely on the ogg package's own test builders by
	// duplicating the wire format here, since it's the only
	// shared bytes we need.
	vc := &flac.VorbisComment{Vendor: "X", Comments: []string{"TITLE=OggTitle", "ARTIST=OggArtist"}}
	cbody, _ := vc.Encode()
	commentPkt := append([]byte{0x03}, []byte("vorbis")...)
	commentPkt = append(commentPkt, cbody...)
	commentPkt = append(commentPkt, 0x01) // framing bit

	identPkt := append([]byte{0x01}, []byte("vorbis")...)
	identPkt = append(identPkt, make([]byte, 23)...)

	stream := makeOggPage(7, 0, 0x02, identPkt)
	stream = append(stream, makeOggPage(7, 1, 0, commentPkt)...)
	p := writeFile(t, "x.ogg", stream)

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatOgg {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "OggTitle" || got.Artist() != "OggArtist" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
}

func makeOggPage(serial, seq uint32, flags byte, packets ...[]byte) []byte {
	var segs []byte
	var body bytes.Buffer
	for _, pkt := range packets {
		n := len(pkt)
		for n >= 255 {
			segs = append(segs, 255)
			n -= 255
		}
		segs = append(segs, byte(n))
		body.Write(pkt)
	}
	var out bytes.Buffer
	out.WriteString("OggS")
	out.WriteByte(0)
	out.WriteByte(flags)
	out.Write(make([]byte, 8))
	_ = binary.Write(&out, binary.LittleEndian, serial)
	_ = binary.Write(&out, binary.LittleEndian, seq)
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	out.WriteByte(byte(len(segs)))
	out.Write(segs)
	out.Write(body.Bytes())
	return out.Bytes()
}

// --- APE -------------------------------------------------------

func TestOpen_APE(t *testing.T) {
	tag := &ape.Tag{HasHeader: true}
	_ = tag.Set("Title", "Ape Title")
	_ = tag.Set("Artist", "Ape Artist")
	body, err := tag.Encode()
	if err != nil {
		t.Fatal(err)
	}
	full := append([]byte("FAKE_WAVPACK_AUDIO"), body...)
	p := writeFile(t, "x.wv", full)

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatAPE {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "Ape Title" || got.Artist() != "Ape Artist" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
}

func TestStrip_APE(t *testing.T) {
	tag := &ape.Tag{HasHeader: true}
	_ = tag.Set("Title", "byebye")
	body, _ := tag.Encode()
	audio := []byte("audio_kept")
	p := writeFile(t, "x.ape", append(append([]byte{}, audio...), body...))

	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// After strip the tag is empty (zero items) but the footer
	// still exists. The audio prefix must still be there.
	if !bytes.HasPrefix(got, audio) {
		t.Errorf("audio body not preserved after Strip")
	}
}

// --- AAC -------------------------------------------------------

func TestOpen_AAC_BareADTS(t *testing.T) {
	// Untagged raw ADTS — should resolve as FormatAAC with
	// empty fields rather than failing.
	body := append([]byte{0xFF, 0xF1, 0x50, 0x80}, make([]byte, 32)...)
	p := writeFile(t, "bare.aac", body)
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatAAC {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "" {
		t.Errorf("Title = %q, want empty", got.Title())
	}
}

// --- Pictures exposed via tunetag.Open -------------------------

func TestOpen_OggPicture(t *testing.T) {
	// Build an Ogg file via the package's own test helpers, add a
	// cover via the public ogg API, then verify the top-level
	// Tag.Pictures() forwards it correctly.
	vc := &flac.VorbisComment{Vendor: "v", Comments: []string{"TITLE=Cover"}}
	cbody, _ := vc.Encode()
	commentPkt := append([]byte{0x03}, []byte("vorbis")...)
	commentPkt = append(commentPkt, cbody...)
	commentPkt = append(commentPkt, 0x01) // framing bit

	identPkt := append([]byte{0x01}, []byte("vorbis")...)
	identPkt = append(identPkt, make([]byte, 23)...)
	stream := makeOggPage(7, 0, 0x02, identPkt)
	stream = append(stream, makeOggPage(7, 1, 0, commentPkt)...)
	stream = append(stream, makeOggPage(7, 2, 0, []byte{0, 0, 0, 0, 0x42, 0x43})...)
	p := writeFile(t, "x.ogg", stream)

	o, err := OpenOgg(p)
	if err != nil {
		t.Fatal(err)
	}
	pic := &flac.Picture{
		PictureType: 3,
		MIME:        "image/jpeg",
		Description: "front",
		Data:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0xAB, 0xCD},
	}
	if err := o.AddPicture(pic); err != nil {
		t.Fatal(err)
	}
	if err := o.WriteFile(p); err != nil {
		t.Fatal(err)
	}

	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := tag.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures via Tag interface = %d, want 1", len(pics))
	}
	got := pics[0]
	if got.MIME != "image/jpeg" || got.Description != "front" || !bytes.Equal(got.Data, pic.Data) {
		t.Errorf("picture mismatch via Tag interface: %+v", got)
	}
}

func TestOpen_APEPicture(t *testing.T) {
	tag := &ape.Tag{HasHeader: true}
	_ = tag.Set("Title", "x")
	pic := &ape.Picture{Filename: "cover.jpg", Data: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x10, 0x20}}
	if err := tag.AddPicture(pic); err != nil {
		t.Fatal(err)
	}
	body, _ := tag.Encode()
	full := append([]byte("AUDIO_BODY"), body...)
	p := writeFile(t, "x.ape", full)

	common, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := common.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures via Tag interface = %d, want 1", len(pics))
	}
	got := pics[0]
	// MIME is sniffed from the bytes since APE doesn't carry it.
	if got.MIME != "image/jpeg" {
		t.Errorf("sniffed MIME = %q, want image/jpeg", got.MIME)
	}
	if got.Description != "cover.jpg" {
		t.Errorf("Description = %q, want %q (Filename)", got.Description, "cover.jpg")
	}
	if !bytes.Equal(got.Data, pic.Data) {
		t.Errorf("data mismatch")
	}
}

func TestSniffImageMIME(t *testing.T) {
	cases := []struct {
		body []byte
		want string
	}{
		{[]byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{[]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{[]byte("GIF89a..."), "image/gif"},
		{[]byte("GIF87a..."), "image/gif"},
		{[]byte{0x42, 0x4D, 0x10, 0x00}, "image/bmp"},
		{[]byte("not an image"), ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := SniffImageMIME(c.body); got != c.want {
			t.Errorf("SniffImageMIME(% X) = %q, want %q", c.body, got, c.want)
		}
	}
}

// --- ASF / WMA -------------------------------------------------

func TestOpen_ASF(t *testing.T) {
	// Build a minimal ASF file with a CDO and an ECDO. We rely
	// on the asf package's exported writers indirectly by
	// constructing a *asf.File and calling WriteFile.
	src := &asf.File{
		Title:  "WMA Title",
		Author: "WMA Author",
	}
	src.SetAlbum("Best of 2026")
	src.SetYear(2026)
	src.SetTrackNumber(4, 10)
	// Build a real path so WriteFile can be called.
	p := filepath.Join(t.TempDir(), "x.wma")
	// Initialise an empty file with just the magic Header
	// object + a Data Object, so asf.ReadFile can be used as
	// the foundation. Easiest: ask asf to round-trip a minimal
	// in-memory file by writing once.
	if err := os.WriteFile(p, buildEmptyASF(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Read what we just wrote, copy our state across, write.
	scaffold, err := asf.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	scaffold.Title = src.Title
	scaffold.Author = src.Author
	scaffold.Extended = append(scaffold.Extended, src.Extended...)
	if err := scaffold.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format() != FormatASF {
		t.Errorf("Format = %s", got.Format())
	}
	if got.Title() != "WMA Title" || got.Artist() != "WMA Author" {
		t.Errorf("Title=%q Artist=%q", got.Title(), got.Artist())
	}
	if got.Album() != "Best of 2026" || got.Year() != 2026 {
		t.Errorf("Album=%q Year=%d", got.Album(), got.Year())
	}
	if n, total := got.TrackNumber(); n != 4 || total != 10 {
		t.Errorf("Track = %d/%d", n, total)
	}
}

// buildEmptyASF builds the smallest valid ASF file: just the
// Header Object containing no children, followed by a Data
// Object containing one byte. Used as a scaffold by the test
// above.
func buildEmptyASF() []byte {
	// Header Object: GUID + size(8) + count(4) + reserved(2).
	const headerObjBodyLen = 4 + 2 // count + reserved
	hdrGUID := []byte{
		0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11,
		0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C,
	}
	dataGUID := []byte{
		0x36, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11,
		0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C,
	}
	var buf bytes.Buffer
	buf.Write(hdrGUID)
	sz := make([]byte, 8)
	binary.LittleEndian.PutUint64(sz, 24+headerObjBodyLen)
	buf.Write(sz)
	cnt := make([]byte, 4)
	binary.LittleEndian.PutUint32(cnt, 0)
	buf.Write(cnt)
	buf.WriteByte(0x01)
	buf.WriteByte(0x02)
	// Data Object with a single byte payload.
	buf.Write(dataGUID)
	binary.LittleEndian.PutUint64(sz, 24+1)
	buf.Write(sz)
	buf.WriteByte(0x00)
	return buf.Bytes()
}

func TestStrip_ASF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wma")
	if err := os.WriteFile(p, buildEmptyASF(), 0o644); err != nil {
		t.Fatal(err)
	}
	scaffold, err := asf.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	scaffold.Title = "to be stripped"
	scaffold.SetAlbum("also stripped")
	if err := scaffold.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	if err := Strip(p); err != nil {
		t.Fatal(err)
	}
	tag, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Title() != "" || tag.Album() != "" {
		t.Errorf("after Strip: Title=%q Album=%q", tag.Title(), tag.Album())
	}
}

func TestOpenASF_NonexistentPath(t *testing.T) {
	if _, err := OpenASF("/nonexistent/x.wma"); err == nil {
		t.Fatal("expected error opening missing ASF")
	}
}

func TestOpen_AAC_WithLeadingID3v2(t *testing.T) {
	tag := &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	tag.SetTitle("AAC w/ ID3")
	var buf bytes.Buffer
	_ = tag.Encode(&buf)
	buf.Write([]byte{0xFF, 0xF1, 0x50, 0x80})
	buf.Write(make([]byte, 32))
	p := writeFile(t, "x.aac", buf.Bytes())

	got, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	// Note: the ID3v2 prefix means Detect classifies this as
	// FormatID3v2, not FormatAAC. Both routes expose the same
	// title, so the user-visible behaviour is identical.
	if got.Title() != "AAC w/ ID3" {
		t.Errorf("Title = %q", got.Title())
	}
}

func TestOpen_FallsBackToID3v1(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 100))
	_ = (&id3v1.Tag{Title: "only v1", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_ = tag.Encode(&buf)
	buf.Write(make([]byte, 50))
	_ = (&id3v1.Tag{Title: "V1", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_ = tag.Encode(&buf)
	buf.Write(make([]byte, 50))
	_ = (&id3v1.Tag{Title: "V1 Title", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_ = (&id3v1.Tag{Title: "Only V1", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_ = (&id3v1.Tag{Title: "x", Genre: id3v1.GenreNone}).Encode(&buf)
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
	_ = tag.Encode(&buf)
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
	_ = (&id3v1.Tag{
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
	_ = tag.Encode(&buf)
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

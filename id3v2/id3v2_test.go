package id3v2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// buildV23Tag builds a minimal hand-crafted ID3v2.3 tag containing
// the given frames (id, body) for use as a test fixture.
func buildV23Tag(t *testing.T, padding int, flags Flags, frames []struct {
	ID   string
	Body []byte
}) []byte {
	t.Helper()
	var fbuf bytes.Buffer
	for _, f := range frames {
		if len(f.ID) != 4 {
			t.Fatalf("buildV23Tag: id %q must be 4 bytes", f.ID)
		}
		var hdr [10]byte
		copy(hdr[0:4], f.ID)
		binary.BigEndian.PutUint32(hdr[4:8], uint32(len(f.Body)))
		// flags zero
		fbuf.Write(hdr[:])
		fbuf.Write(f.Body)
	}
	if padding > 0 {
		fbuf.Write(make([]byte, padding))
	}
	h := Header{Version: V23, Flags: flags, Size: uint32(fbuf.Len())}
	var out bytes.Buffer
	if err := h.writeTo(&out); err != nil {
		t.Fatal(err)
	}
	out.Write(fbuf.Bytes())
	return out.Bytes()
}

func TestRead_NoTag(t *testing.T) {
	r := bytes.NewReader([]byte("ABCDEFGHIJ"))
	if _, err := Read(r); !errors.Is(err, ErrNoTag) {
		t.Fatalf("got %v, want ErrNoTag", err)
	}
}

func TestRead_V23OneFrame(t *testing.T) {
	raw := buildV23Tag(t, 0, 0, []struct {
		ID   string
		Body []byte
	}{
		{"TIT2", []byte{0x00, 'H', 'i'}}, // Latin-1 encoded "Hi"
	})
	tag, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if tag.Version != V23 {
		t.Errorf("Version = %s", tag.Version)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(tag.Frames))
	}
	tf, ok := tag.Frames[0].(*TextFrame)
	if !ok {
		t.Fatalf("frame is %T, want *TextFrame", tag.Frames[0])
	}
	if tf.FrameID != "TIT2" {
		t.Errorf("ID = %q", tf.FrameID)
	}
	if tf.Encoding != EncISO88591 {
		t.Errorf("Encoding = %s", tf.Encoding)
	}
	if len(tf.Text) != 1 || tf.Text[0] != "Hi" {
		t.Errorf("Text = %#v, want [\"Hi\"]", tf.Text)
	}
}

func TestRead_V23WithPadding(t *testing.T) {
	raw := buildV23Tag(t, 256, 0, []struct {
		ID   string
		Body []byte
	}{
		{"TIT2", []byte{0x00, 'X'}},
	})
	tag, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(tag.Frames))
	}
}

func TestRead_V22NormalisesFrameID(t *testing.T) {
	// Build a v2.2 tag with a single TT2 (= TIT2) frame.
	body := []byte{0x00, 'O', 'k'}
	var fbuf bytes.Buffer
	fbuf.WriteString("TT2")
	fbuf.Write([]byte{0x00, 0x00, byte(len(body))})
	fbuf.Write(body)

	h := Header{Version: V22, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	if err := h.writeTo(&raw); err != nil {
		t.Fatal(err)
	}
	raw.Write(fbuf.Bytes())

	tag, err := Read(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if tag.Version != V22 {
		t.Errorf("Version = %s", tag.Version)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d", len(tag.Frames))
	}
	if got := tag.Frames[0].ID(); got != "TIT2" {
		t.Errorf("ID = %q, want TIT2 (canonical form)", got)
	}
}

func TestRoundTrip_V23(t *testing.T) {
	in := &Tag{
		Version: V23,
		Padding: 0,
		Frames: []Frame{
			&TextFrame{FrameID: "TIT2", Encoding: EncISO88591, Text: []string{"AB"}},
			&TextFrame{FrameID: "TPE1", Encoding: EncISO88591, Text: []string{"X"}},
		},
	}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != V23 {
		t.Errorf("Version = %s", out.Version)
	}
	if len(out.Frames) != 2 {
		t.Fatalf("frames = %d", len(out.Frames))
	}
	for i, f := range in.Frames {
		want := f.(*TextFrame)
		got, ok := out.Frames[i].(*TextFrame)
		if !ok {
			t.Fatalf("frame %d: got %T, want *TextFrame", i, out.Frames[i])
		}
		if got.FrameID != want.FrameID || len(got.Text) != 1 || got.Text[0] != want.Text[0] {
			t.Errorf("frame %d:\n got %+v\nwant %+v", i, got, want)
		}
	}
}

func TestRoundTrip_V24(t *testing.T) {
	in := &Tag{
		Version: V24,
		Padding: 64,
		Frames: []Frame{
			&TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"XYZ"}},
		},
	}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != V24 {
		t.Errorf("Version = %s", out.Version)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d", len(out.Frames))
	}
	got, ok := out.Frames[0].(*TextFrame)
	if !ok {
		t.Fatalf("got %T, want *TextFrame", out.Frames[0])
	}
	if got.FrameID != "TIT2" || got.Encoding != EncUTF8 || len(got.Text) != 1 || got.Text[0] != "XYZ" {
		t.Errorf("got %+v", got)
	}
}

func TestEncode_FooterAppendsTrailer(t *testing.T) {
	tag := &Tag{Version: V24, Flags: FlagFooter}
	tag.SetTitle("Footer Test")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.Bytes()
	if len(out) < HeaderSize*2 {
		t.Fatalf("encoded too short for footer (%d bytes)", len(out))
	}
	footer := out[len(out)-HeaderSize:]
	if footer[0] != '3' || footer[1] != 'D' || footer[2] != 'I' {
		t.Errorf("footer magic = %q, want 3DI", footer[:3])
	}
	if footer[3] != byte(V24) {
		t.Errorf("footer version = %d, want 4", footer[3])
	}
	// Footer flags must mirror the header's, including FlagFooter
	// itself, so readers locating the tag from either end agree.
	if out[5] != footer[5] {
		t.Errorf("header flags 0x%02X != footer flags 0x%02X", out[5], footer[5])
	}
	if Flags(footer[5])&FlagFooter == 0 {
		t.Errorf("footer flags 0x%02X missing FlagFooter bit", footer[5])
	}
	headerSize := decodeSynchsafe(out[6:10])
	footerSize := decodeSynchsafe(footer[6:10])
	if headerSize != footerSize {
		t.Errorf("header size %d != footer size %d", headerSize, footerSize)
	}
}

func TestEncode_FooterDisablesPadding(t *testing.T) {
	tag := &Tag{Version: V24, Flags: FlagFooter, Padding: 1024}
	tag.SetTitle("X")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	frames, err := (&Tag{Version: V24, Frames: tag.Frames}).framesEncodedSize()
	if err != nil {
		t.Fatal(err)
	}
	want := HeaderSize + int(frames) + HeaderSize
	if buf.Len() != want {
		t.Errorf("encoded size = %d, want %d (header + frames + footer, no padding)", buf.Len(), want)
	}
}

func TestEncode_FooterRejectedOnV23(t *testing.T) {
	tag := &Tag{Version: V23, Flags: FlagFooter}
	if err := tag.Encode(&bytes.Buffer{}); err == nil {
		t.Fatal("expected error for footer flag on v2.3")
	}
}

func TestRoundTrip_V24WithFooter(t *testing.T) {
	in := &Tag{Version: V24, Flags: FlagFooter}
	in.SetTitle("Roundtrip with footer")
	in.SetArtist("Tester")
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "Roundtrip with footer" {
		t.Errorf("Title = %q", out.Title())
	}
	if out.Artist() != "Tester" {
		t.Errorf("Artist = %q", out.Artist())
	}
}

func TestRead_TagLevelUnsync(t *testing.T) {
	// Body containing a synthetic 0xFF 0xE0 pattern that the tag-level
	// unsync flag has been used to neutralise.
	original := []byte{
		'T', 'X', 'X', 'X', // frame ID
		0x00, 0x00, 0x00, 0x04, // size = 4
		0x00, 0x00, // flags
		0xFF, 0xE0, 0x00, 0x01, // body
	}
	unsynced := unsyncEncode(original)
	h := Header{Version: V23, Flags: FlagUnsync, Size: uint32(len(unsynced))}
	var raw bytes.Buffer
	if err := h.writeTo(&raw); err != nil {
		t.Fatal(err)
	}
	raw.Write(unsynced)

	tag, err := Read(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d", len(tag.Frames))
	}
	gf := tag.Frames[0].(*GenericFrame)
	want := []byte{0xFF, 0xE0, 0x00, 0x01}
	if !bytes.Equal(gf.Body, want) {
		t.Errorf("Body = % X, want % X", gf.Body, want)
	}
}

func TestRead_ExtendedHeaderSkipped_V23(t *testing.T) {
	// v2.3 ext header: 4-byte size (excluding itself) + N bytes.
	frameBody := []byte{0x00, 'X'}
	var f bytes.Buffer
	f.WriteString("TIT2")
	_ = binary.Write(&f, binary.BigEndian, uint32(len(frameBody)))
	f.Write([]byte{0, 0})
	f.Write(frameBody)

	extPayload := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	var body bytes.Buffer
	_ = binary.Write(&body, binary.BigEndian, uint32(len(extPayload)))
	body.Write(extPayload)
	body.Write(f.Bytes())

	h := Header{Version: V23, Flags: FlagExtended, Size: uint32(body.Len())}
	var raw bytes.Buffer
	if err := h.writeTo(&raw); err != nil {
		t.Fatal(err)
	}
	raw.Write(body.Bytes())

	tag, err := Read(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d", len(tag.Frames))
	}
	if tag.Frames[0].ID() != "TIT2" {
		t.Errorf("ID = %q", tag.Frames[0].ID())
	}
}

func TestEncodedSize_IncludesPadding(t *testing.T) {
	tag := &Tag{
		Version: V23,
		Padding: 100,
		Frames: []Frame{
			&GenericFrame{FrameID: "TIT2", Body: []byte{0x00, 'A'}},
		},
	}
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	// header(10) + frame_header(10) + body(2) + padding(100) = 122
	if buf.Len() != 122 {
		t.Errorf("encoded size = %d, want 122", buf.Len())
	}
}

// --- merged from edge_test.go ----------------------------------

func TestRead_EmptyTagPaddingOnly(t *testing.T) {
	// 1 KiB of padding, no frames. Must produce zero frames.
	h := Header{Version: V23, Size: 1024}
	var raw bytes.Buffer
	_ = h.writeTo(&raw)
	raw.Write(make([]byte, 1024))

	tag, err := Read(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 0 {
		t.Errorf("frames = %d, want 0", len(tag.Frames))
	}
}

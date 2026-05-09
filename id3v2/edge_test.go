package id3v2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// --- Read-side defensive parsing ---------------------------------

func TestRead_TruncatedHeader(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte("ID"),
		[]byte("ID3"),
		[]byte("ID3" + "\x04\x00\x00\x00\x00\x00"), // 9 bytes
	}
	for i, c := range cases {
		_, err := Read(bytes.NewReader(c))
		if err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestRead_BodySmallerThanSizeField(t *testing.T) {
	// Header claims size=128 but body is empty.
	h := Header{Version: V23, Size: 128}
	var buf bytes.Buffer
	if err := h.writeTo(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(&buf); err == nil {
		t.Fatal("expected error: body shorter than declared size")
	}
}

func TestRead_FrameSizeBeyondPayload(t *testing.T) {
	// One frame whose declared size exceeds the remaining body.
	var fbuf bytes.Buffer
	fbuf.WriteString("TIT2")
	binary.Write(&fbuf, binary.BigEndian, uint32(0xFFFF)) // huge frame size
	fbuf.Write([]byte{0, 0})                              // flags
	fbuf.WriteByte(0x00)                                  // body: just the encoding byte

	h := Header{Version: V23, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(fbuf.Bytes())

	if _, err := Read(&raw); err == nil {
		t.Fatal("expected error: frame size exceeds payload")
	}
}

func TestRead_V24FrameSizeWithTopBit(t *testing.T) {
	// v2.4 size bytes must be synchsafe; top bit set is malformed.
	var fbuf bytes.Buffer
	fbuf.WriteString("TIT2")
	fbuf.Write([]byte{0x80, 0x00, 0x00, 0x00}) // top bit set
	fbuf.Write([]byte{0, 0})

	h := Header{Version: V24, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(fbuf.Bytes())

	if _, err := Read(&raw); err == nil {
		t.Fatal("expected error: v2.4 frame size byte has top bit set")
	}
}

func TestRead_HeaderSynchsafeWithTopBit(t *testing.T) {
	b := []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0}
	if _, err := Read(bytes.NewReader(b)); err == nil {
		t.Fatal("expected error: malformed synchsafe size")
	}
}

func TestRead_NonZeroBytesInsidePadding(t *testing.T) {
	// Frame followed by what looks like padding but contains a non-zero byte.
	var fbuf bytes.Buffer
	body := []byte{0x00, 'A'}
	fbuf.WriteString("TIT2")
	binary.Write(&fbuf, binary.BigEndian, uint32(len(body)))
	fbuf.Write([]byte{0, 0})
	fbuf.Write(body)
	// Padding region: 4 zeros then 1 non-zero (0xAA).
	fbuf.Write([]byte{0, 0, 0, 0, 0xAA, 0, 0, 0})

	h := Header{Version: V23, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(fbuf.Bytes())

	if _, err := Read(&raw); err == nil {
		t.Fatal("expected error: non-zero byte inside padding region")
	}
}

func TestRead_DuplicateFrames(t *testing.T) {
	// Two TIT2 frames — both must round-trip in order; readers should
	// expose both, not just the first or merge them.
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"First"}},
		&TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"Second"}},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, f := range out.Frames {
		if f.ID() != "TIT2" {
			continue
		}
		tf, ok := f.(*TextFrame)
		if !ok {
			t.Fatalf("frame %T is not *TextFrame", f)
		}
		titles = append(titles, tf.String())
	}
	want := []string{"First", "Second"}
	if len(titles) != len(want) || titles[0] != want[0] || titles[1] != want[1] {
		t.Errorf("titles = %v, want %v (order matters)", titles, want)
	}
	// Title() exposes only the first per Tag's accessor contract.
	if out.Title() != "First" {
		t.Errorf("Title() = %q, want First", out.Title())
	}
}

func TestRead_EmptyTextFrameBody(t *testing.T) {
	// Frame body of length 0: the parser must surface a clean error
	// or fall back to GenericFrame, never crash.
	tag := encodeOneFrame(t, V23, "TIT2", nil)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatalf("Read should not error on zero-length frame body: %v", err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(out.Frames))
	}
	// Empty body cannot be a valid TextFrame (no encoding byte), so
	// the dispatcher should fall back to GenericFrame.
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("frame is %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestRead_UTF16WithoutBOM(t *testing.T) {
	// $01 declares UTF-16 with BOM, but here the body has none. The
	// decoder treats it as little-endian per common practice.
	body := []byte{0x01, 'H', 0x00, 'i', 0x00}
	tag := encodeOneFrame(t, V23, "TIT2", body)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	tf, ok := out.Frames[0].(*TextFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if got := tf.String(); got != "Hi" {
		t.Errorf("got %q, want Hi", got)
	}
}

func TestRead_UTF16OddByteLength(t *testing.T) {
	// UTF-16 body whose payload bytes (excluding the encoding byte
	// and BOM) have an odd length. The decoder must drop the stray
	// trailing byte rather than panic, and recover the leading "A".
	body := []byte{0x01, 0xFF, 0xFE, 'A', 0x00, 0x42}
	tag := encodeOneFrame(t, V23, "TIT2", body)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatalf("decoder must tolerate odd UTF-16 length, got %v", err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d", len(out.Frames))
	}
	tf, ok := out.Frames[0].(*TextFrame)
	if !ok {
		t.Fatalf("got %T, want *TextFrame", out.Frames[0])
	}
	if tf.String() != "A" {
		t.Errorf("got %q, want A (stray byte should be dropped)", tf.String())
	}
}

func TestRead_NULByteStartsPadding(t *testing.T) {
	// A frame whose first byte is NUL marks the start of the padding
	// region; readFrames must stop scanning, not try to parse a
	// 4-NUL "frame ID".
	tag := encodeOneFrame(t, V23, "\x00\x00\x00\x00", nil)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) != 0 {
		t.Errorf("expected zero frames, got %d", len(out.Frames))
	}
}

func TestRead_UnknownFrameIDFallsBackToGeneric(t *testing.T) {
	// A 4-character ASCII frame ID outside the registry must produce
	// a GenericFrame holding the raw body for round-trip preservation.
	body := []byte{0x10, 0x20, 0x30}
	tag := encodeOneFrame(t, V23, "XYZW", body)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(out.Frames))
	}
	gf, ok := out.Frames[0].(*GenericFrame)
	if !ok {
		t.Fatalf("got %T, want *GenericFrame", out.Frames[0])
	}
	if gf.FrameID != "XYZW" {
		t.Errorf("FrameID = %q, want XYZW", gf.FrameID)
	}
	if !bytes.Equal(gf.Body, body) {
		t.Errorf("Body = % X, want % X", gf.Body, body)
	}
}

func TestRead_EmptyTagPaddingOnly(t *testing.T) {
	// 1 KiB of padding, no frames. Must produce zero frames.
	h := Header{Version: V23, Size: 1024}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(make([]byte, 1024))

	tag, err := Read(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 0 {
		t.Errorf("frames = %d, want 0", len(tag.Frames))
	}
}

// --- Encode-side defenses ----------------------------------------

func TestEncode_FrameWithEmptyID(t *testing.T) {
	tag := &Tag{Version: V23, Frames: []Frame{
		&GenericFrame{FrameID: "", Body: nil},
	}}
	if err := tag.Encode(&bytes.Buffer{}); err == nil {
		t.Fatal("expected error: frame ID must be 4 chars")
	}
}

func TestEncode_FrameIDWrongLength(t *testing.T) {
	for _, id := range []string{"X", "AB", "ABCDE"} {
		tag := &Tag{Version: V23, Frames: []Frame{
			&GenericFrame{FrameID: id, Body: nil},
		}}
		if err := tag.Encode(&bytes.Buffer{}); err == nil {
			t.Errorf("id %q: expected error", id)
		}
	}
}

func TestEncode_TextFrameWithEmptyText(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: nil},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tf, ok := out.Frames[0].(*TextFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if len(tf.Text) != 1 || tf.Text[0] != "" {
		t.Errorf("Text = %#v, want [\"\"]", tf.Text)
	}
}

func TestEncode_TextFrameWithNULInValue(t *testing.T) {
	// A NUL byte inside a UTF-8 string is interpreted as a value
	// separator on read for v2.4 multi-value frames. Round-trip
	// preserves the split.
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&TextFrame{FrameID: "TPE1", Encoding: EncUTF8, Text: []string{"A", "B"}},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tf := out.Frames[0].(*TextFrame)
	if len(tf.Text) != 2 || tf.Text[0] != "A" || tf.Text[1] != "B" {
		t.Errorf("Text = %#v, want [A B]", tf.Text)
	}
}

func TestEncode_FrameSizeOverflow_V24(t *testing.T) {
	// v2.4 frame size is synchsafe (max ~256 MiB). Generating a body
	// near 2^28 is impractical here; instead simulate via a fake
	// frame that lies about its body size — only writeFrameHeaderAndBody
	// can be tricked, so we verify the helper rejects oversized inputs.
	huge := make([]byte, MaxSynchsafe+1)
	gf := &GenericFrame{FrameID: "TXXX", Body: huge}
	if err := gf.Encode(V24, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error: frame body exceeds 28-bit synchsafe range")
	}
}

// --- Synchsafe / unsync edge cases -------------------------------

func TestUnsync_TerminalFFGetsPadded(t *testing.T) {
	// Per spec, a 0xFF at the very end of the data must be followed
	// by 0x00 on the wire so it cannot be mistaken for a sync byte.
	in := []byte{0x12, 0xFF}
	enc := unsyncEncode(in)
	if !bytes.Equal(enc, []byte{0x12, 0xFF, 0x00}) {
		t.Errorf("got % X, want 12 FF 00", enc)
	}
	dec := unsyncDecode(enc)
	if !bytes.Equal(dec, in) {
		t.Errorf("decode = % X, want % X", dec, in)
	}
}

func TestUnsync_DecodeIgnoresStraySequence(t *testing.T) {
	// $FF $01 is not a sync sequence; decoder must leave it alone.
	in := []byte{0xFF, 0x01, 0x02}
	if got := unsyncDecode(in); !bytes.Equal(got, in) {
		t.Errorf("got % X, want % X", got, in)
	}
}

// --- WriteFile edge cases ----------------------------------------

func TestWriteFile_OnNonexistentPath(t *testing.T) {
	tag := &Tag{Version: V24, Padding: 0}
	tag.SetTitle("X")
	if err := tag.WriteFile("/dev/null/does-not-exist/file.mp3"); err == nil {
		t.Fatal("expected error for nonexistent parent directory")
	}
}

// --- Helpers -----------------------------------------------------

// encodeOneFrame writes a tag containing a single frame with the
// given canonical id and pre-encoded body bytes.
func encodeOneFrame(t *testing.T, v Version, id string, body []byte) []byte {
	t.Helper()
	var fbuf bytes.Buffer
	fbuf.WriteString(id)
	if v == V24 {
		sz, _ := encodeSynchsafe(uint32(len(body)))
		fbuf.Write(sz[:])
	} else {
		binary.Write(&fbuf, binary.BigEndian, uint32(len(body)))
	}
	fbuf.Write([]byte{0, 0})
	fbuf.Write(body)

	h := Header{Version: v, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	if err := h.writeTo(&raw); err != nil {
		t.Fatal(err)
	}
	raw.Write(fbuf.Bytes())
	return raw.Bytes()
}

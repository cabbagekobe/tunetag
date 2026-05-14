package id3v2

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// roundTripFrame encodes f at version v, parses the resulting body
// back through the dispatcher, and returns the decoded Frame.
func roundTripFrame(t *testing.T, v Version, f Frame) Frame {
	t.Helper()
	tag := &Tag{Version: v, Padding: 0, Frames: []Frame{f}}
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d", len(out.Frames))
	}
	return out.Frames[0]
}

func TestTextFrame_LatinASCII_V23(t *testing.T) {
	in := &TextFrame{FrameID: "TIT2", Encoding: EncISO88591, Text: []string{"Hello"}}
	got := roundTripFrame(t, V23, in).(*TextFrame)
	if got.FrameID != "TIT2" || got.Encoding != EncISO88591 ||
		!reflect.DeepEqual(got.Text, []string{"Hello"}) {
		t.Errorf("got %+v", got)
	}
}

func TestTextFrame_CJK_V23_UpgradesToUTF16(t *testing.T) {
	// Asked for Latin-1 but contains Japanese; encoder must upgrade.
	in := &TextFrame{FrameID: "TIT2", Encoding: EncISO88591, Text: []string{"日本語"}}
	got := roundTripFrame(t, V23, in).(*TextFrame)
	if got.Encoding != EncUTF16 {
		t.Errorf("Encoding = %s, want UTF-16", got.Encoding)
	}
	if !reflect.DeepEqual(got.Text, []string{"日本語"}) {
		t.Errorf("Text = %#v", got.Text)
	}
}

func TestTextFrame_UTF8_V24(t *testing.T) {
	in := &TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"日本語"}}
	got := roundTripFrame(t, V24, in).(*TextFrame)
	if got.Encoding != EncUTF8 || !reflect.DeepEqual(got.Text, []string{"日本語"}) {
		t.Errorf("got %+v", got)
	}
}

func TestTextFrame_UTF16BE_V24(t *testing.T) {
	in := &TextFrame{FrameID: "TIT2", Encoding: EncUTF16BE, Text: []string{"Aé日"}}
	got := roundTripFrame(t, V24, in).(*TextFrame)
	if got.Encoding != EncUTF16BE || !reflect.DeepEqual(got.Text, []string{"Aé日"}) {
		t.Errorf("got %+v", got)
	}
}

func TestTextFrame_UTF8_NotValidInV23_DowngradesToUTF16(t *testing.T) {
	in := &TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"日本語"}}
	got := roundTripFrame(t, V23, in).(*TextFrame)
	if got.Encoding != EncUTF16 {
		t.Errorf("Encoding = %s, want UTF-16", got.Encoding)
	}
	if !reflect.DeepEqual(got.Text, []string{"日本語"}) {
		t.Errorf("Text = %#v", got.Text)
	}
}

func TestTextFrame_MultiValue_V24(t *testing.T) {
	in := &TextFrame{FrameID: "TPE1", Encoding: EncUTF8, Text: []string{"Alice", "Bob", "Charlie"}}
	got := roundTripFrame(t, V24, in).(*TextFrame)
	if !reflect.DeepEqual(got.Text, []string{"Alice", "Bob", "Charlie"}) {
		t.Errorf("Text = %#v", got.Text)
	}
}

func TestUserTextFrame_RoundTrip(t *testing.T) {
	in := &UserTextFrame{Encoding: EncUTF8, Description: "MOOD", Value: "Calm"}
	got := roundTripFrame(t, V24, in).(*UserTextFrame)
	if got.Description != "MOOD" || got.Value != "Calm" {
		t.Errorf("got %+v", got)
	}
}

func TestUserTextFrame_CJK_V23(t *testing.T) {
	in := &UserTextFrame{Encoding: EncISO88591, Description: "気分", Value: "穏やか"}
	got := roundTripFrame(t, V23, in).(*UserTextFrame)
	if got.Encoding != EncUTF16 {
		t.Errorf("Encoding = %s, want UTF-16", got.Encoding)
	}
	if got.Description != "気分" || got.Value != "穏やか" {
		t.Errorf("got %+v", got)
	}
}

func TestURLFrame_RoundTrip(t *testing.T) {
	in := &URLFrame{FrameID: "WOAF", URL: "http://example.com/song"}
	got := roundTripFrame(t, V23, in).(*URLFrame)
	if got.FrameID != "WOAF" || got.URL != "http://example.com/song" {
		t.Errorf("got %+v", got)
	}
}

func TestUserURLFrame_RoundTrip(t *testing.T) {
	in := &UserURLFrame{Encoding: EncUTF8, Description: "homepage", URL: "https://example.com/"}
	got := roundTripFrame(t, V24, in).(*UserURLFrame)
	if got.Description != "homepage" || got.URL != "https://example.com/" {
		t.Errorf("got %+v", got)
	}
}

func TestCommentFrame_RoundTrip(t *testing.T) {
	in := &CommentFrame{Encoding: EncUTF8, Language: "eng", Description: "", Text: "Sample comment"}
	got := roundTripFrame(t, V24, in).(*CommentFrame)
	if got.Language != "eng" || got.Description != "" || got.Text != "Sample comment" {
		t.Errorf("got %+v", got)
	}
}

func TestCommentFrame_LangMissing_DefaultsXXX(t *testing.T) {
	in := &CommentFrame{Encoding: EncUTF8, Language: "", Text: "x"}
	got := roundTripFrame(t, V24, in).(*CommentFrame)
	if got.Language != "XXX" {
		t.Errorf("Language = %q, want XXX", got.Language)
	}
}

func TestUSLT_RoundTrip(t *testing.T) {
	in := &UnsynchronisedLyricsFrame{Encoding: EncUTF8, Language: "jpn", Description: "verse1", Text: "歌詞テキスト"}
	got := roundTripFrame(t, V24, in).(*UnsynchronisedLyricsFrame)
	if got.Language != "jpn" || got.Description != "verse1" || got.Text != "歌詞テキスト" {
		t.Errorf("got %+v", got)
	}
}

func TestPictureFrame_RoundTrip(t *testing.T) {
	in := &PictureFrame{
		Encoding:    EncUTF8,
		MIME:        "image/jpeg",
		PictureType: 3, // CoverFront
		Description: "front cover",
		Data:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00},
	}
	got := roundTripFrame(t, V24, in).(*PictureFrame)
	if got.MIME != in.MIME || got.PictureType != in.PictureType ||
		got.Description != in.Description || !bytes.Equal(got.Data, in.Data) {
		t.Errorf("got %+v\nwant %+v", got, in)
	}
}

func TestPictureFrame_V22PIC_Read(t *testing.T) {
	// Hand-craft a v2.2 tag containing a PIC frame.
	// PIC body: enc(1) + format(3 chars: "JPG") + type(1) + desc(NUL) + data
	picData := []byte{0xFF, 0xD8, 'D', 'A', 'T', 'A'}
	body := append([]byte{
		0x00,                // encoding = Latin-1
		'J', 'P', 'G',       // image format
		0x03,                // picture type = CoverFront
		'C', 'o', 'v', 'r',  // description
		0x00,                // null terminator
	}, picData...)

	var fbuf bytes.Buffer
	fbuf.WriteString("PIC")
	fbuf.WriteByte(byte(len(body) >> 16))
	fbuf.WriteByte(byte(len(body) >> 8))
	fbuf.WriteByte(byte(len(body)))
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
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d", len(tag.Frames))
	}
	pf, ok := tag.Frames[0].(*PictureFrame)
	if !ok {
		t.Fatalf("frame is %T, want *PictureFrame", tag.Frames[0])
	}
	if pf.MIME != "image/jpeg" {
		t.Errorf("MIME = %q, want image/jpeg", pf.MIME)
	}
	if pf.PictureType != 3 {
		t.Errorf("PictureType = %d, want 3", pf.PictureType)
	}
	if pf.Description != "Covr" {
		t.Errorf("Description = %q", pf.Description)
	}
	if !bytes.Equal(pf.Data, picData) {
		t.Errorf("Data = % X, want % X", pf.Data, picData)
	}
}

func TestUFIDFrame_RoundTrip(t *testing.T) {
	in := &UFIDFrame{Owner: "https://musicbrainz.org", Identifier: []byte("abcdef-1234-5678")}
	got := roundTripFrame(t, V24, in).(*UFIDFrame)
	if got.Owner != in.Owner || !bytes.Equal(got.Identifier, in.Identifier) {
		t.Errorf("got %+v\nwant %+v", got, in)
	}
}

func TestPrivFrame_RoundTrip(t *testing.T) {
	in := &PrivFrame{Owner: "WM/ContentID", Data: []byte{0x01, 0x02, 0x03, 0xFF, 0x00}}
	got := roundTripFrame(t, V24, in).(*PrivFrame)
	if got.Owner != in.Owner || !bytes.Equal(got.Data, in.Data) {
		t.Errorf("got %+v\nwant %+v", got, in)
	}
}

func TestDispatcher_FallsBackToGenericOnInvalidEncoding(t *testing.T) {
	// TIT2 with a bogus encoding byte 0xFF should round-trip via
	// GenericFrame so the original bytes are preserved.
	raw := buildV23Tag(t, 0, 0, []struct {
		ID   string
		Body []byte
	}{
		{"TIT2", []byte{0xFF, 0x10, 0x20, 0x30}},
	})
	tag, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(tag.Frames) != 1 {
		t.Fatalf("frames = %d", len(tag.Frames))
	}
	gf, ok := tag.Frames[0].(*GenericFrame)
	if !ok {
		t.Fatalf("got %T, want *GenericFrame fallback", tag.Frames[0])
	}
	if gf.FrameID != "TIT2" {
		t.Errorf("FrameID = %q", gf.FrameID)
	}
	if !bytes.Equal(gf.Body, []byte{0xFF, 0x10, 0x20, 0x30}) {
		t.Errorf("Body = % X", gf.Body)
	}
}

// --- COMM / USLT --------------------------------------------------

func TestCOMM_ShortLanguageDefaultsToXXX(t *testing.T) {
	// Language with length != 3 should be replaced with "XXX" on encode.
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&CommentFrame{Language: "JP", Description: "d", Text: "v"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	c := out.Frames[0].(*CommentFrame)
	if c.Language != "XXX" {
		t.Errorf("Language = %q, want XXX", c.Language)
	}
}

func TestCOMM_RoundTrip(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&CommentFrame{Language: "eng", Description: "mood", Text: "happy"},
		&CommentFrame{Language: "jpn", Description: "気分", Text: "嬉しい"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var got []*CommentFrame
	for _, f := range out.Frames {
		if c, ok := f.(*CommentFrame); ok {
			got = append(got, c)
		}
	}
	if len(got) != 2 {
		t.Fatalf("comments = %d, want 2", len(got))
	}
	if got[0].Text != "happy" || got[1].Text != "嬉しい" {
		t.Errorf("texts = %v", got)
	}
	if got[1].Description != "気分" {
		t.Errorf("desc[1] = %q", got[1].Description)
	}
}

func TestCOMM_ShortBodyRejected(t *testing.T) {
	// COMM with body shorter than 4 bytes (enc + 3-byte lang) must
	// not crash; the parser should fall back to GenericFrame.
	tag := encodeOneFrame(t, V24, "COMM", []byte{0x03})
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(out.Frames))
	}
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("got %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestUSLT_BasicRoundTrip(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&UnsynchronisedLyricsFrame{Language: "eng", Description: "verse 1", Text: "lyrics line"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := out.Frames[0].(*UnsynchronisedLyricsFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if u.Text != "lyrics line" || u.Description != "verse 1" {
		t.Errorf("USLT = %+v", u)
	}
}

// --- UFID / PRIV --------------------------------------------------

func TestUFID_RejectsMissingOwner(t *testing.T) {
	// Body that starts with 0x00 yields an empty owner; parser must
	// surface that as an error so dispatchFrame falls back to generic.
	tag := encodeOneFrame(t, V24, "UFID", []byte{0x00, 'i', 'd'})
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("got %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestUFID_RoundTrip(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&UFIDFrame{Owner: "http://example.com", Identifier: []byte{0x01, 0x02, 0xFF}},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	f := out.Frames[0].(*UFIDFrame)
	if f.Owner != "http://example.com" {
		t.Errorf("Owner = %q", f.Owner)
	}
	if !bytes.Equal(f.Identifier, []byte{0x01, 0x02, 0xFF}) {
		t.Errorf("Identifier = % X", f.Identifier)
	}
}

func TestPRIV_RoundTripEmptyOwner(t *testing.T) {
	// PRIV permits empty owner (rare but valid).
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&PrivFrame{Owner: "", Data: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := out.Frames[0].(*PrivFrame)
	if !ok {
		t.Fatalf("got %T", out.Frames[0])
	}
	if p.Owner != "" {
		t.Errorf("Owner = %q, want empty", p.Owner)
	}
	if !bytes.Equal(p.Data, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("Data = % X", p.Data)
	}
}

// --- APIC ---------------------------------------------------------

func TestAPIC_MissingPictureType(t *testing.T) {
	// Body: enc + Latin-1 MIME + 0x00 terminator (then no further bytes
	// for picture type). Must fall back to GenericFrame.
	body := []byte{0x03}
	body = append(body, "image/jpeg"...)
	body = append(body, 0x00)
	tag := encodeOneFrame(t, V24, "APIC", body)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("got %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestAPIC_DefaultMIMEWhenEmpty(t *testing.T) {
	// Empty MIME on encode should default to image/jpeg.
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&PictureFrame{Encoding: EncUTF8, MIME: "", PictureType: 3, Description: "cover", Data: []byte{0x01}},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	p := out.Frames[0].(*PictureFrame)
	if p.MIME != "image/jpeg" {
		t.Errorf("MIME = %q, want image/jpeg", p.MIME)
	}
}

func TestAPIC_BinaryDataPreserved(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&PictureFrame{Encoding: EncUTF8, MIME: "image/png", PictureType: 3, Description: "", Data: data},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	p := out.Frames[0].(*PictureFrame)
	if !bytes.Equal(p.Data, data) {
		t.Errorf("Data mismatch")
	}
}

func TestMimeToV22Format_Coverage(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": "JPG",
		"image/JPEG": "JPG",
		"image/jpg":  "JPG",
		"image/png":  "PNG",
		"image/gif":  "GIF",
		"image/bmp":  "BMP",
		"image/tiff": "TIF",
	}
	for mime, want := range cases {
		got, err := mimeToV22Format(mime)
		if err != nil {
			t.Errorf("%s: %v", mime, err)
			continue
		}
		if got != want {
			t.Errorf("%s -> %q, want %q", mime, got, want)
		}
	}
	if _, err := mimeToV22Format("image/webp"); err == nil {
		t.Errorf("webp should error: no v2.2 representation")
	}
}

func TestTranslateV22PIC_UnknownFormat(t *testing.T) {
	// 3-char format "XYZ" — not in the known set. translateV22PIC should
	// generate a MIME like "image/xyz" instead of failing.
	body := []byte{0x00, 'X', 'Y', 'Z', 0x03, 0x01, 0x02}
	got, err := translateV22PIC(body)
	if err != nil {
		t.Fatal(err)
	}
	// Layout: enc | mime | 0x00 | pictype | rest...
	if got[0] != 0x00 {
		t.Errorf("encoding byte = % X", got[0])
	}
	want := "image/xyz"
	if string(got[1:1+len(want)]) != want {
		t.Errorf("mime = %q, want %q", got[1:1+len(want)], want)
	}
}

func TestTranslateV22PIC_TooShort(t *testing.T) {
	if _, err := translateV22PIC([]byte{0x00, 'J', 'P'}); err == nil {
		t.Fatal("expected error: PIC body < 5 bytes")
	}
}

// --- TXXX / WXXX --------------------------------------------------

func TestTXXX_RoundTrip(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&UserTextFrame{Encoding: EncUTF8, Description: "MUSICBRAINZ_ID", Value: "abc-123"},
		&UserTextFrame{Encoding: EncUTF8, Description: "", Value: ""},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var got []*UserTextFrame
	for _, f := range out.Frames {
		if u, ok := f.(*UserTextFrame); ok {
			got = append(got, u)
		}
	}
	if len(got) != 2 {
		t.Fatalf("TXXX count = %d", len(got))
	}
	if got[0].Description != "MUSICBRAINZ_ID" || got[0].Value != "abc-123" {
		t.Errorf("got[0] = %+v", got[0])
	}
}

func TestTXXX_EmptyBodyFallsBackToGeneric(t *testing.T) {
	tag := encodeOneFrame(t, V24, "TXXX", nil)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("got %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestWXXX_RoundTrip(t *testing.T) {
	in := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&UserURLFrame{Encoding: EncUTF8, Description: "homepage", URL: "https://example.com"},
	}}
	var buf bytes.Buffer
	if err := in.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w := out.Frames[0].(*UserURLFrame)
	if w.URL != "https://example.com" || w.Description != "homepage" {
		t.Errorf("WXXX = %+v", w)
	}
}

func TestWFrame_NullTerminatedURLDropped(t *testing.T) {
	// URL Latin-1 body with trailing 0x00 + garbage. Reader keeps only
	// the part up to the first null.
	body := append([]byte("https://x"), 0x00, 'J', 'U', 'N', 'K')
	tag := encodeOneFrame(t, V24, "WOAR", body)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatal(err)
	}
	u := out.Frames[0].(*URLFrame)
	if u.URL != "https://x" {
		t.Errorf("URL = %q, want https://x", u.URL)
	}
}

// --- chooseEncoding -----------------------------------------------

func TestChooseEncoding_HonorsValidPreference(t *testing.T) {
	if got := chooseEncoding(V24, EncUTF16BE, "abc"); got != EncUTF16BE {
		t.Errorf("V24 prefer UTF-16BE: got %s", got)
	}
}

func TestChooseEncoding_FallsBackWhenInvalid(t *testing.T) {
	// UTF-8 not valid for V23; choose function must fall back to UTF-16
	// when the string contains non-ASCII.
	if got := chooseEncoding(V23, EncUTF8, "日"); got != EncUTF16 {
		t.Errorf("V23 fallback: got %s, want UTF-16", got)
	}
	if got := chooseEncoding(V23, EncUTF8, "ASCII"); got != EncISO88591 {
		t.Errorf("V23 ASCII fallback: got %s, want ISO-8859-1", got)
	}
}

func TestChooseEncoding_LatinPreferenceRejectedForCJK(t *testing.T) {
	// Latin-1 cannot carry CJK; choose function must upgrade.
	if got := chooseEncoding(V23, EncISO88591, "日"); got != EncUTF16 {
		t.Errorf("got %s, want UTF-16", got)
	}
}

func TestChooseEncoding_V24Always(t *testing.T) {
	// Any non-V24-valid preference on V24 collapses to UTF-8.
	if got := chooseEncoding(V24, Encoding(99), "abc"); got != EncUTF8 {
		t.Errorf("got %s, want UTF-8", got)
	}
}

// --- merged from edge_test.go: frame-level robustness ----------

func TestRead_FrameSizeBeyondPayload(t *testing.T) {
	var fbuf bytes.Buffer
	fbuf.WriteString("TIT2")
	binary.Write(&fbuf, binary.BigEndian, uint32(0xFFFF))
	fbuf.Write([]byte{0, 0})
	fbuf.WriteByte(0x00)

	h := Header{Version: V23, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(fbuf.Bytes())

	if _, err := Read(&raw); err == nil {
		t.Fatal("expected error: frame size exceeds payload")
	}
}

func TestRead_V24FrameSizeWithTopBit(t *testing.T) {
	var fbuf bytes.Buffer
	fbuf.WriteString("TIT2")
	fbuf.Write([]byte{0x80, 0x00, 0x00, 0x00})
	fbuf.Write([]byte{0, 0})

	h := Header{Version: V24, Size: uint32(fbuf.Len())}
	var raw bytes.Buffer
	h.writeTo(&raw)
	raw.Write(fbuf.Bytes())

	if _, err := Read(&raw); err == nil {
		t.Fatal("expected error: v2.4 frame size byte has top bit set")
	}
}

func TestRead_NonZeroBytesInsidePadding(t *testing.T) {
	var fbuf bytes.Buffer
	body := []byte{0x00, 'A'}
	fbuf.WriteString("TIT2")
	binary.Write(&fbuf, binary.BigEndian, uint32(len(body)))
	fbuf.Write([]byte{0, 0})
	fbuf.Write(body)
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
	if out.Title() != "First" {
		t.Errorf("Title() = %q, want First", out.Title())
	}
}

func TestRead_EmptyTextFrameBody(t *testing.T) {
	tag := encodeOneFrame(t, V23, "TIT2", nil)
	out, err := Read(bytes.NewReader(tag))
	if err != nil {
		t.Fatalf("Read should not error on zero-length frame body: %v", err)
	}
	if len(out.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(out.Frames))
	}
	if _, ok := out.Frames[0].(*GenericFrame); !ok {
		t.Errorf("frame is %T, want *GenericFrame fallback", out.Frames[0])
	}
}

func TestRead_UTF16WithoutBOM(t *testing.T) {
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
	huge := make([]byte, MaxSynchsafe+1)
	gf := &GenericFrame{FrameID: "TXXX", Body: huge}
	if err := gf.Encode(V24, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error: frame body exceeds 28-bit synchsafe range")
	}
}

// encodeOneFrame writes a tag containing a single frame with the
// given canonical id and pre-encoded body bytes. Used by edge tests.
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

package id3v2

import (
	"bytes"
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

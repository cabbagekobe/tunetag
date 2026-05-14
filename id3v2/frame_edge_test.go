package id3v2

import (
	"bytes"
	"testing"
)

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

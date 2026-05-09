package id3v2

import (
	"bytes"
	"testing"
)

func TestEncodeDecode_Latin1(t *testing.T) {
	enc, err := encodeString(EncISO88591, "Hello", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{'H', 'e', 'l', 'l', 'o', 0x00}
	if !bytes.Equal(enc, want) {
		t.Errorf("got % X, want % X", enc, want)
	}
	s, rest, err := readNextString(EncISO88591, enc)
	if err != nil {
		t.Fatal(err)
	}
	if s != "Hello" {
		t.Errorf("decoded %q", s)
	}
	if len(rest) != 0 {
		t.Errorf("rest = % X", rest)
	}
}

func TestEncode_Latin1_RejectsNonLatin1(t *testing.T) {
	if _, err := encodeString(EncISO88591, "日本語", false); err != ErrCannotEncodeLatin1 {
		t.Fatalf("got %v, want ErrCannotEncodeLatin1", err)
	}
}

func TestEncodeDecode_UTF8(t *testing.T) {
	enc, err := encodeString(EncUTF8, "日本語", true)
	if err != nil {
		t.Fatal(err)
	}
	if enc[len(enc)-1] != 0 {
		t.Errorf("missing terminator")
	}
	s, _, err := readNextString(EncUTF8, enc)
	if err != nil {
		t.Fatal(err)
	}
	if s != "日本語" {
		t.Errorf("got %q", s)
	}
}

func TestEncodeDecode_UTF16_BOM(t *testing.T) {
	enc, err := encodeString(EncUTF16, "Aé日本", true)
	if err != nil {
		t.Fatal(err)
	}
	// Must start with LE BOM.
	if enc[0] != 0xFF || enc[1] != 0xFE {
		t.Errorf("missing LE BOM: % X", enc[:2])
	}
	// Must end with two-byte terminator.
	n := len(enc)
	if enc[n-1] != 0 || enc[n-2] != 0 {
		t.Errorf("missing UTF-16 terminator: % X", enc[n-2:])
	}
	s, _, err := readNextString(EncUTF16, enc)
	if err != nil {
		t.Fatal(err)
	}
	if s != "Aé日本" {
		t.Errorf("got %q", s)
	}
}

func TestEncodeDecode_UTF16BE(t *testing.T) {
	enc, err := encodeString(EncUTF16BE, "X日Y", true)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := readNextString(EncUTF16BE, enc)
	if err != nil {
		t.Fatal(err)
	}
	if s != "X日Y" {
		t.Errorf("got %q", s)
	}
}

func TestReadNextString_Multiple(t *testing.T) {
	// Concatenate three null-terminated UTF-8 strings.
	a, _ := encodeString(EncUTF8, "alpha", true)
	b, _ := encodeString(EncUTF8, "β", true)
	c, _ := encodeString(EncUTF8, "γ", false)
	body := append(append(a, b...), c...)

	s1, rest, err := readNextString(EncUTF8, body)
	if err != nil || s1 != "alpha" {
		t.Fatalf("first %q err=%v", s1, err)
	}
	s2, rest, err := readNextString(EncUTF8, rest)
	if err != nil || s2 != "β" {
		t.Fatalf("second %q err=%v", s2, err)
	}
	s3, rest, err := readNextString(EncUTF8, rest)
	if err != nil || s3 != "γ" {
		t.Fatalf("third %q err=%v", s3, err)
	}
	if len(rest) != 0 {
		t.Errorf("rest = % X", rest)
	}
}

func TestPickEncodingForText(t *testing.T) {
	if got := pickEncodingForText(V23, "ASCII only"); got != EncISO88591 {
		t.Errorf("v2.3 ASCII -> %s, want ISO-8859-1", got)
	}
	if got := pickEncodingForText(V23, "日本語"); got != EncUTF16 {
		t.Errorf("v2.3 CJK -> %s, want UTF-16", got)
	}
	if got := pickEncodingForText(V24, "anything"); got != EncUTF8 {
		t.Errorf("v2.4 -> %s, want UTF-8", got)
	}
}

func TestValidForVersion(t *testing.T) {
	cases := []struct {
		e    Encoding
		v    Version
		want bool
	}{
		{EncISO88591, V23, true},
		{EncUTF16, V23, true},
		{EncUTF16BE, V23, false},
		{EncUTF8, V23, false},
		{EncUTF16BE, V24, true},
		{EncUTF8, V24, true},
	}
	for _, c := range cases {
		if got := c.e.validForVersion(c.v); got != c.want {
			t.Errorf("%s on %s: got %v, want %v", c.e, c.v, got, c.want)
		}
	}
}

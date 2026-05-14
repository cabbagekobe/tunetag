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

// --- decodeBytes corner cases ------------------------------------

func TestDecodeBytes_UTF16BigEndianBOM(t *testing.T) {
	// $FE $FF marks BE; "Hi" => 0x0048 0x0069.
	in := []byte{0xFE, 0xFF, 0x00, 'H', 0x00, 'i'}
	got, err := decodeBytes(EncUTF16, in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hi" {
		t.Errorf("got %q, want Hi", got)
	}
}

func TestDecodeBytes_UTF16LEEmpty(t *testing.T) {
	// Two-byte BOM only — no content.
	in := []byte{0xFF, 0xFE}
	got, err := decodeBytes(EncUTF16, in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestDecodeBytes_UTF16TooShort(t *testing.T) {
	// A single-byte UTF-16 input is degenerate; the decoder must
	// not panic and should return an empty string.
	got, err := decodeBytes(EncUTF16, []byte{0xFF})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestDecodeBytes_UTF8InvalidRejected(t *testing.T) {
	bad := []byte{0xC0, 0xC0}
	if _, err := decodeBytes(EncUTF8, bad); err == nil {
		t.Fatal("expected error on invalid UTF-8")
	}
}

func TestDecodeBytes_Latin1HighBytes(t *testing.T) {
	// Each byte in 0..255 must round-trip via Latin-1 -> rune -> string.
	in := []byte{0x41, 0xC9, 0xFF, 0x00, 0x7F}
	got, err := decodeBytes(EncISO88591, in)
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune(got)
	if len(runes) != len(in) {
		t.Fatalf("rune count = %d, want %d", len(runes), len(in))
	}
	for i, r := range runes {
		if r != rune(in[i]) {
			t.Errorf("rune %d = U+%04X, want U+%04X", i, r, in[i])
		}
	}
}

func TestDecodeBytes_UnknownEncoding(t *testing.T) {
	if _, err := decodeBytes(Encoding(99), []byte("x")); err == nil {
		t.Fatal("expected error for unknown encoding")
	}
}

// --- readNextString corner cases ---------------------------------

func TestReadNextString_UnterminatedLatin1(t *testing.T) {
	// No 0x00 byte: the whole input is consumed as one string.
	s, rest, err := readNextString(EncISO88591, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if s != "abc" {
		t.Errorf("got %q, want abc", s)
	}
	if rest != nil {
		t.Errorf("rest = % X, want nil", rest)
	}
}

func TestReadNextString_UTF16OddTail(t *testing.T) {
	// Unterminated UTF-16 with odd-length tail — must drop the stray
	// byte and decode the rest, not panic.
	body := []byte{0xFF, 0xFE, 'A', 0x00, 'B'} // BOM + "A" + stray 'B'
	s, rest, err := readNextString(EncUTF16, body)
	if err != nil {
		t.Fatal(err)
	}
	if s != "A" {
		t.Errorf("got %q, want A", s)
	}
	if rest != nil {
		t.Errorf("rest = % X, want nil", rest)
	}
}

func TestReadNextString_DoubleTerminator(t *testing.T) {
	// "alpha\0\0beta" — two terminators in a row, first yields the
	// alpha string, second yields the empty string.
	a, _ := encodeString(EncUTF8, "alpha", true)
	body := append(a, 0)
	body = append(body, []byte("beta")...)
	s1, rest, err := readNextString(EncUTF8, body)
	if err != nil || s1 != "alpha" {
		t.Fatalf("first %q err=%v", s1, err)
	}
	s2, _, err := readNextString(EncUTF8, rest)
	if err != nil {
		t.Fatal(err)
	}
	if s2 != "" {
		t.Errorf("second = %q, want empty", s2)
	}
}

func TestReadNextString_UnknownEncoding(t *testing.T) {
	if _, _, err := readNextString(Encoding(99), []byte("x")); err == nil {
		t.Fatal("expected error for unknown encoding")
	}
}

// --- encodeString corner cases -----------------------------------

func TestEncodeString_EmptyString(t *testing.T) {
	for _, enc := range []Encoding{EncISO88591, EncUTF8, EncUTF16, EncUTF16BE} {
		out, err := encodeString(enc, "", false)
		if err != nil {
			t.Errorf("encode %s: %v", enc, err)
			continue
		}
		// Even with terminate=false, UTF-16 still has its BOM.
		if enc == EncUTF16 {
			if !bytes.Equal(out, []byte{0xFF, 0xFE}) {
				t.Errorf("UTF-16 empty = % X, want BOM only", out)
			}
		} else if len(out) != 0 {
			t.Errorf("%s empty = % X, want empty", enc, out)
		}
	}
}

func TestEncodeString_UnknownEncodingRejected(t *testing.T) {
	if _, err := encodeString(Encoding(99), "x", false); err == nil {
		t.Fatal("expected error for unknown encoding")
	}
}

func TestEncodeString_Latin1Boundary(t *testing.T) {
	// rune U+00FF is the maximum representable in Latin-1.
	out, err := encodeString(EncISO88591, "ÿ", false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, []byte{0xFF}) {
		t.Errorf("got % X, want FF", out)
	}
	// rune U+0100 must be rejected.
	if _, err := encodeString(EncISO88591, "Ā", false); err != ErrCannotEncodeLatin1 {
		t.Errorf("got %v, want ErrCannotEncodeLatin1", err)
	}
}

// --- terminatorLen / Encoding.String -----------------------------

func TestEncoding_TerminatorLen(t *testing.T) {
	cases := map[Encoding]int{
		EncISO88591: 1,
		EncUTF8:     1,
		EncUTF16:    2,
		EncUTF16BE:  2,
	}
	for e, want := range cases {
		if got := e.terminatorLen(); got != want {
			t.Errorf("%s: got %d, want %d", e, got, want)
		}
	}
}

func TestEncoding_StringValues(t *testing.T) {
	cases := map[Encoding]string{
		EncISO88591:   "ISO-8859-1",
		EncUTF16:      "UTF-16",
		EncUTF16BE:    "UTF-16BE",
		EncUTF8:       "UTF-8",
		Encoding(99):  "Encoding(99)",
	}
	for e, want := range cases {
		if got := e.String(); got != want {
			t.Errorf("%v = %q, want %q", uint8(e), got, want)
		}
	}
}

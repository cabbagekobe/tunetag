package id3v2

import (
	"bytes"
	"testing"
)

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

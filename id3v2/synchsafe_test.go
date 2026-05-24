package id3v2

import (
	"bytes"
	"errors"
	"testing"
)

func TestSynchsafe_KnownVectors(t *testing.T) {
	cases := []struct {
		v       uint32
		encoded [4]byte
	}{
		{0, [4]byte{0x00, 0x00, 0x00, 0x00}},
		{1, [4]byte{0x00, 0x00, 0x00, 0x01}},
		{127, [4]byte{0x00, 0x00, 0x00, 0x7F}},
		{128, [4]byte{0x00, 0x00, 0x01, 0x00}},
		{257, [4]byte{0x00, 0x00, 0x02, 0x01}},
		{1234567, [4]byte{0x00, 0x4B, 0x2D, 0x07}},
		{MaxSynchsafe, [4]byte{0x7F, 0x7F, 0x7F, 0x7F}},
	}
	for _, c := range cases {
		got, err := encodeSynchsafe(c.v)
		if err != nil {
			t.Fatalf("encode(%d): %v", c.v, err)
		}
		if got != c.encoded {
			t.Errorf("encode(%d) = % X, want % X", c.v, got[:], c.encoded[:])
		}
		dec := decodeSynchsafe(c.encoded[:])
		if dec != c.v {
			t.Errorf("decode(% X) = %d, want %d", c.encoded[:], dec, c.v)
		}
	}
}

func TestSynchsafe_RoundTrip(t *testing.T) {
	for _, v := range []uint32{0, 7, 127, 128, 16383, 16384, 1<<21 - 1, 1 << 21, MaxSynchsafe} {
		buf, err := encodeSynchsafe(v)
		if err != nil {
			t.Fatalf("encode(%d): %v", v, err)
		}
		// Top bit of every encoded byte must be zero.
		for i, b := range buf {
			if b&0x80 != 0 {
				t.Errorf("encode(%d)[%d] = 0x%02X; top bit set", v, i, b)
			}
		}
		if got := decodeSynchsafe(buf[:]); got != v {
			t.Errorf("round-trip %d: got %d", v, got)
		}
	}
}

func TestSynchsafe_Overflow(t *testing.T) {
	if _, err := encodeSynchsafe(MaxSynchsafe + 1); err != ErrSynchsafeOverflow {
		t.Fatalf("got %v, want ErrSynchsafeOverflow", err)
	}
}

func TestSynchsafe_DecodeIgnoresTopBits(t *testing.T) {
	// All four top bits set should not affect the decoded value.
	got := decodeSynchsafe([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	if got != MaxSynchsafe {
		t.Errorf("decode all-0xFF = %d, want %d", got, MaxSynchsafe)
	}
}

func TestUnsync_RoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00, 0x01, 0x02},
		{0xFF, 0x00},             // already a literal sync candidate
		{0xFF, 0xE0, 0x12},       // 0xFF then high byte
		{0xFF, 0xFF, 0xE0, 0x00}, // adjacent FFs
		{0x12, 0xFF, 0x00, 0x34}, // FF followed by 0x00
		{0x12, 0xFF},             // trailing FF
		bytes.Repeat([]byte{0xFF, 0xE0}, 32),
	}
	for i, in := range cases {
		enc := unsyncEncode(in)
		// No 0xFF in encoded output may be followed by a sync byte.
		for j := 0; j+1 < len(enc); j++ {
			if enc[j] == 0xFF && (enc[j+1] == 0x00 || enc[j+1] >= 0xE0) {
				if enc[j+1] != 0x00 {
					t.Errorf("case %d: encoded contains unprotected sync at %d: % X", i, j, enc[j:j+2])
				}
			}
		}
		dec := unsyncDecode(enc)
		if !bytes.Equal(dec, in) {
			t.Errorf("case %d: round-trip mismatch\n in:  % X\n enc: % X\n dec: % X", i, in, enc, dec)
		}
	}
}

func TestUnsync_DecodeOnly(t *testing.T) {
	// 0xFF 0x00 collapses; 0xFF 0x01 is left alone.
	in := []byte{0xFF, 0x00, 0x10, 0xFF, 0x01, 0xFF, 0x00, 0xFF}
	want := []byte{0xFF, 0x10, 0xFF, 0x01, 0xFF, 0xFF}
	if got := unsyncDecode(in); !bytes.Equal(got, want) {
		t.Errorf("decode mismatch\n got  % X\n want % X", got, want)
	}
}

// --- Synchsafe boundary tests ------------------------------------

func TestSynchsafe_ZeroAndMax(t *testing.T) {
	cases := []uint32{0, 1, 0x7F, 0x80, MaxSynchsafe - 1, MaxSynchsafe}
	for _, v := range cases {
		enc, err := encodeSynchsafe(v)
		if err != nil {
			t.Errorf("encode(%d) returned %v", v, err)
			continue
		}
		// No byte may have its top bit set.
		for i, b := range enc {
			if b&0x80 != 0 {
				t.Errorf("encode(%d): byte %d = 0x%02X has top bit set", v, i, b)
			}
		}
		got := decodeSynchsafe(enc[:])
		if got != v {
			t.Errorf("round trip %d -> % X -> %d", v, enc, got)
		}
	}
}

func TestSynchsafe_OverflowRejected(t *testing.T) {
	_, err := encodeSynchsafe(MaxSynchsafe + 1)
	if !errors.Is(err, ErrSynchsafeOverflow) {
		t.Errorf("got %v, want ErrSynchsafeOverflow", err)
	}
}

// --- Unsync edge cases -------------------------------------------

func TestUnsync_EncodeEmpty(t *testing.T) {
	if got := unsyncEncode(nil); len(got) != 0 {
		t.Errorf("encode(nil) = % X, want empty", got)
	}
}

func TestUnsync_DecodeEmpty(t *testing.T) {
	if got := unsyncDecode(nil); len(got) != 0 {
		t.Errorf("decode(nil) = % X, want empty", got)
	}
}

func TestUnsync_RoundTripWithSyncSequences(t *testing.T) {
	cases := [][]byte{
		{0xFF, 0xE0},       // would look like MPEG sync
		{0xFF, 0xF0, 0x42}, // ditto
		{0xFF, 0xFF, 0xE0}, // double 0xFF
		{0xFF, 0x00, 0xAB}, // 0xFF followed by 0x00 (must keep)
		{0xFF},             // trailing single 0xFF
		bytes.Repeat([]byte{0xFF, 0xE0}, 16),
	}
	for _, in := range cases {
		enc := unsyncEncode(in)
		dec := unsyncDecode(enc)
		if !bytes.Equal(dec, in) {
			t.Errorf("round trip failed: in=% X enc=% X dec=% X", in, enc, dec)
		}
	}
}

func TestUnsync_EncodeIgnoresNonSyncFFNext(t *testing.T) {
	// 0xFF followed by 0x42 (< 0xE0): no padding required.
	got := unsyncEncode([]byte{0xFF, 0x42})
	want := []byte{0xFF, 0x42}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestUnsync_EncodeAllFFThenPadding(t *testing.T) {
	// "FF FF" — first FF is followed by FF (>= 0xE0), so a 0x00 must
	// be inserted; second FF is the terminal byte, also padded.
	got := unsyncEncode([]byte{0xFF, 0xFF})
	want := []byte{0xFF, 0x00, 0xFF, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

func TestUnsync_DecodeDoesNotCollapseFFFE(t *testing.T) {
	// $FF $FE must not be collapsed (only $FF $00 is the unsync pair).
	in := []byte{0xFF, 0xFE}
	if got := unsyncDecode(in); !bytes.Equal(got, in) {
		t.Errorf("decode % X = % X, want unchanged", in, got)
	}
}

// --- merged from edge_test.go: unsync edge cases ---------------

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

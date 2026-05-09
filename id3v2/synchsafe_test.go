package id3v2

import (
	"bytes"
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
		{0xFF, 0x00},                 // already a literal sync candidate
		{0xFF, 0xE0, 0x12},           // 0xFF then high byte
		{0xFF, 0xFF, 0xE0, 0x00},     // adjacent FFs
		{0x12, 0xFF, 0x00, 0x34},     // FF followed by 0x00
		{0x12, 0xFF},                 // trailing FF
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

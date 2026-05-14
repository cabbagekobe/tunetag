package id3v2

import (
	"bytes"
	"errors"
	"testing"
)

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

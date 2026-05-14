package mp4

import (
	"testing"
)

func TestParseDataAtom_TooShort(t *testing.T) {
	if _, err := parseDataAtom([]byte{0, 0, 0, 0}); err == nil {
		t.Fatal("expected error: data atom < 8 bytes")
	}
}

func TestParseDataAtom_LocalePreserved(t *testing.T) {
	body := []byte{
		0x00, 0x00, 0x00, 0x01, // type code = UTF-8
		0x00, 0x00, 0x00, 0x42, // locale = 0x42
		'H', 'i',
	}
	d, err := parseDataAtom(body)
	if err != nil {
		t.Fatal(err)
	}
	if d.Locale != 0x42 {
		t.Errorf("Locale = %d, want 0x42", d.Locale)
	}
	if d.String() != "Hi" {
		t.Errorf("String = %q", d.String())
	}
}

func TestDataAtom_StringOnNonUTF8(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: []byte{0x41}}
	if got := d.String(); got != "" {
		t.Errorf("String on binary type = %q, want empty", got)
	}
}

func TestDataAtom_Int_OnNonIntPayload(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeUTF8, Payload: []byte("foo")}
	if _, err := d.Int(); err == nil {
		t.Fatal("expected error: Int() on UTF-8 typed atom")
	}
}

func TestDataAtom_Int_AllSizes(t *testing.T) {
	cases := []struct {
		v       int64
		size    int
		payload []byte
	}{
		{42, 1, []byte{42}},
		{-1, 1, []byte{0xFF}},
		{300, 2, []byte{0x01, 0x2C}},
		{-1, 4, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{0x100000000, 8, []byte{0, 0, 0, 1, 0, 0, 0, 0}},
	}
	for _, c := range cases {
		d := &DataAtom{TypeCode: DataTypeBEInt, Payload: c.payload}
		got, err := d.Int()
		if err != nil {
			t.Errorf("size %d: %v", c.size, err)
			continue
		}
		if got != c.v {
			t.Errorf("size %d: got %d, want %d", c.size, got, c.v)
		}
	}
}

func TestDataAtom_Int_InvalidPayloadSize(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeBEInt, Payload: []byte{1, 2, 3}}
	if _, err := d.Int(); err == nil {
		t.Fatal("expected error: payload length not in {0,1,2,4,8}")
	}
}

func TestMakeBEIntData_RangeSelection(t *testing.T) {
	cases := []struct {
		v         int64
		wantBytes int
	}{
		{0, 1},
		{127, 1},
		{-128, 1},
		{128, 2},
		{-129, 2},
		{32767, 2},
		{-32768, 2},
		{32768, 4},
		{-32769, 4},
		{1<<31 - 1, 4},
		{-1 << 31, 4},
		{1 << 31, 8},
		{-(1<<31 + 1), 8},
	}
	for _, c := range cases {
		d := makeBEIntData(c.v)
		if len(d.Payload) != c.wantBytes {
			t.Errorf("v=%d: payload %d bytes, want %d", c.v, len(d.Payload), c.wantBytes)
		}
		got, err := d.Int()
		if err != nil {
			t.Errorf("v=%d: %v", c.v, err)
			continue
		}
		if got != c.v {
			t.Errorf("v=%d: round trip = %d", c.v, got)
		}
	}
}

func TestDataAtom_TrackNumber_OnNonBinary(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeUTF8, Payload: []byte("3")}
	if _, _, err := d.TrackNumber(); err == nil {
		t.Fatal("expected error: TrackNumber on non-binary payload")
	}
}

func TestDataAtom_TrackNumber_TooShort(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: []byte{0, 0, 0, 1}}
	if _, _, err := d.TrackNumber(); err == nil {
		t.Fatal("expected error: payload < 6 bytes")
	}
}

func TestMakeTrackNumberData_RoundTrip(t *testing.T) {
	d := makeTrackNumberData(7, 12)
	n, total, err := d.TrackNumber()
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 || total != 12 {
		t.Errorf("got (%d, %d), want (7, 12)", n, total)
	}
}

// TestDataAtom_TrackNumber_iTunesDisk verifies that the 6-byte
// `disk` payload format iTunes emits (no trailing reserved bytes)
// parses correctly. The library used to require 8 bytes which made
// Disc() silently return 0/0 on every iTunes-produced m4a.
func TestDataAtom_TrackNumber_iTunesDisk(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: []byte{0, 0, 0, 1, 0, 3}}
	n, total, err := d.TrackNumber()
	if err != nil {
		t.Fatalf("6-byte disk payload should parse: %v", err)
	}
	if n != 1 || total != 3 {
		t.Errorf("got (%d, %d), want (1, 3)", n, total)
	}
}

package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/testutil"
)

func TestFourCC_StringAndEqual(t *testing.T) {
	f := FourCC{'m', 'o', 'o', 'v'}
	if f.String() != "moov" {
		t.Errorf("String() = %q", f.String())
	}
	if !f.Equal("moov") {
		t.Errorf("Equal(moov) = false")
	}
	if f.Equal("moot") {
		t.Errorf("Equal(moot) should be false")
	}
	if f.Equal("moov ") {
		t.Errorf("Equal must require exactly 4 chars")
	}
}

func TestFourCC_PanicsOnWrongLength(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("expected panic on 3-byte input")
		}
	}()
	_ = fourCC("xyz")
}

func TestReadBoxHeader_LargeSize(t *testing.T) {
	var hdr [16]byte
	binary.BigEndian.PutUint32(hdr[0:4], 1)
	copy(hdr[4:8], "abcd")
	binary.BigEndian.PutUint64(hdr[8:16], 32)
	b, err := readBoxHeader(byteReaderAt(hdr[:]), 0, 16)
	if err != nil {
		t.Fatal(err)
	}
	if b.Size != 32 || b.HeaderSize != 16 {
		t.Errorf("got size=%d header=%d, want 32/16", b.Size, b.HeaderSize)
	}
}

func TestReadBoxHeader_SizeZero_ExtendsToEOF(t *testing.T) {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], 0)
	copy(hdr[4:8], "abcd")
	b, err := readBoxHeader(byteReaderAt(hdr[:]), 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if b.Size != 8 {
		t.Errorf("Size = %d, want 8 (entire file)", b.Size)
	}
}

func TestReadBoxHeader_LargeSizeBelow16Rejected(t *testing.T) {
	var hdr [16]byte
	binary.BigEndian.PutUint32(hdr[0:4], 1)
	copy(hdr[4:8], "abcd")
	binary.BigEndian.PutUint64(hdr[8:16], 8)
	if _, err := readBoxHeader(byteReaderAt(hdr[:]), 0, 16); err == nil {
		t.Fatal("expected error: largesize < 16")
	}
}

func TestReadBoxHeader_SizeZeroAtEndOfFile(t *testing.T) {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], 0)
	copy(hdr[4:8], "xxxx")
	if _, err := readBoxHeader(byteReaderAt(hdr[:]), 8, 8); err == nil {
		t.Fatal("expected error: size=0 with no remaining bytes")
	}
}

func TestReadBoxHeader_ShortReadOf8Bytes(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x10, 'm'}
	if _, err := readBoxHeader(byteReaderAt(body), 0, int64(len(body))); err == nil {
		t.Fatal("expected error: short read of header")
	}
}

func TestScanTopLevel_OnMinimalFile(t *testing.T) {
	body := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x"})
	tops, err := scanTopLevel(byteReaderAt(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(tops) < 3 {
		t.Fatalf("tops = %d, want >= 3", len(tops))
	}
	if !tops[0].Type.Equal("ftyp") {
		t.Errorf("tops[0] = %s, want ftyp", tops[0].Type)
	}
}

func TestSplitChild_TruncatedHeader(t *testing.T) {
	if _, _, _, err := splitChild([]byte{0x00, 0x00, 0x00}, 0); err == nil {
		t.Fatal("expected error: header < 8 bytes")
	}
}

func TestSplitChild_SizeLessThan8(t *testing.T) {
	buf := []byte{0x00, 0x00, 0x00, 0x05, 'a', 'b', 'c', 'd'}
	if _, _, _, err := splitChild(buf, 0); err == nil {
		t.Fatal("expected error: size < 8")
	}
}

func TestSplitChild_LargesizeUnsupportedHere(t *testing.T) {
	buf := []byte{0x00, 0x00, 0x00, 0x01, 'a', 'b', 'c', 'd'}
	if _, _, _, err := splitChild(buf, 0); err == nil {
		t.Fatal("expected error: largesize unsupported")
	}
}

func TestWriteBox_HeaderTooSmall(t *testing.T) {
	if err := writeBoxHeader(&bytes.Buffer{}, fourCC("free"), 4); err == nil {
		t.Fatal("expected error: total size < 8")
	}
}

func TestWriteBox_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBox(&buf, fourCC("free"), []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if binary.BigEndian.Uint32(got[0:4]) != 10 {
		t.Errorf("size = %d, want 10", binary.BigEndian.Uint32(got[0:4]))
	}
	if string(got[4:8]) != "free" {
		t.Errorf("type = %q", string(got[4:8]))
	}
}

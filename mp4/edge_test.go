package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/testutil"
)

// --- Top-level container errors ----------------------------------

func TestRead_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.m4a")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("expected error on empty file")
	}
}

func TestRead_MissingFtyp(t *testing.T) {
	// File with moov but no ftyp.
	body := []byte{0x00, 0x00, 0x00, 0x10, 'm', 'o', 'o', 'v',
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	p := writeBytesToTemp(t, body)
	if _, err := Read(p); !errors.Is(err, ErrNotMP4) {
		t.Errorf("got %v, want ErrNotMP4", err)
	}
}

func TestRead_MissingMoov(t *testing.T) {
	// ftyp present but no moov.
	body := []byte{0x00, 0x00, 0x00, 0x10, 'f', 't', 'y', 'p',
		'M', '4', 'A', ' ', 0x00, 0x00, 0x00, 0x00}
	p := writeBytesToTemp(t, body)
	if _, err := Read(p); !errors.Is(err, ErrNoMoov) {
		t.Errorf("got %v, want ErrNoMoov", err)
	}
}

func TestRead_BoxSizeBelow8(t *testing.T) {
	// First box claims size=4 (legal field but invalid: must be >= 8).
	body := []byte{0x00, 0x00, 0x00, 0x04, 'f', 't', 'y', 'p'}
	p := writeBytesToTemp(t, body)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error: box size < 8")
	}
}

func TestRead_BoxSizeBeyondFile(t *testing.T) {
	// Box claims 0x100 bytes but file is only 8.
	body := []byte{0x00, 0x00, 0x01, 0x00, 'f', 't', 'y', 'p'}
	p := writeBytesToTemp(t, body)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error: box size exceeds file")
	}
}

// --- Largesize and size==0 boxes ---------------------------------

func TestReadBoxHeader_LargeSize(t *testing.T) {
	// Hand-build a 16-byte box header with size=1 (largesize).
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
	// size=0 means "extends to end of file".
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
	binary.BigEndian.PutUint64(hdr[8:16], 8) // < 16 invalid
	if _, err := readBoxHeader(byteReaderAt(hdr[:]), 0, 16); err == nil {
		t.Fatal("expected error: largesize < 16")
	}
}

// --- ilst / data atom edge cases ---------------------------------

func TestParseIlst_TruncatedEntry(t *testing.T) {
	// One ilst entry whose declared size exceeds the body.
	body := []byte{0x00, 0x00, 0x10, 0x00, 0xa9, 'n', 'a', 'm'}
	if _, err := parseIlst(body); err == nil {
		t.Fatal("expected error: ilst entry size exceeds body")
	}
}

func TestParseIlst_EmptyBody(t *testing.T) {
	out, err := parseIlst(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 0 {
		t.Errorf("Items = %d, want 0", len(out.Items))
	}
}

func TestParseDataAtom_TooShort(t *testing.T) {
	if _, err := parseDataAtom([]byte{0, 0, 0, 0}); err == nil {
		t.Fatal("expected error: data atom < 8 bytes")
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

// --- ilst write defenses -----------------------------------------

func TestIlst_EncodeRejectsEmptyKey(t *testing.T) {
	l := &Ilst{Items: []*Item{{Key: ""}}}
	if _, err := l.encode(); err == nil {
		t.Fatal("expected error: empty key")
	}
}

func TestIlst_EncodeFreeformRequiresMeanAndName(t *testing.T) {
	l := &Ilst{Items: []*Item{{Key: "----", Data: []*DataAtom{makeUTF8Data("x")}}}}
	if _, err := l.encode(); err == nil {
		t.Fatal("expected error: freeform missing mean/name")
	}
}

// --- Cover detection ---------------------------------------------

func TestAddCover_PNGDetection(t *testing.T) {
	l := &Ilst{}
	pngMagic := append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x01, 0x02)
	l.AddCover(pngMagic)
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypePNG {
		t.Errorf("got %+v", pics)
	}
}

func TestAddCover_UnknownMagicStaysBinary(t *testing.T) {
	l := &Ilst{}
	l.AddCover([]byte{0x00, 0x01, 0x02, 0x03})
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypeBinary {
		t.Errorf("got %+v", pics)
	}
}

// --- Write-side: fragmented MP4 ---------------------------------

func TestWriteFile_FragmentedRejected(t *testing.T) {
	raw := buildMinimalWithMvex(t)
	p := writeBytesToTemp(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !f.fragmented {
		t.Fatal("Read should mark fragmented file")
	}
	f.Tag.SetTitle("anything")
	if err := f.WriteFile(p); !errors.Is(err, ErrFragmentedUnsupport) {
		t.Errorf("got %v, want ErrFragmentedUnsupport", err)
	}
}

// --- Multi-trak rewrite -----------------------------------------

func TestPatchSTCO_MultipleTraks(t *testing.T) {
	// Hand-build an MP4 with two trak boxes, each containing a stco
	// with one entry. The Tier 2 rewrite must patch both.
	raw := buildMP4WithTwoTraks(t, []uint32{1000}, []uint32{2000})
	p := writeBytesToTemp(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Tag.SetTitle(string(bytes.Repeat([]byte("z"), 256)))
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}

	// Both trak's stco entries should have been shifted by the same delta.
	values := readAllSTCOValues(t, p)
	if len(values) != 2 {
		t.Fatalf("expected 2 stco arrays, got %d", len(values))
	}
	if len(values[0]) != 1 || len(values[1]) != 1 {
		t.Fatalf("entry counts = %d, %d", len(values[0]), len(values[1]))
	}
	d0 := values[0][0] - 1000
	d1 := values[1][0] - 2000
	if d0 != d1 {
		t.Errorf("trak deltas differ: %d vs %d", d0, d1)
	}
	if d0 == 0 {
		t.Errorf("delta = 0; expected positive shift")
	}
}

// --- Helpers -----------------------------------------------------

// byteReaderAt is a tiny adapter so this test file can pass a
// []byte to readBoxHeader without writing it to a temp file.
type byteReaderAt []byte

func (b byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, errors.New("byteReaderAt: out of range")
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, errors.New("byteReaderAt: short read")
	}
	return n, nil
}

func writeBytesToTemp(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// buildMinimalWithMvex builds an MP4 fixture with mvex inside moov
// (signaling fragmented MP4). The reader must flag it; the writer
// must reject it.
func buildMinimalWithMvex(t *testing.T) []byte {
	t.Helper()
	// We build a moov containing udta/meta/ilst (so ilstFound=true)
	// alongside an mvex sibling.
	base := testutil.BuildMinimal(testutil.MinimalOptions{Title: "x"})

	// Locate moov box and inject an mvex child after udta.
	out := injectInMoov(t, base, mvexBox())
	return out
}

func injectInMoov(t *testing.T, base []byte, extra []byte) []byte {
	t.Helper()
	// Find moov box.
	pos := 0
	for pos < len(base) {
		size := binary.BigEndian.Uint32(base[pos : pos+4])
		typ := string(base[pos+4 : pos+8])
		if typ == "moov" {
			// Inject `extra` at end of moov body.
			oldEnd := pos + int(size)
			newSize := size + uint32(len(extra))
			out := make([]byte, 0, len(base)+len(extra))
			out = append(out, base[:pos]...)
			var newHdr [8]byte
			binary.BigEndian.PutUint32(newHdr[0:4], newSize)
			copy(newHdr[4:8], "moov")
			out = append(out, newHdr[:]...)
			out = append(out, base[pos+8:oldEnd]...)
			out = append(out, extra...)
			out = append(out, base[oldEnd:]...)
			return out
		}
		pos += int(size)
	}
	t.Fatal("moov not found in base")
	return nil
}

func mvexBox() []byte {
	// 8-byte mvex with no children.
	var b [8]byte
	binary.BigEndian.PutUint32(b[0:4], 8)
	copy(b[4:8], "mvex")
	return b[:]
}

// buildMP4WithTwoTraks builds an MP4 containing two track structures,
// each with its own stco. Reuses the testutil pattern but emits two
// trak siblings inside moov.
func buildMP4WithTwoTraks(t *testing.T, stco1, stco2 []uint32) []byte {
	t.Helper()
	// Build first version with one trak using testutil, then inject a second.
	base := testutil.BuildMinimal(testutil.MinimalOptions{
		Title:    "tiny",
		WithStco: stco1,
	})
	// Build a second trak with its own stco values.
	second := buildTrakWithStcoValues(stco2)
	return injectInMoov(t, base, second)
}

func buildTrakWithStcoValues(offsets []uint32) []byte {
	var stco bytes.Buffer
	stco.Write([]byte{0x00, 0x00, 0x00, 0x00}) // version+flags
	binary.Write(&stco, binary.BigEndian, uint32(len(offsets)))
	for _, o := range offsets {
		binary.Write(&stco, binary.BigEndian, o)
	}
	stbl := boxFromBody("stbl", boxFromBody("stco", stco.Bytes()))
	minf := boxFromBody("minf", stbl)
	mdia := boxFromBody("mdia", minf)
	return boxFromBody("trak", mdia)
}

func boxFromBody(typ string, body []byte) []byte {
	out := make([]byte, 0, 8+len(body))
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(8+len(body)))
	copy(hdr[4:8], typ)
	out = append(out, hdr[:]...)
	out = append(out, body...)
	return out
}

func readAllSTCOValues(t *testing.T, p string) [][]uint32 {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	tops, err := scanTopLevel(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	var out [][]uint32
	for _, b := range tops {
		if !b.Type.Equal("moov") {
			continue
		}
		body, _ := readBoxBody(f, b)
		findAllSTCO(body, &out)
	}
	return out
}

func findAllSTCO(body []byte, out *[][]uint32) {
	pos := 0
	for pos < len(body) {
		if pos+8 > len(body) {
			return
		}
		size := binary.BigEndian.Uint32(body[pos : pos+4])
		typ := string(body[pos+4 : pos+8])
		if size < 8 || int(size) > len(body)-pos {
			return
		}
		child := body[pos+8 : pos+int(size)]
		switch typ {
		case "stco":
			count := binary.BigEndian.Uint32(child[4:8])
			vals := make([]uint32, count)
			for i := uint32(0); i < count; i++ {
				vals[i] = binary.BigEndian.Uint32(child[8+4*i : 12+4*i])
			}
			*out = append(*out, vals)
		case "trak", "mdia", "minf", "stbl":
			findAllSTCO(child, out)
		}
		pos += int(size)
	}
}

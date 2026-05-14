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

// --- FourCC -----------------------------------------------------

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

// --- readBoxHeader edge ----------------------------------------

func TestReadBoxHeader_SizeZeroAtEndOfFile(t *testing.T) {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], 0)
	copy(hdr[4:8], "xxxx")
	// off == fileSize: no remaining data, must error.
	if _, err := readBoxHeader(byteReaderAt(hdr[:]), 8, 8); err == nil {
		t.Fatal("expected error: size=0 with no remaining bytes")
	}
}

func TestReadBoxHeader_ShortReadOf8Bytes(t *testing.T) {
	// Only 5 bytes available; readBoxHeader must error.
	body := []byte{0x00, 0x00, 0x00, 0x10, 'm'}
	if _, err := readBoxHeader(byteReaderAt(body), 0, int64(len(body))); err == nil {
		t.Fatal("expected error: short read of header")
	}
}

// --- DataAtom additional ----------------------------------------

func TestDataAtom_StringOnNonUTF8(t *testing.T) {
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: []byte{0x41}}
	if got := d.String(); got != "" {
		t.Errorf("String on binary type = %q, want empty", got)
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
	// "00 00 00 01 00 03" => disc 1 of 3, the exact byte sequence
	// observed in real fixtures.
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: []byte{0, 0, 0, 1, 0, 3}}
	n, total, err := d.TrackNumber()
	if err != nil {
		t.Fatalf("6-byte disk payload should parse: %v", err)
	}
	if n != 1 || total != 3 {
		t.Errorf("got (%d, %d), want (1, 3)", n, total)
	}
}

// TestIlst_Disc_FromiTunesPayload exercises the same path via the
// public Ilst.Disc() accessor.
func TestIlst_Disc_FromiTunesPayload(t *testing.T) {
	l := &Ilst{Items: []*Item{{
		Key: KeyDisc,
		Data: []*DataAtom{{
			TypeCode: DataTypeBinary,
			Payload:  []byte{0, 0, 0, 2, 0, 5}, // disc 2 of 5
		}},
	}}}
	n, total := l.Disc()
	if n != 2 || total != 5 {
		t.Errorf("Disc() = (%d,%d), want (2,5)", n, total)
	}
}

// --- parseDataAtom -------------------------------------------

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

// --- Ilst Set/Remove/First -----------------------------------

func TestIlst_SetNilRemoves(t *testing.T) {
	l := &Ilst{Items: []*Item{
		{Key: KeyTitle, Data: []*DataAtom{makeUTF8Data("x")}},
	}}
	l.Set(KeyTitle, nil)
	if l.First(KeyTitle) != nil {
		t.Errorf("Set(nil) did not remove")
	}
}

func TestIlst_SetTextEmptyRemoves(t *testing.T) {
	l := &Ilst{}
	l.SetTitle("hello")
	l.SetTitle("")
	if l.Title() != "" {
		t.Errorf("Title = %q, want empty", l.Title())
	}
	if len(l.Items) != 0 {
		t.Errorf("Items = %d, want 0", len(l.Items))
	}
}

func TestIlst_TrackDiscDefaults(t *testing.T) {
	l := &Ilst{}
	if n, total := l.Track(); n != 0 || total != 0 {
		t.Errorf("Track default = (%d,%d)", n, total)
	}
	if n, total := l.Disc(); n != 0 || total != 0 {
		t.Errorf("Disc default = (%d,%d)", n, total)
	}
	l.SetTrack(3, 9)
	l.SetDisc(1, 2)
	if n, total := l.Track(); n != 3 || total != 9 {
		t.Errorf("Track = (%d,%d)", n, total)
	}
	if n, total := l.Disc(); n != 1 || total != 2 {
		t.Errorf("Disc = (%d,%d)", n, total)
	}
}

func TestIlst_Year_BadString(t *testing.T) {
	l := &Ilst{}
	l.SetDate("not-a-year")
	if l.Year() != 0 {
		t.Errorf("Year on non-numeric date = %d, want 0", l.Year())
	}
	l.SetDate("99")
	if l.Year() != 0 {
		t.Errorf("Year on 2-char date = %d, want 0", l.Year())
	}
	l.SetDate("2026-05-14")
	if l.Year() != 2026 {
		t.Errorf("Year = %d, want 2026", l.Year())
	}
}

func TestIlst_AddCover_JPEG(t *testing.T) {
	l := &Ilst{}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	l.AddCover(jpeg)
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypeJPEG {
		t.Errorf("got %+v", pics)
	}
}

// --- Freeform write -----------------------------------------

func TestIlst_FreeformEncodeRoundTrip(t *testing.T) {
	l := &Ilst{Items: []*Item{{
		Key:        "----",
		MeanDomain: "com.apple.iTunes",
		Name:       "MUSICBRAINZ_ID",
		Data:       []*DataAtom{makeUTF8Data("123-abc")},
	}}}
	body, err := l.encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseIlst(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d", len(out.Items))
	}
	it := out.Items[0]
	if it.Key != "----" || it.MeanDomain != "com.apple.iTunes" || it.Name != "MUSICBRAINZ_ID" {
		t.Errorf("freeform = %+v", it)
	}
	if len(it.Data) != 1 || it.Data[0].String() != "123-abc" {
		t.Errorf("data = %+v", it.Data)
	}
}

// --- Read on hand-built ftyp+moov ----------------------------

func TestRead_RoundTripViaWriteFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	body := testutil.BuildMinimal(testutil.MinimalOptions{
		Title:     "hello",
		FreeBytes: 64,
	})
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tag.Title() != "hello" {
		t.Errorf("Title = %q", f.Tag.Title())
	}
	f.Tag.SetTitle("HELLO")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	again, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if again.Tag.Title() != "HELLO" {
		t.Errorf("Title after WriteFile = %q", again.Tag.Title())
	}
}

// --- WriteFile error paths ----------------------------------

func TestWriteFile_IdenticalSizeIsInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	body := testutil.BuildMinimal(testutil.MinimalOptions{Title: "ABCD"})
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	origSize := int64(len(body))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	// Same-length replacement: delta=0 path must touch only the
	// ilst region; the file size must not change.
	f.Tag.SetTitle("WXYZ")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Size() != origSize {
		t.Errorf("size = %d, want %d", info.Size(), origSize)
	}
	again, _ := Read(p)
	if again.Tag.Title() != "WXYZ" {
		t.Errorf("Title = %q", again.Tag.Title())
	}
}

func TestRead_NonexistentPath(t *testing.T) {
	if _, err := Read("/nonexistent/path/x.m4a"); err == nil {
		t.Fatal("expected error opening missing path")
	}
}

func TestRead_MissingIlstTreatedAsEmpty(t *testing.T) {
	// Build a moov with udta/meta but no ilst — Read should succeed
	// and present an empty Tag.
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	body := testutil.BuildMinimal(testutil.MinimalOptions{}) // no fields => ilst empty
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tag == nil {
		t.Fatal("Tag is nil")
	}
	if len(f.Tag.Items) != 0 {
		t.Errorf("Items = %d, want 0", len(f.Tag.Items))
	}
}

// --- splitChild bounds --------------------------------------

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
	// rawSize == 1 means "use 64-bit largesize"; splitChild does not
	// support that for moov children and must error.
	buf := []byte{0x00, 0x00, 0x00, 0x01, 'a', 'b', 'c', 'd'}
	if _, _, _, err := splitChild(buf, 0); err == nil {
		t.Fatal("expected error: largesize unsupported")
	}
}

// --- scanChildren / scanTopLevel correctness -----------------

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

// --- write_box defenses --------------------------------------

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

// --- ErrFragmentedUnsupport is exposed correctly -------------

func TestErrFragmentedUnsupport_Is(t *testing.T) {
	wrapped := errors.New("wrap: " + ErrFragmentedUnsupport.Error())
	_ = wrapped
	if errors.Is(ErrFragmentedUnsupport, ErrNoMoov) {
		t.Errorf("ErrFragmentedUnsupport must not match ErrNoMoov")
	}
}

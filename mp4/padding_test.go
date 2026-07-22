package mp4

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

// tbox builds an ISO-BMFF box: 8-byte header (size + 4-char type) + body.
func tbox(typ string, body []byte) []byte {
	b := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(b[:4], uint32(8+len(body)))
	copy(b[4:8], typ)
	copy(b[8:], body)
	return b
}

// buildWithMetaPadding builds a minimal tagged m4a whose meta box ends with
// `pad` bytes of zero padding that is NOT wrapped in a free atom — the exact
// shape iTunes / Apple Music.app leave behind (an over-declared meta box). A
// strict child walk reads the zero bytes as a size-0 box.
func buildWithMetaPadding(title string, pad int) []byte {
	dataAtom := tbox("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(title)...)) // type=1 (UTF-8), locale=0
	nam := tbox("\xa9nam", dataAtom)
	ilst := tbox("ilst", nam)

	metaInner := ilst
	if pad > 0 {
		metaInner = append(metaInner, make([]byte, pad)...) // raw zero padding
	}
	metaBody := append([]byte{0, 0, 0, 0}, metaInner...) // meta FullBox: version+flags
	meta := tbox("meta", metaBody)
	udta := tbox("udta", meta)
	moov := tbox("moov", udta)
	ftyp := tbox("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom"))
	mdat := tbox("mdat", make([]byte, 16))
	return append(append(append([]byte{}, ftyp...), moov...), mdat...)
}

func TestRead_MetaTrailingZeroPadding(t *testing.T) {
	// 24 trailing zero bytes inside meta (>= a full size-0 box header).
	raw := buildWithMetaPadding("Hello", 24)
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatalf("Read failed on meta with trailing zero padding: %v", err)
	}
	if f.Tag == nil {
		t.Fatal("Tag is nil")
	}
	if got := f.Tag.Title(); got != "Hello" {
		t.Errorf("Title = %q, want %q", got, "Hello")
	}
}

func TestRead_MetaSubHeaderZeroPadding(t *testing.T) {
	// 4 trailing zero bytes: fewer than a full box header, so a naive walk
	// reads them as a truncated header. They are still iTunes padding and
	// must not fail the read.
	raw := buildWithMetaPadding("Hello", 4)
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatalf("Read failed on meta with sub-header zero padding: %v", err)
	}
	if got := f.Tag.Title(); got != "Hello" {
		t.Errorf("Title = %q, want %q", got, "Hello")
	}
}

func TestSplitChild_MidStreamZeroSize(t *testing.T) {
	// A zero size field followed by non-zero bytes is malformed, not trailing
	// padding: splitChild must error rather than silently ending the walk and
	// dropping the sibling that follows.
	buf := append([]byte{0, 0, 0, 0}, tbox("free", nil)...)
	if _, _, _, err := splitChild(buf, 0); err == nil {
		t.Fatal("expected error: zero-size box with trailing non-zero bytes")
	} else if errors.Is(err, errPaddingBox) {
		t.Fatalf("mid-stream zero-size must not be treated as padding: %v", err)
	}
}

func TestWriteFile_MetaTrailingZeroPadding(t *testing.T) {
	// Editing a tag on an iTunes-padded file must round-trip: the rebuild
	// drops the junk trailing padding, and stco offsets stay consistent
	// because the chunk delta is derived from the rebuilt moov size.
	raw := buildWithMetaPadding("Hello", 24)
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	f.Tag.SetTitle("Changed")
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile on padded file: %v", err)
	}

	f2, err := Read(p)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	if got := f2.Tag.Title(); got != "Changed" {
		t.Errorf("Title after round-trip = %q, want %q", got, "Changed")
	}
}

// buildWithMetaPaddingAndStco is buildWithMetaPadding plus a trak/mdia/minf/
// stbl/stco branch carrying the given chunk offsets, so a Tier 3 rewrite must
// patch stco. moov precedes mdat, so dropping the meta padding shrinks moov and
// every offset must shift by the moov-size delta.
func buildWithMetaPaddingAndStco(title string, pad int, offsets []uint32) []byte {
	dataAtom := tbox("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(title)...))
	ilst := tbox("ilst", tbox("\xa9nam", dataAtom))
	metaInner := append(ilst, make([]byte, pad)...)
	meta := tbox("meta", append([]byte{0, 0, 0, 0}, metaInner...))
	udta := tbox("udta", meta)

	stco := []byte{0, 0, 0, 0} // version+flags
	stco = binary.BigEndian.AppendUint32(stco, uint32(len(offsets)))
	for _, o := range offsets {
		stco = binary.BigEndian.AppendUint32(stco, o)
	}
	trak := tbox("trak", tbox("mdia", tbox("minf", tbox("stbl", tbox("stco", stco)))))

	moov := tbox("moov", append(trak, udta...))
	ftyp := tbox("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom"))
	mdat := tbox("mdat", make([]byte, 32))
	return append(append(append([]byte{}, ftyp...), moov...), mdat...)
}

func TestWriteFile_MetaPaddingWithStcoPatched(t *testing.T) {
	// A tag edit on a padded file that also carries stco offsets must both
	// round-trip the tag AND shift every chunk offset by the moov-size delta —
	// dropping the padding must not desync the audio chunk pointers.
	offsets := []uint32{4096, 8192, 16384}
	raw := buildWithMetaPaddingAndStco("Hello", 24, offsets)
	p := writeTempMP4(t, raw)
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	f, err := Read(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	f.Tag.SetTitle("Changed")
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	after, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	delta := after.Size() - before.Size()

	f2, err := Read(p)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	if got := f2.Tag.Title(); got != "Changed" {
		t.Errorf("Title = %q, want %q", got, "Changed")
	}
	got := readSTCOOffsets(t, p)
	for i, want := range offsets {
		if got[i] != want+uint32(delta) {
			t.Errorf("stco[%d] = %d, want %d (want+delta, delta=%d)", i, got[i], want+uint32(delta), delta)
		}
	}
}

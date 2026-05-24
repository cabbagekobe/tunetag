package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cabbagekobe/tunetag/internal/mp4test"
)

// --- Shared test helpers ---------------------------------------

func writeTempMP4(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.m4a")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// byteReaderAt adapts a []byte to io.ReaderAt for atom-level tests
// that don't need a temp file.
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

// injectInMoov locates the moov box in base and appends extra bytes
// to the end of its body (resizing the box header accordingly).
func injectInMoov(t *testing.T, base []byte, extra []byte) []byte {
	t.Helper()
	pos := 0
	for pos < len(base) {
		size := binary.BigEndian.Uint32(base[pos : pos+4])
		typ := string(base[pos+4 : pos+8])
		if typ == "moov" {
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

func boxFromBody(typ string, body []byte) []byte {
	out := make([]byte, 0, 8+len(body))
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(8+len(body)))
	copy(hdr[4:8], typ)
	out = append(out, hdr[:]...)
	out = append(out, body...)
	return out
}

// --- File-level Read / WriteFile tests --------------------------

func TestRead_Minimal(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title: "Hello", Artist: "Alice", Album: "First",
	})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Tag.Title(); got != "Hello" {
		t.Errorf("Title = %q", got)
	}
	if got := f.Tag.Artist(); got != "Alice" {
		t.Errorf("Artist = %q", got)
	}
	if got := f.Tag.Album(); got != "First" {
		t.Errorf("Album = %q", got)
	}
}

func TestRead_NotMP4(t *testing.T) {
	p := writeTempMP4(t, []byte("not an mp4 file"))
	if _, err := Read(p); err == nil {
		t.Fatal("expected error for non-MP4 input")
	}
}

func TestRead_NonexistentPath(t *testing.T) {
	if _, err := Read("/nonexistent/path/x.m4a"); err == nil {
		t.Fatal("expected error opening missing path")
	}
}

func TestRead_EmptyFile(t *testing.T) {
	p := writeTempMP4(t, nil)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error on empty file")
	}
}

func TestRead_MissingFtyp(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x10, 'm', 'o', 'o', 'v',
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	p := writeTempMP4(t, body)
	if _, err := Read(p); !errors.Is(err, ErrNotMP4) {
		t.Errorf("got %v, want ErrNotMP4", err)
	}
}

func TestRead_MissingMoov(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x10, 'f', 't', 'y', 'p',
		'M', '4', 'A', ' ', 0x00, 0x00, 0x00, 0x00}
	p := writeTempMP4(t, body)
	if _, err := Read(p); !errors.Is(err, ErrNoMoov) {
		t.Errorf("got %v, want ErrNoMoov", err)
	}
}

func TestRead_BoxSizeBelow8(t *testing.T) {
	body := []byte{0x00, 0x00, 0x00, 0x04, 'f', 't', 'y', 'p'}
	p := writeTempMP4(t, body)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error: box size < 8")
	}
}

func TestRead_BoxSizeBeyondFile(t *testing.T) {
	body := []byte{0x00, 0x00, 0x01, 0x00, 'f', 't', 'y', 'p'}
	p := writeTempMP4(t, body)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error: box size exceeds file")
	}
}

func TestRead_MissingIlstTreatedAsEmpty(t *testing.T) {
	body := mp4test.BuildMinimal(mp4test.MinimalOptions{}) // no fields => ilst empty
	p := writeTempMP4(t, body)
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

func TestRead_RoundTripViaWriteFile(t *testing.T) {
	body := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:     "hello",
		FreeBytes: 64,
	})
	p := writeTempMP4(t, body)
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

func TestWriteFile_InPlaceExact(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "Same"})
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, raw) {
		t.Errorf("byte-for-byte equal expected on identity write")
	}
}

func TestWriteFile_IdenticalSizeIsInPlace(t *testing.T) {
	body := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "ABCD"})
	p := writeTempMP4(t, body)
	origSize := int64(len(body))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
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

func TestWriteFile_AbsorbsIntoSiblingFree(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:     "A",
		FreeBytes: 256,
	})
	p := writeTempMP4(t, raw)
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff < 0 || f.freeLen != 256 {
		t.Fatalf("expected sibling free of 256, got off=%d len=%d", f.freeOff, f.freeLen)
	}
	f.Tag.SetTitle("a much longer title to exercise free absorption")

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() != originalSize {
		t.Errorf("file size changed %d -> %d (in-place expected)", originalSize, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Tag.Title(); got != "a much longer title to exercise free absorption" {
		t.Errorf("Title = %q", got)
	}
}

func TestWriteFile_ShrinksByInsertingFree(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:  "a much longer title to be replaced by a short one",
		Artist: "and another field",
	})
	p := writeTempMP4(t, raw)
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff >= 0 {
		t.Fatalf("expected no sibling free in fixture")
	}

	f.Tag.SetTitle("X")
	f.Tag.SetArtist("Y")

	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() != originalSize {
		t.Errorf("file size changed %d -> %d (insert-free expected)", originalSize, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatalf("re-read after shrink: %v", err)
	}
	if got := out.Tag.Title(); got != "X" {
		t.Errorf("Title = %q", got)
	}
	if got := out.Tag.Artist(); got != "Y" {
		t.Errorf("Artist = %q", got)
	}
}

func TestWriteFile_FullRewriteWhenGrowing(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "tiny"})
	p := writeTempMP4(t, raw)
	original := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	longTitle := "tiny title made very long with extra padding " +
		"to overflow any in-place reserve by hundreds of bytes that simply cannot fit"
	f.Tag.SetTitle(longTitle)
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Size() <= original {
		t.Errorf("expected file growth on Tier 2 rewrite (was %d, now %d)", original, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Tag.Title(); got != longTitle {
		t.Errorf("Title = %q", got)
	}
}

// --- Picture / Track round-trip --------------------------------

func TestPicture_AddCover_DetectsJPEG(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "x", FreeBytes: 4096})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'J', 'F', 'I', 'F', 0x00}
	f.Tag.AddCover(jpeg)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	pics := out.Tag.Pictures()
	if len(pics) != 1 {
		t.Fatalf("pictures = %d, want 1", len(pics))
	}
	if pics[0].TypeCode != DataTypeJPEG {
		t.Errorf("TypeCode = %d, want JPEG (%d)", pics[0].TypeCode, DataTypeJPEG)
	}
	if !bytes.Equal(pics[0].Payload, jpeg) {
		t.Errorf("payload mismatch")
	}
}

func TestTrack_RoundTrip(t *testing.T) {
	raw := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "x", FreeBytes: 256})
	p := writeTempMP4(t, raw)
	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Tag.SetTrack(3, 12)
	f.Tag.SetDisc(1, 2)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n, total := out.Tag.Track(); n != 3 || total != 12 {
		t.Errorf("track = %d/%d", n, total)
	}
	if n, total := out.Tag.Disc(); n != 1 || total != 2 {
		t.Errorf("disc = %d/%d", n, total)
	}
}

// --- Multi-trak rewrite + fragmented MP4 -----------------------

func TestWriteFile_FragmentedRejected(t *testing.T) {
	base := mp4test.BuildMinimal(mp4test.MinimalOptions{Title: "x"})
	raw := injectInMoov(t, base, mvexBox())
	p := writeTempMP4(t, raw)
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

func mvexBox() []byte {
	var b [8]byte
	binary.BigEndian.PutUint32(b[0:4], 8)
	copy(b[4:8], "mvex")
	return b[:]
}

func TestPatchSTCO_MultipleTraks(t *testing.T) {
	raw := buildMP4WithTwoTraks(t, []uint32{1000}, []uint32{2000})
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Tag.SetTitle(string(bytes.Repeat([]byte("z"), 256)))
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}

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

func buildMP4WithTwoTraks(t *testing.T, stco1, stco2 []uint32) []byte {
	t.Helper()
	base := mp4test.BuildMinimal(mp4test.MinimalOptions{
		Title:    "tiny",
		WithStco: stco1,
	})
	second := buildTrakWithStcoValues(stco2)
	return injectInMoov(t, base, second)
}

func buildTrakWithStcoValues(offsets []uint32) []byte {
	var stco bytes.Buffer
	stco.Write([]byte{0x00, 0x00, 0x00, 0x00})
	_ = binary.Write(&stco, binary.BigEndian, uint32(len(offsets)))
	for _, o := range offsets {
		_ = binary.Write(&stco, binary.BigEndian, o)
	}
	stbl := boxFromBody("stbl", boxFromBody("stco", stco.Bytes()))
	minf := boxFromBody("minf", stbl)
	mdia := boxFromBody("mdia", minf)
	return boxFromBody("trak", mdia)
}

func readAllSTCOValues(t *testing.T, p string) [][]uint32 {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
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

// --- Misc small tests ------------------------------------------

func TestErrFragmentedUnsupport_Is(t *testing.T) {
	wrapped := errors.New("wrap: " + ErrFragmentedUnsupport.Error())
	_ = wrapped
	if errors.Is(ErrFragmentedUnsupport, ErrNoMoov) {
		t.Errorf("ErrFragmentedUnsupport must not match ErrNoMoov")
	}
}

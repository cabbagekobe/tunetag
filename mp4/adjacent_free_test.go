package mp4

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
)

// buildWithMetaChildren builds a minimal tagged m4a whose meta box holds
// exactly the given byte runs in order (no hdlr; Read does not need it).
// Each run is normally one box, but raw bytes (e.g. zero padding) work too.
func buildWithMetaChildren(children ...[]byte) []byte {
	var inner []byte
	for _, c := range children {
		inner = append(inner, c...)
	}
	meta := tbox("meta", append([]byte{0, 0, 0, 0}, inner...))
	moov := tbox("moov", tbox("udta", meta))
	ftyp := tbox("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom"))
	mdat := tbox("mdat", make([]byte, 16))
	return append(append(append([]byte{}, ftyp...), moov...), mdat...)
}

func ilstWithTitle(title string) []byte {
	data := tbox("data", append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(title)...)) // type=1 (UTF-8), locale=0
	return tbox("ilst", tbox("\xa9nam", data))
}

func uuidBox(payload string) []byte {
	return tbox("uuid", append([]byte("0123456789abcdef"), []byte(payload)...))
}

// metaChildren walks moov/udta/meta of the file at path and returns each
// child as "type:size". It fails the test on any malformed box so a stale
// tail left behind by a bad write is caught.
func metaChildren(t *testing.T, path string) []string {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(off, end, depth int)
	walk = func(off, end, depth int) {
		for off < end {
			if off+8 > end {
				t.Fatalf("truncated box header at %d", off)
			}
			size := int(binary.BigEndian.Uint32(d[off : off+4]))
			typ := string(d[off+4 : off+8])
			if size < 8 || off+size > end {
				t.Fatalf("malformed box %q at %d: size=%d (parent ends at %d)", typ, off, size, end)
			}
			switch {
			case depth == 3:
				out = append(out, fmt.Sprintf("%s:%d", typ, size))
			case typ == "moov" || typ == "udta":
				walk(off+8, off+size, depth+1)
			case typ == "meta":
				walk(off+12, off+size, depth+1)
			}
			off += size
		}
	}
	walk(0, len(d), 0)
	return out
}

const longTitle = "a much longer title that does not fit in the old ilst"

func TestWriteFile_FreeAfterUuidIsNotAbsorbed(t *testing.T) {
	free := tbox("free", make([]byte, 248))
	raw := buildWithMetaChildren(ilstWithTitle("A"), uuidBox("one"), uuidBox("two"), free)
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff >= 0 {
		t.Fatalf("free is not adjacent to ilst but was recorded: off=%d len=%d", f.freeOff, f.freeLen)
	}
	f.Tag.SetTitle(longTitle)
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := Read(p)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got := out.Tag.Title(); got != longTitle {
		t.Errorf("Title = %q", got)
	}
	got := metaChildren(t, p)
	want := []string{fmt.Sprintf("ilst:%d", len(ilstWithTitle(longTitle))), "uuid:27", "uuid:27", "free:256"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("meta children = %v, want %v", got, want)
	}
}

func TestWriteFile_FreeBeforeIlstIsNotAbsorbed(t *testing.T) {
	free := tbox("free", make([]byte, 248))
	raw := buildWithMetaChildren(free, ilstWithTitle("A"))
	p := writeTempMP4(t, raw)

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff >= 0 {
		t.Fatalf("free precedes ilst but was recorded: off=%d len=%d", f.freeOff, f.freeLen)
	}
	f.Tag.SetTitle(longTitle)
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := Read(p)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got := out.Tag.Title(); got != longTitle {
		t.Errorf("Title = %q", got)
	}
	got := metaChildren(t, p)
	want := []string{"free:256", fmt.Sprintf("ilst:%d", len(ilstWithTitle(longTitle)))}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("meta children = %v, want %v", got, want)
	}
}

func TestWriteFile_ShrinkWithNonAdjacentFreeKeepsSiblings(t *testing.T) {
	free := tbox("free", make([]byte, 248))
	raw := buildWithMetaChildren(ilstWithTitle(longTitle), uuidBox("one"), uuidBox("two"), free)
	p := writeTempMP4(t, raw)
	originalSize := int64(len(raw))

	f, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.freeOff >= 0 {
		t.Fatalf("free is not adjacent to ilst but was recorded: off=%d len=%d", f.freeOff, f.freeLen)
	}
	f.Tag.SetTitle("A")
	if err := f.WriteFile(p); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, _ := os.Stat(p)
	if info.Size() != originalSize {
		t.Errorf("file size changed %d -> %d (insert-free expected)", originalSize, info.Size())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got := out.Tag.Title(); got != "A" {
		t.Errorf("Title = %q", got)
	}
	shortIlst := len(ilstWithTitle("A"))
	inserted := len(ilstWithTitle(longTitle)) - shortIlst
	got := metaChildren(t, p)
	want := []string{fmt.Sprintf("ilst:%d", shortIlst), fmt.Sprintf("free:%d", inserted), "uuid:27", "uuid:27", "free:256"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("meta children = %v, want %v", got, want)
	}
}

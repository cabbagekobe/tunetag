package asf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- builders for hand-crafted ASF files -----------------------

// buildObject builds GUID + size + body for one object.
func buildObject(g GUID, body []byte) []byte {
	out := make([]byte, 0, objHeaderSize+len(body))
	out = append(out, g[:]...)
	sz := make([]byte, 8)
	binary.LittleEndian.PutUint64(sz, uint64(objHeaderSize+len(body)))
	out = append(out, sz...)
	out = append(out, body...)
	return out
}

// buildHeader wraps child objects in an ASF Header Object.
func buildHeader(children ...[]byte) []byte {
	var body bytes.Buffer
	count := make([]byte, 4)
	binary.LittleEndian.PutUint32(count, uint32(len(children)))
	body.Write(count)
	body.WriteByte(0x01)
	body.WriteByte(0x02)
	for _, c := range children {
		body.Write(c)
	}
	return buildObject(guidHeaderObject, body.Bytes())
}

func buildCDO(title, author, copyright, descr, rating string) []byte {
	t := encodeUTF16NUL(title)
	a := encodeUTF16NUL(author)
	c := encodeUTF16NUL(copyright)
	d := encodeUTF16NUL(descr)
	r := encodeUTF16NUL(rating)
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(t)))
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(a)))
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(c)))
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(d)))
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(r)))
	body.Write(t)
	body.Write(a)
	body.Write(c)
	body.Write(d)
	body.Write(r)
	return buildObject(guidContentDescriptionObject, body.Bytes())
}

func buildECDO(descriptors []Descriptor) []byte {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint16(len(descriptors)))
	for _, d := range descriptors {
		name := encodeUTF16NUL(d.Name)
		_ = binary.Write(&body, binary.LittleEndian, uint16(len(name)))
		body.Write(name)
		_ = binary.Write(&body, binary.LittleEndian, uint16(d.Type))
		_ = binary.Write(&body, binary.LittleEndian, uint16(len(d.Value)))
		body.Write(d.Value)
	}
	return buildObject(guidExtendedContentDescriptionObject, body.Bytes())
}

func writeTemp(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.wma")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- GUID tests ------------------------------------------------

func TestMustGUID_RoundTrip(t *testing.T) {
	const s = "75B22630-668E-11CF-A6D9-00AA0062CE6C"
	g := mustGUID(s)
	if got := g.String(); got != s {
		t.Errorf("String round-trip = %s, want %s", got, s)
	}
}

func TestMustGUID_HeaderObjectBytes(t *testing.T) {
	// Verify wire-order encoding of the Header Object GUID.
	// First three fields are little-endian:
	//   30 26 B2 75   8E 66   CF 11
	// Last 8 bytes are byte-for-byte:
	//   A6 D9 00 AA 00 62 CE 6C
	expect := []byte{
		0x30, 0x26, 0xB2, 0x75,
		0x8E, 0x66,
		0xCF, 0x11,
		0xA6, 0xD9, 0x00, 0xAA, 0x00, 0x62, 0xCE, 0x6C,
	}
	if !bytes.Equal(guidHeaderObject[:], expect) {
		t.Errorf("Header Object GUID bytes = % X, want % X", guidHeaderObject[:], expect)
	}
}

// --- Read ------------------------------------------------------

func TestRead_RejectsOversizedChildObject(t *testing.T) {
	// Craft a Header Object whose first child declares a size of
	// 0xFFFFFFFFFFFFFFFF. The previous implementation cast that to
	// int64 (= -1), computed a negative body length, and panicked
	// inside make([]byte, ...). Read must return ErrTruncated
	// instead.
	hdrGUID := guidHeaderObject
	// Inner child: 16-byte arbitrary GUID + 8-byte size = max uint64.
	var child [objHeaderSize]byte
	// Use Content Description Object GUID just so the type isn't
	// reserved; the parser must reject by size before it cares.
	copy(child[0:16], guidContentDescriptionObject[:])
	binary.LittleEndian.PutUint64(child[16:24], 0xFFFFFFFFFFFFFFFF)

	var hdrBody bytes.Buffer
	_ = binary.Write(&hdrBody, binary.LittleEndian, uint32(1)) // 1 child
	hdrBody.WriteByte(0x01)
	hdrBody.WriteByte(0x02)
	hdrBody.Write(child[:])

	var raw bytes.Buffer
	raw.Write(hdrGUID[:])
	_ = binary.Write(&raw, binary.LittleEndian, uint64(objHeaderSize+hdrBody.Len()))
	raw.Write(hdrBody.Bytes())

	if _, err := Read(bytes.NewReader(raw.Bytes())); !errors.Is(err, ErrTruncated) {
		t.Errorf("got %v, want ErrTruncated (no panic)", err)
	}
}

func TestRead_RejectsNonASF(t *testing.T) {
	body := bytes.Repeat([]byte{0xAB}, 32)
	if _, err := Read(bytes.NewReader(body)); !errors.Is(err, ErrNoASF) {
		t.Errorf("got %v, want ErrNoASF", err)
	}
}

func TestRead_CDOOnly(t *testing.T) {
	raw := buildHeader(buildCDO("My Title", "Some Artist", "(c) 2026", "A description", "rating-5"))
	// Append a Data Object skeleton so the trailer isn't empty.
	raw = append(raw, buildObject(guidDataObject, []byte("audio bytes here"))...)

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "My Title" {
		t.Errorf("Title = %q", f.Title)
	}
	if f.Author != "Some Artist" {
		t.Errorf("Author = %q", f.Author)
	}
	if f.Description != "A description" {
		t.Errorf("Description = %q", f.Description)
	}
}

func TestRead_ECDOWithKnownNames(t *testing.T) {
	descriptors := []Descriptor{
		{Name: NameAlbumTitle, Type: TypeString, Value: encodeUTF16NUL("My Album")},
		{Name: NameYear, Type: TypeString, Value: encodeUTF16NUL("2026")},
		{Name: NameTrackNumber, Type: TypeString, Value: encodeUTF16NUL("5/12")},
		{Name: NameGenre, Type: TypeString, Value: encodeUTF16NUL("Electronic")},
	}
	raw := buildHeader(buildECDO(descriptors))
	raw = append(raw, buildObject(guidDataObject, []byte("body"))...)

	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Album() != "My Album" {
		t.Errorf("Album = %q", f.Album())
	}
	if f.Year() != 2026 {
		t.Errorf("Year = %d", f.Year())
	}
	if n, total := f.TrackNumber(); n != 5 || total != 12 {
		t.Errorf("Track = %d/%d", n, total)
	}
	if f.Genre() != "Electronic" {
		t.Errorf("Genre = %q", f.Genre())
	}
}

func TestRead_PreservesUnknownObjects(t *testing.T) {
	// Build a header with a CDO + an unknown object. The unknown
	// one must survive Read → WriteFile unchanged.
	unknownGUID := mustGUID("00000000-0000-0000-0000-000000000099")
	unknown := buildObject(unknownGUID, []byte("opaque-payload-bytes"))
	raw := buildHeader(buildCDO("T", "A", "", "", ""), unknown)
	raw = append(raw, buildObject(guidDataObject, []byte("data"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("opaque-payload-bytes")) {
		t.Errorf("unknown object body lost on round-trip")
	}
}

// --- Write -----------------------------------------------------

func TestWriteFile_RoundTripsCDO(t *testing.T) {
	raw := buildHeader(buildCDO("old", "old artist", "", "", ""))
	raw = append(raw, buildObject(guidDataObject, []byte("audio_body"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Title = "new title"
	f.SetArtist("new artist")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "new title" || g.Author != "new artist" {
		t.Errorf("after write: Title=%q Author=%q", g.Title, g.Author)
	}
	// Data Object trailer must remain intact.
	if !bytes.HasSuffix(g.trailer, []byte("audio_body")) {
		t.Errorf("trailer body lost: %q", g.trailer)
	}
}

func TestWriteFile_AppendsECDOWhenAbsent(t *testing.T) {
	raw := buildHeader(buildCDO("", "", "", "", ""))
	raw = append(raw, buildObject(guidDataObject, []byte("audio"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.SetAlbum("Brand New")
	f.SetYear(2026)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Album() != "Brand New" {
		t.Errorf("Album = %q", g.Album())
	}
	if g.Year() != 2026 {
		t.Errorf("Year = %d", g.Year())
	}
}

func TestWriteFile_DropsEmptyMetadata(t *testing.T) {
	descriptors := []Descriptor{{Name: NameAlbumTitle, Type: TypeString, Value: encodeUTF16NUL("byebye")}}
	raw := buildHeader(buildCDO("byebye", "", "", "", ""), buildECDO(descriptors))
	raw = append(raw, buildObject(guidDataObject, []byte("audio"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Title = ""
	f.Extended = nil
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "" || len(g.Extended) != 0 {
		t.Errorf("expected empty metadata, got Title=%q Extended=%+v", g.Title, g.Extended)
	}
	// Both CDO and ECDO should have been dropped.
	for _, c := range g.children {
		if c.kind == childContentDescr || c.kind == childExtendedCD {
			t.Errorf("placeholder for %v survived strip", c.kind)
		}
	}
}

// --- Pictures --------------------------------------------------

func TestPicture_RoundTrip(t *testing.T) {
	p := &Picture{
		Type:        3, // CoverFront
		MIME:        "image/jpeg",
		Description: "front",
		Data:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x42, 0x42},
	}
	f := &File{}
	f.AddPicture(p)
	// Now build a fake file, decode, and check the round-trip.
	descriptors := f.Extended
	raw := buildHeader(buildECDO(descriptors))
	raw = append(raw, buildObject(guidDataObject, []byte("body"))...)

	g, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	pics := g.Pictures()
	if len(pics) != 1 {
		t.Fatalf("Pictures = %d, want 1", len(pics))
	}
	got := pics[0]
	if got.Type != p.Type || got.MIME != p.MIME || got.Description != p.Description || !bytes.Equal(got.Data, p.Data) {
		t.Errorf("Picture mismatch: %+v vs %+v", got, p)
	}
}

func TestRemovePictures(t *testing.T) {
	f := &File{}
	f.SetAlbum("Keep me")
	f.AddPicture(&Picture{Type: 3, MIME: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47}})
	f.RemovePictures()
	if len(f.Pictures()) != 0 {
		t.Errorf("Pictures not removed")
	}
	if f.Album() != "Keep me" {
		t.Errorf("Other descriptors lost")
	}
}

// --- UTF-16 helpers --------------------------------------------

func TestDescriptor_NonStringTypesRoundTrip(t *testing.T) {
	// Build descriptors of every non-string type and verify
	// they survive Read → WriteFile → Read with bytes intact
	// and Uint32 accessor returning the expected value where
	// applicable.
	dword := make([]byte, 4)
	binary.LittleEndian.PutUint32(dword, 0xDEADBEEF)
	word := make([]byte, 2)
	binary.LittleEndian.PutUint16(word, 0xC0FE)
	qword := make([]byte, 8)
	binary.LittleEndian.PutUint64(qword, 0x1122334455667788)
	boolBytes := []byte{0x01, 0x00, 0x00, 0x00} // ASF bool is a 4-byte DWORD-style value

	descriptors := []Descriptor{
		{Name: NameYear, Type: TypeDWord, Value: dword},
		{Name: "WM/Custom16", Type: TypeWord, Value: word},
		{Name: "WM/Custom64", Type: TypeQWord, Value: qword},
		{Name: "WM/IsCompilation", Type: TypeBool, Value: boolBytes},
		{Name: "WM/Sig", Type: TypeBinary, Value: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
	}
	raw := buildHeader(buildECDO(descriptors))
	raw = append(raw, buildObject(guidDataObject, []byte("body"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Extended) != len(descriptors) {
		t.Fatalf("descriptor count = %d, want %d", len(g.Extended), len(descriptors))
	}
	for i, want := range descriptors {
		got := g.Extended[i]
		if got.Name != want.Name || got.Type != want.Type || !bytes.Equal(got.Value, want.Value) {
			t.Errorf("descriptor[%d] = %+v, want %+v", i, got, want)
		}
	}
	// Year() must see WM/Year as a DWord and decode it.
	if got := g.Year(); got != 0xDEADBEEF {
		t.Errorf("Year() = %d, want %d (DWORD)", got, 0xDEADBEEF)
	}
	// Uint32 accessor on a Word descriptor returns the 16-bit
	// value zero-extended.
	if got := g.FindExt("WM/Custom16").Uint32(); got != 0xC0FE {
		t.Errorf("Custom16.Uint32() = 0x%X, want 0xC0FE", got)
	}
}

func TestSetArtist_RoundTrips(t *testing.T) {
	// A file with both Author and WM/AlbumArtist must round-trip
	// SetArtist correctly: Artist() should return the new value,
	// not the old WM/AlbumArtist.
	descriptors := []Descriptor{
		{Name: NameAlbumArtist, Type: TypeString, Value: encodeUTF16NUL("Old Album Artist")},
	}
	raw := buildHeader(
		buildCDO("Title", "Old Author", "", "", ""),
		buildECDO(descriptors),
	)
	raw = append(raw, buildObject(guidDataObject, []byte("body"))...)
	p := writeTemp(t, raw)

	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.SetArtist("New Author")
	if got := f.Artist(); got != "New Author" {
		t.Errorf("immediately after SetArtist: Artist() = %q, want %q", got, "New Author")
	}
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Artist(); got != "New Author" {
		t.Errorf("after round-trip: Artist() = %q, want %q", got, "New Author")
	}
	// AlbumArtist should still be the original (we did not
	// touch WM/AlbumArtist).
	if got := g.AlbumArtist(); got != "Old Album Artist" {
		t.Errorf("AlbumArtist preserved: got %q, want %q", got, "Old Album Artist")
	}
}

func TestArtist_FallsBackToAlbumArtistWhenAuthorEmpty(t *testing.T) {
	descriptors := []Descriptor{
		{Name: NameAlbumArtist, Type: TypeString, Value: encodeUTF16NUL("Various")},
	}
	raw := buildHeader(buildECDO(descriptors))
	raw = append(raw, buildObject(guidDataObject, []byte("body"))...)
	f, err := Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Artist(); got != "Various" {
		t.Errorf("Artist() = %q, want fallback to WM/AlbumArtist %q", got, "Various")
	}
}

func TestEncodeDecodeUTF16NUL_RoundTrip(t *testing.T) {
	cases := []string{
		"",
		"hello",
		"日本語",
		"Mix€d 文字",
	}
	for _, s := range cases {
		b := encodeUTF16NUL(s)
		got := decodeUTF16NUL(b)
		if got != s {
			t.Errorf("round-trip(%q) = %q", s, got)
		}
	}
}

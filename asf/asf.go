// Package asf reads and writes ASF / WMA file metadata.
//
// ASF (Advanced Systems Format) is Microsoft's audio/video
// container. .wma audio files and .wmv video files both use it;
// this package handles only the tag metadata and treats every
// audio / video data object as opaque bytes that round-trip
// verbatim.
//
// Two metadata objects are recognised:
//
//   - Content Description Object — the five classic fields
//     (Title, Author, Copyright, Description, Rating) stored as
//     length-prefixed UTF-16LE strings.
//
//   - Extended Content Description Object — a list of typed
//     name / value descriptors. Common names like
//     WM/AlbumTitle, WM/Year, WM/Genre, WM/TrackNumber,
//     WM/AlbumArtist, WM/Composer, and WM/Picture are exposed
//     via the high-level accessors below; the raw Descriptor
//     slice is accessible for callers that need full control.
//
// All other top-level Header child objects (File Properties,
// Stream Properties, Header Extension, Codec List, …) and the
// Data + Index objects following the header are preserved
// byte-for-byte across writes.
//
// Memory: Read loads the entire post-header region (Data Object
// plus any Index objects, i.e. effectively the audio body) into
// memory so WriteFile can re-emit it after the rewritten header.
// This is fine for typical music files but expensive for
// multi-GB WMV. A streaming rewrite is left for a future
// release.
//
// A *File is not safe for concurrent use.
package asf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Errors returned by this package.
var (
	// ErrNoASF is returned by Read when the input does not begin
	// with the ASF Header Object GUID.
	ErrNoASF = errors.New("asf: not an ASF / WMA stream")

	// ErrTruncated is returned when an object's declared size
	// exceeds the available bytes.
	ErrTruncated = errors.New("asf: object size exceeds stream")
)

// File holds the parsed metadata of an ASF / WMA file plus
// enough bookkeeping to rewrite it without disturbing the audio.
type File struct {
	// Content Description Object fields. Empty strings cause the
	// corresponding entry to be dropped (or the whole object
	// removed if all five are empty).
	Title       string
	Author      string
	Copyright   string
	Description string
	Rating      string

	// Extended Content Description Object descriptors in stream
	// order. Modify directly or via the accessors below.
	Extended []Descriptor

	// children holds every Header child object in stream order.
	// Metadata objects are stored as placeholders so their
	// position is preserved on write; everything else is raw.
	children []headerChild

	// trailer is the bytes after the Header Object — the Data
	// Object and any optional Index Object(s). Preserved
	// verbatim.
	trailer []byte
}

// headerChild is one child of the top-level ASF Header Object.
type headerChild struct {
	guid GUID
	body []byte // raw bytes (object header NOT included); used only for chunkRaw
	kind childKind
}

type childKind uint8

const (
	childRaw          childKind = iota // body holds the bytes after the 24-byte object header
	childContentDescr                  // placeholder; regenerated from File.{Title,…,Rating}
	childExtendedCD                    // placeholder; regenerated from File.Extended
)

// DescriptorType is the on-disk type code for an Extended
// Content Description Object descriptor value.
type DescriptorType uint16

const (
	TypeString DescriptorType = 0
	TypeBinary DescriptorType = 1
	TypeBool   DescriptorType = 2
	TypeDWord  DescriptorType = 3
	TypeQWord  DescriptorType = 4
	TypeWord   DescriptorType = 5
)

// Descriptor is one Extended Content Description entry.
type Descriptor struct {
	Name  string
	Type  DescriptorType
	Value []byte // raw bytes; helpers below decode/encode common types
}

// String returns the value as a UTF-8 string if the descriptor's
// type is TypeString; otherwise empty.
func (d Descriptor) String() string {
	if d.Type != TypeString {
		return ""
	}
	return decodeUTF16NUL(d.Value)
}

// Uint32 returns the value as a uint32 if the descriptor's type
// is TypeDWord or TypeWord; otherwise 0.
func (d Descriptor) Uint32() uint32 {
	switch d.Type {
	case TypeDWord:
		if len(d.Value) >= 4 {
			return binary.LittleEndian.Uint32(d.Value[:4])
		}
	case TypeWord:
		if len(d.Value) >= 2 {
			return uint32(binary.LittleEndian.Uint16(d.Value[:2]))
		}
	}
	return 0
}

// --- Read ------------------------------------------------------

const objHeaderSize = 24 // GUID (16) + uint64 size (8)

// Read parses an ASF stream and returns a populated *File. The
// audio body is captured into File.trailer for re-emission by
// WriteFile.
func Read(rs io.ReadSeeker) (*File, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var hdr [objHeaderSize]byte
	if _, err := io.ReadFull(rs, hdr[:]); err != nil {
		return nil, fmt.Errorf("asf: short header: %w", err)
	}
	if readGUID(hdr[0:16]) != guidHeaderObject {
		return nil, ErrNoASF
	}
	headerSize := binary.LittleEndian.Uint64(hdr[16:24])
	if headerSize < objHeaderSize+6 {
		return nil, fmt.Errorf("asf: implausible header size %d", headerSize)
	}
	// 6 bytes after objHeaderSize: child count (uint32 LE) + 2
	// reserved bytes.
	var meta [6]byte
	if _, err := io.ReadFull(rs, meta[:]); err != nil {
		return nil, err
	}
	childCount := binary.LittleEndian.Uint32(meta[0:4])
	// Read all children sequentially. We use the declared size
	// of each child to advance.
	remaining := int64(headerSize) - int64(objHeaderSize) - 6
	f := &File{}
	for i := uint32(0); i < childCount; i++ {
		if remaining < int64(objHeaderSize) {
			return nil, fmt.Errorf("asf: child %d header runs past header object", i)
		}
		var ch [objHeaderSize]byte
		if _, err := io.ReadFull(rs, ch[:]); err != nil {
			return nil, err
		}
		g := readGUID(ch[0:16])
		size := binary.LittleEndian.Uint64(ch[16:24])
		if size < objHeaderSize {
			return nil, fmt.Errorf("asf: child %d declared size %d less than header", i, size)
		}
		// remaining is positive here (loop guard above checked >=
		// objHeaderSize), so converting to uint64 is safe. Doing
		// the bounds check in uint64 space avoids the int64-overflow
		// panic that would otherwise hit make([]byte, ...) when a
		// malformed file declares a size ≥ 2^63.
		if size > uint64(remaining) {
			return nil, ErrTruncated
		}
		body := make([]byte, int64(size)-int64(objHeaderSize))
		if _, err := io.ReadFull(rs, body); err != nil {
			return nil, err
		}
		remaining -= int64(size)

		switch g {
		case guidContentDescriptionObject:
			if err := f.parseContentDescription(body); err != nil {
				return nil, err
			}
			f.children = append(f.children, headerChild{guid: g, kind: childContentDescr})
		case guidExtendedContentDescriptionObject:
			if err := f.parseExtendedContentDescription(body); err != nil {
				return nil, err
			}
			f.children = append(f.children, headerChild{guid: g, kind: childExtendedCD})
		default:
			f.children = append(f.children, headerChild{guid: g, body: body, kind: childRaw})
		}
	}
	// Capture everything past the header.
	tail, err := io.ReadAll(rs)
	if err != nil {
		return nil, fmt.Errorf("asf: read body: %w", err)
	}
	f.trailer = tail
	return f, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Read(f)
}

func (f *File) parseContentDescription(body []byte) error {
	if len(body) < 10 {
		return fmt.Errorf("asf: CDO too small (%d bytes)", len(body))
	}
	titleLen := binary.LittleEndian.Uint16(body[0:2])
	authorLen := binary.LittleEndian.Uint16(body[2:4])
	copyLen := binary.LittleEndian.Uint16(body[4:6])
	descrLen := binary.LittleEndian.Uint16(body[6:8])
	ratingLen := binary.LittleEndian.Uint16(body[8:10])
	pos := 10
	read := func(n uint16) (string, error) {
		if pos+int(n) > len(body) {
			return "", fmt.Errorf("asf: CDO field length %d overflows", n)
		}
		s := decodeUTF16NUL(body[pos : pos+int(n)])
		pos += int(n)
		return s, nil
	}
	var err error
	if f.Title, err = read(titleLen); err != nil {
		return err
	}
	if f.Author, err = read(authorLen); err != nil {
		return err
	}
	if f.Copyright, err = read(copyLen); err != nil {
		return err
	}
	if f.Description, err = read(descrLen); err != nil {
		return err
	}
	if f.Rating, err = read(ratingLen); err != nil {
		return err
	}
	return nil
}

func (f *File) parseExtendedContentDescription(body []byte) error {
	if len(body) < 2 {
		return fmt.Errorf("asf: ECDO too small")
	}
	count := binary.LittleEndian.Uint16(body[0:2])
	pos := 2
	for i := uint16(0); i < count; i++ {
		if pos+2 > len(body) {
			return fmt.Errorf("asf: ECDO descriptor %d: short name length", i)
		}
		nameLen := binary.LittleEndian.Uint16(body[pos : pos+2])
		pos += 2
		if pos+int(nameLen) > len(body) {
			return fmt.Errorf("asf: ECDO descriptor %d: name overflows", i)
		}
		name := decodeUTF16NUL(body[pos : pos+int(nameLen)])
		pos += int(nameLen)
		if pos+4 > len(body) {
			return fmt.Errorf("asf: ECDO descriptor %d: short type/value-length", i)
		}
		typ := DescriptorType(binary.LittleEndian.Uint16(body[pos : pos+2]))
		valLen := binary.LittleEndian.Uint16(body[pos+2 : pos+4])
		pos += 4
		if pos+int(valLen) > len(body) {
			return fmt.Errorf("asf: ECDO descriptor %d: value overflows", i)
		}
		val := make([]byte, valLen)
		copy(val, body[pos:pos+int(valLen)])
		pos += int(valLen)
		f.Extended = append(f.Extended, Descriptor{Name: name, Type: typ, Value: val})
	}
	return nil
}

// --- Write -----------------------------------------------------

// WriteFile rewrites path with the current metadata. The audio
// body and any non-metadata Header child objects are preserved
// byte-for-byte.
func (f *File) WriteFile(path string) error {
	out, err := f.encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-asf-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(out); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (f *File) encode() ([]byte, error) {
	// Materialise children, substituting placeholders.
	saw := map[childKind]bool{}
	out := make([]headerChild, 0, len(f.children)+2)
	for _, c := range f.children {
		switch c.kind {
		case childContentDescr:
			saw[childContentDescr] = true
			body := f.encodeContentDescription()
			if body == nil {
				continue
			}
			out = append(out, headerChild{guid: guidContentDescriptionObject, body: body, kind: childRaw})
		case childExtendedCD:
			saw[childExtendedCD] = true
			body := f.encodeExtendedContentDescription()
			if body == nil {
				continue
			}
			out = append(out, headerChild{guid: guidExtendedContentDescriptionObject, body: body, kind: childRaw})
		default:
			out = append(out, c)
		}
	}
	if !saw[childContentDescr] {
		if body := f.encodeContentDescription(); body != nil {
			out = append(out, headerChild{guid: guidContentDescriptionObject, body: body, kind: childRaw})
		}
	}
	if !saw[childExtendedCD] {
		if body := f.encodeExtendedContentDescription(); body != nil {
			out = append(out, headerChild{guid: guidExtendedContentDescriptionObject, body: body, kind: childRaw})
		}
	}

	// Build header body: the 6-byte fixed prologue + each child
	// object's GUID + size + body.
	var hdrBody bytes.Buffer
	_ = binary.Write(&hdrBody, binary.LittleEndian, uint32(len(out)))
	hdrBody.WriteByte(0x01) // reserved
	hdrBody.WriteByte(0x02) // reserved
	for _, c := range out {
		hdrBody.Write(c.guid[:])
		_ = binary.Write(&hdrBody, binary.LittleEndian, uint64(objHeaderSize+len(c.body)))
		hdrBody.Write(c.body)
	}
	totalHeader := uint64(objHeaderSize + hdrBody.Len())

	// Top-level emit.
	var buf bytes.Buffer
	buf.Write(guidHeaderObject[:])
	_ = binary.Write(&buf, binary.LittleEndian, totalHeader)
	buf.Write(hdrBody.Bytes())
	buf.Write(f.trailer)
	return buf.Bytes(), nil
}

// encodeContentDescription returns the CDO body bytes (without
// the 24-byte object header) or nil when all five fields are
// empty.
func (f *File) encodeContentDescription() []byte {
	t := encodeUTF16NUL(f.Title)
	a := encodeUTF16NUL(f.Author)
	c := encodeUTF16NUL(f.Copyright)
	d := encodeUTF16NUL(f.Description)
	r := encodeUTF16NUL(f.Rating)
	if len(t)+len(a)+len(c)+len(d)+len(r) == 0 {
		return nil
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(t)))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(a)))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(c)))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(d)))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(r)))
	buf.Write(t)
	buf.Write(a)
	buf.Write(c)
	buf.Write(d)
	buf.Write(r)
	return buf.Bytes()
}

// encodeExtendedContentDescription returns the ECDO body bytes
// or nil when no descriptors are present.
func (f *File) encodeExtendedContentDescription() []byte {
	if len(f.Extended) == 0 {
		return nil
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(f.Extended)))
	for _, d := range f.Extended {
		name := encodeUTF16NUL(d.Name)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(len(name)))
		buf.Write(name)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(d.Type))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(len(d.Value)))
		buf.Write(d.Value)
	}
	return buf.Bytes()
}

// --- helpers ---------------------------------------------------

// encodeUTF16NUL encodes s as UTF-16LE with a trailing U+0000
// terminator. An empty s returns nil so callers can drop a field
// by length rather than emit a 2-byte NUL-only blob.
func encodeUTF16NUL(s string) []byte {
	if s == "" {
		return nil
	}
	u := utf16.Encode([]rune(s))
	buf := make([]byte, 2*len(u)+2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(buf[i*2:], c)
	}
	// trailing NUL already present (buf was zero-initialised)
	return buf
}

// decodeUTF16NUL decodes a UTF-16LE byte string and strips any
// trailing U+0000. Odd byte counts yield the truncated value
// (rather than an error) since real-world files occasionally
// pad sloppy values.
func decodeUTF16NUL(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
	}
	// Trim trailing NUL(s).
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

// --- accessors -------------------------------------------------

// Well-known Extended Content Description names.
const (
	NameAlbumTitle  = "WM/AlbumTitle"
	NameAlbumArtist = "WM/AlbumArtist"
	NameComposer    = "WM/Composer"
	NameGenre       = "WM/Genre"
	NameYear        = "WM/Year"
	NameTrackNumber = "WM/TrackNumber"
	NamePartOfSet   = "WM/PartOfSet"
	NameTrack       = "WM/Track" // zero-indexed; deprecated in favour of TrackNumber
	NamePicture     = "WM/Picture"
	NameProvider    = "WM/Provider"
	NamePublisher   = "WM/Publisher"
	NameMediaClass  = "WM/MediaClassPrimaryID"
)

// FindExt returns a pointer to the first Extended Content
// Description descriptor whose name matches (case-sensitive).
// nil if absent.
func (f *File) FindExt(name string) *Descriptor {
	for i := range f.Extended {
		if f.Extended[i].Name == name {
			return &f.Extended[i]
		}
	}
	return nil
}

// GetExt returns the string value of the named descriptor (only
// for TypeString descriptors), or "".
func (f *File) GetExt(name string) string {
	if d := f.FindExt(name); d != nil {
		return d.String()
	}
	return ""
}

// SetExt sets or replaces a string-valued descriptor. Empty
// value removes it.
func (f *File) SetExt(name, value string) {
	for i := range f.Extended {
		if f.Extended[i].Name == name {
			if value == "" {
				f.Extended = append(f.Extended[:i], f.Extended[i+1:]...)
				return
			}
			f.Extended[i].Type = TypeString
			f.Extended[i].Value = encodeUTF16NUL(value)
			return
		}
	}
	if value == "" {
		return
	}
	f.Extended = append(f.Extended, Descriptor{
		Name: name, Type: TypeString, Value: encodeUTF16NUL(value),
	})
}

// Artist returns the track artist. The Content Description
// Object's Author field is the canonical location for a per-track
// artist; WM/AlbumArtist (in the Extended Content Description
// Object) is a fallback for files that only populate the album
// artist. SetArtist writes to Author so this round-trips.
func (f *File) Artist() string {
	if f.Author != "" {
		return f.Author
	}
	return f.GetExt(NameAlbumArtist)
}

// Album returns WM/AlbumTitle.
func (f *File) Album() string { return f.GetExt(NameAlbumTitle) }

// AlbumArtist returns WM/AlbumArtist.
func (f *File) AlbumArtist() string { return f.GetExt(NameAlbumArtist) }

// Composer returns WM/Composer.
func (f *File) Composer() string { return f.GetExt(NameComposer) }

// Genre returns WM/Genre.
func (f *File) Genre() string { return f.GetExt(NameGenre) }

// Comment returns the CDO Description field (the closest analogue
// to a "comment").
func (f *File) Comment() string { return f.Description }

// Year returns the WM/Year value as an integer, falling back to
// the first four digits of any string form.
func (f *File) Year() int {
	d := f.FindExt(NameYear)
	if d == nil {
		return 0
	}
	switch d.Type {
	case TypeDWord:
		return int(d.Uint32())
	case TypeWord:
		return int(d.Uint32())
	case TypeString:
		s := d.String()
		if len(s) < 4 {
			return 0
		}
		var y int
		for i := 0; i < 4; i++ {
			c := s[i]
			if c < '0' || c > '9' {
				return 0
			}
			y = y*10 + int(c-'0')
		}
		return y
	}
	return 0
}

// TrackNumber returns (number, total). WM/TrackNumber is
// typically a uint32; some writers store "n/total" as a string.
func (f *File) TrackNumber() (n, total int) {
	d := f.FindExt(NameTrackNumber)
	if d == nil {
		return 0, 0
	}
	switch d.Type {
	case TypeDWord, TypeWord:
		return int(d.Uint32()), 0
	case TypeString:
		return parseSlashed(d.String())
	}
	return 0, 0
}

// DiscNumber returns (number, total) from WM/PartOfSet, which is
// conventionally "n" or "n/total" as a string.
func (f *File) DiscNumber() (n, total int) {
	return parseSlashed(f.GetExt(NamePartOfSet))
}

// --- accessors mirroring tunetag.Tag ---------------------------

// SetArtist writes to the CDO Author field (the most-compatible
// location). Callers that specifically want WM/AlbumArtist can
// use SetExt(NameAlbumArtist, …) directly.
func (f *File) SetArtist(s string) { f.Author = s }

// SetAlbum sets WM/AlbumTitle.
func (f *File) SetAlbum(s string) { f.SetExt(NameAlbumTitle, s) }

// SetGenre sets WM/Genre.
func (f *File) SetGenre(s string) { f.SetExt(NameGenre, s) }

// SetComposer sets WM/Composer.
func (f *File) SetComposer(s string) { f.SetExt(NameComposer, s) }

// SetYear sets WM/Year (stored as a string for compatibility).
func (f *File) SetYear(year int) {
	if year == 0 {
		f.SetExt(NameYear, "")
		return
	}
	f.SetExt(NameYear, fmt.Sprintf("%04d", year))
}

// SetTrackNumber sets WM/TrackNumber. When total > 0 the value
// is stored as "n/total"; otherwise a plain decimal string.
func (f *File) SetTrackNumber(n, total int) {
	if n == 0 && total == 0 {
		f.SetExt(NameTrackNumber, "")
		return
	}
	if total > 0 {
		f.SetExt(NameTrackNumber, fmt.Sprintf("%d/%d", n, total))
		return
	}
	f.SetExt(NameTrackNumber, fmt.Sprintf("%d", n))
}

// SetDiscNumber sets WM/PartOfSet.
func (f *File) SetDiscNumber(n, total int) {
	if n == 0 && total == 0 {
		f.SetExt(NamePartOfSet, "")
		return
	}
	if total > 0 {
		f.SetExt(NamePartOfSet, fmt.Sprintf("%d/%d", n, total))
		return
	}
	f.SetExt(NamePartOfSet, fmt.Sprintf("%d", n))
}

func parseSlashed(s string) (n, total int) {
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, "/", 2)
	n = atoi(parts[0])
	if len(parts) == 2 {
		total = atoi(parts[1])
	}
	return n, total
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

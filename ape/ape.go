// Package ape reads and writes APEv2 tags. APEv2 is the canonical
// tagging format for Monkey's Audio (.ape) and WavPack (.wv) files,
// and is occasionally found on MP3 / WAV files as well.
//
// An APEv2 tag is stored at the end of the file (after the audio
// data and, if present, before the 128-byte ID3v1 trailer). A
// 32-byte header may optionally precede the items; a 32-byte
// footer is always present and is the entry point for locating
// the tag from the end of the file.
//
// All numeric fields are little-endian.
//
// Item keys are ASCII (0x20 – 0x7E) between 2 and 255 bytes; the
// spec defines case-insensitive lookup but mandates exact-case
// preservation on disk. This package preserves the on-disk casing
// of every item and matches keys case-insensitively for Get / Set.
//
// A *Tag is not safe for concurrent use.
package ape

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Preamble is the 8-byte sentinel at the start of an APE header or
// footer.
var Preamble = [8]byte{'A', 'P', 'E', 'T', 'A', 'G', 'E', 'X'}

// Header / footer / item flag bits (4-byte little-endian word at
// the end of every header, footer, and item).
const (
	FlagReadOnly        uint32 = 1 << 0
	FlagItemTypeBinary  uint32 = 1 << 1
	FlagItemTypeUTF8URL uint32 = 1 << 2
	FlagItemTypeReserve uint32 = 1 << 3
	FlagHasHeader       uint32 = 1 << 31
	FlagHasNoFooter     uint32 = 1 << 30
	FlagIsHeader        uint32 = 1 << 29
)

// ItemType extracted from the flag bits.
type ItemType uint8

const (
	ItemUTF8   ItemType = 0
	ItemBinary ItemType = 1
	ItemURL    ItemType = 2
)

// Errors returned by this package.
var (
	// ErrNoTag is returned by Read when no APEv2 footer is found
	// at the end of the input.
	ErrNoTag = errors.New("ape: no APEv2 tag found")

	// ErrUnsupportedVersion is returned for APEv1 (version 1000)
	// tags, which use a different on-disk layout.
	ErrUnsupportedVersion = errors.New("ape: APEv1 is not supported")

	// ErrInvalidKey is returned by Set when the key is empty or
	// not 2-255 bytes of ASCII.
	ErrInvalidKey = errors.New("ape: invalid item key")
)

// Tag is the in-memory representation of an APEv2 tag.
type Tag struct {
	Items []Item

	// HasHeader controls whether a 32-byte header is written
	// before the items. Most modern writers set this true; the
	// official Monkey's Audio spec requires it for APEv2 but
	// real-world files vary.
	HasHeader bool
}

// Item is one APEv2 entry.
type Item struct {
	Key   string
	Type  ItemType
	Flags uint32

	// Value is the raw bytes. For UTF-8 / URL items, this is a
	// UTF-8 string (multi-value items use NUL as separator). For
	// binary items, it's an opaque blob.
	Value []byte
}

// String returns the UTF-8 value (or empty string for binary items
// or when Value is empty).
func (i Item) String() string {
	if i.Type == ItemBinary {
		return ""
	}
	return string(i.Value)
}

// Values returns each NUL-separated sub-value. For binary items
// the function returns nil; for single-value items it returns a
// one-element slice.
func (i Item) Values() []string {
	if i.Type == ItemBinary {
		return nil
	}
	return strings.Split(string(i.Value), "\x00")
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*Tag, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read locates an APEv2 tag at the end of rs and returns it.
// rs must support seeking to absolute and end-relative positions.
func Read(rs io.ReadSeeker) (*Tag, error) {
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	footerStart, err := locateFooter(rs, end)
	if err != nil {
		return nil, err
	}
	if _, err := rs.Seek(footerStart, io.SeekStart); err != nil {
		return nil, err
	}
	var foot [32]byte
	if _, err := io.ReadFull(rs, foot[:]); err != nil {
		return nil, err
	}
	if !bytes.Equal(foot[0:8], Preamble[:]) {
		return nil, ErrNoTag
	}
	version := binary.LittleEndian.Uint32(foot[8:12])
	if version != 2000 {
		return nil, fmt.Errorf("%w (version=%d)", ErrUnsupportedVersion, version)
	}
	size := binary.LittleEndian.Uint32(foot[12:16]) // tag size INCLUDING footer
	count := binary.LittleEndian.Uint32(foot[16:20])
	flags := binary.LittleEndian.Uint32(foot[20:24])

	hasHeader := flags&FlagHasHeader != 0
	// APEv2 footer field `size` covers items + footer only (the
	// optional 32-byte header is NOT counted). So the items
	// region always starts at footerStart - (size - 32),
	// regardless of whether a header is present — the header
	// occupies the 32 bytes BEFORE that, but we never need to
	// read it (its content mirrors the footer).
	bodyStart := footerStart - int64(size) + 32
	if bodyStart < 0 || bodyStart > footerStart {
		return nil, fmt.Errorf("ape: footer claims tag size %d that overflows file", size)
	}
	if _, err := rs.Seek(bodyStart, io.SeekStart); err != nil {
		return nil, err
	}
	body := make([]byte, footerStart-bodyStart)
	if _, err := io.ReadFull(rs, body); err != nil {
		return nil, err
	}
	items, err := parseItems(body, count)
	if err != nil {
		return nil, err
	}
	return &Tag{Items: items, HasHeader: hasHeader}, nil
}

// locateFooter walks back from end-of-file (skipping a trailing
// ID3v1 if present) to the start of an APEv2 footer. Returns the
// absolute offset of the footer, or ErrNoTag.
func locateFooter(rs io.ReadSeeker, end int64) (int64, error) {
	tryAt := func(off int64) (bool, error) {
		if off < 0 {
			return false, nil
		}
		if _, err := rs.Seek(off, io.SeekStart); err != nil {
			return false, err
		}
		var hdr [8]byte
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			return false, err
		}
		return bytes.Equal(hdr[:], Preamble[:]), nil
	}
	// Case 1: footer at end-32.
	if ok, err := tryAt(end - 32); err != nil {
		return 0, err
	} else if ok {
		return end - 32, nil
	}
	// Case 2: footer before a 128-byte ID3v1 trailer.
	if end >= 128+32 {
		if _, err := rs.Seek(end-128, io.SeekStart); err != nil {
			return 0, err
		}
		var tag3 [3]byte
		if _, err := io.ReadFull(rs, tag3[:]); err == nil && string(tag3[:]) == "TAG" {
			if ok, err := tryAt(end - 128 - 32); err != nil {
				return 0, err
			} else if ok {
				return end - 128 - 32, nil
			}
		}
	}
	return 0, ErrNoTag
}

func parseItems(body []byte, count uint32) ([]Item, error) {
	// Cap the initial capacity. An APEv2 item is at least 9 bytes
	// on disk (4-byte value-length + 4-byte flags + 1-byte
	// key-NUL); body cannot physically hold more than len(body)/9
	// items. Without this guard, a hostile footer claiming count
	// = 4 GiB would force a multi-hundred-GiB allocation.
	capHint := count
	if int64(capHint) > int64(len(body))/9 {
		capHint = uint32(len(body) / 9)
	}
	items := make([]Item, 0, capHint)
	i := 0
	for k := uint32(0); k < count; k++ {
		if i+8 > len(body) {
			return nil, fmt.Errorf("ape: item %d header runs past end", k)
		}
		valueLen := binary.LittleEndian.Uint32(body[i : i+4])
		flags := binary.LittleEndian.Uint32(body[i+4 : i+8])
		i += 8
		// Key is NUL-terminated ASCII.
		end := bytes.IndexByte(body[i:], 0)
		if end < 0 {
			return nil, fmt.Errorf("ape: item %d: unterminated key", k)
		}
		key := string(body[i : i+end])
		i += end + 1
		if i+int(valueLen) > len(body) {
			return nil, fmt.Errorf("ape: item %q value runs past end", key)
		}
		value := make([]byte, valueLen)
		copy(value, body[i:i+int(valueLen)])
		i += int(valueLen)
		typ := ItemType((flags >> 1) & 0x3)
		items = append(items, Item{Key: key, Type: typ, Flags: flags, Value: value})
	}
	return items, nil
}

// Find returns the first item whose key matches name (case
// insensitively). nil if absent.
func (t *Tag) Find(name string) *Item {
	for i := range t.Items {
		if strings.EqualFold(t.Items[i].Key, name) {
			return &t.Items[i]
		}
	}
	return nil
}

// Get returns the first UTF-8 value for name, or "".
func (t *Tag) Get(name string) string {
	if it := t.Find(name); it != nil {
		return it.String()
	}
	return ""
}

// Set inserts or replaces a UTF-8 item. Empty value removes the
// item. Returns ErrInvalidKey on invalid keys.
func (t *Tag) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	for i, it := range t.Items {
		if strings.EqualFold(it.Key, key) {
			if value == "" {
				t.Items = append(t.Items[:i], t.Items[i+1:]...)
				return nil
			}
			t.Items[i].Value = []byte(value)
			t.Items[i].Type = ItemUTF8
			return nil
		}
	}
	if value == "" {
		return nil
	}
	t.Items = append(t.Items, Item{Key: key, Type: ItemUTF8, Value: []byte(value)})
	return nil
}

// Remove deletes every item whose key matches name (case
// insensitively). Returns the number removed.
func (t *Tag) Remove(name string) int {
	out := t.Items[:0]
	removed := 0
	for _, it := range t.Items {
		if strings.EqualFold(it.Key, name) {
			removed++
			continue
		}
		out = append(out, it)
	}
	t.Items = out
	return removed
}

func validateKey(key string) error {
	if len(key) < 2 || len(key) > 255 {
		return ErrInvalidKey
	}
	for _, r := range key {
		if r < 0x20 || r > 0x7E {
			return ErrInvalidKey
		}
	}
	// "ID3", "TAG", "OggS", and "MP+" are reserved by the spec.
	lower := strings.ToLower(key)
	switch lower {
	case "id3", "tag", "oggs", "mp+":
		return ErrInvalidKey
	}
	return nil
}

// Encode returns the on-disk bytes of the tag (optional header +
// items + footer).
func (t *Tag) Encode() ([]byte, error) {
	var items bytes.Buffer
	for _, it := range t.Items {
		if err := validateKey(it.Key); err != nil {
			return nil, fmt.Errorf("ape: encode: item %q: %w", it.Key, err)
		}
		_ = binary.Write(&items, binary.LittleEndian, uint32(len(it.Value)))
		flags := it.Flags &^ uint32(0x6) // clear type bits
		flags |= (uint32(it.Type) & 0x3) << 1
		_ = binary.Write(&items, binary.LittleEndian, flags)
		items.WriteString(it.Key)
		items.WriteByte(0)
		items.Write(it.Value)
	}
	bodyLen := items.Len()
	// Size in header/footer counts everything except the header.
	tagSize := uint32(bodyLen + 32)
	count := uint32(len(t.Items))

	var out bytes.Buffer
	if t.HasHeader {
		writeAPEFrame(&out, tagSize, count, true, true)
	}
	out.Write(items.Bytes())
	writeAPEFrame(&out, tagSize, count, t.HasHeader, false)
	return out.Bytes(), nil
}

func writeAPEFrame(buf *bytes.Buffer, size, count uint32, hasHeader, isHeader bool) {
	buf.Write(Preamble[:])
	_ = binary.Write(buf, binary.LittleEndian, uint32(2000)) // version
	_ = binary.Write(buf, binary.LittleEndian, size)
	_ = binary.Write(buf, binary.LittleEndian, count)
	var flags uint32
	if hasHeader {
		flags |= FlagHasHeader
	}
	if isHeader {
		flags |= FlagIsHeader
	}
	_ = binary.Write(buf, binary.LittleEndian, flags)
	buf.Write(make([]byte, 8)) // reserved
}

// WriteFile replaces (or appends) the APEv2 tag of path. The
// audio body before any pre-existing APEv2 tag is preserved
// byte-for-byte. A trailing ID3v1 trailer, if present, is also
// preserved at the very end (after the new APEv2 tag).
func (t *Tag) WriteFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	end, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	// Discover existing layout: where does the audio body end?
	audioEnd, id3v1, err := scanTrailers(src, end)
	if err != nil {
		return err
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tagBytes, err := t.Encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-ape-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := io.CopyN(tmp, src, audioEnd); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(tagBytes); err != nil {
		cleanup()
		return err
	}
	if len(id3v1) > 0 {
		if _, err := tmp.Write(id3v1); err != nil {
			cleanup()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := src.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// scanTrailers returns the offset at which the original audio body
// ends and a copy of any ID3v1 trailer found. Any existing APEv2
// tag is treated as not part of the audio body.
func scanTrailers(rs io.ReadSeeker, end int64) (audioEnd int64, id3v1 []byte, err error) {
	audioEnd = end
	if end >= 128 {
		if _, err := rs.Seek(end-128, io.SeekStart); err != nil {
			return 0, nil, err
		}
		var tag3 [3]byte
		if _, err := io.ReadFull(rs, tag3[:]); err != nil {
			return 0, nil, err
		}
		if string(tag3[:]) == "TAG" {
			if _, err := rs.Seek(end-128, io.SeekStart); err != nil {
				return 0, nil, err
			}
			id3v1 = make([]byte, 128)
			if _, err := io.ReadFull(rs, id3v1); err != nil {
				return 0, nil, err
			}
			audioEnd = end - 128
		}
	}
	// Look for APE footer at audioEnd-32.
	if audioEnd >= 32 {
		if _, err := rs.Seek(audioEnd-32, io.SeekStart); err != nil {
			return 0, nil, err
		}
		var hdr [32]byte
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			return 0, nil, err
		}
		if bytes.Equal(hdr[0:8], Preamble[:]) {
			size := binary.LittleEndian.Uint32(hdr[12:16])
			flags := binary.LittleEndian.Uint32(hdr[20:24])
			total := int64(size) // footer-relative size (includes footer)
			if flags&FlagHasHeader != 0 {
				total += 32 // also count the header
			}
			audioEnd -= total
		}
	}
	return audioEnd, id3v1, nil
}

// --- convenience accessors -------------------------------------

// Standard APEv2 key names per the spec.
const (
	KeyTitle       = "Title"
	KeyArtist      = "Artist"
	KeyAlbum       = "Album"
	KeyAlbumArtist = "Album Artist"
	KeyYear        = "Year"
	KeyTrack       = "Track"
	KeyDisc        = "Disc"
	KeyGenre       = "Genre"
	KeyComposer    = "Composer"
	KeyComment     = "Comment"
)

func (t *Tag) Title() string       { return t.Get(KeyTitle) }
func (t *Tag) Artist() string      { return t.Get(KeyArtist) }
func (t *Tag) Album() string       { return t.Get(KeyAlbum) }
func (t *Tag) AlbumArtist() string { return t.Get(KeyAlbumArtist) }
func (t *Tag) Composer() string    { return t.Get(KeyComposer) }
func (t *Tag) Genre() string       { return t.Get(KeyGenre) }
func (t *Tag) Comment() string     { return t.Get(KeyComment) }

func (t *Tag) Year() int {
	s := t.Get(KeyYear)
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

func (t *Tag) TrackNumber() (n, total int) { return parseSlashed(t.Get(KeyTrack)) }
func (t *Tag) DiscNumber() (n, total int)  { return parseSlashed(t.Get(KeyDisc)) }

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

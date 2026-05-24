package mp4

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// Standard iTunes 4-byte keys. The leading 0xA9 byte is the
// "©" prefix used by all of the so-called "common metadata" atoms;
// each constant below is exactly 4 bytes long.
const (
	KeyTitle       = "\xa9nam"
	KeyArtist      = "\xa9ART"
	KeyAlbum       = "\xa9alb"
	KeyDate        = "\xa9day"
	KeyComment     = "\xa9cmt"
	KeyComposer    = "\xa9wrt"
	KeyEncoder     = "\xa9too"
	KeyGenreText   = "\xa9gen"
	KeyAlbumArtist = "aART"
	KeyTrack       = "trkn"
	KeyDisc        = "disk"
	KeyCover       = "covr"
	KeyGenreIndex  = "gnre"
	KeyBPM         = "tmpo"
	KeyCompilation = "cpil"
)

// Item is one ilst entry. Standard items have a 4-char Key and one
// or more data atoms (multiple data atoms are rare; iTunes normally
// emits exactly one). Freeform items use Key="----" together with
// MeanDomain and Name.
type Item struct {
	Key        string // 4-char standard key, or "----" for freeform
	MeanDomain string // freeform only, e.g. "com.apple.iTunes"
	Name       string // freeform only, e.g. "iTunNORM"
	Data       []*DataAtom
}

// Ilst is the parsed content of the moov/udta/meta/ilst box.
type Ilst struct {
	Items []*Item
}

func parseIlst(body []byte) (*Ilst, error) {
	out := &Ilst{}
	pos := 0
	for pos < len(body) {
		if pos+8 > len(body) {
			return nil, fmt.Errorf("mp4: ilst entry header truncated at %d", pos)
		}
		size := binary.BigEndian.Uint32(body[pos : pos+4])
		var typ FourCC
		copy(typ[:], body[pos+4:pos+8])
		if size < 8 || int(size) > len(body)-pos {
			return nil, fmt.Errorf("mp4: ilst entry %s size %d out of range", typ, size)
		}
		entryBody := body[pos+8 : pos+int(size)]
		pos += int(size)

		item, err := parseIlstEntry(typ, entryBody)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func parseIlstEntry(typ FourCC, body []byte) (*Item, error) {
	item := &Item{Key: typ.String()}
	// The body of an ilst entry is itself a sequence of child boxes:
	//   - "data" (one or more) for standard keys
	//   - "mean", "name", and "data" children for freeform "----"
	pos := 0
	for pos < len(body) {
		if pos+8 > len(body) {
			return nil, fmt.Errorf("mp4: ilst child header truncated in %s at %d", typ, pos)
		}
		size := binary.BigEndian.Uint32(body[pos : pos+4])
		var ctyp FourCC
		copy(ctyp[:], body[pos+4:pos+8])
		if size < 8 || int(size) > len(body)-pos {
			return nil, fmt.Errorf("mp4: ilst child %s size %d in %s out of range", ctyp, size, typ)
		}
		childBody := body[pos+8 : pos+int(size)]
		pos += int(size)

		switch ctyp.String() {
		case "data":
			d, err := parseDataAtom(childBody)
			if err != nil {
				return nil, fmt.Errorf("mp4: %s/data: %w", typ, err)
			}
			item.Data = append(item.Data, d)
		case "mean":
			if len(childBody) < 4 {
				return nil, fmt.Errorf("mp4: ----/mean too short")
			}
			item.MeanDomain = string(childBody[4:])
		case "name":
			if len(childBody) < 4 {
				return nil, fmt.Errorf("mp4: ----/name too short")
			}
			item.Name = string(childBody[4:])
		default:
			// Unknown child: skip silently for forward compatibility.
		}
	}
	return item, nil
}

// encode returns the bytes of the ilst body (no header). Each item
// is emitted as a sized box of type Key (or "----") containing the
// child boxes in standard order.
func (l *Ilst) encode() ([]byte, error) {
	var out bytes.Buffer
	for _, item := range l.Items {
		body, err := item.encode()
		if err != nil {
			return nil, err
		}
		key := item.Key
		if key == "" {
			return nil, fmt.Errorf("mp4: ilst item with empty Key")
		}
		var keyCC FourCC
		copy(keyCC[:], key)
		if err := writeBox(&out, keyCC, body); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func (i *Item) encode() ([]byte, error) {
	var out bytes.Buffer
	if i.Key == "----" {
		if i.MeanDomain == "" || i.Name == "" {
			return nil, fmt.Errorf("mp4: freeform item missing mean/name")
		}
		// mean / name children carry a 4-byte version+flags prefix.
		mean := append([]byte{0, 0, 0, 0}, []byte(i.MeanDomain)...)
		if err := writeBox(&out, fourCC("mean"), mean); err != nil {
			return nil, err
		}
		name := append([]byte{0, 0, 0, 0}, []byte(i.Name)...)
		if err := writeBox(&out, fourCC("name"), name); err != nil {
			return nil, err
		}
	}
	for _, d := range i.Data {
		if err := writeBox(&out, fourCC("data"), d.encode()); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// First returns the first data atom of the named key, or nil.
//
// First is intended for the standard 4-character keys. Calling it with
// "----" returns the data atom of whichever freeform item happens to come
// first, which is rarely what callers want; use FirstFreeform instead.
func (l *Ilst) First(key string) *DataAtom {
	for _, it := range l.Items {
		if it.Key == key && len(it.Data) > 0 {
			return it.Data[0]
		}
	}
	return nil
}

// Set replaces every existing item for key with one carrying a
// single data atom. Pass nil to remove the key entirely.
//
// Set is intended for the standard 4-character keys. Calling it with
// "----" removes every freeform item regardless of mean/name and emits a
// new item with MeanDomain/Name empty, which is invalid per spec; use
// SetFreeform to address one specific freeform item by (mean, name).
func (l *Ilst) Set(key string, data *DataAtom) {
	l.Remove(key)
	if data == nil {
		return
	}
	l.Items = append(l.Items, &Item{Key: key, Data: []*DataAtom{data}})
}

// Remove deletes every item whose Key equals key.
//
// Remove is intended for the standard 4-character keys. Calling it with
// "----" deletes every freeform item indiscriminately; use
// RemoveFreeform to delete just one specific (mean, name) pair.
func (l *Ilst) Remove(key string) {
	out := l.Items[:0]
	for _, it := range l.Items {
		if it.Key != key {
			out = append(out, it)
		}
	}
	l.Items = out
}

// FirstFreeform returns the first data atom of the freeform "----" item
// identified by (meanDomain, name), or nil if no matching item exists.
//
// Freeform atoms encode tagger-specific fields that the standard
// 4-character keys cannot express. For example, the Mixed In Key /
// Music.app "Initial Key" tag lives at
// (meanDomain="com.apple.iTunes", name="initialkey").
func (l *Ilst) FirstFreeform(meanDomain, name string) *DataAtom {
	for _, it := range l.Items {
		if it.Key == "----" && it.MeanDomain == meanDomain && it.Name == name && len(it.Data) > 0 {
			return it.Data[0]
		}
	}
	return nil
}

// SetFreeform replaces the freeform "----" item identified by
// (meanDomain, name) with one carrying a single data atom. Other
// freeform items (different mean/name pairs) are left untouched.
// Passing data=nil removes only the matching (mean, name) item, which
// is equivalent to RemoveFreeform. An empty meanDomain or name is a
// no-op, since the resulting item would be invalid per spec and
// would fail at encode time.
func (l *Ilst) SetFreeform(meanDomain, name string, data *DataAtom) {
	if meanDomain == "" || name == "" {
		return
	}
	l.RemoveFreeform(meanDomain, name)
	if data == nil {
		return
	}
	l.Items = append(l.Items, &Item{
		Key:        "----",
		MeanDomain: meanDomain,
		Name:       name,
		Data:       []*DataAtom{data},
	})
}

// RemoveFreeform deletes every freeform "----" item whose
// (MeanDomain, Name) matches the arguments. Standard 4-character items
// and freeform items with a different (mean, name) pair are kept.
func (l *Ilst) RemoveFreeform(meanDomain, name string) {
	out := l.Items[:0]
	for _, it := range l.Items {
		if it.Key == "----" && it.MeanDomain == meanDomain && it.Name == name {
			continue
		}
		out = append(out, it)
	}
	l.Items = out
}

// Items by key: convenience read accessors.

func (l *Ilst) Title() string       { return firstString(l, KeyTitle) }
func (l *Ilst) Artist() string      { return firstString(l, KeyArtist) }
func (l *Ilst) Album() string       { return firstString(l, KeyAlbum) }
func (l *Ilst) AlbumArtist() string { return firstString(l, KeyAlbumArtist) }
func (l *Ilst) Composer() string    { return firstString(l, KeyComposer) }
func (l *Ilst) GenreText() string   { return firstString(l, KeyGenreText) }
func (l *Ilst) Comment() string     { return firstString(l, KeyComment) }
func (l *Ilst) Date() string        { return firstString(l, KeyDate) }

// Year returns the leading 4-digit year of the ©day field, or 0.
func (l *Ilst) Year() int {
	s := l.Date()
	if len(s) < 4 {
		return 0
	}
	year := 0
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		year = year*10 + int(c-'0')
	}
	return year
}

func (l *Ilst) Track() (n, total uint16) {
	d := l.First(KeyTrack)
	if d == nil {
		return 0, 0
	}
	n, total, _ = d.TrackNumber()
	return
}

func (l *Ilst) Disc() (n, total uint16) {
	d := l.First(KeyDisc)
	if d == nil {
		return 0, 0
	}
	n, total, _ = d.TrackNumber()
	return
}

// Pictures returns every covr data atom in ilst order.
func (l *Ilst) Pictures() []*DataAtom {
	var out []*DataAtom
	for _, it := range l.Items {
		if it.Key != KeyCover {
			continue
		}
		out = append(out, it.Data...)
	}
	return out
}

// SetText sets a UTF-8 textual key, replacing any existing value.
// An empty string removes the key.
func (l *Ilst) SetText(key, value string) {
	if value == "" {
		l.Remove(key)
		return
	}
	l.Set(key, makeUTF8Data(value))
}

func (l *Ilst) SetTitle(s string)       { l.SetText(KeyTitle, s) }
func (l *Ilst) SetArtist(s string)      { l.SetText(KeyArtist, s) }
func (l *Ilst) SetAlbum(s string)       { l.SetText(KeyAlbum, s) }
func (l *Ilst) SetAlbumArtist(s string) { l.SetText(KeyAlbumArtist, s) }
func (l *Ilst) SetComposer(s string)    { l.SetText(KeyComposer, s) }
func (l *Ilst) SetGenreText(s string)   { l.SetText(KeyGenreText, s) }
func (l *Ilst) SetComment(s string)     { l.SetText(KeyComment, s) }
func (l *Ilst) SetDate(s string)        { l.SetText(KeyDate, s) }

// SetTrack writes the trkn box. Pass total=0 to leave the total
// slot unset.
func (l *Ilst) SetTrack(number, total uint16) {
	l.Set(KeyTrack, makeTrackNumberData(number, total))
}

// SetDisc writes the disk box.
func (l *Ilst) SetDisc(number, total uint16) {
	l.Set(KeyDisc, makeTrackNumberData(number, total))
}

// AddCover appends a cover-art image. The MIME is detected from the
// data magic and the data atom is typed accordingly (JPEG vs PNG).
// If the magic is unrecognised the atom is typed as binary.
func (l *Ilst) AddCover(data []byte) {
	d := &DataAtom{TypeCode: DataTypeBinary, Payload: append([]byte(nil), data...)}
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		d.TypeCode = DataTypeJPEG
	case len(data) >= 8 && string(data[0:8]) == "\x89PNG\r\n\x1a\n":
		d.TypeCode = DataTypePNG
	}
	l.Items = append(l.Items, &Item{Key: KeyCover, Data: []*DataAtom{d}})
}

func firstString(l *Ilst, key string) string {
	d := l.First(key)
	if d == nil {
		return ""
	}
	if d.TypeCode != DataTypeUTF8 {
		// Some tools emit UTF-16; we don't support that on read here.
		return strings.TrimRight(string(d.Payload), "\x00")
	}
	return d.String()
}

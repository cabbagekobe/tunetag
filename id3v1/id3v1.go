// Package id3v1 reads and writes ID3v1 / ID3v1.1 trailer tags
// (the fixed 128-byte block at the end of an MP3 file).
//
// ID3v1.0 packs Title, Artist, Album, Year (4 chars), a 30-byte
// Comment and a 1-byte Genre. ID3v1.1 reuses the last two bytes of
// the Comment field as a track number when byte 28 is zero and byte
// 29 is non-zero; this package detects that on read and re-emits the
// v1.1 layout when Track is non-zero.
//
// A *Tag is not safe for concurrent use.
package id3v1

import (
	"errors"
	"io"
	"os"
)

// TagSize is the on-disk size of an ID3v1 trailer in bytes.
const TagSize = 128

// GenreNone is the sentinel value used when no genre is set.
const GenreNone uint8 = 255

// ErrNoTag is returned by Read when the input does not end in an
// ID3v1 trailer.
var ErrNoTag = errors.New("id3v1: no tag found")

var magic = [3]byte{'T', 'A', 'G'}

// Tag is the parsed contents of an ID3v1 trailer. Fields longer than
// the on-disk slot are silently truncated by Encode.
type Tag struct {
	Title   string // up to 30 bytes
	Artist  string // up to 30 bytes
	Album   string // up to 30 bytes
	Year    string // up to 4 bytes
	Comment string // up to 30 bytes (28 when Track != 0)
	Track   uint8  // 0 means ID3v1.0 (no track byte)
	Genre   uint8  // 255 = none; see Genres for names
}

// Read parses the last 128 bytes of rs as an ID3v1 trailer. If the
// stream is shorter than 128 bytes or does not end in "TAG", ErrNoTag
// is returned.
func Read(rs io.ReadSeeker) (*Tag, error) {
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if end < TagSize {
		return nil, ErrNoTag
	}
	if _, err := rs.Seek(-TagSize, io.SeekEnd); err != nil {
		return nil, err
	}
	var buf [TagSize]byte
	if _, err := io.ReadFull(rs, buf[:]); err != nil {
		return nil, err
	}
	if buf[0] != 'T' || buf[1] != 'A' || buf[2] != 'G' {
		return nil, ErrNoTag
	}
	t := &Tag{
		Title:  trimPad(buf[3:33]),
		Artist: trimPad(buf[33:63]),
		Album:  trimPad(buf[63:93]),
		Year:   trimPad(buf[93:97]),
		Genre:  buf[127],
	}
	cmt := buf[97:127]
	if cmt[28] == 0 && cmt[29] != 0 {
		t.Comment = trimPad(cmt[:28])
		t.Track = cmt[29]
	} else {
		t.Comment = trimPad(cmt)
	}
	return t, nil
}

// ReadFile is a convenience wrapper around Read for filesystem paths.
func ReadFile(path string) (*Tag, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Read(f)
}

// Encode writes a 128-byte ID3v1 trailer to w. The v1.1 layout is
// used when Track is non-zero; otherwise v1.0 is used.
func (t *Tag) Encode(w io.Writer) error {
	var buf [TagSize]byte
	copy(buf[0:3], "TAG")
	writeFixed(buf[3:33], t.Title)
	writeFixed(buf[33:63], t.Artist)
	writeFixed(buf[63:93], t.Album)
	writeFixed(buf[93:97], t.Year)
	if t.Track != 0 {
		writeFixed(buf[97:125], t.Comment)
		buf[125] = 0
		buf[126] = t.Track
	} else {
		writeFixed(buf[97:127], t.Comment)
	}
	buf[127] = t.Genre
	_, err := w.Write(buf[:])
	return err
}

// WriteFile writes t into the trailing 128 bytes of path. If the
// file already ends in an ID3v1 tag the trailer is overwritten in
// place; otherwise the new trailer is appended. The audio body is
// never touched.
func (t *Tag) WriteFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	hasTag, err := hasTrailer(f)
	if err != nil {
		return err
	}
	if hasTag {
		if _, err := f.Seek(-TagSize, io.SeekEnd); err != nil {
			return err
		}
	} else {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
	}
	return t.Encode(f)
}

// StripFile removes any ID3v1 trailer from path. It is a no-op when
// the file has no trailer.
func StripFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	hasTag, err := hasTrailer(f)
	if err != nil {
		return err
	}
	if !hasTag {
		return nil
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return f.Truncate(info.Size() - TagSize)
}

// GenreName returns the textual name for t.Genre, or "" when the
// code is outside the known table (192-254 and the "None" sentinel
// 255).
func (t *Tag) GenreName() string {
	if int(t.Genre) >= len(Genres) {
		return ""
	}
	return Genres[t.Genre]
}

func hasTrailer(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() < TagSize {
		return false, nil
	}
	if _, err := f.Seek(-TagSize, io.SeekEnd); err != nil {
		return false, err
	}
	var marker [3]byte
	if _, err := io.ReadFull(f, marker[:]); err != nil {
		return false, err
	}
	return marker == magic, nil
}

func trimPad(b []byte) string {
	end := len(b)
	for end > 0 && (b[end-1] == 0 || b[end-1] == ' ') {
		end--
	}
	return string(b[:end])
}

func writeFixed(dst []byte, s string) {
	n := copy(dst, s)
	for i := n; i < len(dst); i++ {
		dst[i] = 0
	}
}

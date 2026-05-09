package tunetag

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
)

// Detect inspects the start (and, when needed, the end) of rs to
// identify the container format. The seek position of rs is restored
// before returning when possible.
func Detect(rs io.ReadSeeker) (Format, error) {
	cur, _ := rs.Seek(0, io.SeekCurrent)
	defer rs.Seek(cur, io.SeekStart)

	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, err
	}
	var hdr [12]byte
	n, err := io.ReadFull(rs, hdr[:])
	// Short streams are common and not an error: just sniff what we
	// got and decide based on the bytes that did arrive.
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return FormatUnknown, err
	}
	if n >= 3 && hdr[0] == 'I' && hdr[1] == 'D' && hdr[2] == '3' {
		return FormatID3v2, nil
	}
	if n >= 4 && string(hdr[0:4]) == "fLaC" {
		return FormatFLAC, nil
	}
	if n >= 8 && string(hdr[4:8]) == "ftyp" {
		return FormatMP4, nil
	}
	// Fall back to ID3v1 trailer detection.
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return FormatUnknown, err
	}
	if end >= id3v1.TagSize {
		if _, err := rs.Seek(end-id3v1.TagSize, io.SeekStart); err != nil {
			return FormatUnknown, err
		}
		var marker [3]byte
		if _, err := io.ReadFull(rs, marker[:]); err == nil && marker[0] == 'T' && marker[1] == 'A' && marker[2] == 'G' {
			return FormatID3v1, nil
		}
	}
	return FormatUnknown, ErrUnknownFormat
}

// Open auto-detects the container at path and returns a read-only
// Tag for the most informative metadata block in the file.
//
//   - MP3: ID3v2 if present, otherwise ID3v1.
//   - FLAC / MP4: the corresponding format-specific reader.
//
// For format-specific writes, use OpenMP3, OpenFLAC, or OpenMP4
// directly.
func Open(path string) (Tag, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	format, err := Detect(f)
	if err != nil {
		return nil, err
	}
	switch format {
	case FormatID3v2:
		t, err := id3v2.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &mp3Tag{v2: t}, nil
	case FormatID3v1:
		t, err := id3v1.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &mp3Tag{v1: t}, nil
	case FormatFLAC:
		fl, err := flac.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &flacTag{f: fl}, nil
	case FormatMP4:
		m, err := mp4.Read(path)
		if err != nil {
			return nil, err
		}
		return &mp4Tag{f: m}, nil
	}
	return nil, ErrUnknownFormat
}

// OpenMP3 reads either the ID3v2 tag at the start of path or, if
// none exists, falls back to the ID3v1 trailer. The returned tag
// always carries the parsed v2 frames; the v1 fallback is exposed
// via the V1 field for inspection but writes always go through the
// v2 representation.
type MP3 struct {
	V2 *id3v2.Tag // may be nil if the file had only an ID3v1 trailer
	V1 *id3v1.Tag // may be nil
}

// OpenMP3 returns the parsed ID3v2 (preferred) and/or ID3v1 tag
// found in path.
func OpenMP3(path string) (*MP3, error) {
	out := &MP3{}
	v2, err := id3v2.ReadFile(path)
	if err != nil && !errors.Is(err, id3v2.ErrNoTag) {
		return nil, err
	}
	out.V2 = v2

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	v1, err := id3v1.Read(f)
	if err != nil && !errors.Is(err, id3v1.ErrNoTag) {
		return nil, err
	}
	out.V1 = v1
	if out.V2 == nil && out.V1 == nil {
		return nil, ErrUnknownFormat
	}
	return out, nil
}

// OpenFLAC opens a FLAC file for read-write metadata access.
func OpenFLAC(path string) (*flac.File, error) {
	return flac.ReadFile(path)
}

// OpenMP4 opens an MP4 / M4A file for read-write metadata access.
func OpenMP4(path string) (*mp4.File, error) {
	return mp4.Read(path)
}

// Strip removes every metadata block at path, leaving the audio
// body intact. The format is auto-detected; ID3v2 is removed by
// rewriting the file without the leading tag, ID3v1 by truncating
// the trailer, FLAC by replacing all metadata blocks with a single
// empty STREAMINFO + minimal padding, and MP4 by emptying ilst.
func Strip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	format, err := Detect(f)
	f.Close()
	if err != nil {
		return err
	}
	switch format {
	case FormatID3v2:
		return stripID3v2(path)
	case FormatID3v1:
		return id3v1.StripFile(path)
	case FormatFLAC:
		return stripFLAC(path)
	case FormatMP4:
		return stripMP4(path)
	}
	return ErrUnknownFormat
}

func stripID3v2(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	t, err := id3v2.Read(src)
	if err != nil {
		return err
	}
	// Keep file as a tagless audio stream by writing zero frames and
	// zero padding via WriteFile, then re-opening to truncate down.
	t.Frames = nil
	t.Padding = 0
	return t.WriteFile(path)
}

func stripFLAC(path string) error {
	src, err := flac.ReadFile(path)
	if err != nil {
		return err
	}
	// Keep STREAMINFO; drop everything else.
	if len(src.Blocks) == 0 {
		return errors.New("tunetag: FLAC file has no blocks to keep")
	}
	src.Blocks = []flac.Block{src.Blocks[0]}
	return src.WriteFile(path)
}

func stripMP4(path string) error {
	src, err := mp4.Read(path)
	if err != nil {
		return err
	}
	src.Tag.Items = nil
	return src.WriteFile(path)
}

// Format-specific wrappers that satisfy the Tag interface.

type mp3Tag struct {
	v1 *id3v1.Tag
	v2 *id3v2.Tag
}

func (m *mp3Tag) Title() string {
	if m.v2 != nil && m.v2.Title() != "" {
		return m.v2.Title()
	}
	if m.v1 != nil {
		return m.v1.Title
	}
	return ""
}
func (m *mp3Tag) Artist() string {
	if m.v2 != nil && m.v2.Artist() != "" {
		return m.v2.Artist()
	}
	if m.v1 != nil {
		return m.v1.Artist
	}
	return ""
}
func (m *mp3Tag) AlbumArtist() string {
	if m.v2 != nil {
		return m.v2.AlbumArtist()
	}
	return ""
}
func (m *mp3Tag) Album() string {
	if m.v2 != nil && m.v2.Album() != "" {
		return m.v2.Album()
	}
	if m.v1 != nil {
		return m.v1.Album
	}
	return ""
}
func (m *mp3Tag) Year() int {
	if m.v2 != nil && m.v2.Year() != 0 {
		return m.v2.Year()
	}
	if m.v1 != nil && m.v1.Year != "" {
		var y int
		fmt.Sscanf(m.v1.Year, "%d", &y)
		return y
	}
	return 0
}
func (m *mp3Tag) TrackNumber() (n, total int) {
	if m.v2 != nil {
		n, total = m.v2.TrackNumber()
		if n != 0 || total != 0 {
			return
		}
	}
	if m.v1 != nil && m.v1.Track != 0 {
		return int(m.v1.Track), 0
	}
	return 0, 0
}
func (m *mp3Tag) DiscNumber() (n, total int) {
	if m.v2 != nil {
		return m.v2.DiscNumber()
	}
	return 0, 0
}
func (m *mp3Tag) Genre() string {
	if m.v2 != nil && m.v2.Genre() != "" {
		return m.v2.Genre()
	}
	if m.v1 != nil {
		return m.v1.GenreName()
	}
	return ""
}
func (m *mp3Tag) Composer() string {
	if m.v2 != nil {
		return m.v2.Composer()
	}
	return ""
}
func (m *mp3Tag) Comment() string {
	if m.v2 != nil && m.v2.Comment() != "" {
		return m.v2.Comment()
	}
	if m.v1 != nil {
		return m.v1.Comment
	}
	return ""
}
func (m *mp3Tag) Pictures() []Picture {
	if m.v2 == nil {
		return nil
	}
	frames := m.v2.PictureFrames()
	out := make([]Picture, 0, len(frames))
	for _, p := range frames {
		out = append(out, Picture{
			MIME:        p.MIME,
			Type:        PictureType(p.PictureType),
			Description: p.Description,
			Data:        p.Data,
		})
	}
	return out
}
func (m *mp3Tag) Format() Format {
	if m.v2 != nil {
		return FormatID3v2
	}
	return FormatID3v1
}

type flacTag struct {
	f *flac.File
}

func (f *flacTag) vc() *flac.VorbisComment {
	for _, b := range f.f.Blocks {
		if vc, ok := b.(*flac.VorbisComment); ok {
			return vc
		}
	}
	return nil
}

func (f *flacTag) Title() string       { return firstVC(f.vc(), "TITLE") }
func (f *flacTag) Artist() string      { return firstVC(f.vc(), "ARTIST") }
func (f *flacTag) AlbumArtist() string { return firstVC(f.vc(), "ALBUMARTIST") }
func (f *flacTag) Album() string       { return firstVC(f.vc(), "ALBUM") }
func (f *flacTag) Composer() string    { return firstVC(f.vc(), "COMPOSER") }
func (f *flacTag) Genre() string       { return firstVC(f.vc(), "GENRE") }
func (f *flacTag) Comment() string     { return firstVC(f.vc(), "DESCRIPTION") }
func (f *flacTag) Year() int {
	s := firstVC(f.vc(), "DATE")
	if len(s) < 4 {
		return 0
	}
	var y int
	fmt.Sscanf(s[:4], "%d", &y)
	return y
}
func (f *flacTag) TrackNumber() (n, total int) {
	return numTotal(firstVC(f.vc(), "TRACKNUMBER"), firstVC(f.vc(), "TRACKTOTAL"))
}
func (f *flacTag) DiscNumber() (n, total int) {
	return numTotal(firstVC(f.vc(), "DISCNUMBER"), firstVC(f.vc(), "DISCTOTAL"))
}
func (f *flacTag) Pictures() []Picture {
	pics := f.f.Pictures()
	out := make([]Picture, 0, len(pics))
	for _, p := range pics {
		out = append(out, Picture{
			MIME:        p.MIME,
			Type:        PictureType(p.PictureType),
			Description: p.Description,
			Data:        p.Data,
		})
	}
	return out
}
func (f *flacTag) Format() Format { return FormatFLAC }

type mp4Tag struct {
	f *mp4.File
}

func (m *mp4Tag) Title() string       { return m.f.Tag.Title() }
func (m *mp4Tag) Artist() string      { return m.f.Tag.Artist() }
func (m *mp4Tag) AlbumArtist() string { return m.f.Tag.AlbumArtist() }
func (m *mp4Tag) Album() string       { return m.f.Tag.Album() }
func (m *mp4Tag) Composer() string    { return m.f.Tag.Composer() }
func (m *mp4Tag) Genre() string       { return m.f.Tag.GenreText() }
func (m *mp4Tag) Year() int           { return m.f.Tag.Year() }
func (m *mp4Tag) Comment() string     { return m.f.Tag.Comment() }
func (m *mp4Tag) TrackNumber() (n, total int) {
	a, b := m.f.Tag.Track()
	return int(a), int(b)
}
func (m *mp4Tag) DiscNumber() (n, total int) {
	a, b := m.f.Tag.Disc()
	return int(a), int(b)
}
func (m *mp4Tag) Pictures() []Picture {
	pics := m.f.Tag.Pictures()
	out := make([]Picture, 0, len(pics))
	for _, p := range pics {
		mime := ""
		switch p.TypeCode {
		case mp4.DataTypeJPEG:
			mime = "image/jpeg"
		case mp4.DataTypePNG:
			mime = "image/png"
		}
		out = append(out, Picture{MIME: mime, Data: p.Payload})
	}
	return out
}
func (m *mp4Tag) Format() Format { return FormatMP4 }

func firstVC(vc *flac.VorbisComment, key string) string {
	if vc == nil {
		return ""
	}
	return vc.First(key)
}

func numTotal(num, total string) (n, t int) {
	if num != "" {
		fmt.Sscanf(num, "%d", &n)
	}
	if total != "" {
		fmt.Sscanf(total, "%d", &t)
	}
	return
}

// Compile-time check that the wrappers all satisfy Tag.
var (
	_ Tag = (*mp3Tag)(nil)
	_ Tag = (*flacTag)(nil)
	_ Tag = (*mp4Tag)(nil)
)

package tunetag

import (
	"fmt"

	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
)

// mp3Tag adapts an MP3's ID3v2 and/or ID3v1 tag to the read-only
// Tag interface. ID3v2 takes precedence when both are present.
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

// flacTag adapts a *flac.File's Vorbis comments and pictures to the
// read-only Tag interface.
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

// mp4Tag adapts a *mp4.File to the read-only Tag interface.
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

// firstVC returns the first value for key in vc, or "" if vc is
// nil or the key is absent.
func firstVC(vc *flac.VorbisComment, key string) string {
	if vc == nil {
		return ""
	}
	return vc.First(key)
}

// numTotal parses two stringified integers into (n, t).
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

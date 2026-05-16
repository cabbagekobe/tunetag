package tunetag

import (
	"fmt"

	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/ogg"
	"github.com/cabbagekobe/tunetag/wav"
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

// wavTag adapts a *wav.File to the read-only Tag interface. The
// underlying wav.File already arbitrates between the embedded
// ID3v2 chunk and LIST/INFO entries (ID3v2 wins), so this wrapper
// is a thin forwarding layer.
type wavTag struct {
	f *wav.File
}

func (w *wavTag) Title() string       { return w.f.Title() }
func (w *wavTag) Artist() string      { return w.f.Artist() }
func (w *wavTag) AlbumArtist() string { return w.f.AlbumArtist() }
func (w *wavTag) Album() string       { return w.f.Album() }
func (w *wavTag) Composer() string    { return w.f.Composer() }
func (w *wavTag) Genre() string       { return w.f.Genre() }
func (w *wavTag) Year() int           { return w.f.Year() }
func (w *wavTag) Comment() string     { return w.f.Comment() }

func (w *wavTag) TrackNumber() (n, total int) { return w.f.TrackNumber() }
func (w *wavTag) DiscNumber() (n, total int)  { return w.f.DiscNumber() }

func (w *wavTag) Pictures() []Picture {
	frames := w.f.Pictures()
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

func (w *wavTag) Format() Format { return FormatWAV }

// aiffTag adapts *aiff.File to the read-only Tag interface.
type aiffTag struct{ f *aiff.File }

func (a *aiffTag) Title() string               { return a.f.Title() }
func (a *aiffTag) Artist() string              { return a.f.Artist() }
func (a *aiffTag) AlbumArtist() string         { return a.f.AlbumArtist() }
func (a *aiffTag) Album() string               { return a.f.Album() }
func (a *aiffTag) Composer() string            { return a.f.Composer() }
func (a *aiffTag) Genre() string               { return a.f.Genre() }
func (a *aiffTag) Year() int                   { return a.f.Year() }
func (a *aiffTag) Comment() string             { return a.f.Comment() }
func (a *aiffTag) TrackNumber() (n, total int) { return a.f.TrackNumber() }
func (a *aiffTag) DiscNumber() (n, total int)  { return a.f.DiscNumber() }
func (a *aiffTag) Format() Format              { return FormatAIFF }

func (a *aiffTag) Pictures() []Picture {
	return wrapPictureFrames(a.f.Pictures())
}

// oggTag adapts *ogg.File to the read-only Tag interface. Ogg
// has no embedded pictures via Vorbis Comment (the
// METADATA_BLOCK_PICTURE convention is FLAC-specific in this
// codebase) so Pictures always returns nil.
type oggTag struct{ f *ogg.File }

func (o *oggTag) Title() string               { return o.f.Title() }
func (o *oggTag) Artist() string              { return o.f.Artist() }
func (o *oggTag) AlbumArtist() string         { return o.f.AlbumArtist() }
func (o *oggTag) Album() string               { return o.f.Album() }
func (o *oggTag) Composer() string            { return o.f.Composer() }
func (o *oggTag) Genre() string               { return o.f.Genre() }
func (o *oggTag) Year() int                   { return o.f.Year() }
func (o *oggTag) Comment() string             { return o.f.Comment() }
func (o *oggTag) TrackNumber() (n, total int) { return o.f.TrackNumber() }
func (o *oggTag) DiscNumber() (n, total int)  { return o.f.DiscNumber() }
func (o *oggTag) Pictures() []Picture         { return nil }
func (o *oggTag) Format() Format              { return FormatOgg }

// apeTag adapts *ape.Tag to the read-only Tag interface.
type apeTag struct{ t *ape.Tag }

func (a *apeTag) Title() string               { return a.t.Title() }
func (a *apeTag) Artist() string              { return a.t.Artist() }
func (a *apeTag) AlbumArtist() string         { return a.t.AlbumArtist() }
func (a *apeTag) Album() string               { return a.t.Album() }
func (a *apeTag) Composer() string            { return a.t.Composer() }
func (a *apeTag) Genre() string               { return a.t.Genre() }
func (a *apeTag) Year() int                   { return a.t.Year() }
func (a *apeTag) Comment() string             { return a.t.Comment() }
func (a *apeTag) TrackNumber() (n, total int) { return a.t.TrackNumber() }
func (a *apeTag) DiscNumber() (n, total int)  { return a.t.DiscNumber() }
func (a *apeTag) Pictures() []Picture         { return nil }
func (a *apeTag) Format() Format              { return FormatAPE }

// aacTag adapts *aac.File to the read-only Tag interface.
type aacTag struct{ f *aac.File }

func (a *aacTag) Title() string               { return a.f.Title() }
func (a *aacTag) Artist() string              { return a.f.Artist() }
func (a *aacTag) AlbumArtist() string         { return a.f.AlbumArtist() }
func (a *aacTag) Album() string               { return a.f.Album() }
func (a *aacTag) Composer() string            { return a.f.Composer() }
func (a *aacTag) Genre() string               { return a.f.Genre() }
func (a *aacTag) Year() int                   { return a.f.Year() }
func (a *aacTag) Comment() string             { return a.f.Comment() }
func (a *aacTag) TrackNumber() (n, total int) { return a.f.TrackNumber() }
func (a *aacTag) DiscNumber() (n, total int)  { return a.f.DiscNumber() }
func (a *aacTag) Format() Format              { return FormatAAC }

func (a *aacTag) Pictures() []Picture {
	return wrapPictureFrames(a.f.Pictures())
}

// wrapPictureFrames converts id3v2 picture frames into the
// container-agnostic Picture type. Used by every wrapper whose
// underlying tag is (or contains) an ID3v2 tag.
func wrapPictureFrames(frames []*id3v2.PictureFrame) []Picture {
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

// Compile-time check that the wrappers all satisfy Tag.
var (
	_ Tag = (*mp3Tag)(nil)
	_ Tag = (*flacTag)(nil)
	_ Tag = (*mp4Tag)(nil)
	_ Tag = (*wavTag)(nil)
	_ Tag = (*aiffTag)(nil)
	_ Tag = (*oggTag)(nil)
	_ Tag = (*apeTag)(nil)
	_ Tag = (*aacTag)(nil)
)

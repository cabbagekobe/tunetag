package main

import (
	"flag"
	"fmt"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/asf"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/ogg"
	"github.com/cabbagekobe/tunetag/wav"
)

type setFlags struct {
	title, artist, album, year, genre, track, disc, composer, comment string
}

func cmdSet(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("set: file argument required")
	}
	path := args[0]
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	var sf setFlags
	fs.StringVar(&sf.title, "title", "", "set title")
	fs.StringVar(&sf.artist, "artist", "", "set artist")
	fs.StringVar(&sf.album, "album", "", "set album")
	fs.StringVar(&sf.year, "year", "", "set 4-digit year")
	fs.StringVar(&sf.genre, "genre", "", "set genre")
	fs.StringVar(&sf.track, "track", "", "set track number (e.g. 3 or 3/12)")
	fs.StringVar(&sf.disc, "disc", "", "set disc number (e.g. 1 or 1/2)")
	fs.StringVar(&sf.composer, "composer", "", "set composer")
	fs.StringVar(&sf.comment, "comment", "", "set comment")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	format, err := detect(path)
	if err != nil {
		return err
	}
	switch format {
	case tunetag.FormatID3v1, tunetag.FormatID3v2:
		return setMP3(path, &sf, format)
	case tunetag.FormatFLAC:
		return setFLAC(path, &sf)
	case tunetag.FormatMP4:
		return setMP4(path, &sf)
	case tunetag.FormatWAV:
		return setWAV(path, &sf)
	case tunetag.FormatAIFF:
		return setAIFF(path, &sf)
	case tunetag.FormatAPE:
		return setAPE(path, &sf)
	case tunetag.FormatAAC:
		return setAAC(path, &sf)
	case tunetag.FormatASF:
		return setASF(path, &sf)
	case tunetag.FormatOgg:
		return setOgg(path, &sf)
	}
	return fmt.Errorf("set: unsupported format %s", format)
}

func setMP3(path string, sf *setFlags, source tunetag.Format) error {
	t, err := id3v2.ReadFile(path)
	if err != nil {
		// No v2 tag yet; build one from scratch.
		t = &id3v2.Tag{Version: id3v2.V24, Padding: id3v2.DefaultPadding}
		_ = source
	}
	if sf.title != "" {
		t.SetTitle(sf.title)
	}
	if sf.artist != "" {
		t.SetArtist(sf.artist)
	}
	if sf.album != "" {
		t.SetAlbum(sf.album)
	}
	if sf.year != "" {
		t.SetText("TDRC", sf.year)
	}
	if sf.genre != "" {
		t.SetGenre(sf.genre)
	}
	if sf.composer != "" {
		t.SetComposer(sf.composer)
	}
	if sf.track != "" {
		t.SetText("TRCK", sf.track)
	}
	if sf.disc != "" {
		t.SetText("TPOS", sf.disc)
	}
	if sf.comment != "" {
		t.RemoveFrames("COMM")
		t.AddFrame(&id3v2.CommentFrame{
			Encoding: id3v2.EncUTF8, Language: "eng", Text: sf.comment,
		})
	}
	return t.WriteFile(path)
}

func setFLAC(path string, sf *setFlags) error {
	f, err := flac.ReadFile(path)
	if err != nil {
		return err
	}
	vc := f.VorbisComment()
	if sf.title != "" {
		vc.Set("TITLE", sf.title)
	}
	if sf.artist != "" {
		vc.Set("ARTIST", sf.artist)
	}
	if sf.album != "" {
		vc.Set("ALBUM", sf.album)
	}
	if sf.year != "" {
		vc.Set("DATE", sf.year)
	}
	if sf.genre != "" {
		vc.Set("GENRE", sf.genre)
	}
	if sf.composer != "" {
		vc.Set("COMPOSER", sf.composer)
	}
	if sf.track != "" {
		vc.Set("TRACKNUMBER", sf.track)
	}
	if sf.disc != "" {
		vc.Set("DISCNUMBER", sf.disc)
	}
	if sf.comment != "" {
		vc.Set("DESCRIPTION", sf.comment)
	}
	return f.WriteFile(path)
}

func setMP4(path string, sf *setFlags) error {
	f, err := mp4.Read(path)
	if err != nil {
		return err
	}
	if sf.title != "" {
		f.Tag.SetTitle(sf.title)
	}
	if sf.artist != "" {
		f.Tag.SetArtist(sf.artist)
	}
	if sf.album != "" {
		f.Tag.SetAlbum(sf.album)
	}
	if sf.year != "" {
		f.Tag.SetDate(sf.year)
	}
	if sf.genre != "" {
		f.Tag.SetGenreText(sf.genre)
	}
	if sf.composer != "" {
		f.Tag.SetComposer(sf.composer)
	}
	if sf.track != "" {
		n, total := parseSlash(sf.track)
		f.Tag.SetTrack(uint16(n), uint16(total))
	}
	if sf.disc != "" {
		n, total := parseSlash(sf.disc)
		f.Tag.SetDisc(uint16(n), uint16(total))
	}
	if sf.comment != "" {
		f.Tag.SetComment(sf.comment)
	}
	return f.WriteFile(path)
}

func setAIFF(path string, sf *setFlags) error {
	f, err := aiff.ReadFile(path)
	if err != nil {
		return err
	}
	if sf.title != "" {
		f.SetTitle(sf.title)
	}
	if sf.artist != "" {
		f.SetAuthor(sf.artist)
	}
	// AIFF has no native album / year / genre / track / disc text
	// chunks; everything beyond NAME/AUTH must go through the
	// embedded ID3 tag. Also: if an ID3 tag already exists, mirror
	// title and artist into it too — otherwise the ID3 (which the
	// Open() wrapper prefers) would shadow the NAME/AUTH update.
	needsID3 := f.ID3 != nil || sf.album != "" || sf.year != "" ||
		sf.genre != "" || sf.track != "" || sf.disc != "" ||
		sf.composer != "" || sf.comment != ""
	if needsID3 {
		if f.ID3 == nil {
			f.ID3 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
		}
		applyID3v2Flags(f.ID3, sf)
	}
	return f.WriteFile(path)
}

func setAPE(path string, sf *setFlags) error {
	t, err := ape.ReadFile(path)
	if err != nil {
		return err
	}
	if sf.title != "" {
		_ = t.Set(ape.KeyTitle, sf.title)
	}
	if sf.artist != "" {
		_ = t.Set(ape.KeyArtist, sf.artist)
	}
	if sf.album != "" {
		_ = t.Set(ape.KeyAlbum, sf.album)
	}
	if sf.year != "" {
		_ = t.Set(ape.KeyYear, sf.year)
	}
	if sf.genre != "" {
		_ = t.Set(ape.KeyGenre, sf.genre)
	}
	if sf.composer != "" {
		_ = t.Set(ape.KeyComposer, sf.composer)
	}
	if sf.track != "" {
		_ = t.Set(ape.KeyTrack, sf.track)
	}
	if sf.disc != "" {
		_ = t.Set(ape.KeyDisc, sf.disc)
	}
	if sf.comment != "" {
		_ = t.Set(ape.KeyComment, sf.comment)
	}
	return t.WriteFile(path)
}

func setOgg(path string, sf *setFlags) error {
	f, err := ogg.ReadFile(path)
	if err != nil {
		return err
	}
	if f.Comments == nil {
		return fmt.Errorf("ogg: file has no comment block to update")
	}
	if sf.title != "" {
		f.Comments.Set(ogg.FieldTitle, sf.title)
	}
	if sf.artist != "" {
		f.Comments.Set(ogg.FieldArtist, sf.artist)
	}
	if sf.album != "" {
		f.Comments.Set(ogg.FieldAlbum, sf.album)
	}
	if sf.year != "" {
		f.Comments.Set(ogg.FieldDate, sf.year)
	}
	if sf.genre != "" {
		f.Comments.Set(ogg.FieldGenre, sf.genre)
	}
	if sf.composer != "" {
		f.Comments.Set(ogg.FieldComposer, sf.composer)
	}
	if sf.track != "" {
		f.Comments.Set(ogg.FieldTrack, sf.track)
	}
	if sf.disc != "" {
		f.Comments.Set(ogg.FieldDisc, sf.disc)
	}
	if sf.comment != "" {
		f.Comments.Set(ogg.FieldDescription, sf.comment)
	}
	return f.WriteFile(path)
}

func setASF(path string, sf *setFlags) error {
	f, err := asf.ReadFile(path)
	if err != nil {
		return err
	}
	if sf.title != "" {
		f.Title = sf.title
	}
	if sf.artist != "" {
		f.SetArtist(sf.artist) // CDO Author
	}
	if sf.album != "" {
		f.SetAlbum(sf.album)
	}
	if sf.year != "" {
		// Accept both "YYYY" and arbitrary "YYYY-MM-DD" strings.
		y := 0
		for _, c := range sf.year {
			if c < '0' || c > '9' {
				break
			}
			y = y*10 + int(c-'0')
			if y > 99999 {
				y = 0
				break
			}
		}
		if y != 0 {
			f.SetYear(y)
		} else {
			// fall back to literal storage
			f.SetExt(asf.NameYear, sf.year)
		}
	}
	if sf.genre != "" {
		f.SetGenre(sf.genre)
	}
	if sf.composer != "" {
		f.SetComposer(sf.composer)
	}
	if sf.track != "" {
		n, total := parseSlash(sf.track)
		f.SetTrackNumber(int(n), int(total))
	}
	if sf.disc != "" {
		n, total := parseSlash(sf.disc)
		f.SetDiscNumber(int(n), int(total))
	}
	if sf.comment != "" {
		f.Description = sf.comment
	}
	return f.WriteFile(path)
}

func setAAC(path string, sf *setFlags) error {
	f, err := aac.ReadFile(path)
	if err != nil {
		return err
	}
	if f.V2 == nil {
		f.V2 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
	}
	applyID3v2Flags(f.V2, sf)
	return f.WriteFile(path)
}

// applyID3v2Flags copies a setFlags into an existing ID3v2 tag.
// Shared by setMP3 (refactor target) and setAIFF / setAAC.
func applyID3v2Flags(t *id3v2.Tag, sf *setFlags) {
	if sf.title != "" {
		t.SetTitle(sf.title)
	}
	if sf.artist != "" {
		t.SetArtist(sf.artist)
	}
	if sf.album != "" {
		t.SetAlbum(sf.album)
	}
	if sf.year != "" {
		t.SetText("TDRC", sf.year)
	}
	if sf.genre != "" {
		t.SetGenre(sf.genre)
	}
	if sf.composer != "" {
		t.SetComposer(sf.composer)
	}
	if sf.track != "" {
		t.SetText("TRCK", sf.track)
	}
	if sf.disc != "" {
		t.SetText("TPOS", sf.disc)
	}
	if sf.comment != "" {
		t.RemoveFrames("COMM")
		t.AddFrame(&id3v2.CommentFrame{
			Encoding: id3v2.EncUTF8, Language: "eng", Text: sf.comment,
		})
	}
}

func setWAV(path string, sf *setFlags) error {
	f, err := wav.ReadFile(path)
	if err != nil {
		return err
	}
	if sf.title != "" {
		f.SetInfo(wav.InfoTitle, sf.title)
	}
	if sf.artist != "" {
		f.SetInfo(wav.InfoArtist, sf.artist)
	}
	if sf.album != "" {
		f.SetInfo(wav.InfoAlbum, sf.album)
	}
	if sf.year != "" {
		f.SetInfo(wav.InfoDate, sf.year)
	}
	if sf.genre != "" {
		f.SetInfo(wav.InfoGenre, sf.genre)
	}
	if sf.composer != "" {
		f.SetInfo(wav.InfoComposer, sf.composer)
	}
	if sf.track != "" {
		f.SetInfo(wav.InfoTrack, sf.track)
	}
	if sf.comment != "" {
		f.SetInfo(wav.InfoComment, sf.comment)
	}
	// disc has no canonical LIST/INFO key; skip silently.
	return f.WriteFile(path)
}

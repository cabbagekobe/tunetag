package main

import (
	"flag"
	"fmt"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
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

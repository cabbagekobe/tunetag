// tunetag is a command-line driver for the tunetag library.
//
// Usage:
//
//	tunetag print  <file>
//	tunetag set    <file> [--title=...] [--artist=...] [--album=...] [--year=YYYY] [--genre=...] [--track=N[/M]] [--disc=N[/M]]
//	tunetag strip  <file>
//	tunetag cover  <file> (--extract <out>) | (--set <in>)
//
// The set command auto-selects the underlying writer (id3v2 / flac /
// mp4) based on the file's container.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "print":
		err = cmdPrint(args)
	case "set":
		err = cmdSet(args)
	case "strip":
		err = cmdStrip(args)
	case "cover":
		err = cmdCover(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "tunetag: unknown command %q\n", cmd)
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "tunetag: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  tunetag print  <file>")
	fmt.Fprintln(os.Stderr, "  tunetag set    <file> [--title=...] [--artist=...] [--album=...] [--year=YYYY] [--genre=...] [--track=N[/M]] [--disc=N[/M]]")
	fmt.Fprintln(os.Stderr, "  tunetag strip  <file>")
	fmt.Fprintln(os.Stderr, "  tunetag cover  <file> (--extract <out>) | (--set <in>)")
	os.Exit(2)
}

func cmdPrint(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("print: exactly one file argument required")
	}
	tag, err := tunetag.Open(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Format: %s\n", tag.Format())
	fmt.Printf("Title:  %s\n", tag.Title())
	fmt.Printf("Artist: %s\n", tag.Artist())
	if v := tag.AlbumArtist(); v != "" {
		fmt.Printf("AlbumArtist: %s\n", v)
	}
	fmt.Printf("Album:  %s\n", tag.Album())
	if y := tag.Year(); y != 0 {
		fmt.Printf("Year:   %d\n", y)
	}
	if g := tag.Genre(); g != "" {
		fmt.Printf("Genre:  %s\n", g)
	}
	if c := tag.Composer(); c != "" {
		fmt.Printf("Composer: %s\n", c)
	}
	if n, total := tag.TrackNumber(); n != 0 || total != 0 {
		fmt.Printf("Track:  %d/%d\n", n, total)
	}
	if n, total := tag.DiscNumber(); n != 0 || total != 0 {
		fmt.Printf("Disc:   %d/%d\n", n, total)
	}
	if c := tag.Comment(); c != "" {
		fmt.Printf("Comment: %s\n", c)
	}
	if pics := tag.Pictures(); len(pics) > 0 {
		for i, p := range pics {
			fmt.Printf("Picture[%d]: type=%d mime=%s desc=%q size=%d bytes\n",
				i, p.Type, p.MIME, p.Description, len(p.Data))
		}
	}
	return nil
}

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

func cmdStrip(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("strip: exactly one file argument required")
	}
	return tunetag.Strip(args[0])
}

func cmdCover(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("cover: file and one of --extract/--set required")
	}
	path := args[0]
	fs := flag.NewFlagSet("cover", flag.ContinueOnError)
	extract := fs.String("extract", "", "write the first cover image to this path")
	set := fs.String("set", "", "read this file and embed it as cover art")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if (*extract == "" && *set == "") || (*extract != "" && *set != "") {
		return fmt.Errorf("cover: exactly one of --extract or --set is required")
	}

	if *extract != "" {
		tag, err := tunetag.Open(path)
		if err != nil {
			return err
		}
		pics := tag.Pictures()
		if len(pics) == 0 {
			return fmt.Errorf("cover: no embedded pictures")
		}
		return os.WriteFile(*extract, pics[0].Data, 0o644)
	}

	data, err := os.ReadFile(*set)
	if err != nil {
		return err
	}
	format, err := detect(path)
	if err != nil {
		return err
	}
	switch format {
	case tunetag.FormatID3v2:
		t, err := id3v2.ReadFile(path)
		if err != nil {
			return err
		}
		t.RemoveFrames("APIC")
		t.AddFrame(&id3v2.PictureFrame{
			Encoding:    id3v2.EncUTF8,
			MIME:        guessMIME(data),
			PictureType: 3, // CoverFront
			Data:        data,
		})
		return t.WriteFile(path)
	case tunetag.FormatFLAC:
		f, err := flac.ReadFile(path)
		if err != nil {
			return err
		}
		f.RemovePictures()
		f.AddPicture(&flac.Picture{
			PictureType: 3,
			MIME:        guessMIME(data),
			Data:        data,
		})
		return f.WriteFile(path)
	case tunetag.FormatMP4:
		f, err := mp4.Read(path)
		if err != nil {
			return err
		}
		f.Tag.Remove(mp4.KeyCover)
		f.Tag.AddCover(data)
		return f.WriteFile(path)
	}
	return fmt.Errorf("cover --set: unsupported format %s", format)
}

func detect(path string) (tunetag.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return tunetag.FormatUnknown, err
	}
	defer f.Close()
	return tunetag.Detect(f)
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

func parseSlash(s string) (n, total int) {
	for i, c := range s {
		if c == '/' {
			fmt.Sscanf(s[:i], "%d", &n)
			fmt.Sscanf(s[i+1:], "%d", &total)
			return
		}
	}
	fmt.Sscanf(s, "%d", &n)
	return n, 0
}

func guessMIME(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && string(data[0:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	}
	return "application/octet-stream"
}

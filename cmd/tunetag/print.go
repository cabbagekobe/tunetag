package main

import (
	"fmt"

	"github.com/cabbagekobe/tunetag"
)

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

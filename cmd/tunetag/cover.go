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

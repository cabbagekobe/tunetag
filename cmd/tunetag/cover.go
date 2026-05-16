package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/wav"
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
	case tunetag.FormatWAV:
		f, err := wav.ReadFile(path)
		if err != nil {
			return err
		}
		// WAV carries cover art only via an embedded id3 chunk;
		// create one if the file doesn't already have it.
		if f.ID3 == nil {
			f.ID3 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
		}
		f.ID3.RemoveFrames("APIC")
		f.ID3.AddFrame(coverAPIC(data))
		return f.WriteFile(path)
	case tunetag.FormatAIFF:
		f, err := aiff.ReadFile(path)
		if err != nil {
			return err
		}
		if f.ID3 == nil {
			f.ID3 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
		}
		f.ID3.RemoveFrames("APIC")
		f.ID3.AddFrame(coverAPIC(data))
		return f.WriteFile(path)
	case tunetag.FormatAAC:
		f, err := aac.ReadFile(path)
		if err != nil {
			return err
		}
		if f.V2 == nil {
			f.V2 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
		}
		f.V2.RemoveFrames("APIC")
		f.V2.AddFrame(coverAPIC(data))
		return f.WriteFile(path)
	}
	return fmt.Errorf("cover --set: unsupported format %s", format)
}

// coverAPIC builds a CoverFront APIC frame from arbitrary image
// bytes. Shared by every ID3v2-backed container.
func coverAPIC(data []byte) *id3v2.PictureFrame {
	return &id3v2.PictureFrame{
		Encoding:    id3v2.EncUTF8,
		MIME:        guessMIME(data),
		PictureType: 3, // CoverFront
		Data:        data,
	}
}

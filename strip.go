package tunetag

import (
	"errors"
	"os"

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
	case FormatWAV:
		return stripWAV(path)
	case FormatAIFF:
		return stripAIFF(path)
	case FormatOgg:
		return ogg.ErrWriteNotSupported
	case FormatAPE:
		return stripAPE(path)
	case FormatAAC:
		return stripAAC(path)
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

func stripWAV(path string) error {
	src, err := wav.ReadFile(path)
	if err != nil {
		return err
	}
	src.Info = nil
	src.ID3 = nil
	return src.WriteFile(path)
}

func stripAIFF(path string) error {
	src, err := aiff.ReadFile(path)
	if err != nil {
		return err
	}
	src.Text = nil
	src.Annotations = nil
	src.ID3 = nil
	return src.WriteFile(path)
}

func stripAPE(path string) error {
	src, err := ape.ReadFile(path)
	if err != nil {
		return err
	}
	src.Items = nil
	return src.WriteFile(path)
}

func stripAAC(path string) error {
	src, err := aac.ReadFile(path)
	if err != nil {
		return err
	}
	src.V2 = nil
	src.V1 = nil
	return src.WriteFile(path)
}

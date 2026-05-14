package tunetag

import (
	"errors"
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

// MP3 carries the parsed ID3v2 and/or ID3v1 tags from an MP3 file.
// V2 is preferred; V1 is exposed for inspection.
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

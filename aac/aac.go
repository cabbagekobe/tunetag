// Package aac handles raw ADTS-framed AAC files.
//
// Raw AAC has no native metadata container. The only metadata
// that ships inside a .aac file is an optional ID3v2 tag at the
// very start (some encoders, notably FAAC and Wavelab, write one)
// and/or an ID3v1 trailer at the end. This package exposes a
// File type that owns whichever of those is present and rewrites
// it in place when WriteFile is called.
//
// Detection: an ADTS frame begins with the 12-bit sync word 0xFFF
// and a layer field of 00. The IsADTS helper recognises that
// signature; tunetag.Detect uses it to route .aac files without
// an ID3v2 prefix here.
//
// A *File is not safe for concurrent use.
package aac

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
)

// Errors returned by this package.
var (
	// ErrNotAAC is returned by Read when the input contains
	// neither an ID3v2 prefix nor a recognisable ADTS frame at
	// offset 0.
	ErrNotAAC = errors.New("aac: not an AAC stream (no ID3v2 prefix and no ADTS sync)")
)

// File holds the metadata associated with a raw ADTS AAC file.
// Both fields may be nil; an untagged AAC file is represented by
// a *File with V2 == nil and V1 == nil.
type File struct {
	V2 *id3v2.Tag // leading ID3v2 tag, if any
	V1 *id3v1.Tag // trailing ID3v1 tag, if any

	// audioOffset and audioEnd record where the raw audio body
	// lives in the source file. WriteFile uses them to splice in
	// a new tag without re-encoding the audio.
	audioOffset int64
	audioEnd    int64
}

// IsADTS reports whether b begins with an ADTS frame sync (the
// 12-bit 0xFFF prefix followed by a layer-00 field). The check
// requires at least 2 bytes.
func IsADTS(b []byte) bool {
	if len(b) < 2 {
		return false
	}
	// Byte 0: 0xFF (8 sync bits).
	if b[0] != 0xFF {
		return false
	}
	// Byte 1 bits 7-4 must be 1111, bits 2-1 (layer) must be 00.
	// Valid byte[1] values: 0xF0, 0xF1, 0xF8, 0xF9.
	switch b[1] {
	case 0xF0, 0xF1, 0xF8, 0xF9:
		return true
	}
	return false
}

// Read parses any ID3v2 prefix and ID3v1 trailer of an AAC file.
// At least one of those tags, or an ADTS sync at offset 0, is
// required; otherwise ErrNotAAC is returned.
func Read(rs io.ReadSeeker) (*File, error) {
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var probe [2]byte
	n, _ := io.ReadFull(rs, probe[:])
	hasID3 := n >= 2 && probe[0] == 'I' && probe[1] == 'D'
	hasADTS := IsADTS(probe[:n])
	if !hasID3 && !hasADTS {
		return nil, ErrNotAAC
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	out := &File{audioEnd: end}
	if hasID3 {
		t, err := id3v2.Read(rs)
		if err != nil && !errors.Is(err, id3v2.ErrNoTag) {
			return nil, err
		}
		if t != nil {
			out.V2 = t
			off, err := rs.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, err
			}
			out.audioOffset = off
		}
	}
	// ID3v1 trailer detection.
	if end >= int64(id3v1.TagSize) {
		if _, err := rs.Seek(end-int64(id3v1.TagSize), io.SeekStart); err != nil {
			return nil, err
		}
		v1, err := id3v1.Read(rs)
		if err == nil {
			out.V1 = v1
			out.audioEnd = end - int64(id3v1.TagSize)
		} else if !errors.Is(err, id3v1.ErrNoTag) {
			return nil, err
		}
	}
	return out, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// --- common accessors ------------------------------------------

func (f *File) Title() string {
	if f.V2 != nil && f.V2.Title() != "" {
		return f.V2.Title()
	}
	if f.V1 != nil {
		return f.V1.Title
	}
	return ""
}

func (f *File) Artist() string {
	if f.V2 != nil && f.V2.Artist() != "" {
		return f.V2.Artist()
	}
	if f.V1 != nil {
		return f.V1.Artist
	}
	return ""
}

func (f *File) Album() string {
	if f.V2 != nil && f.V2.Album() != "" {
		return f.V2.Album()
	}
	if f.V1 != nil {
		return f.V1.Album
	}
	return ""
}

func (f *File) AlbumArtist() string {
	if f.V2 != nil {
		return f.V2.AlbumArtist()
	}
	return ""
}

func (f *File) Composer() string {
	if f.V2 != nil {
		return f.V2.Composer()
	}
	return ""
}

func (f *File) Genre() string {
	if f.V2 != nil && f.V2.Genre() != "" {
		return f.V2.Genre()
	}
	if f.V1 != nil {
		return f.V1.GenreName()
	}
	return ""
}

func (f *File) Comment() string {
	if f.V2 != nil && f.V2.Comment() != "" {
		return f.V2.Comment()
	}
	if f.V1 != nil {
		return f.V1.Comment
	}
	return ""
}

func (f *File) Year() int {
	if f.V2 != nil && f.V2.Year() != 0 {
		return f.V2.Year()
	}
	if f.V1 != nil && f.V1.Year != "" {
		var y int
		for _, c := range f.V1.Year {
			if c < '0' || c > '9' {
				return 0
			}
			y = y*10 + int(c-'0')
		}
		return y
	}
	return 0
}

func (f *File) TrackNumber() (n, total int) {
	if f.V2 != nil {
		if a, b := f.V2.TrackNumber(); a != 0 || b != 0 {
			return a, b
		}
	}
	if f.V1 != nil && f.V1.Track != 0 {
		return int(f.V1.Track), 0
	}
	return 0, 0
}

func (f *File) DiscNumber() (n, total int) {
	if f.V2 != nil {
		return f.V2.DiscNumber()
	}
	return 0, 0
}

func (f *File) Pictures() []*id3v2.PictureFrame {
	if f.V2 == nil {
		return nil
	}
	return f.V2.PictureFrames()
}

// WriteFile rewrites path with the current ID3v2 / ID3v1 tags
// wrapped around the original audio body. If V2 is nil the
// leading ID3v2 chunk is removed; if V1 is nil the trailer is
// removed. The audio bytes between audioOffset and audioEnd are
// preserved unchanged.
func (f *File) WriteFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := src.Seek(f.audioOffset, io.SeekStart); err != nil {
		return err
	}
	audio := make([]byte, f.audioEnd-f.audioOffset)
	if _, err := io.ReadFull(src, audio); err != nil {
		return fmt.Errorf("aac: read audio body: %w", err)
	}

	var prefix bytes.Buffer
	if f.V2 != nil {
		if err := f.V2.Encode(&prefix); err != nil {
			return err
		}
	}
	var suffix bytes.Buffer
	if f.V1 != nil {
		if err := f.V1.Encode(&suffix); err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-aac-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(prefix.Bytes()); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(audio); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(suffix.Bytes()); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := src.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

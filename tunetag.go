package tunetag

import (
	"errors"
	"io"
	"os"

	"github.com/cabbagekobe/tunetag/aac"
	"github.com/cabbagekobe/tunetag/aiff"
	"github.com/cabbagekobe/tunetag/ape"
	"github.com/cabbagekobe/tunetag/asf"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v1"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
	"github.com/cabbagekobe/tunetag/ogg"
	"github.com/cabbagekobe/tunetag/wav"
)

// Detect inspects the start (and, when needed, the end) of rs to
// identify the container format. The seek position of rs is restored
// before returning when possible.
//
// On failure the returned error is one of:
//   - ErrEmptyFile when rs is zero bytes long,
//   - ErrFileTooSmall when rs is shorter than any supported tag
//     header can be,
//   - ErrUnknownFormat otherwise.
//
// ErrEmptyFile and ErrFileTooSmall are refinements of
// ErrUnknownFormat — errors.Is reports true for both, so callers that
// only branch on ErrUnknownFormat keep working unchanged.
func Detect(rs io.ReadSeeker) (Format, error) {
	cur, _ := rs.Seek(0, io.SeekCurrent)
	defer func() { _, _ = rs.Seek(cur, io.SeekStart) }()

	// Resolve the total readable size up front so empty and extremely
	// short inputs can be reported with a clearer sentinel than the
	// generic "unknown format".
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return FormatUnknown, err
	}
	if end == 0 {
		return FormatUnknown, ErrEmptyFile
	}

	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, err
	}
	// 16-byte sniff is enough for every supported format's start
	// signature, including ASF's 16-byte Header Object GUID.
	var hdr [16]byte
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
	// RIFF/WAVE: "RIFF" at offset 0, "WAVE" at offset 8. The wav
	// subpackage decides separately whether the body is parseable
	// (and rejects RF64 / BW64).
	if n >= 12 && string(hdr[0:4]) == "RIFF" && string(hdr[8:12]) == "WAVE" {
		return FormatWAV, nil
	}
	// AIFF / AIFC: "FORM" at offset 0, "AIFF" or "AIFC" at offset 8.
	if n >= 12 && string(hdr[0:4]) == "FORM" && (string(hdr[8:12]) == "AIFF" || string(hdr[8:12]) == "AIFC") {
		return FormatAIFF, nil
	}
	// Ogg: "OggS" at offset 0.
	if n >= 4 && string(hdr[0:4]) == "OggS" {
		return FormatOgg, nil
	}
	// ASF / WMA: 16-byte Header Object GUID at offset 0.
	if n >= 16 && asf.IsHeaderGUID(hdr[:16]) {
		return FormatASF, nil
	}
	// Raw ADTS AAC: 12-bit 0xFFF sync with layer 00.
	if n >= 2 && aac.IsADTS(hdr[:n]) {
		return FormatAAC, nil
	}
	// APEv2 footer at end (optionally with ID3v1 trailer after it).
	if end >= 32 {
		if format, ok := detectAPEFooter(rs, end); ok {
			return format, nil
		}
	}
	// Fall back to ID3v1 trailer detection.
	if end >= id3v1.TagSize {
		if _, err := rs.Seek(end-id3v1.TagSize, io.SeekStart); err != nil {
			return FormatUnknown, err
		}
		var marker [3]byte
		if _, err := io.ReadFull(rs, marker[:]); err == nil && marker[0] == 'T' && marker[1] == 'A' && marker[2] == 'G' {
			return FormatID3v1, nil
		}
	}
	// Nothing matched. 12 bytes is the smallest non-ASF format
	// header we recognise (RIFF+size+WAVE / FORM+size+AIFF /
	// MP4's [0-3]=size [4-7]=ftyp + 4-byte brand). Anything
	// shorter cannot contain a complete format header, so it
	// gets the more specific ErrFileTooSmall.
	if end < 12 {
		return FormatUnknown, ErrFileTooSmall
	}
	return FormatUnknown, ErrUnknownFormat
}

// detectAPEFooter checks whether rs ends in an APEv2 footer
// (optionally preceded by 128 bytes of ID3v1). The seek position
// is not restored — the caller is responsible (Detect does so via
// its outer defer).
func detectAPEFooter(rs io.ReadSeeker, end int64) (Format, bool) {
	check := func(off int64) bool {
		if off < 0 {
			return false
		}
		if _, err := rs.Seek(off, io.SeekStart); err != nil {
			return false
		}
		var sig [8]byte
		if _, err := io.ReadFull(rs, sig[:]); err != nil {
			return false
		}
		return sig == ape.Preamble
	}
	if check(end - 32) {
		return FormatAPE, true
	}
	if end >= 128+32 {
		if _, err := rs.Seek(end-128, io.SeekStart); err == nil {
			var tag3 [3]byte
			if _, err := io.ReadFull(rs, tag3[:]); err == nil && string(tag3[:]) == "TAG" {
				if check(end - 128 - 32) {
					return FormatAPE, true
				}
			}
		}
	}
	return FormatUnknown, false
}

// Open auto-detects the container at path and returns a read-only
// Tag for the most informative metadata block in the file.
//
//   - MP3: ID3v2 if present, otherwise ID3v1.
//   - FLAC / MP4: the corresponding format-specific reader.
//   - WAV: embedded "id3 " chunk if present, else LIST/INFO.
//   - AIFF / AIFC: embedded "ID3 " chunk if present, else
//     NAME / AUTH / "(c) " / ANNO text chunks.
//   - Ogg (Vorbis / Opus): the codec's comment header.
//   - APEv2 (.ape, .wv, or any file with a trailing APEv2 tag).
//   - AAC: leading ID3v2 if present, else trailing ID3v1, else
//     an empty tag (so untagged .aac files still resolve).
//   - ASF / WMA: the Content Description Object fields, with
//     WM/AlbumArtist (in the Extended Content Description Object)
//     preferred over the CDO's Author field when both exist.
//
// For format-specific writes, use OpenMP3, OpenFLAC, OpenMP4,
// OpenWAV, OpenAIFF, OpenOgg, OpenAPE, OpenAAC, or OpenASF
// directly.
func Open(path string) (Tag, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
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
	case FormatWAV:
		w, err := wav.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &wavTag{f: w}, nil
	case FormatAIFF:
		a, err := aiff.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &aiffTag{f: a}, nil
	case FormatOgg:
		o, err := ogg.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &oggTag{f: o}, nil
	case FormatAPE:
		t, err := ape.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &apeTag{t: t}, nil
	case FormatAAC:
		a, err := aac.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &aacTag{f: a}, nil
	case FormatASF:
		a, err := asf.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &asfTag{f: a}, nil
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
	defer func() { _ = f.Close() }()
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

// OpenWAV opens a WAV (RIFF/WAVE) file for read-write metadata
// access. The returned *wav.File exposes the LIST/INFO entries and
// any embedded id3 chunk via its Info field and ID3 field.
func OpenWAV(path string) (*wav.File, error) {
	return wav.ReadFile(path)
}

// OpenAIFF opens an AIFF / AIFC file for read-write metadata
// access. The returned *aiff.File exposes the NAME / AUTH /
// "(c) " / ANNO text chunks via Text + Annotations and any
// embedded "ID3 " chunk via ID3.
func OpenAIFF(path string) (*aiff.File, error) {
	return aiff.ReadFile(path)
}

// OpenOgg opens an Ogg (Vorbis or Opus) file. The returned
// *ogg.File is read-only; writing Ogg comments is not yet
// implemented.
func OpenOgg(path string) (*ogg.File, error) {
	return ogg.ReadFile(path)
}

// OpenAPE locates and parses an APEv2 tag at the end of path. The
// audio container itself can be anything: APEv2 is the canonical
// tag format for Monkey's Audio (.ape) and WavPack (.wv) but is
// also valid on MP3, MPC, OFR, and others.
func OpenAPE(path string) (*ape.Tag, error) {
	return ape.ReadFile(path)
}

// OpenAAC opens a raw ADTS AAC file. Both a leading ID3v2 tag and
// a trailing ID3v1 tag are recognised; an untagged file is
// represented by a *aac.File with V2 == V1 == nil.
func OpenAAC(path string) (*aac.File, error) {
	return aac.ReadFile(path)
}

// OpenASF opens an ASF / WMA file. The returned *asf.File exposes
// the Content Description Object fields (Title / Author /
// Copyright / Description / Rating) plus the Extended Content
// Description Object descriptors (Extended) and any embedded
// WM/Picture entries.
func OpenASF(path string) (*asf.File, error) {
	return asf.ReadFile(path)
}

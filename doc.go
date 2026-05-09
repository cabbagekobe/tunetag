// Package tunetag is a pure Go audio metadata library supporting MP3
// (ID3v1, ID3v2.2/2.3/2.4), FLAC (Vorbis Comment + Picture), and
// MP4/M4A (iTunes-style ilst). It reads and writes tags using only
// the Go standard library — no cgo and no bundled native binaries.
//
// Most users should use the top-level Open function for read access
// and the format-specific subpackages (id3v1, id3v2, flac, mp4) for
// detailed writes. Tag values returned by this package are not safe
// for concurrent use.
package tunetag

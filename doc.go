// Package tunetag is a pure Go audio metadata library supporting
// MP3 (ID3v1, ID3v2.2/2.3/2.4), FLAC (Vorbis Comment + Picture),
// and MP4/M4A (iTunes-style ilst). It reads and writes tags using
// only the Go standard library — no cgo and no bundled native
// binaries.
//
// # Decision tree
//
// Pick the entry point based on whether you need read or write
// access:
//
//   - For read-only access where the file's container does not
//     matter, use [Detect] (to identify the format) and [Open]
//     (which returns a [Tag] interface exposing the common fields).
//   - For format-specific reads and writes, use the typed openers
//     [OpenMP3], [OpenFLAC], or [OpenMP4]. Each returns a value
//     from the respective subpackage that supports the full set of
//     mutations and a WriteFile method.
//   - To erase every metadata block from a file while preserving
//     the audio body, call [Strip].
//
// # Read-only Tag interface
//
// The [Tag] interface returned by [Open] exposes only fields that
// are universally meaningful across formats (Title, Artist, Album,
// Year, Genre, Track, Disc, Composer, Comment, AlbumArtist,
// Pictures). Writes deliberately are not on this interface because
// each format's setter semantics differ in ways that a unified API
// would obscure (notably the multiple genre representations on MP4
// and the Vorbis case-sensitivity rules on FLAC).
//
// # Format-specific subpackages
//
//   - [github.com/cabbagekobe/tunetag/id3v1]: ID3v1 / v1.1 trailer.
//   - [github.com/cabbagekobe/tunetag/id3v2]: ID3v2.2 / 2.3 / 2.4.
//   - [github.com/cabbagekobe/tunetag/flac]: Vorbis Comment +
//     Picture blocks; unknown blocks round-trip verbatim.
//   - [github.com/cabbagekobe/tunetag/mp4]: iTunes-style ilst,
//     including freeform "----" atoms.
//
// # Concurrency
//
// [Tag], [*id3v2.Tag], [*flac.File], and [*mp4.File] are not safe
// for concurrent use. The pure parsing functions ([Detect],
// [id3v2.Read], [flac.Read], [mp4.Read]) are re-entrant.
package tunetag

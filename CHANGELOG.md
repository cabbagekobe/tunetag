# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.3] - 2026-05-17

### Added

- WAV (RIFF/WAVE) support via the new `wav` subpackage. Both
  classic LIST/INFO entries (INAM / IART / IPRD / ICRD / IGNR /
  ICMT / ITRK / IMUS / …) and embedded `id3 ` chunks (an ID3v2
  tag parsed via the existing `id3v2` package) are read and
  written. Non-metadata chunks (`fmt `, `data`, `fact`, `JUNK`,
  …) round-trip byte-for-byte; `WriteFile` rewrites atomically
  via a sibling temp file and the RIFF size field is rebuilt.
  RF64 / BW64 (64-bit RIFF) is detected and rejected with
  `wav.ErrRF64Unsupported`.
- New top-level entries `tunetag.FormatWAV` and `tunetag.OpenWAV`.
  `Detect` recognises WAV from the `RIFF…WAVE` header at offsets
  0 and 8. `Open` returns a `Tag` adapter that prefers the
  embedded `id3 ` tag over LIST/INFO when both are present.
  `Strip` removes both metadata containers from a WAV.
- AIFF / AIFC support via the new `aiff` subpackage. NAME / AUTH
  / "(c) " / ANNO text chunks (with multi-instance ANNO) and
  embedded `ID3 ` chunks round-trip. Non-metadata chunks (COMM,
  SSND, FVER, MARK, …) are preserved byte-for-byte. New
  top-level entries `tunetag.FormatAIFF` and `tunetag.OpenAIFF`.
- Ogg Vorbis / Ogg Opus read support via the new `ogg`
  subpackage. The package demuxes the first logical bitstream,
  detects the codec from the identification packet, and parses
  the comment packet (with `0x03 "vorbis"` or `"OpusTags"`
  prefix stripped). Vorbis Comment parsing is shared with the
  `flac` package via the newly-exported `flac.ParseVorbisComment`.
  Write is not yet supported (`ogg.ErrWriteNotSupported`). New
  top-level entries `tunetag.FormatOgg` and `tunetag.OpenOgg`.
- APEv2 read+write via the new `ape` subpackage. Locates an
  APEv2 footer at the end of any container (Monkey's Audio
  `.ape`, WavPack `.wv`, but also MP3 / MPC / OFR). An ID3v1
  trailer following the APEv2 tag is preserved across writes.
  APEv1 (version 1000) is detected and rejected with
  `ape.ErrUnsupportedVersion`. New top-level entries
  `tunetag.FormatAPE` and `tunetag.OpenAPE`. `Detect` recognises
  APEv2 footers at the end of the file.
- Raw ADTS AAC support via the new `aac` subpackage. Reads any
  leading ID3v2 prefix and trailing ID3v1 trailer; an untagged
  raw ADTS file is now recognised as `FormatAAC` so
  `tunetag.Open` returns an empty tag instead of
  `ErrUnknownFormat`. `aac.IsADTS` is exposed for callers that
  want to detect ADTS sync independently. New top-level entries
  `tunetag.FormatAAC` and `tunetag.OpenAAC`.
- `flac.ParseVorbisComment` is now a public wrapper around the
  package-internal parser, so callers outside FLAC (notably the
  new `ogg` package) can decode Vorbis Comment blocks without
  duplicating the format.
- CLI: `tunetag print / dump / set / strip / cover` now accept
  `.wav`, `.aif` / `.aiff` / `.aifc`, `.ogg` / `.opus`,
  `.ape` / `.wv`, `.aac`, and `.wma` / `.wmv` paths.
- ASF / WMA support via the new `asf` subpackage. Both the
  Content Description Object (Title / Author / Copyright /
  Description / Rating) and the Extended Content Description
  Object (WM/AlbumTitle, WM/Year, WM/TrackNumber, WM/Genre,
  WM/AlbumArtist, WM/Composer, WM/PartOfSet, WM/Picture, …)
  round-trip in full. All other Header child objects (File
  Properties, Stream Properties, Header Extension, Codec List,
  …) plus the Data + Index objects are preserved
  byte-for-byte. WM/Picture cover art has decode + encode
  helpers via `asf.Picture`. UTF-16LE encoding is handled
  internally; callers see Go-native UTF-8 strings. New
  top-level entries `tunetag.FormatASF` and
  `tunetag.OpenASF`. `Detect` matches the 16-byte ASF Header
  Object GUID at offset 0; `Detect` now reads a 16-byte sniff
  buffer (was 12) so any input < 12 bytes still hits
  `ErrFileTooSmall`.
- Ogg now supports writing. `ogg.File.WriteFile` re-encodes the
  Vorbis Comment block (Vorbis: framed with `0x03 "vorbis"` +
  framing bit; Opus: framed with `"OpusTags"`), splits the
  result into one or more Ogg pages, and rewrites every
  subsequent page of the same logical bitstream with shifted
  sequence numbers and freshly-computed Ogg CRC-32 values.
  Concurrently-multiplexed streams pass through unchanged.
  Strip on Ogg now empties the Comments list (rather than
  returning ErrWriteNotSupported), preserving the codec's
  default vendor string. Intermediate pages of a multi-page
  comment packet correctly stamp `granule_position = -1` per
  the Ogg spec ("no packet ends on this page").
- Ogg cover art via `METADATA_BLOCK_PICTURE`: `ogg.File`
  exposes `Pictures()`, `AddPicture(*flac.Picture)`, and
  `RemovePictures()`. The on-disk format is base64-encoded
  FLAC PICTURE block bytes — shared with the `flac`
  subpackage, which now exports `ParsePicture` for that
  purpose.
- APEv2 cover art via "Cover Art (Front)" / "Cover Art (Back)"
  / "Cover Art (Other)" binary items. `ape.Tag` exposes
  `Pictures()`, `AddPicture(*Picture)`, `AddPictureAs(key, p)`,
  and `RemovePictures()`. The wire format is the standard
  `<filename>\x00<image bytes>` layout used by foobar2000 and
  MusicBrainz Picard.
- CLI `cover --set` now accepts `.ogg` / `.opus`, `.ape` /
  `.wv`, in addition to the previously-supported MP3, FLAC,
  MP4, WAV, AIFF, AAC, and WMA.

### Fixed

- `asf.File.SetArtist` no longer leaves the file's Artist field
  out of sync: `Artist()` now reads Content Description Object
  Author first (which `SetArtist` writes to), falling back to
  WM/AlbumArtist only when Author is empty. The previous
  ordering caused a round-trip bug where `SetArtist("X")` could
  appear to have no effect if WM/AlbumArtist was already
  populated.
- WAV and AIFF readers reject chunks whose declared size exceeds
  the remaining file instead of attempting a multi-GiB
  allocation up front. APEv2 `parseItems` caps the initial
  `[]Item` capacity by the body's physical maximum so a footer
  claiming `count = 4 GiB` no longer triggers a giant
  allocation. ASF refuses child objects whose declared size
  overflows int64 (would previously panic inside `make`).

## [0.1.2] - 2026-05-16

### Added

- `Detect` / `Open` が 0 バイト入力には新エラー `ErrEmptyFile`
  （`"tunetag: empty file"`）、12 バイト未満で magic 不一致のときは
  `ErrFileTooSmall`（`"tunetag: file too small to contain any tag"`）
  を返すようになった。両エラーは `errors.Is(err, ErrUnknownFormat)`
  で引き続き true を返すため、既存呼び出し側の判定は変更不要。

## [0.1.1] - 2026-05-15

### Fixed

- LICENSE text now matches the canonical SPDX MIT template so that
  pkg.go.dev recognises the license as redistributable. The previous
  `v0.1.0` LICENSE used the "OPERATION OF" variant which scored 0%
  on Google's licensecheck library. Licensing intent (MIT) is
  unchanged.

## [0.1.0] - 2026-05-15

Initial public release.

### Added

- Top-level read API: `Detect`, `Open`, `Strip`, and the common
  `Tag` interface (Title / Artist / Album / Year / TrackNumber /
  DiscNumber / Genre / Composer / Comment / AlbumArtist / Pictures
  / Format).
- Format-specific openers `OpenMP3`, `OpenFLAC`, `OpenMP4` for
  read-write access through the typed subpackages.
- **id3v1**: full ID3v1 / 1.1 trailer read + write with Winamp
  genres.
- **id3v2**:
  - All three revisions (2.2, 2.3, 2.4) for both read and write.
  - Frames: TextFrame (T***), UserTextFrame (TXXX), URLFrame
    (W***), UserURLFrame (WXXX), CommentFrame (COMM),
    UnsynchronisedLyricsFrame (USLT), PictureFrame (APIC / PIC),
    UFIDFrame (UFID), PrivFrame (PRIV), GenericFrame fallback.
  - v2.4 footer flag (mutually exclusive with padding per spec).
  - v2.2 ↔ v2.3 frame-ID normalisation; PIC body translated to
    APIC layout on read.
  - Tag-level unsynchronisation decoded on read.
  - In-place rewrite when the new tag fits in existing padding;
    atomic temp-file rewrite otherwise.
- **flac**: VORBIS_COMMENT and PICTURE round-trip with case-
  insensitive Vorbis lookups, PADDING-block absorption to keep
  audio offset stable, atomic temp-file rewrite fallback, and
  byte-perfect preservation of unknown blocks (SEEKTABLE,
  CUESHEET, APPLICATION, etc.).
- **mp4**: iTunes-style ilst with the standard 4-byte keys and
  freeform `----` items. Three-tier write strategy:
  - Tier 1: same-size in-place ilst overwrite.
  - Tier 2: absorb the delta into a sibling `free` atom inside
    `meta` (or insert a new `free` when shrinking).
  - Tier 3: full atomic rewrite with stco / co64 patching for
    every trak in the file.
  - Auto-promotion stco → co64 when patching would overflow 32
    bits.
  - Fragmented MP4 (mvex / moof) detected on read; rejected on
    write via `ErrFragmentedUnsupport`.
  - iTunes-format 6-byte `disk` atom accepted (matches the de
    facto encoding).
- **cmd/tunetag**: command-line driver with `print`, `dump`,
  `set`, `strip`, and `cover` subcommands.
- **CI**: GitHub Actions test matrix across Ubuntu / macOS /
  Windows × Go 1.23 / 1.24 with `go vet`, build, and
  `go test -race`.
- **Tests**: extensive unit coverage including round-trip,
  defensive parsing, fuzz seeds, and per-package benchmarks.

[Unreleased]: https://github.com/cabbagekobe/tunetag/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/cabbagekobe/tunetag/releases/tag/v0.1.2
[0.1.1]: https://github.com/cabbagekobe/tunetag/releases/tag/v0.1.1
[0.1.0]: https://github.com/cabbagekobe/tunetag/releases/tag/v0.1.0

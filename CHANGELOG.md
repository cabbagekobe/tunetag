# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  `.wav`, `.aif` / `.aiff` / `.aifc`, `.ogg` / `.opus` (read
  paths only), `.ape` / `.wv`, and `.aac` paths.

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

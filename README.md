# tunetag

[![test](https://github.com/cabbagekobe/tunetag/actions/workflows/test.yml/badge.svg)](https://github.com/cabbagekobe/tunetag/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cabbagekobe/tunetag.svg)](https://pkg.go.dev/github.com/cabbagekobe/tunetag)

Pure Go audio metadata library. Reads and writes tags for **MP3**,
**FLAC**, **MP4 / M4A**, **WAV**, **AIFF / AIFC**, **Ogg Vorbis /
Opus**, **APEv2** (Monkey's Audio / WavPack), raw **AAC**, and
**ASF / WMA**. All using only the Go standard library — no cgo,
no bundled WASM, no external taglib.

## Status

Approaching feature-complete for v1. Read and write paths are
solid for every supported container:

- **MP3**: ID3v1, ID3v2.2, ID3v2.3, ID3v2.4. Writes use in-place
  overwrites when the new tag fits in the existing slot and atomic
  temp-file rewrites otherwise. The v2.4 footer flag is honoured
  (mutually-exclusive with padding per spec).
- **FLAC**: VORBIS_COMMENT and PICTURE round-trip with PADDING-block
  absorption, atomic rewrite fallback, and non-metadata blocks
  preserved byte-for-byte.
- **MP4 / M4A**: in-place via sibling `free` absorption (Tier 1) and
  full atomic rewrite with stco / co64 patching (Tier 2/3). When
  patching would push a 32-bit chunk offset past 2^32-1, every stco
  in the file is auto-promoted to co64. Fragmented MP4 (mvex / moof)
  is detected and rejected.
- **WAV (RIFF/WAVE)**: LIST/INFO entries and embedded `id3 ` chunks
  (ID3v2 inside WAV) both round-trip. Non-metadata chunks
  (`fmt `, `data`, `fact`, `JUNK`, …) are preserved byte-for-byte.
  RF64 / BW64 (64-bit RIFF) is detected and rejected.
- **AIFF / AIFC**: NAME / AUTH / "(c) " / ANNO text chunks (with
  multi-instance ANNO) and embedded `ID3 ` chunks round-trip;
  non-metadata chunks (COMM / SSND / FVER / MARK / …) pass
  through verbatim. Big-endian sizes; both AIFF and AIFC form
  types are recognised.
- **Ogg Vorbis / Opus**: comment packets round-trip with full
  re-paging — the writer encodes a fresh comment packet, splits
  it across as many pages as needed (with continuation flags and
  per-page CRC-32), and rewrites subsequent pages of the same
  logical bitstream with shifted sequence numbers. Cover art via
  Xiph's `METADATA_BLOCK_PICTURE` (base64 of a FLAC PICTURE
  block) is supported.
- **APEv2** (Monkey's Audio `.ape`, WavPack `.wv`, or any file
  with an APEv2 trailer): text items, binary items, and cover
  art ("Cover Art (Front)" / "(Back)" / "(Other)") round-trip.
  ID3v1 trailers after the APEv2 tag are preserved.
- **Raw AAC (ADTS)**: optional leading ID3v2 + trailing ID3v1
  round-trip; bare ADTS files (no tags) are recognised so
  `tunetag.Open` returns an empty tag rather than
  `ErrUnknownFormat`.
- **ASF / WMA**: Content Description Object (Title / Author /
  Copyright / Description / Rating) and Extended Content
  Description Object (any WM/* descriptor, with typed
  Bool / Word / DWord / QWord / String / Binary values). WM/Picture
  cover art is supported. Non-metadata Header child objects
  (File Properties, Stream Properties, Header Extension, Codec
  List, …) and the Data + Index objects round-trip
  byte-for-byte.

## Install

```
go get github.com/cabbagekobe/tunetag
```

Requires Go 1.23 or later.

## Format support matrix

| Container | Read | Write | Notes |
|-----------|:----:|:-----:|-------|
| ID3v1 / v1.1                | ✅ | ✅ | trailer in / out, Winamp genres |
| ID3v2.2                     | ✅ | ✅ | 4-char canonical IDs in memory; PIC body translated |
| ID3v2.3                     | ✅ | ✅ | UTF-16 default to preserve CJK |
| ID3v2.4                     | ✅ | ✅ | UTF-8 default |
| ID3v2 unsynchronisation     | ✅ | ❌ | decoded on read; never re-emitted |
| ID3v2 extended header       | ✅ | ❌ | read-skipped; not preserved |
| ID3v2 footer (v2.4)         | ✅ | ✅ | excludes padding when emitted |
| FLAC VORBIS_COMMENT         | ✅ | ✅ | UTF-8, case-insensitive lookup |
| FLAC PICTURE                | ✅ | ✅ | 21 ID3-compatible picture types |
| FLAC unknown blocks         | ✅ | ✅ | preserved verbatim |
| MP4 ilst (©nam, ©ART, …)    | ✅ | ✅ | Tier 1 in-place + Tier 2/3 rewrite |
| MP4 freeform `----` (read)  | ✅ | ✅ | mean / name / data preserved |
| MP4 covr (JPEG / PNG)       | ✅ | ✅ | as above |
| MP4 stco / co64 patch       | ✅ | ✅ | shifted by moov delta on rewrite |
| stco → co64 auto promotion  | — | ✅ | triggered when entries overflow |
| Fragmented MP4 (mvex/moof)  | — | ❌ | rejected on write |
| WAV LIST/INFO               | ✅ | ✅ | INAM / IART / IPRD / ICRD / IGNR / ICMT / ITRK |
| WAV embedded `id3 ` chunk   | ✅ | ✅ | full ID3v2 tag round-trip (incl. APIC) |
| WAV non-metadata chunks     | ✅ | ✅ | `fmt `, `data`, `fact`, `JUNK`, … preserved verbatim |
| RF64 / BW64 (64-bit RIFF)   | — | ❌ | detected and rejected |
| AIFF / AIFC text chunks     | ✅ | ✅ | NAME / AUTH / "(c) " / ANNO (multi-instance) |
| AIFF embedded `ID3 ` chunk  | ✅ | ✅ | full ID3v2 round-trip; preferred over text chunks |
| AIFF non-metadata chunks    | ✅ | ✅ | COMM / SSND / FVER / MARK / … preserved verbatim |
| Ogg Vorbis comment header   | ✅ | ✅ | re-pages comment packet + renumbers / re-CRCs subsequent pages |
| Ogg Opus comment header     | ✅ | ✅ | as above; supports OpusHead / OpusTags variant |
| Ogg METADATA_BLOCK_PICTURE  | ✅ | ✅ | base64-wrapped FLAC PICTURE block; shared format helpers |
| APEv2 (.ape / .wv / any)    | ✅ | ✅ | text + binary items, with/without header, ID3v1-coexistence |
| APEv2 Cover Art             | ✅ | ✅ | "Cover Art (Front)" / "(Back)" / "(Other)" binary items |
| APEv1                       | — | ❌ | refused with ErrUnsupportedVersion |
| Raw AAC (ADTS)              | ✅ | ✅ | leading ID3v2 prefix + trailing ID3v1; bare ADTS recognised |
| ASF / WMA CDO               | ✅ | ✅ | Title / Author / Copyright / Description / Rating |
| ASF / WMA ECDO              | ✅ | ✅ | WM/AlbumTitle / WM/AlbumArtist / WM/Year / WM/Genre / WM/TrackNumber / WM/Composer / WM/PartOfSet / … |
| ASF WM/Picture              | ✅ | ✅ | full cover-art round-trip |
| ASF non-metadata objects    | ✅ | ✅ | File Properties / Stream Properties / Header Extension / Data / Index preserved verbatim |

## Usage

### Reading any container

```go
tag, err := tunetag.Open("song.mp3")
if err != nil { log.Fatal(err) }
fmt.Println(tag.Title(), tag.Artist(), tag.Year(), tag.Format())
```

`tunetag.Tag` is a read-only common interface (`Title`, `Artist`,
`Album`, `Year`, `TrackNumber`, `DiscNumber`, `Genre`, `Composer`,
`Comment`, `Pictures`, `Format`). For writes, use the format-
specific subpackages.

### MP3 (ID3v2)

```go
t, err := id3v2.ReadFile("song.mp3")
if err != nil { log.Fatal(err) }
t.SetTitle("New Title")
t.SetArtist("New Artist")
if err := t.WriteFile("song.mp3"); err != nil { log.Fatal(err) }
```

The first edit usually rewrites the file (because the source has no
padding); subsequent edits fit in the 1 KiB padding tunetag adds by
default and stay in place.

### FLAC

```go
f, err := flac.ReadFile("song.flac")
if err != nil { log.Fatal(err) }
vc := f.VorbisComment() // creates one if absent
vc.Set("TITLE", "New Title")
vc.Set("DATE", "2026")
if err := f.WriteFile("song.flac"); err != nil { log.Fatal(err) }
```

PADDING blocks are absorbed / created automatically so the audio
offset stays stable when room exists. Otherwise the file is
rewritten via a temp file and atomic rename.

### MP4 / M4A

```go
m, err := mp4.Read("song.m4a")
if err != nil { log.Fatal(err) }
m.Tag.SetTitle("New Title")
m.Tag.SetArtist("New Artist")
m.Tag.SetTrack(3, 12)
if err := m.WriteFile("song.m4a"); err != nil {
    // Fragmented MP4 returns mp4.ErrFragmentedUnsupport. Any other
    // failure is an I/O or container error.
    log.Fatal(err)
}
```

### WAV

```go
w, err := wav.ReadFile("song.wav")
if err != nil { log.Fatal(err) }
// LIST/INFO entries:
w.SetInfo(wav.InfoTitle,  "New Title")
w.SetInfo(wav.InfoArtist, "New Artist")
w.SetInfo(wav.InfoDate,   "2026")
// Or use the embedded id3 chunk for richer fields (APIC, etc.):
if w.ID3 == nil {
    w.ID3 = &id3v2.Tag{Version: id3v2.V24, Padding: 0}
}
w.ID3.SetTitle("New Title")
if err := w.WriteFile("song.wav"); err != nil { log.Fatal(err) }
```

When both a LIST/INFO chunk and an `id3 ` chunk are present, the
common `tunetag.Tag` interface prefers the `id3 ` chunk's values.
RF64 / BW64 (64-bit RIFF) files are rejected with
`wav.ErrRF64Unsupported` rather than silently mis-parsed.

### AIFF / AIFC

```go
a, err := aiff.ReadFile("song.aif")
if err != nil { log.Fatal(err) }
a.SetTitle("New Title")     // NAME chunk
a.SetAuthor("New Artist")   // AUTH chunk
// Or use the embedded ID3 tag for richer fields:
if a.ID3 == nil { a.ID3 = &id3v2.Tag{Version: id3v2.V24} }
a.ID3.SetAlbum("Album")
a.ID3.SetText("TDRC", "2026")
if err := a.WriteFile("song.aif"); err != nil { log.Fatal(err) }
```

### Ogg Vorbis / Opus

```go
o, err := ogg.ReadFile("song.ogg")
if err != nil { log.Fatal(err) }
o.Comments.Set("TITLE",  "New Title")
o.Comments.Set("ARTIST", "New Artist")
if err := o.WriteFile("song.ogg"); err != nil { log.Fatal(err) }
```

The writer encodes the new comment packet, re-pages it (a single
page when the bytes fit; multiple pages with continuation flags
when they don't), and rewrites the per-page sequence numbers +
CRC-32 for every subsequent page of the same logical bitstream.
Concurrently-multiplexed streams pass through unchanged.

Cover art uses the Xiph `METADATA_BLOCK_PICTURE` convention (the
value is base64 of a FLAC PICTURE block):

```go
o.AddPicture(&flac.Picture{
    PictureType: 3, // CoverFront
    MIME:        "image/jpeg",
    Data:        jpegBytes,
})
```

### APEv2 (Monkey's Audio / WavPack)

```go
t, err := ape.ReadFile("song.wv")
if err != nil { log.Fatal(err) }
t.Set(ape.KeyTitle,  "New Title")
t.Set(ape.KeyArtist, "New Artist")
t.Set(ape.KeyYear,   "2026")
if err := t.WriteFile("song.wv"); err != nil { log.Fatal(err) }
```

The same package works on any file with an APEv2 trailer (MPC,
MP3-with-APE, etc.). An ID3v1 trailer following the APEv2 tag is
preserved across writes.

Cover art is stored as a binary item under "Cover Art (Front)"
(or "(Back)" / "(Other)"). The on-disk value is
`<filename>\x00<image bytes>`:

```go
t.AddPicture(&ape.Picture{Filename: "cover.jpg", Data: jpegBytes})
```

### Raw AAC (ADTS)

```go
a, err := aac.ReadFile("song.aac")
if err != nil { log.Fatal(err) }
if a.V2 == nil { a.V2 = &id3v2.Tag{Version: id3v2.V24} }
a.V2.SetTitle("New Title")
if err := a.WriteFile("song.aac"); err != nil { log.Fatal(err) }
```

Bare ADTS files (no tags at all) are recognised as
`FormatAAC` so `tunetag.Open` succeeds and returns an empty tag
rather than `ErrUnknownFormat`.

### ASF / WMA

```go
w, err := asf.ReadFile("song.wma")
if err != nil { log.Fatal(err) }
w.Title = "New Title"
w.SetArtist("New Artist")  // CDO Author
w.SetAlbum("Best of 2026") // WM/AlbumTitle in the ECDO
w.SetYear(2026)
w.SetTrackNumber(3, 12)
if err := w.WriteFile("song.wma"); err != nil { log.Fatal(err) }
```

The package handles only ASF metadata (Header Object → Content
Description + Extended Content Description). File Properties,
Stream Properties, Header Extension, Codec List, Data Object,
and Index Object(s) round-trip byte-for-byte. RF64-style 64-bit
sizes inside Header Extension objects are preserved verbatim
(they live inside opaque child object bodies).

WM/Picture cover art is accessible via `File.Pictures()` /
`AddPicture` / `RemovePictures` for full read-write round-trip.

## CLI

A thin command-line driver lives in `cmd/tunetag`.

```
tunetag print  song.mp3
tunetag dump   song.mp3
tunetag set    song.mp3 --title="Hello" --artist="Alice" --year=2026 --track=3/12
tunetag strip  song.mp3
tunetag cover  song.mp3 --extract /tmp/cover.jpg
tunetag cover  song.mp3 --set    /tmp/cover.jpg
```

`print` shows the common metadata fields; `dump` lists every parsed
frame / ilst item / FLAC block including unknown or non-standard ones
(useful for inspecting iTunes private data, Traktor PRIV payloads,
etc.).

Build with `go install github.com/cabbagekobe/tunetag/cmd/tunetag@latest`.

## Practical patterns

- **Bulk library scan**: `tunetag.Open(path)` is ~50 µs per file
  (cycling through real-world MP3 / M4A fixtures on Apple M4 Pro).
  A 100 k-track library scans in roughly five seconds.
- **In-place re-tag without growing the file**: ID3v2 writes
  default to 1 KiB of padding, and FLAC writes absorb diffs into
  an existing PADDING block when possible. Mutating Title /
  Artist on an already-tagged file usually does not touch any
  audio bytes.
- **Preserving unknown metadata**: tunetag never silently drops
  data. Unknown ID3v2 frames are kept as `GenericFrame`; unknown
  FLAC blocks ride through as `RawBlock`; iTunes purchase info
  and freeform `----` atoms in MP4 are preserved across writes
  unless `Strip` is called.

## Concurrency

A `*Tag`, `*flac.File`, or `*mp4.File` value is **not** safe for
concurrent use. Holding format-specific values across goroutines
requires external synchronisation. The pure parsing functions
(`id3v2.Read`, `flac.Read`, `mp4.Read`, `tunetag.Detect`) are
re-entrant.

## Comparison

- **`dhowden/tag`** — pure Go but read-only.
- **`bogem/id3v2`** — pure Go and read+write, but ID3 only.
- **`go-taglib/go-taglib`** — wide format coverage but ships an
  embedded WASM build of taglib; not strictly pure Go.

tunetag aims to fill the gap: multi-format, read+write, true pure Go.

## License

MIT — see [LICENSE](LICENSE).

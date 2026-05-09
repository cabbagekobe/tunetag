# tunetag

Pure Go audio metadata library. Reads and writes tags for **MP3**,
**FLAC**, and **MP4 / M4A** using only the Go standard library — no
cgo, no bundled WASM, no external taglib.

## Status

Approaching feature-complete for v1. Read paths are solid for all
three formats. Write paths cover:

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

## CLI

A thin command-line driver lives in `cmd/tunetag`.

```
tunetag print  song.mp3
tunetag set    song.mp3 --title="Hello" --artist="Alice" --year=2026 --track=3/12
tunetag strip  song.mp3
tunetag cover  song.mp3 --extract /tmp/cover.jpg
tunetag cover  song.mp3 --set    /tmp/cover.jpg
```

Build with `go install github.com/cabbagekobe/tunetag/cmd/tunetag@latest`.

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

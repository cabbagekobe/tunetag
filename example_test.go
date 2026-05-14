package tunetag_test

import (
	"fmt"
	"log"

	"github.com/cabbagekobe/tunetag"
	"github.com/cabbagekobe/tunetag/flac"
	"github.com/cabbagekobe/tunetag/id3v2"
	"github.com/cabbagekobe/tunetag/mp4"
)

// ExampleOpen shows the simplest read path: auto-detect the
// container and pull out a handful of common fields via the
// read-only Tag interface.
func ExampleOpen() {
	tag, err := tunetag.Open("/path/to/song.mp3")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(tag.Format(), tag.Title(), "/", tag.Artist())
}

// ExampleOpenMP3 demonstrates editing an MP3's ID3v2 tag in place
// and writing it back to disk. tunetag.OpenMP3 returns both the v2
// and v1 parses; writes go through the v2 representation.
func ExampleOpenMP3() {
	mp3, err := tunetag.OpenMP3("/path/to/song.mp3")
	if err != nil {
		log.Fatal(err)
	}
	if mp3.V2 == nil {
		// Build a fresh ID3v2.4 tag.
		mp3.V2 = &id3v2.Tag{Version: id3v2.V24, Padding: id3v2.DefaultPadding}
	}
	mp3.V2.SetTitle("New Title")
	mp3.V2.SetArtist("New Artist")
	if err := mp3.V2.WriteFile("/path/to/song.mp3"); err != nil {
		log.Fatal(err)
	}
}

// ExampleOpenFLAC shows the Vorbis Comment workflow: get the
// (creating-if-absent) VorbisComment block, mutate it, and call
// WriteFile to persist.
func ExampleOpenFLAC() {
	f, err := tunetag.OpenFLAC("/path/to/song.flac")
	if err != nil {
		log.Fatal(err)
	}
	vc := f.VorbisComment()
	vc.Set("TITLE", "New Title")
	vc.Set("DATE", "2026")
	if err := f.WriteFile("/path/to/song.flac"); err != nil {
		log.Fatal(err)
	}
}

// ExampleOpenMP4 demonstrates editing an MP4 / M4A. The library
// auto-promotes 32-bit stco chunk offsets to 64-bit co64 when a
// rewrite would otherwise overflow; freeform "----" items are
// preserved.
func ExampleOpenMP4() {
	m, err := tunetag.OpenMP4("/path/to/song.m4a")
	if err != nil {
		log.Fatal(err)
	}
	m.Tag.SetTitle("New Title")
	m.Tag.SetArtist("New Artist")
	m.Tag.SetTrack(3, 12)
	if err := m.WriteFile("/path/to/song.m4a"); err != nil {
		log.Fatal(err)
	}
}

// ExampleStrip removes every metadata block from a file, leaving
// the audio body untouched.
func ExampleStrip() {
	if err := tunetag.Strip("/path/to/song.mp3"); err != nil {
		log.Fatal(err)
	}
}

// ExampleTag_Pictures shows the cover-art read path through the
// common Tag interface.
func ExampleTag_Pictures() {
	tag, err := tunetag.Open("/path/to/song.mp3")
	if err != nil {
		log.Fatal(err)
	}
	for i, p := range tag.Pictures() {
		fmt.Printf("cover %d: %s (%d bytes)\n", i, p.MIME, len(p.Data))
	}
}

// Avoid unused-import warnings if a reader only inspects the
// top-level examples without the format-specific snippets.
var (
	_ = flac.ErrNoFLAC
	_ = mp4.ErrNotMP4
)

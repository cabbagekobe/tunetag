package id3v2

import (
	"bytes"
	"testing"
)

func TestAccessors_PopulatedTag(t *testing.T) {
	tag := &Tag{Version: V24, Padding: 0, Frames: []Frame{
		&TextFrame{FrameID: "TIT2", Encoding: EncUTF8, Text: []string{"Hello World"}},
		&TextFrame{FrameID: "TPE1", Encoding: EncUTF8, Text: []string{"Alice"}},
		&TextFrame{FrameID: "TPE2", Encoding: EncUTF8, Text: []string{"Various Artists"}},
		&TextFrame{FrameID: "TALB", Encoding: EncUTF8, Text: []string{"My Album"}},
		&TextFrame{FrameID: "TCOM", Encoding: EncUTF8, Text: []string{"Bob"}},
		&TextFrame{FrameID: "TCON", Encoding: EncUTF8, Text: []string{"Rock"}},
		&TextFrame{FrameID: "TDRC", Encoding: EncUTF8, Text: []string{"2026-05-09"}},
		&TextFrame{FrameID: "TRCK", Encoding: EncUTF8, Text: []string{"3/12"}},
		&TextFrame{FrameID: "TPOS", Encoding: EncUTF8, Text: []string{"1/2"}},
		&CommentFrame{Encoding: EncUTF8, Language: "eng", Text: "first comment"},
	}}
	if got := tag.Title(); got != "Hello World" {
		t.Errorf("Title = %q", got)
	}
	if got := tag.Artist(); got != "Alice" {
		t.Errorf("Artist = %q", got)
	}
	if got := tag.AlbumArtist(); got != "Various Artists" {
		t.Errorf("AlbumArtist = %q", got)
	}
	if got := tag.Album(); got != "My Album" {
		t.Errorf("Album = %q", got)
	}
	if got := tag.Composer(); got != "Bob" {
		t.Errorf("Composer = %q", got)
	}
	if got := tag.Genre(); got != "Rock" {
		t.Errorf("Genre = %q", got)
	}
	if got := tag.Year(); got != 2026 {
		t.Errorf("Year = %d", got)
	}
	if n, total := tag.TrackNumber(); n != 3 || total != 12 {
		t.Errorf("TrackNumber = %d/%d", n, total)
	}
	if n, total := tag.DiscNumber(); n != 1 || total != 2 {
		t.Errorf("DiscNumber = %d/%d", n, total)
	}
	if got := tag.Comment(); got != "first comment" {
		t.Errorf("Comment = %q", got)
	}
}

func TestAccessors_YearFromTYER(t *testing.T) {
	tag := &Tag{Version: V23, Frames: []Frame{
		&TextFrame{FrameID: "TYER", Encoding: EncISO88591, Text: []string{"1999"}},
	}}
	if got := tag.Year(); got != 1999 {
		t.Errorf("Year = %d", got)
	}
}

func TestAccessors_EmptyTagReturnsZeroValues(t *testing.T) {
	tag := &Tag{Version: V24}
	if tag.Title() != "" || tag.Artist() != "" || tag.Year() != 0 || tag.Comment() != "" {
		t.Errorf("expected zero values from empty tag")
	}
	n, total := tag.TrackNumber()
	if n != 0 || total != 0 {
		t.Errorf("TrackNumber = %d/%d", n, total)
	}
}

func TestSetText_ReplacesAndAppends(t *testing.T) {
	tag := &Tag{Version: V24, Padding: 0}
	tag.SetTitle("First")
	if tag.Title() != "First" {
		t.Errorf("after first set Title = %q", tag.Title())
	}
	tag.SetTitle("Second")
	if tag.Title() != "Second" {
		t.Errorf("after second set Title = %q", tag.Title())
	}
	count := 0
	for _, f := range tag.Frames {
		if f.ID() == "TIT2" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("TIT2 frames = %d, want 1", count)
	}
}

func TestSetText_EmptyRemoves(t *testing.T) {
	tag := &Tag{Version: V24, Padding: 0}
	tag.SetTitle("X")
	tag.SetTitle("")
	if tag.Title() != "" {
		t.Errorf("Title not removed")
	}
	for _, f := range tag.Frames {
		if f.ID() == "TIT2" {
			t.Fatalf("TIT2 still present after empty set")
		}
	}
}

func TestSetText_RoundTripsThroughEncode(t *testing.T) {
	tag := &Tag{Version: V24, Padding: 0}
	tag.SetTitle("テスト")
	tag.SetArtist("アーティスト")
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	out, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title() != "テスト" {
		t.Errorf("Title = %q", out.Title())
	}
	if out.Artist() != "アーティスト" {
		t.Errorf("Artist = %q", out.Artist())
	}
}

package mp4

import (
	"testing"
)

func TestParseIlst_TruncatedEntry(t *testing.T) {
	body := []byte{0x00, 0x00, 0x10, 0x00, 0xa9, 'n', 'a', 'm'}
	if _, err := parseIlst(body); err == nil {
		t.Fatal("expected error: ilst entry size exceeds body")
	}
}

func TestParseIlst_EmptyBody(t *testing.T) {
	out, err := parseIlst(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 0 {
		t.Errorf("Items = %d, want 0", len(out.Items))
	}
}

func TestIlst_EncodeRejectsEmptyKey(t *testing.T) {
	l := &Ilst{Items: []*Item{{Key: ""}}}
	if _, err := l.encode(); err == nil {
		t.Fatal("expected error: empty key")
	}
}

func TestIlst_EncodeFreeformRequiresMeanAndName(t *testing.T) {
	l := &Ilst{Items: []*Item{{Key: "----", Data: []*DataAtom{makeUTF8Data("x")}}}}
	if _, err := l.encode(); err == nil {
		t.Fatal("expected error: freeform missing mean/name")
	}
}

func TestIlst_FreeformEncodeRoundTrip(t *testing.T) {
	l := &Ilst{Items: []*Item{{
		Key:        "----",
		MeanDomain: "com.apple.iTunes",
		Name:       "MUSICBRAINZ_ID",
		Data:       []*DataAtom{makeUTF8Data("123-abc")},
	}}}
	body, err := l.encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseIlst(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d", len(out.Items))
	}
	it := out.Items[0]
	if it.Key != "----" || it.MeanDomain != "com.apple.iTunes" || it.Name != "MUSICBRAINZ_ID" {
		t.Errorf("freeform = %+v", it)
	}
	if len(it.Data) != 1 || it.Data[0].String() != "123-abc" {
		t.Errorf("data = %+v", it.Data)
	}
}

func TestIlst_SetNilRemoves(t *testing.T) {
	l := &Ilst{Items: []*Item{
		{Key: KeyTitle, Data: []*DataAtom{makeUTF8Data("x")}},
	}}
	l.Set(KeyTitle, nil)
	if l.First(KeyTitle) != nil {
		t.Errorf("Set(nil) did not remove")
	}
}

func TestIlst_SetFreeform_AddsAndUpdates(t *testing.T) {
	l := &Ilst{}
	l.SetFreeform("com.apple.iTunes", "initialkey", makeUTF8Data("8A"))
	got := l.FirstFreeform("com.apple.iTunes", "initialkey")
	if got == nil || got.String() != "8A" {
		t.Fatalf("FirstFreeform = %v, want \"8A\"", got)
	}
	// Update in place.
	l.SetFreeform("com.apple.iTunes", "initialkey", makeUTF8Data("9B"))
	got = l.FirstFreeform("com.apple.iTunes", "initialkey")
	if got == nil || got.String() != "9B" {
		t.Errorf("after update FirstFreeform = %v, want \"9B\"", got)
	}
	if n := len(l.Items); n != 1 {
		t.Errorf("Items after update = %d, want 1 (update must not duplicate)", n)
	}
}

func TestIlst_SetFreeformNilRemoves(t *testing.T) {
	l := &Ilst{}
	l.SetFreeform("com.apple.iTunes", "initialkey", makeUTF8Data("8A"))
	l.SetFreeform("com.apple.iTunes", "initialkey", nil)
	if got := l.FirstFreeform("com.apple.iTunes", "initialkey"); got != nil {
		t.Errorf("FirstFreeform = %v, want nil after SetFreeform(nil)", got)
	}
}

func TestIlst_RemoveFreeform_OnlyTargets(t *testing.T) {
	// Three coexisting items: standard Title, a freeform initialkey,
	// and an unrelated freeform iTunNORM. RemoveFreeform on the
	// initialkey pair must leave the other two alone.
	l := &Ilst{}
	l.SetTitle("Song")
	l.SetFreeform("com.apple.iTunes", "initialkey", makeUTF8Data("8A"))
	l.SetFreeform("com.apple.iTunes", "iTunNORM", makeUTF8Data(" 00000111"))

	l.RemoveFreeform("com.apple.iTunes", "initialkey")

	if l.Title() != "Song" {
		t.Errorf("Title = %q, want untouched", l.Title())
	}
	if got := l.FirstFreeform("com.apple.iTunes", "initialkey"); got != nil {
		t.Errorf("initialkey survived: %v", got)
	}
	if got := l.FirstFreeform("com.apple.iTunes", "iTunNORM"); got == nil {
		t.Errorf("iTunNORM was wrongly removed")
	}
}

func TestIlst_FreeformRoundTripsThroughEncode(t *testing.T) {
	l := &Ilst{}
	l.SetTitle("Song")
	l.SetFreeform("com.apple.iTunes", "initialkey", makeUTF8Data("8A"))

	body, err := l.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := parseIlst(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.Title() != "Song" {
		t.Errorf("Title = %q after round-trip", out.Title())
	}
	d := out.FirstFreeform("com.apple.iTunes", "initialkey")
	if d == nil || d.String() != "8A" {
		t.Errorf("freeform after round-trip = %v, want \"8A\"", d)
	}
}

func TestIlst_SetTextEmptyRemoves(t *testing.T) {
	l := &Ilst{}
	l.SetTitle("hello")
	l.SetTitle("")
	if l.Title() != "" {
		t.Errorf("Title = %q, want empty", l.Title())
	}
	if len(l.Items) != 0 {
		t.Errorf("Items = %d, want 0", len(l.Items))
	}
}

func TestIlst_TrackDiscDefaults(t *testing.T) {
	l := &Ilst{}
	if n, total := l.Track(); n != 0 || total != 0 {
		t.Errorf("Track default = (%d,%d)", n, total)
	}
	if n, total := l.Disc(); n != 0 || total != 0 {
		t.Errorf("Disc default = (%d,%d)", n, total)
	}
	l.SetTrack(3, 9)
	l.SetDisc(1, 2)
	if n, total := l.Track(); n != 3 || total != 9 {
		t.Errorf("Track = (%d,%d)", n, total)
	}
	if n, total := l.Disc(); n != 1 || total != 2 {
		t.Errorf("Disc = (%d,%d)", n, total)
	}
}

func TestIlst_Year_BadString(t *testing.T) {
	l := &Ilst{}
	l.SetDate("not-a-year")
	if l.Year() != 0 {
		t.Errorf("Year on non-numeric date = %d, want 0", l.Year())
	}
	l.SetDate("99")
	if l.Year() != 0 {
		t.Errorf("Year on 2-char date = %d, want 0", l.Year())
	}
	l.SetDate("2026-05-14")
	if l.Year() != 2026 {
		t.Errorf("Year = %d, want 2026", l.Year())
	}
}

func TestIlst_Disc_FromiTunesPayload(t *testing.T) {
	l := &Ilst{Items: []*Item{{
		Key: KeyDisc,
		Data: []*DataAtom{{
			TypeCode: DataTypeBinary,
			Payload:  []byte{0, 0, 0, 2, 0, 5},
		}},
	}}}
	n, total := l.Disc()
	if n != 2 || total != 5 {
		t.Errorf("Disc() = (%d,%d), want (2,5)", n, total)
	}
}

func TestIlst_AddCover_JPEG(t *testing.T) {
	l := &Ilst{}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	l.AddCover(jpeg)
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypeJPEG {
		t.Errorf("got %+v", pics)
	}
}

func TestAddCover_PNGDetection(t *testing.T) {
	l := &Ilst{}
	pngMagic := append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x01, 0x02)
	l.AddCover(pngMagic)
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypePNG {
		t.Errorf("got %+v", pics)
	}
}

func TestAddCover_UnknownMagicStaysBinary(t *testing.T) {
	l := &Ilst{}
	l.AddCover([]byte{0x00, 0x01, 0x02, 0x03})
	pics := l.Pictures()
	if len(pics) != 1 || pics[0].TypeCode != DataTypeBinary {
		t.Errorf("got %+v", pics)
	}
}

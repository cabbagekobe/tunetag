package flac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// --- Round-trip + helpers ---------------------------------------

func TestVorbisComment_RoundTrip(t *testing.T) {
	in := &VorbisComment{
		Vendor:   "tunetag-test",
		Comments: []string{"TITLE=Hello", "ARTIST=Alice", "ARTIST=Bob"},
	}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Vendor != in.Vendor {
		t.Errorf("Vendor = %q", out.Vendor)
	}
	if len(out.Comments) != 3 {
		t.Errorf("Comments len = %d", len(out.Comments))
	}
	for i := range in.Comments {
		if in.Comments[i] != out.Comments[i] {
			t.Errorf("Comments[%d] = %q, want %q", i, out.Comments[i], in.Comments[i])
		}
	}
}

func TestVorbisComment_GetSetRemove(t *testing.T) {
	vc := &VorbisComment{Vendor: "x"}
	vc.Set("title", "First Title")
	if got := vc.First("TITLE"); got != "First Title" {
		t.Errorf("case-insensitive lookup failed: %q", got)
	}
	vc.Set("Title", "Second Title")
	if vals := vc.Get("title"); len(vals) != 1 || vals[0] != "Second Title" {
		t.Errorf("Set should replace: %v", vals)
	}
	vc.Add("ARTIST", "A")
	vc.Add("artist", "B")
	if vals := vc.Get("Artist"); len(vals) != 2 {
		t.Errorf("Add multi-value: %v", vals)
	}
	vc.Remove("ARTIST")
	if vals := vc.Get("artist"); len(vals) != 0 {
		t.Errorf("Remove failed: %v", vals)
	}
}

func TestVorbisComment_RemoveAllOccurrences(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"ARTIST=A", "Artist=B", "artist=C", "TITLE=X",
	}}
	vc.Remove("ARTIST")
	if vc.Get("artist") != nil {
		t.Errorf("Remove left artist values: %v", vc.Comments)
	}
	if vc.First("TITLE") != "X" {
		t.Errorf("Remove dropped unrelated entry: %v", vc.Comments)
	}
}

func TestVorbisComment_SetEmptyClearsKey(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"TITLE=X"}}
	vc.Set("TITLE", "")
	if len(vc.Comments) != 0 {
		t.Errorf("Set with empty value should clear: %v", vc.Comments)
	}
}

func TestVorbisComment_VendorEmptyDefaults(t *testing.T) {
	vc := &VorbisComment{}
	body, err := vc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Vendor != VendorString {
		t.Errorf("Vendor = %q, want %q", out.Vendor, VendorString)
	}
}

func TestVC_Set_RemovesAllMatchingCaseInsensitive(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"TITLE=A", "Title=B", "title=C", "Artist=X",
	}}
	vc.Set("title", "Z")
	titles := vc.Get("TITLE")
	if len(titles) != 1 || titles[0] != "Z" {
		t.Errorf("Get TITLE = %v, want [Z]", titles)
	}
	if vc.First("Artist") != "X" {
		t.Errorf("Artist dropped accidentally")
	}
}

func TestVC_Add_AppendsDuplicates(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"GENRE=Rock"}}
	vc.Add("GENRE", "Jazz")
	vc.Add("GENRE", "Blues")
	got := vc.Get("GENRE")
	if len(got) != 3 {
		t.Errorf("GENRE count = %d, want 3", len(got))
	}
}

func TestVC_First_EmptyOnMissing(t *testing.T) {
	vc := &VorbisComment{}
	if got := vc.First("NOPE"); got != "" {
		t.Errorf("First on missing key = %q, want empty", got)
	}
}

func TestVC_Get_PreservesValueOrder(t *testing.T) {
	vc := &VorbisComment{Comments: []string{
		"ARTIST=A", "TITLE=T", "ARTIST=B", "ARTIST=C",
	}}
	got := vc.Get("ARTIST")
	want := []string{"A", "B", "C"}
	if len(got) != 3 {
		t.Fatalf("Get count = %d", len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Get[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVC_RemoveCaseInsensitive(t *testing.T) {
	vc := &VorbisComment{Comments: []string{"Date=2026", "DATE=2025"}}
	vc.Remove("date")
	if vc.Get("DATE") != nil {
		t.Errorf("Remove left entries: %v", vc.Comments)
	}
}

func TestVC_RoundTripPreservesCase(t *testing.T) {
	in := &VorbisComment{Vendor: "vendor", Comments: []string{"TiTlE=case-preserved"}}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Comments) != 1 || out.Comments[0] != "TiTlE=case-preserved" {
		t.Errorf("comment = %v, want unchanged case", out.Comments)
	}
	if v := out.First("title"); v != "case-preserved" {
		t.Errorf("First = %q", v)
	}
}

// --- parseVorbisComment defensive ---------------------------------

func TestParseVorbisComment_CountOverflowDoesNotAllocate(t *testing.T) {
	// Regression: a count of 0xFFFFFFFF used to flow into
	// make([]string, 0, count) and try to allocate ~32 GiB, which
	// crashed Go fuzz workers with "hung or terminated unexpectedly".
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0))
	_ = binary.Write(&body, binary.LittleEndian, uint32(0xFFFFFFFF))
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: comment count overflow")
	}
}

func TestParseVorbisComment_CountBoundaryFits(t *testing.T) {
	// count*4 == remaining bytes: two empty comments fit exactly.
	// Guards against an off-by-one tightening of the count check.
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len
	_ = binary.Write(&body, binary.LittleEndian, uint32(2)) // 2 comments
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // comment 0 len
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // comment 1 len
	vc, err := parseVorbisComment(body.Bytes())
	if err != nil {
		t.Fatalf("expected success at exact boundary, got %v", err)
	}
	if len(vc.Comments) != 2 {
		t.Fatalf("Comments = %d, want 2", len(vc.Comments))
	}
}

func TestParseVorbisComment_CountBoundaryJustOver(t *testing.T) {
	// count*4 == remaining + 1: should be rejected by the early guard.
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len
	_ = binary.Write(&body, binary.LittleEndian, uint32(3)) // 3 comments
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // only 2 length prefixes follow
	_ = binary.Write(&body, binary.LittleEndian, uint32(0))
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: comment count exceeds body by one entry")
	}
}

func TestRead_VorbisCommentStringLenOverflow(t *testing.T) {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // vendor len
	_ = binary.Write(&body, binary.LittleEndian, uint32(1)) // 1 comment
	_ = binary.Write(&body, binary.LittleEndian, uint32(0xFFFFFFFF))
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: comment length exceeds body")
	}
}

func TestRead_VorbisCommentNoEqualsSign(t *testing.T) {
	in := &VorbisComment{Vendor: "v", Comments: []string{"NotAValidEntry"}}
	body, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := parseVorbisComment(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Comments) != 1 {
		t.Fatalf("Comments = %d, want 1", len(out.Comments))
	}
	if got := out.Get("NotAValidEntry"); len(got) != 1 || got[0] != "" {
		t.Errorf("Get(\"NotAValidEntry\") = %#v, want [\"\"]", got)
	}
	if out.Get("TITLE") != nil {
		t.Errorf("unrelated key should return nil")
	}
}

func TestParseVorbisComment_TruncatedVendorLen(t *testing.T) {
	if _, err := parseVorbisComment([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error: truncated vendor length")
	}
}

func TestParseVorbisComment_VendorLenExceedsBody(t *testing.T) {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(100))
	body.WriteString("x")
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: vendor length exceeds body")
	}
}

func TestParseVorbisComment_TruncatedBetweenVendorAndCount(t *testing.T) {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0))
	body.WriteByte(0)
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: truncated before comment count")
	}
}

func TestParseVorbisComment_TruncatedAtCommentI(t *testing.T) {
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, uint32(0))
	_ = binary.Write(&body, binary.LittleEndian, uint32(2))
	_ = binary.Write(&body, binary.LittleEndian, uint32(1))
	body.WriteByte('A')
	if _, err := parseVorbisComment(body.Bytes()); err == nil {
		t.Fatal("expected error: truncated at comment 1")
	}
}

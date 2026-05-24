package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/cabbagekobe/tunetag/flac"
)

// buildPage assembles a single Ogg page with the given packets.
// headerType: 0x02 for BOS, 0x04 for EOS, 0 otherwise. Packets
// shorter than 255 bytes each give one segment of (len) and the
// page contains exactly len(packets) packets (no continuation).
//
// CRC is left zero (the reader does not verify checksums).
func buildPage(serial uint32, seqNum uint32, headerType byte, packets ...[]byte) []byte {
	var segs []byte
	var body bytes.Buffer
	for _, pkt := range packets {
		n := len(pkt)
		for n >= 255 {
			segs = append(segs, 255)
			n -= 255
		}
		segs = append(segs, byte(n))
		body.Write(pkt)
	}
	var out bytes.Buffer
	out.WriteString("OggS")
	out.WriteByte(0)          // version
	out.WriteByte(headerType) // flags
	out.Write(make([]byte, 8))
	_ = binary.Write(&out, binary.LittleEndian, serial)
	_ = binary.Write(&out, binary.LittleEndian, seqNum)
	_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // CRC
	out.WriteByte(byte(len(segs)))
	out.Write(segs)
	out.Write(body.Bytes())
	return out.Bytes()
}

func buildVorbisIdent() []byte {
	// 30 bytes typical: 0x01 + "vorbis" + version(4) + channels(1) +
	// rate(4) + bitrates(12) + blocksize(1) + framing(1). We don't
	// need correct values for tag tests; just the magic prefix.
	pkt := []byte{0x01}
	pkt = append(pkt, []byte("vorbis")...)
	pkt = append(pkt, make([]byte, 23)...)
	return pkt
}

func buildVorbisComment(vendor string, pairs ...[2]string) []byte {
	vc := &flac.VorbisComment{Vendor: vendor}
	for _, p := range pairs {
		vc.Set(p[0], p[1])
	}
	body, err := vc.Encode()
	if err != nil {
		panic(err)
	}
	pkt := []byte{0x03}
	pkt = append(pkt, []byte("vorbis")...)
	pkt = append(pkt, body...)
	pkt = append(pkt, 0x01) // framing bit
	return pkt
}

func buildOpusHead() []byte {
	pkt := []byte("OpusHead")
	pkt = append(pkt, make([]byte, 11)...) // version, channels, preskip, etc.
	return pkt
}

func buildOpusComment(vendor string, pairs ...[2]string) []byte {
	vc := &flac.VorbisComment{Vendor: vendor}
	for _, p := range pairs {
		vc.Set(p[0], p[1])
	}
	body, err := vc.Encode()
	if err != nil {
		panic(err)
	}
	pkt := []byte("OpusTags")
	pkt = append(pkt, body...)
	return pkt
}

func TestRead_RejectsNonOgg(t *testing.T) {
	_, err := Read(bytes.NewReader([]byte("not ogg at all")))
	if !errors.Is(err, ErrNoOgg) {
		t.Errorf("got %v, want ErrNoOgg", err)
	}
}

func TestRead_Vorbis(t *testing.T) {
	ident := buildVorbisIdent()
	comment := buildVorbisComment("MyEncoder",
		[2]string{"TITLE", "Hello"},
		[2]string{"ARTIST", "Alice"},
		[2]string{"DATE", "2026-05-17"},
		[2]string{"TRACKNUMBER", "5"},
		[2]string{"TRACKTOTAL", "12"},
	)
	// BOS page with ident, then a page with the comment packet.
	stream := buildPage(0xCAFEBABE, 0, 0x02, ident)
	stream = append(stream, buildPage(0xCAFEBABE, 1, 0, comment)...)

	f, err := Read(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if f.Codec != CodecVorbis {
		t.Errorf("Codec = %s, want Vorbis", f.Codec)
	}
	if f.Vendor != "MyEncoder" {
		t.Errorf("Vendor = %q", f.Vendor)
	}
	if f.Title() != "Hello" || f.Artist() != "Alice" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
	if f.Year() != 2026 {
		t.Errorf("Year = %d", f.Year())
	}
	if n, total := f.TrackNumber(); n != 5 || total != 12 {
		t.Errorf("Track = %d/%d", n, total)
	}
}

func TestRead_Opus(t *testing.T) {
	ident := buildOpusHead()
	comment := buildOpusComment("opus-encoder",
		[2]string{"TITLE", "Sparkle"},
		[2]string{"ARTIST", "Bob"},
	)
	stream := buildPage(0xDEADBEEF, 0, 0x02, ident)
	stream = append(stream, buildPage(0xDEADBEEF, 1, 0, comment)...)

	f, err := Read(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if f.Codec != CodecOpus {
		t.Errorf("Codec = %s, want Opus", f.Codec)
	}
	if f.Title() != "Sparkle" || f.Artist() != "Bob" {
		t.Errorf("Title=%q Artist=%q", f.Title(), f.Artist())
	}
	if f.Vendor != "opus-encoder" {
		t.Errorf("Vendor = %q", f.Vendor)
	}
}

func TestRead_UnsupportedCodec(t *testing.T) {
	// A Speex stream begins with "Speex   " — not handled.
	ident := append([]byte("Speex   "), make([]byte, 20)...)
	stream := buildPage(1, 0, 0x02, ident)
	if _, err := Read(bytes.NewReader(stream)); !errors.Is(err, ErrUnsupportedCodec) {
		t.Errorf("got %v, want ErrUnsupportedCodec", err)
	}
}

func TestRead_TruncatedBeforeComment(t *testing.T) {
	ident := buildVorbisIdent()
	stream := buildPage(1, 0, 0x02, ident)
	// No comment page → truncation.
	if _, err := Read(bytes.NewReader(stream)); !errors.Is(err, ErrTruncated) {
		t.Errorf("got %v, want ErrTruncated", err)
	}
}

func TestRead_PacketSpanningPages(t *testing.T) {
	// Build a comment packet large enough to span two pages.
	bigVendor := string(bytes.Repeat([]byte("X"), 600))
	comment := buildVorbisComment(bigVendor, [2]string{"TITLE", "Big"})
	// Split the packet artificially: first page has 255+255 segs,
	// the rest spills into the next page.
	ident := buildVorbisIdent()

	// Manually craft two pages with one packet split across both.
	// We do this by emitting two pages where the first one's
	// trailing segment is 255 (continuation indicator) and the
	// second page's first segment finishes the packet.
	first := buildPage(7, 0, 0x02, ident)

	// Build the second BOS-after-ident page split.
	splitAt := 510 // forces two 255-byte segments on this page
	if splitAt > len(comment) {
		splitAt = len(comment)
	}
	pageA := buildPageSplit(7, 1, 0, comment[:splitAt])
	pageB := buildPage(7, 2, 0x01, comment[splitAt:]) // continuation flag
	stream := append(append(first, pageA...), pageB...)

	f, err := Read(bytes.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "Big" {
		t.Errorf("Title = %q", f.Title())
	}
}

// buildPageSplit emits a page whose packet bytes don't form a
// complete packet on this page — i.e. the final segment is 255 so
// the packet continues on the next page. The provided body length
// must be a multiple of 255.
func buildPageSplit(serial, seq uint32, flags byte, body []byte) []byte {
	if len(body)%255 != 0 {
		// Add a trailing 255-segment by padding; this is just
		// for test convenience.
		panic("buildPageSplit: body must be multiple of 255")
	}
	var segs []byte
	for i := 0; i < len(body)/255; i++ {
		segs = append(segs, 255)
	}
	var out bytes.Buffer
	out.WriteString("OggS")
	out.WriteByte(0)
	out.WriteByte(flags)
	out.Write(make([]byte, 8))
	_ = binary.Write(&out, binary.LittleEndian, serial)
	_ = binary.Write(&out, binary.LittleEndian, seq)
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	out.WriteByte(byte(len(segs)))
	out.Write(segs)
	out.Write(body)
	return out.Bytes()
}

func TestWriteFile_RoundTrip(t *testing.T) {
	ident := buildVorbisIdent()
	comment := buildVorbisComment("orig-vendor", [2]string{"TITLE", "First"})
	// Audio page that follows the comment.
	audio := buildPage(7, 2, 0, []byte("\x00\x00\x00\x00fake audio packet"))

	stream := buildPage(7, 0, 0x02, ident)
	stream = append(stream, buildPage(7, 1, 0, comment)...)
	stream = append(stream, audio...)

	p := writeOggTemp(t, stream)
	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Comments.Set("TITLE", "Second")
	f.Comments.Set("ARTIST", "Someone")
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title() != "Second" || g.Artist() != "Someone" {
		t.Errorf("after write: Title=%q Artist=%q", g.Title(), g.Artist())
	}
	// Audio page should still be present at the end of the
	// file (we can't readily decode it but we can search for
	// our distinctive marker bytes).
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("fake audio packet")) {
		t.Errorf("audio packet body lost after WriteFile")
	}
}

func TestWriteFile_PageCountChange(t *testing.T) {
	// Build a comment with a small payload, then grow it
	// beyond one page on rewrite. The subsequent audio page's
	// sequence number must be shifted and its CRC recomputed
	// so Read continues to succeed.
	ident := buildVorbisIdent()
	comment := buildVorbisComment("v", [2]string{"TITLE", "x"})
	audio := buildPage(99, 2, 0, []byte("\x00\x00\x00\x00audio"))

	stream := buildPage(99, 0, 0x02, ident)
	stream = append(stream, buildPage(99, 1, 0, comment)...)
	stream = append(stream, audio...)

	p := writeOggTemp(t, stream)
	f, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Inflate the comment to span two pages: 60 KiB > 65025 max
	// per page minus headers, so two pages required.
	big := string(bytes.Repeat([]byte("X"), 70000))
	f.Comments.Set("TITLE", big)
	if err := f.WriteFile(p); err != nil {
		t.Fatal(err)
	}
	g, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.Title() != big {
		t.Errorf("Title mismatch after multi-page comment write (got %d chars, want %d)", len(g.Title()), len(big))
	}
}

func writeOggTemp(t *testing.T, body []byte) string {
	t.Helper()
	p := tempPath(t, "x.ogg")
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func tempPath(t *testing.T, name string) string {
	t.Helper()
	return t.TempDir() + "/" + name
}

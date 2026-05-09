// Package testutil provides minimal MP4 byte-blob fixtures for the
// mp4 package tests. The generated files are not playable; their
// only purpose is to exercise tunetag's box reader and writer.
package testutil

import (
	"bytes"
	"encoding/binary"
)

// box wraps body in a 32-bit-size MP4 box.
func box(typ string, body []byte) []byte {
	if len(typ) != 4 {
		panic("box: type must be 4 bytes")
	}
	out := make([]byte, 0, 8+len(body))
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(8+len(body)))
	copy(hdr[4:8], typ)
	out = append(out, hdr[:]...)
	out = append(out, body...)
	return out
}

// concat returns the concatenation of every byte slice in chunks.
func concat(chunks ...[]byte) []byte {
	var total int
	for _, c := range chunks {
		total += len(c)
	}
	out := make([]byte, 0, total)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}

// data builds the body of a "data" atom.
func data(typeCode uint32, payload []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, typeCode)
	binary.Write(&b, binary.BigEndian, uint32(0)) // locale
	b.Write(payload)
	return b.Bytes()
}

// MinimalOptions configures BuildMinimal. Zero values are sensible
// defaults: title "Hello", no other tags, mdat after moov, 16 KiB of
// dummy mdat bytes, and a 0-byte trailing free atom.
type MinimalOptions struct {
	Title      string
	Artist     string
	Album      string
	FreeBytes  int  // size of a sibling free atom inside meta (incl header); 0 = none
	MdatBytes  int  // length of mdat body; default 16 if 0
	MdatBefore bool // place mdat before moov

	// WithStco adds a single trak/mdia/minf/stbl/stco branch
	// containing the given uint32 offsets. The values are stored
	// verbatim and let tests verify that Tier 2 rewrites patch them
	// by the expected delta. Empty disables the trak entirely.
	WithStco []uint32
}

// BuildMinimal returns a minimal MP4-shaped byte slice with an
// ftyp, a moov containing only udta/meta/ilst (no trak/stbl), and
// an mdat. There is intentionally no stco/co64 so Tier 2 patching
// is not exercised by these fixtures.
func BuildMinimal(opt MinimalOptions) []byte {
	mdatLen := opt.MdatBytes
	if mdatLen == 0 {
		mdatLen = 16
	}
	mdat := box("mdat", bytes.Repeat([]byte{0xAA}, mdatLen))

	// Build ilst items.
	var ilstBody bytes.Buffer
	addText := func(key, value string) {
		if value == "" {
			return
		}
		entry := box("data", data(1, []byte(value)))
		ilstBody.Write(box(key, entry))
	}
	addText("\xa9nam", opt.Title)
	addText("\xa9ART", opt.Artist)
	addText("\xa9alb", opt.Album)

	ilst := box("ilst", ilstBody.Bytes())

	// hdlr inside meta: minimal valid handler.
	hdlrBody := concat(
		[]byte{0x00, 0x00, 0x00, 0x00},               // version+flags
		[]byte{0x00, 0x00, 0x00, 0x00},               // pre_defined
		[]byte("mdir"),                                // handler_type ("mdir")
		[]byte{0x00, 0x00, 0x00, 0x00},               // reserved[0]
		[]byte("appl"),                                // reserved[1] (any 4 bytes)
		[]byte{0x00, 0x00, 0x00, 0x00},               // reserved[2]
		[]byte{0x00},                                  // empty name (null-terminated)
	)
	hdlr := box("hdlr", hdlrBody)

	metaInner := concat(hdlr, ilst)
	if opt.FreeBytes > 0 {
		freeBody := bytes.Repeat([]byte{0x00}, opt.FreeBytes-8)
		metaInner = concat(metaInner, box("free", freeBody))
	}
	// meta is a FullBox: 4-byte version+flags before children.
	metaBody := append([]byte{0x00, 0x00, 0x00, 0x00}, metaInner...)
	meta := box("meta", metaBody)

	udta := box("udta", meta)

	moovChildren := udta
	if len(opt.WithStco) > 0 {
		moovChildren = concat(buildTrakWithStco(opt.WithStco), udta)
	}
	moov := box("moov", moovChildren)

	ftyp := box("ftyp", concat(
		[]byte("M4A "),                          // major_brand
		[]byte{0x00, 0x00, 0x00, 0x00},          // minor_version
		[]byte("M4A mp42isom"),                   // compatible_brands
	))

	if opt.MdatBefore {
		return concat(ftyp, mdat, moov)
	}
	return concat(ftyp, moov, mdat)
}

// buildTrakWithStco builds a minimal trak/mdia/minf/stbl chain with
// a single stco box carrying the given offsets. None of the boxes
// are valid for playback — only the stco entries are meaningful.
func buildTrakWithStco(offsets []uint32) []byte {
	var stco bytes.Buffer
	stco.Write([]byte{0x00, 0x00, 0x00, 0x00}) // version+flags
	binary.Write(&stco, binary.BigEndian, uint32(len(offsets)))
	for _, o := range offsets {
		binary.Write(&stco, binary.BigEndian, o)
	}
	stbl := box("stbl", box("stco", stco.Bytes()))
	minf := box("minf", stbl)
	mdia := box("mdia", minf)
	return box("trak", mdia)
}

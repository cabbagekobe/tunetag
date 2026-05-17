// Package ogg reads Vorbis Comment metadata from Ogg Vorbis and
// Ogg Opus streams.
//
// Both codecs use Vorbis Comment for tags. The encapsulating
// difference is the codec-specific magic that prefixes the
// comment packet:
//
//   - Vorbis : 0x03 + "vorbis" (7 bytes), followed by the Vorbis
//     comment block, followed by a trailing framing bit (0x01).
//   - Opus   : "OpusTags" (8 bytes), followed by the Vorbis
//     comment block (no framing bit).
//
// Read parses only the first logical bitstream's header packets
// (the identification packet and the comment packet) and
// captures enough state for WriteFile to splice in a re-paged
// replacement comment packet. The audio packets are not
// decoded; their bytes round-trip with sequence numbers
// adjusted and per-page CRCs recomputed when the comment
// packet's page count changes.
//
// A *File is not safe for concurrent use.
package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cabbagekobe/tunetag/flac"
)

// Codec identifies the audio codec inside the Ogg container.
type Codec int

const (
	CodecUnknown Codec = iota
	CodecVorbis
	CodecOpus
)

func (c Codec) String() string {
	switch c {
	case CodecVorbis:
		return "Vorbis"
	case CodecOpus:
		return "Opus"
	default:
		return "Unknown"
	}
}

// Errors returned by this package.
var (
	// ErrNoOgg is returned when the input does not begin with an
	// "OggS" page.
	ErrNoOgg = errors.New("ogg: not an Ogg stream")

	// ErrUnsupportedCodec is returned when the first logical
	// bitstream is neither Vorbis nor Opus.
	ErrUnsupportedCodec = errors.New("ogg: unsupported codec (only Vorbis and Opus are recognised)")

	// ErrTruncated is returned when the stream ends before the
	// comment packet has been fully read.
	ErrTruncated = errors.New("ogg: stream ended before comment header was complete")
)

// File holds the parsed Vorbis-Comment metadata of an Ogg stream.
// The original raw bytes are not retained; this is a read-only
// view.
type File struct {
	Codec    Codec
	Vendor   string
	Comments *flac.VorbisComment // shares the same on-disk format

	// Serial is the bitstream serial number of the first logical
	// stream. Provided for callers that want to verify identity.
	Serial uint32
}

// Read parses the first logical bitstream of rs and returns its
// codec + comment metadata.
func Read(rs io.ReadSeeker) (*File, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	br, err := newPacketReader(rs)
	if err != nil {
		return nil, err
	}
	// Packet 1: identification header. Use it to detect codec.
	pkt1, err := br.next()
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
	codec, err := detectCodec(pkt1)
	if err != nil {
		return nil, err
	}
	// Packet 2: comment header.
	pkt2, err := br.next()
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
	commentBody, err := stripCommentPrefix(codec, pkt2)
	if err != nil {
		return nil, err
	}
	vc, err := flac.ParseVorbisComment(commentBody)
	if err != nil {
		return nil, fmt.Errorf("ogg: parse comment block: %w", err)
	}
	return &File{
		Codec:    codec,
		Vendor:   vc.Vendor,
		Comments: vc,
		Serial:   br.serial,
	}, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// detectCodec inspects the first packet of a logical Ogg
// bitstream and returns the matching codec.
func detectCodec(pkt []byte) (Codec, error) {
	if len(pkt) >= 7 && pkt[0] == 0x01 && string(pkt[1:7]) == "vorbis" {
		return CodecVorbis, nil
	}
	if len(pkt) >= 8 && string(pkt[0:8]) == "OpusHead" {
		return CodecOpus, nil
	}
	return CodecUnknown, ErrUnsupportedCodec
}

// stripCommentPrefix removes the codec-specific magic that wraps
// the comment header packet and returns the bare Vorbis-Comment
// block body.
func stripCommentPrefix(codec Codec, pkt []byte) ([]byte, error) {
	switch codec {
	case CodecVorbis:
		if len(pkt) < 7 || pkt[0] != 0x03 || string(pkt[1:7]) != "vorbis" {
			return nil, fmt.Errorf("ogg: Vorbis comment packet missing 0x03 \"vorbis\" prefix")
		}
		body := pkt[7:]
		// The Vorbis spec terminates the packet with a framing
		// bit (last byte's LSB = 1). Strip it if present so the
		// trailing 0x01 isn't misread as part of a comment.
		if len(body) > 0 {
			body = body[:len(body)-1]
		}
		return body, nil
	case CodecOpus:
		if len(pkt) < 8 || string(pkt[0:8]) != "OpusTags" {
			return nil, fmt.Errorf("ogg: Opus comment packet missing \"OpusTags\" prefix")
		}
		return pkt[8:], nil
	}
	return nil, ErrUnsupportedCodec
}

// --- accessors -------------------------------------------------

// Vorbis Comment standard field names (case-insensitive on
// lookup).
const (
	FieldTitle       = "TITLE"
	FieldArtist      = "ARTIST"
	FieldAlbum       = "ALBUM"
	FieldAlbumArtist = "ALBUMARTIST"
	FieldDate        = "DATE"
	FieldGenre       = "GENRE"
	FieldComposer    = "COMPOSER"
	FieldTrack       = "TRACKNUMBER"
	FieldTrackTotal  = "TRACKTOTAL"
	FieldDisc        = "DISCNUMBER"
	FieldDiscTotal   = "DISCTOTAL"
	FieldDescription = "DESCRIPTION"
)

func (f *File) Title() string       { return f.first(FieldTitle) }
func (f *File) Artist() string      { return f.first(FieldArtist) }
func (f *File) Album() string       { return f.first(FieldAlbum) }
func (f *File) AlbumArtist() string { return f.first(FieldAlbumArtist) }
func (f *File) Composer() string    { return f.first(FieldComposer) }
func (f *File) Genre() string       { return f.first(FieldGenre) }
func (f *File) Comment() string     { return f.first(FieldDescription) }

func (f *File) Year() int {
	s := f.first(FieldDate)
	if len(s) < 4 {
		return 0
	}
	var y int
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

func (f *File) TrackNumber() (n, total int) {
	n = atoi(f.first(FieldTrack))
	total = atoi(f.first(FieldTrackTotal))
	if total == 0 {
		// Some files use "n/total" in TRACKNUMBER alone.
		_, t := parseSlash(f.first(FieldTrack))
		if t != 0 {
			total = t
		}
	}
	return n, total
}

func (f *File) DiscNumber() (n, total int) {
	n = atoi(f.first(FieldDisc))
	total = atoi(f.first(FieldDiscTotal))
	if total == 0 {
		_, t := parseSlash(f.first(FieldDisc))
		if t != 0 {
			total = t
		}
	}
	return n, total
}

func (f *File) first(key string) string {
	if f.Comments == nil {
		return ""
	}
	return f.Comments.First(key)
}

func atoi(s string) int {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func parseSlash(s string) (n, total int) {
	idx := -1
	for i, r := range s {
		if r == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return atoi(s), 0
	}
	return atoi(s[:idx]), atoi(s[idx+1:])
}

// --- Ogg page demuxer ------------------------------------------

// packetReader reassembles packets of the first logical
// bitstream. It only follows packets belonging to the BOS page's
// serial number, ignoring any concurrently-multiplexed streams
// (rare for Vorbis / Opus).
type packetReader struct {
	rs        io.Reader
	serial    uint32
	pending   bytes.Buffer // current packet's accumulated bytes
	queue     [][]byte     // completed packets not yet handed out
	exhausted bool
}

func newPacketReader(rs io.Reader) (*packetReader, error) {
	pr := &packetReader{rs: rs}
	if err := pr.readPage(true); err != nil {
		return nil, err
	}
	return pr, nil
}

// next returns the next reassembled packet. Returns io.EOF when
// the stream is exhausted before a complete packet is available.
func (p *packetReader) next() ([]byte, error) {
	for len(p.queue) == 0 {
		if p.exhausted {
			return nil, io.EOF
		}
		if err := p.readPage(false); err != nil {
			if errors.Is(err, io.EOF) {
				p.exhausted = true
				if len(p.queue) == 0 {
					return nil, io.EOF
				}
				break
			}
			return nil, err
		}
	}
	pkt := p.queue[0]
	p.queue = p.queue[1:]
	return pkt, nil
}

func (p *packetReader) readPage(firstPage bool) error {
	var hdr [27]byte
	// Read magic separately so a short / non-Ogg input returns
	// ErrNoOgg instead of io.ErrUnexpectedEOF.
	n, err := io.ReadFull(p.rs, hdr[0:4])
	if err != nil {
		if firstPage && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)) {
			return ErrNoOgg
		}
		return err
	}
	if n < 4 || string(hdr[0:4]) != "OggS" {
		if firstPage {
			return ErrNoOgg
		}
		return fmt.Errorf("ogg: lost page sync (got %q)", hdr[0:4])
	}
	if _, err := io.ReadFull(p.rs, hdr[4:]); err != nil {
		return err
	}
	if hdr[4] != 0 {
		return fmt.Errorf("ogg: unsupported page version %d", hdr[4])
	}
	headerType := hdr[5]
	serial := binary.LittleEndian.Uint32(hdr[14:18])
	segCount := int(hdr[26])
	segs := make([]byte, segCount)
	if _, err := io.ReadFull(p.rs, segs); err != nil {
		return err
	}
	// Total page-body length.
	totalBody := 0
	for _, s := range segs {
		totalBody += int(s)
	}
	body := make([]byte, totalBody)
	if _, err := io.ReadFull(p.rs, body); err != nil {
		return err
	}
	if firstPage {
		p.serial = serial
		if headerType&0x02 == 0 {
			// Not a BOS page. We require the file to begin with a
			// BOS page for the first logical stream.
			return fmt.Errorf("ogg: first page is not a BOS page (flags=0x%02X)", headerType)
		}
	}
	if serial != p.serial {
		// A concurrent stream's page; skip it.
		return nil
	}

	// Walk the segment table to reassemble packets. A packet
	// ends at any segment whose size is < 255; a segment of
	// size 255 means continuation in the next segment (and
	// possibly across pages).
	off := 0
	for i, s := range segs {
		size := int(s)
		p.pending.Write(body[off : off+size])
		off += size
		if size < 255 {
			// Packet boundary reached. Flush.
			out := make([]byte, p.pending.Len())
			copy(out, p.pending.Bytes())
			p.pending.Reset()
			p.queue = append(p.queue, out)
		} else if i == len(segs)-1 {
			// Last segment is 255 → packet continues on next
			// page. The continuation flag should be set on
			// that page, but we don't need to verify.
		}
	}
	return nil
}

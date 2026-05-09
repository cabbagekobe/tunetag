package id3v2

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame is one ID3v2 frame. The internal ID is the canonical
// 4-character v2.3/v2.4 form; v2.2 IDs are normalised on read and
// re-emitted in their original form when written back to v2.2.
type Frame interface {
	ID() string
	Encode(v Version, w io.Writer) error
}

// GenericFrame is the fallback representation when a frame's body is
// not recognised by any typed parser. Its raw bytes are preserved
// verbatim for loss-less round-trip.
type GenericFrame struct {
	// FrameID is the canonical 4-character ID (e.g. "TIT2").
	// For unknown v2.2 IDs without a known canonical form the raw
	// 3-character ID is used instead and is treated specially when
	// re-emitting to v2.2.
	FrameID string
	// Body is the raw frame contents excluding the frame header.
	Body []byte
	// StatusFlags / FormatFlags are the two flag bytes from the
	// v2.3 / v2.4 frame header. Always zero for v2.2 frames.
	StatusFlags byte
	FormatFlags byte
}

func (f *GenericFrame) ID() string { return f.FrameID }

func (f *GenericFrame) Encode(v Version, w io.Writer) error {
	return writeFrameHeaderAndBody(v, w, f.FrameID, f.StatusFlags, f.FormatFlags, f.Body)
}

func readFrames(v Version, payload []byte) ([]Frame, error) {
	var out []Frame
	i := 0
	for i < len(payload) {
		if payload[i] == 0 {
			// Padding starts here; require all remaining bytes to be zero.
			for j := i; j < len(payload); j++ {
				if payload[j] != 0 {
					return nil, fmt.Errorf("id3v2: non-zero byte 0x%02X inside padding at offset %d", payload[j], j)
				}
			}
			break
		}
		f, n, err := readOneFrame(v, payload[i:])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
		i += n
	}
	return out, nil
}

func readOneFrame(v Version, payload []byte) (Frame, int, error) {
	if v == V22 {
		if len(payload) < 6 {
			return nil, 0, fmt.Errorf("id3v2: short v2.2 frame header (have %d bytes)", len(payload))
		}
		id := string(payload[0:3])
		size := uint32(payload[3])<<16 | uint32(payload[4])<<8 | uint32(payload[5])
		end := 6 + int(size)
		if end > len(payload) {
			return nil, 0, fmt.Errorf("id3v2: v2.2 frame %q size %d exceeds payload (have %d body bytes)", id, size, len(payload)-6)
		}
		body := append([]byte(nil), payload[6:end]...)
		canonical := canonicalFromV22(id)
		if canonical == "" {
			canonical = id // unknown v2.2 ID: keep raw 3-char form
		}
		// v2.2 PIC has a different body layout; translate to v2.3
		// APIC layout so the typed parser can handle it uniformly.
		if id == "PIC" && canonical == "APIC" {
			if t, err := translateV22PIC(body); err == nil {
				body = t
			}
		}
		return dispatchFrame(canonical, body, 0, 0), end, nil
	}
	if len(payload) < 10 {
		return nil, 0, fmt.Errorf("id3v2: short frame header (have %d bytes)", len(payload))
	}
	id := string(payload[0:4])
	var size uint32
	if v == V24 {
		// v2.4 frame size is synchsafe; reject malformed top bits.
		for i := 4; i < 8; i++ {
			if payload[i]&0x80 != 0 {
				return nil, 0, fmt.Errorf("id3v2: v2.4 frame %q has non-synchsafe size byte at %d", id, i-4)
			}
		}
		size = decodeSynchsafe(payload[4:8])
	} else {
		size = binary.BigEndian.Uint32(payload[4:8])
	}
	statusFlags := payload[8]
	formatFlags := payload[9]
	end := 10 + int(size)
	if end > len(payload) {
		return nil, 0, fmt.Errorf("id3v2: frame %q size %d exceeds payload (have %d body bytes)", id, size, len(payload)-10)
	}
	body := append([]byte(nil), payload[10:end]...)
	return dispatchFrame(id, body, statusFlags, formatFlags), end, nil
}

// frameParser parses a frame body for a known canonical ID.
type frameParser func(canonicalID string, body []byte, statusFlags, formatFlags byte) (Frame, error)

// frameParsers maps canonical 4-char IDs to typed parsers. Frames
// not in the table fall back to the T*** / W*** prefix matchers and
// finally GenericFrame.
var frameParsers = map[string]frameParser{
	"TXXX": parseUserTextFrame,
	"WXXX": parseUserURLFrame,
	"COMM": parseCommentFrame,
	"USLT": parseUnsyncLyricsFrame,
	"APIC": parsePictureFrame,
	"UFID": parseUFIDFrame,
	"PRIV": parsePrivFrame,
}

// dispatchFrame returns the most specific Frame implementation that
// can parse body. Parser failures fall back to GenericFrame so that
// tag reads stay tolerant of malformed individual frames.
func dispatchFrame(canonicalID string, body []byte, statusFlags, formatFlags byte) Frame {
	if p, ok := frameParsers[canonicalID]; ok {
		if f, err := p(canonicalID, body, statusFlags, formatFlags); err == nil {
			return f
		}
	}
	if len(canonicalID) == 4 && canonicalID[0] == 'T' {
		if f, err := parseTextFrame(canonicalID, body, statusFlags, formatFlags); err == nil {
			return f
		}
	}
	if len(canonicalID) == 4 && canonicalID[0] == 'W' {
		if f, err := parseURLFrame(canonicalID, body, statusFlags, formatFlags); err == nil {
			return f
		}
	}
	return &GenericFrame{
		FrameID:     canonicalID,
		Body:        body,
		StatusFlags: statusFlags,
		FormatFlags: formatFlags,
	}
}

func writeFrameHeaderAndBody(v Version, w io.Writer, id string, statusFlags, formatFlags byte, body []byte) error {
	if v == V22 {
		// If the canonical 4-char ID has a v2.2 inverse, use it;
		// otherwise the caller may have stored a raw 3-char ID for
		// an unknown v2.2 frame.
		var id3 string
		if len(id) == 3 {
			id3 = id
		} else {
			id3 = v22FromCanonical(id)
		}
		if len(id3) != 3 {
			return fmt.Errorf("id3v2: frame %q has no v2.2 representation", id)
		}
		if len(body) > 1<<24-1 {
			return fmt.Errorf("id3v2: v2.2 frame %q body too large (%d bytes, max %d)", id3, len(body), 1<<24-1)
		}
		var hdr [6]byte
		copy(hdr[0:3], id3)
		hdr[3] = byte(len(body) >> 16)
		hdr[4] = byte(len(body) >> 8)
		hdr[5] = byte(len(body))
		if _, err := w.Write(hdr[:]); err != nil {
			return err
		}
		_, err := w.Write(body)
		return err
	}
	if len(id) != 4 {
		return fmt.Errorf("id3v2: invalid frame id %q (must be 4 chars for v2.3/v2.4)", id)
	}
	var hdr [10]byte
	copy(hdr[0:4], id)
	if v == V24 {
		if uint32(len(body)) > MaxSynchsafe {
			return fmt.Errorf("id3v2: v2.4 frame %q body too large (%d bytes)", id, len(body))
		}
		sz, err := encodeSynchsafe(uint32(len(body)))
		if err != nil {
			return err
		}
		copy(hdr[4:8], sz[:])
	} else {
		binary.BigEndian.PutUint32(hdr[4:8], uint32(len(body)))
	}
	hdr[8] = statusFlags
	hdr[9] = formatFlags
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

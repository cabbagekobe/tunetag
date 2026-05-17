package ogg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFile rewrites path with the current Vorbis Comment.
//
// The implementation relies on the Ogg-spec requirement that the
// two (Opus) or three (Vorbis) header packets each begin on a
// fresh page. That gives a clean byte boundary at which to
// splice in a freshly re-paged comment packet. Pages following
// the comment packet are emitted with their sequence numbers
// shifted by (new comment page count − old comment page count)
// and the per-page CRC recomputed.
//
// Pages belonging to a different (concurrently-multiplexed)
// logical bitstream are passed through unchanged, which is the
// correct behaviour for the rare case of an Ogg file with
// multiple streams sharing the same physical file.
//
// Limits: the new comment packet must fit in fewer than ~16 MiB
// (255 * 255 * 255 ≈ 16M bytes is the theoretical maximum per
// page; we emit at most one page per group of 255 segments).
func (f *File) WriteFile(path string) error {
	if f.Codec == CodecUnknown {
		return ErrUnsupportedCodec
	}
	if f.Comments == nil {
		return errors.New("ogg: cannot write file with nil Comments")
	}

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	layout, err := scanLayout(src, f.Serial)
	if err != nil {
		return err
	}

	// Build the new comment packet body.
	commentBody, err := f.Comments.Encode()
	if err != nil {
		return fmt.Errorf("ogg: encode comment block: %w", err)
	}
	var commentPkt bytes.Buffer
	switch f.Codec {
	case CodecVorbis:
		commentPkt.WriteByte(0x03)
		commentPkt.WriteString("vorbis")
		commentPkt.Write(commentBody)
		commentPkt.WriteByte(0x01) // framing bit
	case CodecOpus:
		commentPkt.WriteString("OpusTags")
		commentPkt.Write(commentBody)
	}

	// Re-page the comment packet starting from the seqnum that
	// the original first comment page used (so adjacent pages of
	// the same logical bitstream stay consistent when the page
	// count is unchanged).
	newPages, err := pagePacket(f.Serial, layout.commentFirstSeq, layout.commentLastGranule, false, commentPkt.Bytes())
	if err != nil {
		return err
	}
	seqShift := int64(len(newPages)) - int64(layout.commentPageCount)

	// Build output.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-ogg-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	// 1. Copy everything up to (but not including) the first
	//    comment page.
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return err
	}
	if _, err := io.CopyN(tmp, src, layout.commentStart); err != nil {
		cleanup()
		return err
	}
	// 2. Skip over the original comment pages.
	if _, err := src.Seek(layout.commentEnd-layout.commentStart, io.SeekCurrent); err != nil {
		cleanup()
		return err
	}
	// 3. Write the new comment pages.
	for _, p := range newPages {
		if _, err := tmp.Write(p); err != nil {
			cleanup()
			return err
		}
	}
	// 4. Stream every remaining page, fixing seqnum + CRC for
	//    those belonging to the comment packet's stream.
	if err := streamPagesAdjustingSeq(tmp, src, f.Serial, seqShift); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := src.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// pageLayout summarises the byte ranges within an Ogg file that
// the writer needs to locate.
type pageLayout struct {
	// Byte offset of the first page that carries the comment
	// packet (which the spec requires to be a fresh page).
	commentStart int64
	// Byte offset of the first byte AFTER the last page that
	// carries the comment packet.
	commentEnd int64
	// Number of original Ogg pages used by the comment packet.
	commentPageCount uint32
	// Sequence number on the first new comment page.
	commentFirstSeq uint32
	// Granule position to stamp on the last new comment page.
	// For header packets this is always 0, but we capture the
	// original value for fidelity.
	commentLastGranule uint64
}

// scanLayout walks the Ogg pages of the file and returns the
// byte ranges occupied by the comment packet for the given
// logical bitstream serial.
func scanLayout(rs io.ReadSeeker, serial uint32) (*pageLayout, error) {
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var off int64
	// packetsBeforePage tracks the number of packets that finished
	// in earlier pages of this logical bitstream. The comment
	// packet is the second packet (index 1), so we recognise its
	// starting page as the one we enter with packetsBeforePage==1.
	var packetsBeforePage int
	const commentPacketIndex = 1
	var layout pageLayout
	inComment := false
	commentStartOff := int64(-1)

	for {
		startOff := off
		var hdr [27]byte
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("ogg: short page header at offset %d", startOff)
			}
			return nil, err
		}
		if string(hdr[0:4]) != "OggS" {
			return nil, fmt.Errorf("ogg: lost page sync at offset %d (got %q)", startOff, hdr[0:4])
		}
		pageSerial := binary.LittleEndian.Uint32(hdr[14:18])
		pageSeq := binary.LittleEndian.Uint32(hdr[18:22])
		granule := binary.LittleEndian.Uint64(hdr[6:14])
		segCount := int(hdr[26])
		segs := make([]byte, segCount)
		if _, err := io.ReadFull(rs, segs); err != nil {
			return nil, err
		}
		bodyLen := 0
		for _, s := range segs {
			bodyLen += int(s)
		}
		// Skip body.
		if _, err := rs.Seek(int64(bodyLen), io.SeekCurrent); err != nil {
			return nil, err
		}
		off = startOff + 27 + int64(segCount) + int64(bodyLen)

		if pageSerial != serial {
			// Concurrent stream; ignore for layout purposes.
			continue
		}
		// Count completed packets on this page: every segment
		// with size < 255 marks a packet boundary.
		completedHere := 0
		for _, s := range segs {
			if s < 255 {
				completedHere++
			}
		}
		// If we haven't reached the comment packet yet, and this
		// page is where it BEGINS (the spec puts it on a fresh
		// page), capture commentStart.
		if !inComment && packetsBeforePage == commentPacketIndex {
			// This page starts the comment packet.
			inComment = true
			commentStartOff = startOff
			layout.commentFirstSeq = pageSeq
		}
		packetsBeforePage += completedHere
		if inComment {
			layout.commentLastGranule = granule
			layout.commentPageCount++
			// If at least one packet completed on this page,
			// the comment packet has ended.
			if completedHere >= 1 {
				layout.commentStart = commentStartOff
				layout.commentEnd = off
				return &layout, nil
			}
		}
	}
	return nil, fmt.Errorf("ogg: comment packet not found")
}

// streamPagesAdjustingSeq copies pages from src to dst. Pages
// whose serial matches `serial` get their sequence number
// shifted by seqShift and CRC recomputed; other pages pass
// through verbatim.
func streamPagesAdjustingSeq(dst io.Writer, src io.Reader, serial uint32, seqShift int64) error {
	br := bufferedReader{r: src}
	for {
		hdr, err := br.readN(27)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if string(hdr[0:4]) != "OggS" {
			return fmt.Errorf("ogg: lost page sync mid-stream (got %q)", hdr[0:4])
		}
		segCount := int(hdr[26])
		segs, err := br.readN(segCount)
		if err != nil {
			return err
		}
		bodyLen := 0
		for _, s := range segs {
			bodyLen += int(s)
		}
		body, err := br.readN(bodyLen)
		if err != nil {
			return err
		}
		pageSerial := binary.LittleEndian.Uint32(hdr[14:18])
		if pageSerial == serial && seqShift != 0 {
			pageSeq := binary.LittleEndian.Uint32(hdr[18:22])
			newSeq := uint32(int64(pageSeq) + seqShift)
			binary.LittleEndian.PutUint32(hdr[18:22], newSeq)
			binary.LittleEndian.PutUint32(hdr[22:26], 0) // zero CRC before recompute
			crc := oggPageCRC(hdr, segs, body)
			binary.LittleEndian.PutUint32(hdr[22:26], crc)
		}
		if _, err := dst.Write(hdr); err != nil {
			return err
		}
		if _, err := dst.Write(segs); err != nil {
			return err
		}
		if _, err := dst.Write(body); err != nil {
			return err
		}
	}
}

// bufferedReader returns short-read protected reads on top of an
// arbitrary io.Reader without buffering more than each call's
// payload.
type bufferedReader struct{ r io.Reader }

func (b *bufferedReader) readN(n int) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	out := make([]byte, n)
	read := 0
	for read < n {
		k, err := b.r.Read(out[read:])
		read += k
		if err != nil {
			if read > 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
	return out, nil
}

// pagePacket emits one or more Ogg pages carrying the single
// supplied packet. The packets are split into 255-byte lacing
// segments and at most 255 segments per page. The continuation
// flag (0x01) is set on every page after the first. EOS (0x04)
// is not set; this routine is used only for header packets.
//
// finalGranule is the granule position to stamp on the LAST
// page (where the packet ends). Intermediate pages of a
// multi-page packet get granule -1 (0xFFFFFFFFFFFFFFFF) per the
// Ogg spec ("no packet ends on this page"). For header packets
// the final granule is conventionally 0.
//
// Each element of the returned slice is one complete page,
// already CRC-finalised.
func pagePacket(serial, firstSeq uint32, finalGranule uint64, eos bool, pkt []byte) ([][]byte, error) {
	// A packet always ends at a segment with size < 255. For
	// packet sizes that are an exact multiple of 255, we need a
	// trailing 0-byte segment so the parser knows the packet
	// ended (and didn't continue past EOF).
	segments := make([]byte, 0, len(pkt)/255+1)
	rem := len(pkt)
	for rem >= 255 {
		segments = append(segments, 255)
		rem -= 255
	}
	segments = append(segments, byte(rem)) // final < 255 (may be 0)

	var out [][]byte
	seq := firstSeq
	bodyOff := 0
	first := true
	for len(segments) > 0 {
		thisCount := len(segments)
		if thisCount > 255 {
			thisCount = 255
		}
		thisSegs := segments[:thisCount]
		segments = segments[thisCount:]
		thisBody := 0
		for _, s := range thisSegs {
			thisBody += int(s)
		}
		flags := byte(0)
		if !first {
			flags |= 0x01 // continuation
		}
		isLastPage := len(segments) == 0
		if eos && isLastPage {
			flags |= 0x04
		}
		// Per the Ogg spec: granule_position is the position of
		// the last completed packet on the page; pages where no
		// packet ends use 0xFFFFFFFFFFFFFFFF (-1) as the sentinel.
		pageGranule := uint64(0xFFFFFFFFFFFFFFFF)
		if isLastPage {
			pageGranule = finalGranule
		}
		var hdr [27]byte
		copy(hdr[0:4], "OggS")
		hdr[4] = 0
		hdr[5] = flags
		binary.LittleEndian.PutUint64(hdr[6:14], pageGranule)
		binary.LittleEndian.PutUint32(hdr[14:18], serial)
		binary.LittleEndian.PutUint32(hdr[18:22], seq)
		binary.LittleEndian.PutUint32(hdr[22:26], 0) // CRC placeholder
		hdr[26] = byte(thisCount)
		body := pkt[bodyOff : bodyOff+thisBody]
		bodyOff += thisBody
		crc := oggPageCRC(hdr[:], thisSegs, body)
		binary.LittleEndian.PutUint32(hdr[22:26], crc)
		page := make([]byte, 0, 27+thisCount+thisBody)
		page = append(page, hdr[:]...)
		page = append(page, thisSegs...)
		page = append(page, body...)
		out = append(out, page)
		seq++
		first = false
	}
	if bodyOff != len(pkt) {
		return nil, fmt.Errorf("ogg: pagePacket internal consistency: bodyOff=%d != packet len=%d", bodyOff, len(pkt))
	}
	return out, nil
}

// --- CRC-32 (Ogg variant) -------------------------------------

// oggCRCTable is the standard Ogg CRC-32 table. Polynomial
// 0x04C11DB7, init 0, no reflect, final XOR 0.
var oggCRCTable [256]uint32

func init() {
	const poly uint32 = 0x04C11DB7
	for i := 0; i < 256; i++ {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = (c << 1) ^ poly
			} else {
				c <<= 1
			}
		}
		oggCRCTable[i] = c
	}
}

// oggPageCRC computes the CRC for one Ogg page assembled from the
// 27-byte header (with CRC bytes already zeroed), the segment
// table, and the page body.
func oggPageCRC(hdr, segs, body []byte) uint32 {
	var crc uint32
	for _, b := range hdr {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	for _, b := range segs {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	for _, b := range body {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

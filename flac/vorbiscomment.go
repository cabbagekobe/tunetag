package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// VendorString is the default vendor comment emitted on encode if
// the block has no vendor set.
const VendorString = "tunetag"

// VorbisComment is a FLAC METADATA_BLOCK_VORBIS_COMMENT. Comments
// are stored as raw "KEY=value" UTF-8 strings preserving the
// original case of KEY, but lookups via Get / Set / Remove use
// case-insensitive comparison per the Vorbis Comment specification.
//
// All multi-byte integers in the on-disk representation are
// little-endian, in contrast to the rest of the FLAC stream which
// is big-endian.
type VorbisComment struct {
	Vendor   string
	Comments []string
}

func (vc *VorbisComment) Type() uint8 { return BlockVorbisComment }

func (vc *VorbisComment) Encode() ([]byte, error) {
	vendor := vc.Vendor
	if vendor == "" {
		vendor = VendorString
	}
	var buf bytes.Buffer
	if err := writeLEUint32(&buf, uint32(len(vendor))); err != nil {
		return nil, err
	}
	buf.WriteString(vendor)
	if err := writeLEUint32(&buf, uint32(len(vc.Comments))); err != nil {
		return nil, err
	}
	for _, c := range vc.Comments {
		if err := writeLEUint32(&buf, uint32(len(c))); err != nil {
			return nil, err
		}
		buf.WriteString(c)
	}
	if buf.Len() > MaxBlockSize {
		return nil, fmt.Errorf("flac: VORBIS_COMMENT block too large (%d bytes, max %d)", buf.Len(), MaxBlockSize)
	}
	return buf.Bytes(), nil
}

// ParseVorbisComment decodes a Vorbis Comment block body
// (vendor length + vendor + comment count + length-prefixed
// "KEY=value" entries) into a VorbisComment. The on-disk format
// is identical to the one used by Ogg Vorbis and Ogg Opus comment
// packets, so callers outside FLAC can reuse this parser by
// passing the body after stripping any codec-specific magic
// prefix (and Vorbis's trailing framing bit, if present).
func ParseVorbisComment(body []byte) (*VorbisComment, error) {
	return parseVorbisComment(body)
}

func parseVorbisComment(body []byte) (*VorbisComment, error) {
	if len(body) < 4 {
		return nil, errors.New("flac: VORBIS_COMMENT truncated before vendor length")
	}
	pos := 0
	vendorLen := binary.LittleEndian.Uint32(body[pos : pos+4])
	pos += 4
	if int(vendorLen) > len(body)-pos {
		return nil, fmt.Errorf("flac: VORBIS_COMMENT vendor length %d exceeds body", vendorLen)
	}
	vendor := string(body[pos : pos+int(vendorLen)])
	pos += int(vendorLen)
	if pos+4 > len(body) {
		return nil, errors.New("flac: VORBIS_COMMENT truncated before comment count")
	}
	count := binary.LittleEndian.Uint32(body[pos : pos+4])
	pos += 4
	// Each comment carries at least a 4-byte length prefix, so a
	// count that cannot fit in the remaining body is invalid. Reject
	// early to avoid allocating gigabytes for attacker-controlled values.
	if int64(count)*4 > int64(len(body)-pos) {
		return nil, fmt.Errorf("flac: VORBIS_COMMENT count %d exceeds body", count)
	}
	comments := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		if pos+4 > len(body) {
			return nil, fmt.Errorf("flac: VORBIS_COMMENT truncated at comment %d", i)
		}
		clen := binary.LittleEndian.Uint32(body[pos : pos+4])
		pos += 4
		if int(clen) > len(body)-pos {
			return nil, fmt.Errorf("flac: VORBIS_COMMENT comment %d length %d exceeds body", i, clen)
		}
		comments = append(comments, string(body[pos:pos+int(clen)]))
		pos += int(clen)
	}
	return &VorbisComment{Vendor: vendor, Comments: comments}, nil
}

// Get returns every value for the given key, case-insensitively.
func (vc *VorbisComment) Get(key string) []string {
	var out []string
	prefixUpper := strings.ToUpper(key)
	for _, c := range vc.Comments {
		k, v := splitComment(c)
		if strings.ToUpper(k) == prefixUpper {
			out = append(out, v)
		}
	}
	return out
}

// First is a convenience: returns the first value for key or "".
func (vc *VorbisComment) First(key string) string {
	v := vc.Get(key)
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// Set replaces every existing entry for key with a single value.
// An empty value removes all entries for the key.
func (vc *VorbisComment) Set(key, value string) {
	vc.Remove(key)
	if value == "" {
		return
	}
	vc.Comments = append(vc.Comments, key+"="+value)
}

// Add appends a value without removing any existing entries.
func (vc *VorbisComment) Add(key, value string) {
	vc.Comments = append(vc.Comments, key+"="+value)
}

// Remove deletes every entry whose key matches case-insensitively.
func (vc *VorbisComment) Remove(key string) {
	upper := strings.ToUpper(key)
	out := vc.Comments[:0]
	for _, c := range vc.Comments {
		k, _ := splitComment(c)
		if strings.ToUpper(k) != upper {
			out = append(out, c)
		}
	}
	vc.Comments = out
}

func splitComment(c string) (key, value string) {
	if i := strings.IndexByte(c, '='); i >= 0 {
		return c[:i], c[i+1:]
	}
	return c, ""
}

func writeLEUint32(buf *bytes.Buffer, v uint32) error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	_, err := buf.Write(b[:])
	return err
}

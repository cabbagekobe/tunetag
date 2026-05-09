package id3v2

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// Encoding identifies the text encoding byte that prefixes most
// ID3v2 textual fields. Every value of Encoding can be read; on
// write, EncUTF16BE and EncUTF8 are valid only for v2.4 frames.
type Encoding uint8

const (
	EncISO88591 Encoding = 0 // Latin-1, null-terminated with one zero byte
	EncUTF16    Encoding = 1 // UTF-16 with BOM, null-terminated with two zero bytes
	EncUTF16BE  Encoding = 2 // UTF-16BE without BOM (v2.4 only), null-terminated with two zero bytes
	EncUTF8     Encoding = 3 // UTF-8 (v2.4 only), null-terminated with one zero byte
)

func (e Encoding) String() string {
	switch e {
	case EncISO88591:
		return "ISO-8859-1"
	case EncUTF16:
		return "UTF-16"
	case EncUTF16BE:
		return "UTF-16BE"
	case EncUTF8:
		return "UTF-8"
	default:
		return fmt.Sprintf("Encoding(%d)", uint8(e))
	}
}

// terminatorLen returns the byte length of a null terminator in the
// given encoding.
func (e Encoding) terminatorLen() int {
	if e == EncUTF16 || e == EncUTF16BE {
		return 2
	}
	return 1
}

// validForVersion reports whether enc may be emitted for v.
func (e Encoding) validForVersion(v Version) bool {
	switch e {
	case EncISO88591, EncUTF16:
		return true
	case EncUTF16BE, EncUTF8:
		return v == V24
	default:
		return false
	}
}

// readNextString consumes one possibly-null-terminated string from b
// in the given encoding and returns the decoded text and the bytes
// remaining after the terminator. If no terminator is found, the
// whole of b is consumed.
func readNextString(enc Encoding, b []byte) (string, []byte, error) {
	switch enc {
	case EncISO88591, EncUTF8:
		// Find the first 0x00 byte.
		end := -1
		for i, c := range b {
			if c == 0 {
				end = i
				break
			}
		}
		if end < 0 {
			s, err := decodeBytes(enc, b)
			return s, nil, err
		}
		s, err := decodeBytes(enc, b[:end])
		return s, b[end+1:], err
	case EncUTF16, EncUTF16BE:
		end := -1
		// Walk in 2-byte units; require even alignment for a terminator.
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				end = i
				break
			}
		}
		if end < 0 {
			// Trim any trailing single byte (malformed but tolerate).
			tail := b
			if len(tail)%2 == 1 {
				tail = tail[:len(tail)-1]
			}
			s, err := decodeBytes(enc, tail)
			return s, nil, err
		}
		s, err := decodeBytes(enc, b[:end])
		return s, b[end+2:], err
	}
	return "", nil, fmt.Errorf("id3v2: unknown text encoding %d", enc)
}

// decodeBytes decodes a single string (no terminator) from raw bytes
// in the given encoding.
func decodeBytes(enc Encoding, b []byte) (string, error) {
	switch enc {
	case EncISO88591:
		// Each byte is one Unicode code point in [0,255].
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return string(runes), nil
	case EncUTF8:
		if !utf8.Valid(b) {
			return "", errors.New("id3v2: invalid UTF-8 in text field")
		}
		return string(b), nil
	case EncUTF16:
		if len(b) < 2 {
			return "", nil
		}
		bom := uint16(b[0])<<8 | uint16(b[1])
		var le bool
		switch bom {
		case 0xFFFE:
			le = true
		case 0xFEFF:
			le = false
		default:
			// No BOM: assume LE per common practice.
			le = true
			b = append([]byte{0xFF, 0xFE}, b...)
		}
		return decodeUTF16Pairs(b[2:], le), nil
	case EncUTF16BE:
		return decodeUTF16Pairs(b, false), nil
	}
	return "", fmt.Errorf("id3v2: unknown text encoding %d", enc)
}

func decodeUTF16Pairs(b []byte, littleEndian bool) string {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		hi, lo := b[2*i], b[2*i+1]
		if littleEndian {
			u[i] = uint16(lo)<<8 | uint16(hi)
		} else {
			u[i] = uint16(hi)<<8 | uint16(lo)
		}
	}
	return string(utf16.Decode(u))
}

// encodeString encodes s in enc, optionally appending a terminator.
// EncISO88591 returns ErrCannotEncodeLatin1 if any rune is not in
// [0,255].
func encodeString(enc Encoding, s string, terminate bool) ([]byte, error) {
	switch enc {
	case EncISO88591:
		out := make([]byte, 0, len(s)+1)
		for _, r := range s {
			if r < 0 || r > 0xFF {
				return nil, ErrCannotEncodeLatin1
			}
			out = append(out, byte(r))
		}
		if terminate {
			out = append(out, 0)
		}
		return out, nil
	case EncUTF8:
		out := make([]byte, 0, len(s)+1)
		out = append(out, s...)
		if terminate {
			out = append(out, 0)
		}
		return out, nil
	case EncUTF16:
		// LE with BOM.
		u := utf16.Encode([]rune(s))
		out := make([]byte, 0, 2+2*len(u)+2)
		out = append(out, 0xFF, 0xFE)
		for _, r := range u {
			out = append(out, byte(r), byte(r>>8))
		}
		if terminate {
			out = append(out, 0, 0)
		}
		return out, nil
	case EncUTF16BE:
		u := utf16.Encode([]rune(s))
		out := make([]byte, 0, 2*len(u)+2)
		for _, r := range u {
			out = append(out, byte(r>>8), byte(r))
		}
		if terminate {
			out = append(out, 0, 0)
		}
		return out, nil
	}
	return nil, fmt.Errorf("id3v2: unknown text encoding %d", enc)
}

// ErrCannotEncodeLatin1 is returned by encodeString when the input
// contains code points outside the ISO-8859-1 range.
var ErrCannotEncodeLatin1 = errors.New("id3v2: text contains non-Latin-1 characters")

// pickEncodingForText returns a sensible default encoding for the
// given target version such that s round-trips without loss.
//
//	v2.3: UTF-16 (Latin-1 only when s is pure ASCII to save bytes)
//	v2.4: UTF-8
func pickEncodingForText(v Version, s string) Encoding {
	if v == V24 {
		return EncUTF8
	}
	for _, r := range s {
		if r > 0x7F {
			return EncUTF16
		}
	}
	return EncISO88591
}

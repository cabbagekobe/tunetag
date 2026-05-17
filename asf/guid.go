package asf

import (
	"encoding/binary"
	"fmt"
)

// GUID is a 16-byte Microsoft globally-unique identifier as
// stored on disk in ASF files. The first three fields (Data1,
// Data2, Data3) are little-endian; the last two (Data4) are a
// raw 8-byte sequence. This matches Microsoft's wire encoding.
//
// Display order is the canonical "8-4-4-4-12 hex" form used in
// the ASF spec, e.g.
// 75B22630-668E-11CF-A6D9-00AA0062CE6C.
type GUID [16]byte

// readGUID decodes 16 bytes into a GUID. (Identity on the bytes
// — the field-order conversion is purely a display concern.)
func readGUID(b []byte) GUID {
	var g GUID
	copy(g[:], b)
	return g
}

func (g GUID) String() string {
	d1 := binary.LittleEndian.Uint32(g[0:4])
	d2 := binary.LittleEndian.Uint16(g[4:6])
	d3 := binary.LittleEndian.Uint16(g[6:8])
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		d1, d2, d3,
		g[8], g[9],
		g[10], g[11], g[12], g[13], g[14], g[15])
}

// mustGUID parses a canonical "8-4-4-4-12" hex GUID into the
// 16-byte on-disk form. Used at package init to build the
// constant GUID table.
func mustGUID(s string) GUID {
	// Expected format: 8-4-4-4-12 with dashes; 36 chars total.
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		panic("asf: malformed GUID literal " + s)
	}
	parseUint := func(hex string) uint64 {
		var v uint64
		for _, c := range hex {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= uint64(c - '0')
			case c >= 'A' && c <= 'F':
				v |= uint64(c-'A') + 10
			case c >= 'a' && c <= 'f':
				v |= uint64(c-'a') + 10
			default:
				panic("asf: bad hex digit in GUID " + s)
			}
		}
		return v
	}
	d1 := uint32(parseUint(s[0:8]))
	d2 := uint16(parseUint(s[9:13]))
	d3 := uint16(parseUint(s[14:18]))
	var d4 [8]byte
	d4[0] = byte(parseUint(s[19:21]))
	d4[1] = byte(parseUint(s[21:23]))
	d4[2] = byte(parseUint(s[24:26]))
	d4[3] = byte(parseUint(s[26:28]))
	d4[4] = byte(parseUint(s[28:30]))
	d4[5] = byte(parseUint(s[30:32]))
	d4[6] = byte(parseUint(s[32:34]))
	d4[7] = byte(parseUint(s[34:36]))
	var g GUID
	binary.LittleEndian.PutUint32(g[0:4], d1)
	binary.LittleEndian.PutUint16(g[4:6], d2)
	binary.LittleEndian.PutUint16(g[6:8], d3)
	copy(g[8:16], d4[:])
	return g
}

// Known ASF object GUIDs. Only the ones tunetag inspects or
// rewrites are named; everything else round-trips as opaque
// bytes.
var (
	guidHeaderObject                     = mustGUID("75B22630-668E-11CF-A6D9-00AA0062CE6C")
	guidDataObject                       = mustGUID("75B22636-668E-11CF-A6D9-00AA0062CE6C")
	guidContentDescriptionObject         = mustGUID("75B22633-668E-11CF-A6D9-00AA0062CE6C")
	guidExtendedContentDescriptionObject = mustGUID("D2D0A440-E307-11D2-97F0-00A0C95EA850")
)

// IsHeaderGUID reports whether the first 16 bytes of b match the
// ASF Header Object GUID. Used by tunetag.Detect to recognise
// .wma / .wmv files without having to parse the full header.
func IsHeaderGUID(b []byte) bool {
	if len(b) < 16 {
		return false
	}
	for i := 0; i < 16; i++ {
		if b[i] != guidHeaderObject[i] {
			return false
		}
	}
	return true
}

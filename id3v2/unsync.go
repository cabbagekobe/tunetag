package id3v2

// unsyncDecode removes the 0x00 byte inserted after every 0xFF that
// was unsynchronised on the wire. It is safe to call on data that
// was not actually unsynchronised; only literal 0xFF 0x00 sequences
// are collapsed.
func unsyncDecode(src []byte) []byte {
	dst := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		dst = append(dst, src[i])
		if src[i] == 0xFF && i+1 < len(src) && src[i+1] == 0x00 {
			i++
		}
	}
	return dst
}

// unsyncEncode inserts a 0x00 byte after every 0xFF that would
// otherwise look like a sync byte: that is, every 0xFF whose next
// byte is 0x00 or has its top three bits set (0xE0..0xFF). A
// trailing 0xFF at the very end of src is also padded, matching the
// ID3v2 specification.
func unsyncEncode(src []byte) []byte {
	dst := make([]byte, 0, len(src)+len(src)/64)
	for i, b := range src {
		dst = append(dst, b)
		if b != 0xFF {
			continue
		}
		if i+1 == len(src) {
			dst = append(dst, 0x00)
			continue
		}
		next := src[i+1]
		if next == 0x00 || next >= 0xE0 {
			dst = append(dst, 0x00)
		}
	}
	return dst
}

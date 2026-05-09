package id3v2

import "errors"

// MaxSynchsafe is the largest value that fits in a 28-bit synchsafe
// integer (4 bytes, top bit of each byte zero).
const MaxSynchsafe uint32 = 1<<28 - 1

// ErrSynchsafeOverflow is returned by encodeSynchsafe when the value
// exceeds 2^28-1.
var ErrSynchsafeOverflow = errors.New("id3v2: synchsafe value exceeds 28 bits")

// decodeSynchsafe reads a 4-byte synchsafe integer (big-endian, top
// bit of each byte ignored) into a uint32.
func decodeSynchsafe(b []byte) uint32 {
	return uint32(b[0]&0x7F)<<21 |
		uint32(b[1]&0x7F)<<14 |
		uint32(b[2]&0x7F)<<7 |
		uint32(b[3]&0x7F)
}

// encodeSynchsafe encodes v as a 4-byte synchsafe integer. v must be
// in [0, MaxSynchsafe].
func encodeSynchsafe(v uint32) ([4]byte, error) {
	var out [4]byte
	if v > MaxSynchsafe {
		return out, ErrSynchsafeOverflow
	}
	out[0] = byte((v >> 21) & 0x7F)
	out[1] = byte((v >> 14) & 0x7F)
	out[2] = byte((v >> 7) & 0x7F)
	out[3] = byte(v & 0x7F)
	return out, nil
}

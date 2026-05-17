package tunetag

// SniffImageMIME returns a best-effort image MIME type sniffed
// from the leading bytes of b. Recognised: JPEG, PNG, GIF, BMP.
// Returns "" when no signature matches.
//
// This is intended for callers building Picture-like values for
// containers that do not carry MIME alongside the image data
// (notably APEv2 and Vorbis Comment's METADATA_BLOCK_PICTURE
// before the FLAC PICTURE block fields are filled in). It is
// deliberately narrower than [net/http.DetectContentType] —
// only image formats are tested, so non-image input always
// returns "" rather than a spurious match.
func SniffImageMIME(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) >= 8 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47:
		return "image/png"
	case len(b) >= 6 && (string(b[0:6]) == "GIF87a" || string(b[0:6]) == "GIF89a"):
		return "image/gif"
	case len(b) >= 2 && b[0] == 0x42 && b[1] == 0x4D:
		return "image/bmp"
	}
	return ""
}

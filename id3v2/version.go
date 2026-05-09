package id3v2

// Version is the major ID3v2 revision (2, 3, or 4). Frame syntax,
// frame-ID length, and tag-level flag semantics differ between
// versions. The minor revision is always treated as 0.
type Version uint8

const (
	V22 Version = 2
	V23 Version = 3
	V24 Version = 4
)

func (v Version) String() string {
	switch v {
	case V22:
		return "ID3v2.2"
	case V23:
		return "ID3v2.3"
	case V24:
		return "ID3v2.4"
	default:
		return "ID3v2.?"
	}
}

// FrameIDLen returns the byte length of a frame identifier in this
// version: 3 for v2.2, 4 for v2.3 and v2.4.
func (v Version) FrameIDLen() int {
	if v == V22 {
		return 3
	}
	return 4
}

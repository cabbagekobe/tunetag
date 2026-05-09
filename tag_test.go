package tunetag

import "testing"

func TestFormatString(t *testing.T) {
	cases := []struct {
		f    Format
		want string
	}{
		{FormatUnknown, "Unknown"},
		{FormatID3v1, "ID3v1"},
		{FormatID3v2, "ID3v2"},
		{FormatFLAC, "FLAC"},
		{FormatMP4, "MP4"},
		{Format(99), "Unknown"},
	}
	for _, c := range cases {
		if got := c.f.String(); got != c.want {
			t.Errorf("Format(%d).String() = %q, want %q", c.f, got, c.want)
		}
	}
}

func TestPictureTypeRange(t *testing.T) {
	if PictureOther != 0 {
		t.Errorf("PictureOther = %d, want 0", PictureOther)
	}
	if PicturePublisherLogo != 20 {
		t.Errorf("PicturePublisherLogo = %d, want 20", PicturePublisherLogo)
	}
}

package id3v2

import "strconv"

// firstText returns the first value of the first TextFrame with id,
// or "".
func (t *Tag) firstText(id string) string {
	for _, f := range t.Frames {
		if tf, ok := f.(*TextFrame); ok && tf.FrameID == id {
			return tf.String()
		}
	}
	return ""
}

// Convenience read-only accessors. They look at typed frames only;
// callers that need the underlying frames should iterate t.Frames.

func (t *Tag) Title() string       { return t.firstText("TIT2") }
func (t *Tag) Artist() string      { return t.firstText("TPE1") }
func (t *Tag) AlbumArtist() string { return t.firstText("TPE2") }
func (t *Tag) Album() string       { return t.firstText("TALB") }
func (t *Tag) Composer() string    { return t.firstText("TCOM") }
func (t *Tag) Genre() string       { return t.firstText("TCON") }

// Year extracts a four-digit year from TYER (v2.3) or the leading
// digits of TDRC (v2.4). Returns 0 if neither is present or parses.
func (t *Tag) Year() int {
	if v := t.firstText("TYER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if v := t.firstText("TDRC"); len(v) >= 4 {
		if n, err := strconv.Atoi(v[:4]); err == nil {
			return n
		}
	}
	return 0
}

// TrackNumber and DiscNumber parse "n/total" or "n" from TRCK and
// TPOS respectively. Missing components are 0.
func (t *Tag) TrackNumber() (n, total int) { return parseSlashed(t.firstText("TRCK")) }
func (t *Tag) DiscNumber() (n, total int)  { return parseSlashed(t.firstText("TPOS")) }

// Comment returns the text of the first COMM frame, or "".
func (t *Tag) Comment() string {
	for _, f := range t.Frames {
		if cf, ok := f.(*CommentFrame); ok {
			return cf.Text
		}
	}
	return ""
}

// PictureFrames returns every APIC frame in tag order. It is
// primarily a helper for the top-level wrapper that converts these
// to tunetag.Picture; callers can use them directly too.
func (t *Tag) PictureFrames() []*PictureFrame {
	var out []*PictureFrame
	for _, f := range t.Frames {
		if pf, ok := f.(*PictureFrame); ok {
			out = append(out, pf)
		}
	}
	return out
}

// SetText replaces (or, if absent, appends) the TextFrame for id
// with a single value. An empty value removes any existing frame.
// The encoding is auto-selected per the tag version.
func (t *Tag) SetText(id, value string) {
	if value == "" {
		t.RemoveFrames(id)
		return
	}
	enc := EncUTF8
	if t.Version == V23 {
		enc = pickEncodingForText(V23, value)
	}
	frame := &TextFrame{FrameID: id, Encoding: enc, Text: []string{value}}
	for i, f := range t.Frames {
		if f.ID() == id {
			t.Frames[i] = frame
			return
		}
	}
	t.Frames = append(t.Frames, frame)
}

// RemoveFrames removes every frame with the given canonical ID.
func (t *Tag) RemoveFrames(id string) {
	out := t.Frames[:0]
	for _, f := range t.Frames {
		if f.ID() != id {
			out = append(out, f)
		}
	}
	t.Frames = out
}

// AddFrame appends f to t.Frames.
func (t *Tag) AddFrame(f Frame) {
	t.Frames = append(t.Frames, f)
}

func (t *Tag) SetTitle(s string)       { t.SetText("TIT2", s) }
func (t *Tag) SetArtist(s string)      { t.SetText("TPE1", s) }
func (t *Tag) SetAlbumArtist(s string) { t.SetText("TPE2", s) }
func (t *Tag) SetAlbum(s string)       { t.SetText("TALB", s) }
func (t *Tag) SetComposer(s string)    { t.SetText("TCOM", s) }
func (t *Tag) SetGenre(s string)       { t.SetText("TCON", s) }

func parseSlashed(s string) (n, total int) {
	if s == "" {
		return 0, 0
	}
	for i, c := range s {
		if c == '/' {
			n, _ = strconv.Atoi(s[:i])
			total, _ = strconv.Atoi(s[i+1:])
			return
		}
	}
	n, _ = strconv.Atoi(s)
	return n, 0
}

package id3v2

import (
	"bytes"
	"fmt"
	"io"
)

// CommentFrame is the COMM frame: a per-language, per-description
// comment string.
type CommentFrame struct {
	Encoding    Encoding
	Language    string // 3-character ISO-639-2 code, e.g. "eng"; "XXX" if unknown
	Description string
	Text        string
}

func (f *CommentFrame) ID() string { return "COMM" }

func parseCommentFrame(_ string, body []byte, _, _ byte) (Frame, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("id3v2: COMM body too short (%d bytes)", len(body))
	}
	enc := Encoding(body[0])
	lang := string(body[1:4])
	desc, rest, err := readNextString(enc, body[4:])
	if err != nil {
		return nil, fmt.Errorf("id3v2: COMM description: %w", err)
	}
	text, _, err := readNextString(enc, rest)
	if err != nil {
		return nil, fmt.Errorf("id3v2: COMM text: %w", err)
	}
	return &CommentFrame{Encoding: enc, Language: lang, Description: desc, Text: text}, nil
}

func (f *CommentFrame) Encode(v Version, w io.Writer) error {
	enc := chooseEncoding(v, f.Encoding, f.Description, f.Text)
	lang := f.Language
	if len(lang) != 3 {
		lang = "XXX"
	}
	var body bytes.Buffer
	body.WriteByte(byte(enc))
	body.WriteString(lang)
	desc, err := encodeString(enc, f.Description, true)
	if err != nil {
		return err
	}
	body.Write(desc)
	text, err := encodeString(enc, f.Text, false)
	if err != nil {
		return err
	}
	body.Write(text)
	return writeFrameHeaderAndBody(v, w, "COMM", 0, 0, body.Bytes())
}

// UnsynchronisedLyricsFrame is the USLT frame; its body layout is
// identical to COMM.
type UnsynchronisedLyricsFrame struct {
	Encoding    Encoding
	Language    string
	Description string
	Text        string
}

func (f *UnsynchronisedLyricsFrame) ID() string { return "USLT" }

func parseUnsyncLyricsFrame(_ string, body []byte, _, _ byte) (Frame, error) {
	c, err := parseCommentFrame("COMM", body, 0, 0)
	if err != nil {
		return nil, err
	}
	cf := c.(*CommentFrame)
	return &UnsynchronisedLyricsFrame{
		Encoding: cf.Encoding, Language: cf.Language,
		Description: cf.Description, Text: cf.Text,
	}, nil
}

func (f *UnsynchronisedLyricsFrame) Encode(v Version, w io.Writer) error {
	c := &CommentFrame{Encoding: f.Encoding, Language: f.Language, Description: f.Description, Text: f.Text}
	var buf bytes.Buffer
	if err := c.Encode(v, &buf); err != nil {
		return err
	}
	// Replace the frame ID "COMM" written by CommentFrame.Encode with "USLT".
	out := buf.Bytes()
	if v == V22 {
		copy(out[0:3], "ULT")
	} else {
		copy(out[0:4], "USLT")
	}
	_, err := w.Write(out)
	return err
}

package id3v2

import (
	"bytes"
	"fmt"
	"io"
)

// TextFrame represents any T*** frame other than TXXX. Multiple
// values are stored in Text and emitted null-separated; v2.3 readers
// that strictly follow the spec will see only the first value, but
// mutagen / TagLib / foobar2000 all interpret the convention.
type TextFrame struct {
	FrameID  string
	Encoding Encoding
	Text     []string
}

func (f *TextFrame) ID() string { return f.FrameID }

// String is a convenience accessor for the first value.
func (f *TextFrame) String() string {
	if len(f.Text) == 0 {
		return ""
	}
	return f.Text[0]
}

func parseTextFrame(id string, body []byte, _, _ byte) (Frame, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("id3v2: text frame %q has empty body", id)
	}
	enc := Encoding(body[0])
	rest := body[1:]
	var values []string
	for len(rest) > 0 {
		s, next, err := readNextString(enc, rest)
		if err != nil {
			return nil, err
		}
		values = append(values, s)
		rest = next
	}
	if len(values) == 0 {
		values = []string{""}
	}
	return &TextFrame{FrameID: id, Encoding: enc, Text: values}, nil
}

func (f *TextFrame) Encode(v Version, w io.Writer) error {
	enc := chooseEncoding(v, f.Encoding, f.Text...)
	var body bytes.Buffer
	body.WriteByte(byte(enc))
	for i, s := range f.Text {
		terminate := i < len(f.Text)-1
		b, err := encodeString(enc, s, terminate)
		if err != nil {
			return err
		}
		body.Write(b)
	}
	return writeFrameHeaderAndBody(v, w, f.FrameID, 0, 0, body.Bytes())
}

// UserTextFrame is the TXXX user-defined text frame, which stores a
// description-keyed string value.
type UserTextFrame struct {
	Encoding    Encoding
	Description string
	Value       string
}

func (f *UserTextFrame) ID() string { return "TXXX" }

func parseUserTextFrame(id string, body []byte, _, _ byte) (Frame, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("id3v2: TXXX has empty body")
	}
	enc := Encoding(body[0])
	desc, rest, err := readNextString(enc, body[1:])
	if err != nil {
		return nil, fmt.Errorf("id3v2: TXXX description: %w", err)
	}
	val, _, err := readNextString(enc, rest)
	if err != nil {
		return nil, fmt.Errorf("id3v2: TXXX value: %w", err)
	}
	return &UserTextFrame{Encoding: enc, Description: desc, Value: val}, nil
}

func (f *UserTextFrame) Encode(v Version, w io.Writer) error {
	enc := chooseEncoding(v, f.Encoding, f.Description, f.Value)
	var body bytes.Buffer
	body.WriteByte(byte(enc))
	desc, err := encodeString(enc, f.Description, true)
	if err != nil {
		return err
	}
	body.Write(desc)
	val, err := encodeString(enc, f.Value, false)
	if err != nil {
		return err
	}
	body.Write(val)
	return writeFrameHeaderAndBody(v, w, "TXXX", 0, 0, body.Bytes())
}

// chooseEncoding selects an encoding that can losslessly carry every
// string for the target version. The frame's preferred encoding is
// honoured when it satisfies the constraint; otherwise UTF-16 (v2.3)
// or UTF-8 (v2.4) is used.
func chooseEncoding(v Version, want Encoding, texts ...string) Encoding {
	if want.validForVersion(v) && canEncodeAll(want, texts) {
		return want
	}
	if v == V24 {
		return EncUTF8
	}
	for _, s := range texts {
		for _, r := range s {
			if r > 0x7F {
				return EncUTF16
			}
		}
	}
	return EncISO88591
}

func canEncodeAll(enc Encoding, texts []string) bool {
	if enc != EncISO88591 {
		return true
	}
	for _, s := range texts {
		for _, r := range s {
			if r > 0xFF {
				return false
			}
		}
	}
	return true
}

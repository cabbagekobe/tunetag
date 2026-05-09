package id3v2

import (
	"bytes"
	"fmt"
	"io"
)

// URLFrame represents any W*** frame other than WXXX. The URL is
// always Latin-1 per the spec.
type URLFrame struct {
	FrameID string
	URL     string
}

func (f *URLFrame) ID() string { return f.FrameID }

func parseURLFrame(id string, body []byte, _, _ byte) (Frame, error) {
	// W*** frames carry no encoding byte. The URL is null-terminated
	// Latin-1; trailing data after the first null is conventionally
	// discarded.
	end := len(body)
	for i, b := range body {
		if b == 0 {
			end = i
			break
		}
	}
	url, err := decodeBytes(EncISO88591, body[:end])
	if err != nil {
		return nil, fmt.Errorf("id3v2: URL frame %q: %w", id, err)
	}
	return &URLFrame{FrameID: id, URL: url}, nil
}

func (f *URLFrame) Encode(v Version, w io.Writer) error {
	enc, err := encodeString(EncISO88591, f.URL, false)
	if err != nil {
		return err
	}
	return writeFrameHeaderAndBody(v, w, f.FrameID, 0, 0, enc)
}

// UserURLFrame is the WXXX user-defined URL frame.
type UserURLFrame struct {
	Encoding    Encoding
	Description string
	URL         string
}

func (f *UserURLFrame) ID() string { return "WXXX" }

func parseUserURLFrame(id string, body []byte, _, _ byte) (Frame, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("id3v2: WXXX has empty body")
	}
	enc := Encoding(body[0])
	desc, rest, err := readNextString(enc, body[1:])
	if err != nil {
		return nil, fmt.Errorf("id3v2: WXXX description: %w", err)
	}
	// URL is always Latin-1, possibly null-terminated.
	end := len(rest)
	for i, b := range rest {
		if b == 0 {
			end = i
			break
		}
	}
	url, err := decodeBytes(EncISO88591, rest[:end])
	if err != nil {
		return nil, fmt.Errorf("id3v2: WXXX URL: %w", err)
	}
	return &UserURLFrame{Encoding: enc, Description: desc, URL: url}, nil
}

func (f *UserURLFrame) Encode(v Version, w io.Writer) error {
	enc := chooseEncoding(v, f.Encoding, f.Description)
	var body bytes.Buffer
	body.WriteByte(byte(enc))
	desc, err := encodeString(enc, f.Description, true)
	if err != nil {
		return err
	}
	body.Write(desc)
	url, err := encodeString(EncISO88591, f.URL, false)
	if err != nil {
		return err
	}
	body.Write(url)
	return writeFrameHeaderAndBody(v, w, "WXXX", 0, 0, body.Bytes())
}

package id3v2

import (
	"bytes"
	"errors"
	"io"
)

// UFIDFrame is the unique file identifier frame. The Owner is a
// Latin-1 owner identifier (URL or email-like) and Identifier is up
// to 64 bytes of opaque data per spec.
type UFIDFrame struct {
	Owner      string
	Identifier []byte
}

func (f *UFIDFrame) ID() string { return "UFID" }

func parseUFIDFrame(_ string, body []byte, _, _ byte) (Frame, error) {
	owner, rest, err := readNextString(EncISO88591, body)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		return nil, errors.New("id3v2: UFID missing owner identifier")
	}
	id := append([]byte(nil), rest...)
	return &UFIDFrame{Owner: owner, Identifier: id}, nil
}

func (f *UFIDFrame) Encode(v Version, w io.Writer) error {
	var body bytes.Buffer
	owner, err := encodeString(EncISO88591, f.Owner, true)
	if err != nil {
		return err
	}
	body.Write(owner)
	body.Write(f.Identifier)
	return writeFrameHeaderAndBody(v, w, "UFID", 0, 0, body.Bytes())
}

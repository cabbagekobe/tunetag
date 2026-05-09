package id3v2

import (
	"bytes"
	"errors"
	"io"
)

// UFIDFrame is the unique file identifier frame.
type UFIDFrame struct {
	Owner      string // Latin-1 owner identifier (URL or email-like)
	Identifier []byte // up to 64 bytes per spec
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

// PrivFrame is the PRIV private frame.
type PrivFrame struct {
	Owner string
	Data  []byte
}

func (f *PrivFrame) ID() string { return "PRIV" }

func parsePrivFrame(_ string, body []byte, _, _ byte) (Frame, error) {
	owner, rest, err := readNextString(EncISO88591, body)
	if err != nil {
		return nil, err
	}
	data := append([]byte(nil), rest...)
	return &PrivFrame{Owner: owner, Data: data}, nil
}

func (f *PrivFrame) Encode(v Version, w io.Writer) error {
	var body bytes.Buffer
	owner, err := encodeString(EncISO88591, f.Owner, true)
	if err != nil {
		return err
	}
	body.Write(owner)
	body.Write(f.Data)
	return writeFrameHeaderAndBody(v, w, "PRIV", 0, 0, body.Bytes())
}

package id3v2

import (
	"bytes"
	"io"
)

// PrivFrame is the PRIV private frame. The Owner identifies the
// producer (often a URL or email); Data is opaque application bytes.
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

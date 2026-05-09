package id3v2

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PictureFrame is the APIC (v2.3/v2.4) / PIC (v2.2) attached
// picture frame. v2.2 PIC is read transparently as APIC: the 3-char
// image format is converted to a MIME type on read, and APIC is
// always emitted on write since v2.2 writing is not supported.
type PictureFrame struct {
	Encoding    Encoding
	MIME        string // e.g. "image/jpeg"
	PictureType uint8  // see tunetag.PictureType (0..20)
	Description string
	Data        []byte
}

func (f *PictureFrame) ID() string { return "APIC" }

func parsePictureFrame(_ string, body []byte, _, _ byte) (Frame, error) {
	if len(body) < 1 {
		return nil, errors.New("id3v2: APIC body too short")
	}
	enc := Encoding(body[0])
	rest := body[1:]
	mime, rest, err := readNextString(EncISO88591, rest)
	if err != nil {
		return nil, fmt.Errorf("id3v2: APIC MIME: %w", err)
	}
	if len(rest) < 1 {
		return nil, errors.New("id3v2: APIC body truncated before picture type")
	}
	ptype := rest[0]
	rest = rest[1:]
	desc, rest, err := readNextString(enc, rest)
	if err != nil {
		return nil, fmt.Errorf("id3v2: APIC description: %w", err)
	}
	data := append([]byte(nil), rest...)
	return &PictureFrame{Encoding: enc, MIME: mime, PictureType: ptype, Description: desc, Data: data}, nil
}

func (f *PictureFrame) Encode(v Version, w io.Writer) error {
	enc := chooseEncoding(v, f.Encoding, f.Description)
	var body bytes.Buffer
	body.WriteByte(byte(enc))
	mime := f.MIME
	if mime == "" {
		mime = "image/jpeg"
	}
	mimeBytes, err := encodeString(EncISO88591, mime, true)
	if err != nil {
		return err
	}
	body.Write(mimeBytes)
	body.WriteByte(f.PictureType)
	desc, err := encodeString(enc, f.Description, true)
	if err != nil {
		return err
	}
	body.Write(desc)
	body.Write(f.Data)
	return writeFrameHeaderAndBody(v, w, "APIC", 0, 0, body.Bytes())
}

// translateV22PIC rewrites a v2.2 PIC frame body into the v2.3 APIC
// body layout: the 3-character image format becomes a null-terminated
// MIME type. Returns the original bytes unchanged if the body is too
// short to be a valid PIC frame.
func translateV22PIC(body []byte) ([]byte, error) {
	if len(body) < 5 {
		return nil, errors.New("id3v2: PIC body too short")
	}
	enc := body[0]
	imgFmt := strings.ToUpper(string(body[1:4]))
	pictype := body[4]
	rest := body[5:]
	var mime string
	switch imgFmt {
	case "JPG":
		mime = "image/jpeg"
	case "PNG":
		mime = "image/png"
	default:
		mime = "image/" + strings.ToLower(imgFmt)
	}
	var out bytes.Buffer
	out.WriteByte(enc)
	out.WriteString(mime)
	out.WriteByte(0)
	out.WriteByte(pictype)
	out.Write(rest)
	return out.Bytes(), nil
}

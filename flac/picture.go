package flac

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Picture is a FLAC METADATA_BLOCK_PICTURE. The PictureType byte
// values match the 21 ID3v2 APIC types and the tunetag.PictureType
// enum, so block contents can be moved between formats losslessly.
type Picture struct {
	PictureType   uint32
	MIME          string
	Description   string
	Width         uint32
	Height        uint32
	Depth         uint32 // bits per pixel
	IndexedColors uint32 // 0 for non-indexed images
	Data          []byte
}

func (p *Picture) Type() uint8 { return BlockPicture }

func (p *Picture) Encode() ([]byte, error) {
	mime := []byte(p.MIME)
	desc := []byte(p.Description)
	total := 4 + 4 + len(mime) + 4 + len(desc) + 4 + 4 + 4 + 4 + 4 + len(p.Data)
	if total > MaxBlockSize {
		return nil, fmt.Errorf("flac: PICTURE block too large (%d bytes, max %d)", total, MaxBlockSize)
	}
	buf := bytes.NewBuffer(make([]byte, 0, total))
	writeBE := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		buf.Write(b[:])
	}
	writeBE(p.PictureType)
	writeBE(uint32(len(mime)))
	buf.Write(mime)
	writeBE(uint32(len(desc)))
	buf.Write(desc)
	writeBE(p.Width)
	writeBE(p.Height)
	writeBE(p.Depth)
	writeBE(p.IndexedColors)
	writeBE(uint32(len(p.Data)))
	buf.Write(p.Data)
	return buf.Bytes(), nil
}

// ParsePicture decodes a FLAC METADATA_BLOCK_PICTURE body into a
// *Picture. Exposed for callers outside FLAC (notably the ogg
// package, where Vorbis Comment's METADATA_BLOCK_PICTURE entries
// contain the same byte layout, base64-encoded).
func ParsePicture(body []byte) (*Picture, error) {
	return parsePicture(body)
}

func parsePicture(body []byte) (*Picture, error) {
	if len(body) < 32 {
		return nil, errors.New("flac: PICTURE block truncated")
	}
	pos := 0
	readBE := func() (uint32, error) {
		if pos+4 > len(body) {
			return 0, errors.New("flac: PICTURE block truncated")
		}
		v := binary.BigEndian.Uint32(body[pos : pos+4])
		pos += 4
		return v, nil
	}
	readBytes := func(n uint32) ([]byte, error) {
		if int(n) > len(body)-pos {
			return nil, fmt.Errorf("flac: PICTURE field length %d exceeds body", n)
		}
		out := body[pos : pos+int(n)]
		pos += int(n)
		return out, nil
	}

	ptype, err := readBE()
	if err != nil {
		return nil, err
	}
	mlen, err := readBE()
	if err != nil {
		return nil, err
	}
	mime, err := readBytes(mlen)
	if err != nil {
		return nil, err
	}
	dlen, err := readBE()
	if err != nil {
		return nil, err
	}
	desc, err := readBytes(dlen)
	if err != nil {
		return nil, err
	}
	width, err := readBE()
	if err != nil {
		return nil, err
	}
	height, err := readBE()
	if err != nil {
		return nil, err
	}
	depth, err := readBE()
	if err != nil {
		return nil, err
	}
	indexed, err := readBE()
	if err != nil {
		return nil, err
	}
	dataLen, err := readBE()
	if err != nil {
		return nil, err
	}
	data, err := readBytes(dataLen)
	if err != nil {
		return nil, err
	}
	return &Picture{
		PictureType:   ptype,
		MIME:          string(mime),
		Description:   string(desc),
		Width:         width,
		Height:        height,
		Depth:         depth,
		IndexedColors: indexed,
		Data:          append([]byte(nil), data...),
	}, nil
}

package ape

import (
	"bytes"
	"fmt"
	"strings"
)

// Cover-art item keys. APEv2 doesn't strictly mandate names but
// these three are the de-facto conventions, matching what
// foobar2000, MusicBrainz Picard, and similar taggers write.
const (
	KeyCoverArtFront = "Cover Art (Front)"
	KeyCoverArtBack  = "Cover Art (Back)"
	KeyCoverArtOther = "Cover Art (Other)"
)

// Picture is one cover-art entry from an APEv2 binary item. The
// on-disk format of the item value is:
//
//	<filename as ASCII or UTF-8>\x00<image data bytes>
//
// The leading filename is informational only; many taggers
// write an empty string before the NUL. APEv2 does not carry
// MIME alongside the image data — consumers that need it should
// sniff the leading bytes of Data (e.g. via
// [github.com/cabbagekobe/tunetag.SniffImageMIME]).
type Picture struct {
	// Filename is the optional file name embedded before the
	// NUL separator. Often empty.
	Filename string

	// Data is the raw image bytes (typically JPEG or PNG).
	Data []byte
}

// DecodePicture splits one APEv2 binary item value into the
// embedded filename and image bytes.
func DecodePicture(raw []byte) (*Picture, error) {
	idx := bytes.IndexByte(raw, 0)
	if idx < 0 {
		return nil, fmt.Errorf("ape: cover-art item missing NUL separator (%d bytes)", len(raw))
	}
	p := &Picture{
		Filename: string(raw[:idx]),
		Data:     append([]byte(nil), raw[idx+1:]...),
	}
	return p, nil
}

// Encode produces the item value bytes for a cover-art Picture.
// Caller is responsible for wrapping the result in an Item with
// Type=ItemBinary and an appropriate key (typically
// KeyCoverArtFront).
func (p *Picture) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteString(p.Filename)
	buf.WriteByte(0)
	buf.Write(p.Data)
	return buf.Bytes()
}

// Pictures returns every cover-art binary item decoded. The
// returned slice preserves on-disk order (front, back, other).
func (t *Tag) Pictures() []*Picture {
	var out []*Picture
	for _, it := range t.Items {
		if it.Type != ItemBinary {
			continue
		}
		if !isCoverArtKey(it.Key) {
			continue
		}
		p, err := DecodePicture(it.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// AddPicture appends a binary item carrying the supplied
// picture. The default key is "Cover Art (Front)"; pass a
// different key (e.g. KeyCoverArtBack) via AddPictureAs when a
// non-front role is needed.
func (t *Tag) AddPicture(p *Picture) error {
	return t.AddPictureAs(KeyCoverArtFront, p)
}

// AddPictureAs appends a cover-art binary item under the given
// key. Returns ErrInvalidKey when key is malformed for APEv2.
func (t *Tag) AddPictureAs(key string, p *Picture) error {
	if err := validateKey(key); err != nil {
		return err
	}
	t.Items = append(t.Items, Item{
		Key:   key,
		Type:  ItemBinary,
		Value: p.Encode(),
	})
	return nil
}

// RemovePictures deletes every "Cover Art (...)" binary item.
func (t *Tag) RemovePictures() {
	out := t.Items[:0]
	for _, it := range t.Items {
		if it.Type == ItemBinary && isCoverArtKey(it.Key) {
			continue
		}
		out = append(out, it)
	}
	t.Items = out
}

func isCoverArtKey(key string) bool {
	return strings.EqualFold(key, KeyCoverArtFront) ||
		strings.EqualFold(key, KeyCoverArtBack) ||
		strings.EqualFold(key, KeyCoverArtOther)
}

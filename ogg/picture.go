package ogg

import (
	"encoding/base64"
	"fmt"

	"github.com/cabbagekobe/tunetag/flac"
)

// FieldMetadataBlockPicture is the Vorbis Comment field name
// reserved by Xiph for embedded cover art. The value is the
// base64 encoding of a FLAC METADATA_BLOCK_PICTURE block body,
// so any Vorbis Comment-bearing container (Ogg Vorbis, Ogg
// Opus, FLAC) can carry cover art using the same on-disk bytes.
const FieldMetadataBlockPicture = "METADATA_BLOCK_PICTURE"

// Pictures decodes every METADATA_BLOCK_PICTURE entry in the
// Vorbis Comment block. Entries whose base64 / FLAC body fails
// to decode are silently skipped; the raw text remains
// accessible via f.Comments.
func (f *File) Pictures() []*flac.Picture {
	if f.Comments == nil {
		return nil
	}
	var out []*flac.Picture
	for _, v := range f.Comments.Get(FieldMetadataBlockPicture) {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			continue
		}
		p, err := flac.ParsePicture(raw)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// AddPicture appends a METADATA_BLOCK_PICTURE entry encoding p.
// The picture bytes are FLAC-encoded then base64-wrapped per the
// Xiph convention.
func (f *File) AddPicture(p *flac.Picture) error {
	if f.Comments == nil {
		return fmt.Errorf("ogg: AddPicture: nil Comments block")
	}
	body, err := p.Encode()
	if err != nil {
		return fmt.Errorf("ogg: encode picture: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(body)
	f.Comments.Add(FieldMetadataBlockPicture, encoded)
	return nil
}

// RemovePictures deletes every METADATA_BLOCK_PICTURE entry.
func (f *File) RemovePictures() {
	if f.Comments == nil {
		return
	}
	f.Comments.Remove(FieldMetadataBlockPicture)
}

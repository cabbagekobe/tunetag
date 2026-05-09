package mp4

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Sentinel errors returned by Read / WriteFile.
var (
	ErrNotMP4              = errors.New("mp4: not an MP4 / ISO BMFF file")
	ErrNoMoov              = errors.New("mp4: missing moov box")
	ErrFragmentedUnsupport = errors.New("mp4: fragmented MP4 (mvex/moof) is not supported")
)

// File holds enough state to read tags from an MP4 / M4A and write
// them back. The atom layout outside of moov/udta/meta/ilst is
// preserved verbatim through `rawTopBoxes`.
type File struct {
	path string

	rawFtyp []byte // body of the first ftyp (or zero-length if absent)
	rawMoov []byte // entire moov box body (header excluded)

	// Indices within rawMoov pointing at the udta -> meta -> ilst
	// chain, filled in by readMoovStructure(). When ilstFound is false
	// the moov has no metadata yet.
	udtaOff, udtaLen   int
	metaOff, metaLen   int
	ilstOff, ilstLen   int
	ilstFound          bool

	// freeOff / freeLen point at a sibling `free` (or `skip`) atom
	// inside moov that Tier 1 writes can absorb space from. Both are
	// -1 when no such atom exists.
	freeOff, freeLen int

	// fragmented is true if mvex / moof was detected in the file.
	fragmented bool

	Tag *Ilst
}

// Read parses path and returns its parsed metadata.
func Read(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return readFromReaderAt(f, info.Size(), path)
}

func readFromReaderAt(r io.ReaderAt, size int64, path string) (*File, error) {
	tops, err := scanTopLevel(r, size)
	if err != nil {
		return nil, err
	}

	out := &File{
		path:    path,
		freeOff: -1, freeLen: -1,
	}
	var foundFtyp, foundMoov bool
	for _, b := range tops {
		switch {
		case b.Type.Equal("ftyp"):
			body, err := readBoxBody(r, b)
			if err != nil {
				return nil, err
			}
			out.rawFtyp = body
			foundFtyp = true
		case b.Type.Equal("moov"):
			body, err := readBoxBody(r, b)
			if err != nil {
				return nil, err
			}
			out.rawMoov = body
			foundMoov = true
		}
	}
	if !foundFtyp {
		return nil, ErrNotMP4
	}
	if !foundMoov {
		return nil, ErrNoMoov
	}

	if err := out.parseMoov(); err != nil {
		return nil, err
	}
	if out.fragmented {
		// Read still succeeds; only writes are blocked.
	}
	return out, nil
}

// parseMoov walks moov to locate udta/meta/ilst (parsing ilst when
// present) and any sibling free atom usable for Tier 1 writes.
func (f *File) parseMoov() error {
	// Walk top-level moov children to locate udta and to detect mvex.
	pos := 0
	for pos < len(f.rawMoov) {
		size, typ, body, err := splitChild(f.rawMoov, pos)
		if err != nil {
			return err
		}
		switch typ.String() {
		case "udta":
			f.udtaOff = pos
			f.udtaLen = int(size)
			if err := f.parseUdta(body, pos+8); err != nil {
				return err
			}
		case "mvex":
			f.fragmented = true
		}
		pos += int(size)
	}
	if !f.ilstFound {
		f.Tag = &Ilst{}
	}
	return nil
}

func (f *File) parseUdta(udtaBody []byte, udtaBodyOff int) error {
	pos := 0
	for pos < len(udtaBody) {
		size, typ, body, err := splitChild(udtaBody, pos)
		if err != nil {
			return err
		}
		if typ.Equal("meta") {
			// meta in udta is a FullBox: 4 bytes (version+flags)
			// before child boxes.
			f.metaOff = udtaBodyOff + pos
			f.metaLen = int(size)
			if len(body) < 4 {
				return fmt.Errorf("mp4: meta box too short")
			}
			if err := f.parseMeta(body[4:], udtaBodyOff+pos+8+4); err != nil {
				return err
			}
		}
		pos += int(size)
	}
	return nil
}

func (f *File) parseMeta(metaBody []byte, metaBodyOff int) error {
	pos := 0
	for pos < len(metaBody) {
		size, typ, body, err := splitChild(metaBody, pos)
		if err != nil {
			return err
		}
		if typ.Equal("ilst") {
			f.ilstOff = metaBodyOff + pos
			f.ilstLen = int(size)
			f.ilstFound = true
			parsed, err := parseIlst(body)
			if err != nil {
				return err
			}
			f.Tag = parsed
		}
		// Track sibling `free`/`skip` inside meta, the most common
		// place writers reserve scratch space for retag operations.
		if typ.Equal("free") || typ.Equal("skip") {
			f.freeOff = metaBodyOff + pos
			f.freeLen = int(size)
		}
		pos += int(size)
	}
	return nil
}

// splitChild reads one child box header from buf at pos, returning
// its total size, type, body slice (referencing buf), and error.
func splitChild(buf []byte, pos int) (size uint32, typ FourCC, body []byte, err error) {
	if pos+8 > len(buf) {
		return 0, FourCC{}, nil, fmt.Errorf("mp4: child header truncated at offset %d", pos)
	}
	rawSize := uint32(buf[pos])<<24 | uint32(buf[pos+1])<<16 | uint32(buf[pos+2])<<8 | uint32(buf[pos+3])
	copy(typ[:], buf[pos+4:pos+8])
	if rawSize == 1 {
		return 0, FourCC{}, nil, fmt.Errorf("mp4: 64-bit largesize box %s in moov children not supported here", typ)
	}
	if rawSize < 8 || int(rawSize) > len(buf)-pos {
		return 0, FourCC{}, nil, fmt.Errorf("mp4: child %s size %d out of range", typ, rawSize)
	}
	body = buf[pos+8 : pos+int(rawSize)]
	return rawSize, typ, body, nil
}

// WriteFile writes f back to path. The strategy ladder is:
//
//  1. Identical-size in-place: when the new ilst encodes to exactly
//     the same byte count as the existing one, the bytes are
//     overwritten in place.
//  2. Sibling-free absorb: when a `free` atom sits immediately after
//     ilst inside meta, its size is grown or shrunk to absorb the
//     ilst delta so the moov outline stays the same and no stco
//     patching is needed.
//  3. Insert a fresh free atom when the new ilst is at least 8 bytes
//     smaller than the old one and no sibling free exists.
//  4. Full rewrite via temp file: the moov is rebuilt and the audio
//     bytes are re-emitted at their new offset. Every stco / co64
//     entry in the file is shifted by the moov delta. Returns
//     ErrStcoOverflow when patching would push a 32-bit chunk
//     offset past 2^32-1 (auto co64 promotion is not yet wired up).
func (f *File) WriteFile(path string) error {
	if f.fragmented {
		return ErrFragmentedUnsupport
	}
	if !f.ilstFound {
		return errors.New("mp4: cannot write: source had no udta/meta/ilst (creating from scratch is not supported in v1)")
	}
	newIlstBody, err := f.Tag.encode()
	if err != nil {
		return err
	}
	newIlstTotal := 8 + len(newIlstBody)
	delta := newIlstTotal - f.ilstLen

	switch {
	case delta == 0:
		return f.overwriteIlstInPlace(path, newIlstBody)
	case f.freeOff >= 0 && f.freeLen-delta >= 8:
		// Shrink/grow the sibling free atom by `delta` bytes so the
		// surrounding meta/udta/moov sizes stay constant.
		return f.absorbWithFree(path, newIlstBody, delta)
	case f.freeOff < 0 && delta < 0 && -delta >= 8:
		// We can introduce a new free atom in the freed bytes.
		return f.insertFreeAfterIlst(path, newIlstBody, -delta)
	}
	// Tier 2/3: full rewrite with stco/co64 patch when needed.
	return f.rewriteFile(path)
}

func (f *File) overwriteIlstInPlace(path string, newIlstBody []byte) error {
	// Build new ilst box (header + body) and overwrite at f.ilstOff
	// in the file. ilstOff is an offset into the moov body, so the
	// absolute file offset is moovBodyOffset + ilstOff. We compute it
	// by re-reading the moov header from disk.
	moovAbs, err := f.absoluteMoovBodyOffset(path)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Seek(moovAbs+int64(f.ilstOff), io.SeekStart); err != nil {
		return err
	}
	if err := writeBox(out, fourCC("ilst"), newIlstBody); err != nil {
		return err
	}
	return nil
}

func (f *File) absorbWithFree(path string, newIlstBody []byte, delta int) error {
	moovAbs, err := f.absoluteMoovBodyOffset(path)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write new ilst at its current offset (moov-relative).
	if _, err := out.Seek(moovAbs+int64(f.ilstOff), io.SeekStart); err != nil {
		return err
	}
	if err := writeBox(out, fourCC("ilst"), newIlstBody); err != nil {
		return err
	}
	// Then rewrite the free atom directly after, with size adjusted
	// by -delta. The free body bytes are zero-fill.
	newFreeLen := f.freeLen - delta
	if newFreeLen < 8 {
		return fmt.Errorf("mp4: absorbWithFree: new free len %d < 8", newFreeLen)
	}
	freeBody := make([]byte, newFreeLen-8)
	if err := writeBox(out, fourCC("free"), freeBody); err != nil {
		return err
	}
	return nil
}

func (f *File) insertFreeAfterIlst(path string, newIlstBody []byte, freeTotal int) error {
	moovAbs, err := f.absoluteMoovBodyOffset(path)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.Seek(moovAbs+int64(f.ilstOff), io.SeekStart); err != nil {
		return err
	}
	if err := writeBox(out, fourCC("ilst"), newIlstBody); err != nil {
		return err
	}
	if err := writeBox(out, fourCC("free"), make([]byte, freeTotal-8)); err != nil {
		return err
	}
	return nil
}

// absoluteMoovBodyOffset re-walks the file's top-level boxes to find
// the offset where the moov body begins (header excluded). It also
// detects whether path is still a valid MP4 before any writes.
func (f *File) absoluteMoovBodyOffset(path string) (int64, error) {
	src, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return 0, err
	}
	tops, err := scanTopLevel(src, info.Size())
	if err != nil {
		return 0, err
	}
	for _, b := range tops {
		if b.Type.Equal("moov") {
			return b.BodyOffset, nil
		}
	}
	return 0, ErrNoMoov
}

// rewriteAtomic is reserved for Phase 6 (Tier 2/3) writes. It is
// declared here so subsequent phases can plug in without touching
// this file's exported surface.
//
//nolint:unused
func (f *File) rewriteAtomic(path string, build func() ([]byte, error)) error {
	body, err := build()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-mp4-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(body); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// EncodedMoovBytes returns a freshly built moov body with the
// current Tag persisted into the udta/meta/ilst chain. Useful for
// Phase 6 rewrites and for tests.
func (f *File) EncodedMoovBytes() ([]byte, error) {
	if !f.ilstFound {
		return nil, errors.New("mp4: source had no ilst chain to clone")
	}
	newIlstBody, err := f.Tag.encode()
	if err != nil {
		return nil, err
	}
	// Replace ilst bytes within rawMoov.
	var buf bytes.Buffer
	buf.Write(f.rawMoov[:f.ilstOff])
	if err := writeBox(&buf, fourCC("ilst"), newIlstBody); err != nil {
		return nil, err
	}
	buf.Write(f.rawMoov[f.ilstOff+f.ilstLen:])
	return buf.Bytes(), nil
}

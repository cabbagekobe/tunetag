package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrStcoOverflow signals that a 32-bit stco entry would exceed
// 2^32 - 1 once shifted by the requested delta. WriteFile uses this
// internally as a trigger to retry the rewrite with stco→co64
// promotion enabled, so callers normally never see it.
var ErrStcoOverflow = errors.New("mp4: chunk offset exceeds 32-bit range")

// patchMoovForRewrite produces a new moov body that has:
//   - the udta/meta/ilst chain replaced with the current Tag
//   - every stco / co64 entry shifted by chunkDelta to account for
//     the change in mdat position relative to the start of file
//
// chunkDelta should be the signed change in mdat offset (positive
// when mdat moves later, negative when earlier). It must be 0 when
// mdat sits before moov (in which case mdat does not move).
//
// When promoteToCo64 is true, every stco encountered in the moov
// hierarchy is rewritten as a co64 box (8-byte entries) regardless
// of overflow. This is used by rewriteFile as a fallback after a
// non-promoted attempt returned ErrStcoOverflow.
func (f *File) patchMoovForRewrite(chunkDelta int64, promoteToCo64 bool) ([]byte, error) {
	if !f.ilstFound {
		return nil, errors.New("mp4: cannot rewrite moov without an existing ilst")
	}
	newIlstBody, err := f.Tag.encode()
	if err != nil {
		return nil, err
	}
	return rewriteMoov(f.rawMoov, newIlstBody, chunkDelta, promoteToCo64)
}

// rewriteMoov builds a new moov body by:
//   - replacing the bytes of the inner ilst with newIlstBody
//   - patching stco/co64 entries by delta when delta != 0
//   - promoting stco to co64 when promoteToCo64 is true
func rewriteMoov(moovBody, newIlstBody []byte, delta int64, promoteToCo64 bool) ([]byte, error) {
	return rewriteContainer(moovBody, "moov", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		switch typ.String() {
		case "trak":
			out, err := rewriteTrak(body, delta, promoteToCo64)
			return typ, out, err
		case "udta":
			out, err := rewriteUdta(body, newIlstBody)
			return typ, out, err
		}
		return typ, body, nil
	})
}

func rewriteUdta(body, newIlstBody []byte) ([]byte, error) {
	return rewriteContainer(body, "udta", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		if !typ.Equal("meta") {
			return typ, body, nil
		}
		// meta is a FullBox: keep its 4-byte version+flags prefix.
		if len(body) < 4 {
			return typ, body, errors.New("mp4: meta body too short")
		}
		head := body[:4]
		inner, err := rewriteContainer(body[4:], "meta", func(typ FourCC, body []byte) (FourCC, []byte, error) {
			if typ.Equal("ilst") {
				return typ, newIlstBody, nil
			}
			return typ, body, nil
		})
		if err != nil {
			return typ, nil, err
		}
		return typ, append(append([]byte(nil), head...), inner...), nil
	})
}

func rewriteTrak(body []byte, delta int64, promoteToCo64 bool) ([]byte, error) {
	return rewriteContainer(body, "trak", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		if typ.Equal("mdia") {
			out, err := rewriteMdia(body, delta, promoteToCo64)
			return typ, out, err
		}
		return typ, body, nil
	})
}

func rewriteMdia(body []byte, delta int64, promoteToCo64 bool) ([]byte, error) {
	return rewriteContainer(body, "mdia", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		if typ.Equal("minf") {
			out, err := rewriteMinf(body, delta, promoteToCo64)
			return typ, out, err
		}
		return typ, body, nil
	})
}

func rewriteMinf(body []byte, delta int64, promoteToCo64 bool) ([]byte, error) {
	return rewriteContainer(body, "minf", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		if typ.Equal("stbl") {
			out, err := rewriteStbl(body, delta, promoteToCo64)
			return typ, out, err
		}
		return typ, body, nil
	})
}

func rewriteStbl(body []byte, delta int64, promoteToCo64 bool) ([]byte, error) {
	return rewriteContainer(body, "stbl", func(typ FourCC, body []byte) (FourCC, []byte, error) {
		switch typ.String() {
		case "stco":
			if delta == 0 && !promoteToCo64 {
				return typ, body, nil
			}
			newBody, becameCo64, err := patchOrPromoteSTCO(body, delta, promoteToCo64)
			if err != nil {
				return typ, nil, err
			}
			if becameCo64 {
				return fourCC("co64"), newBody, nil
			}
			return typ, newBody, nil
		case "co64":
			if delta == 0 {
				return typ, body, nil
			}
			out, err := patchCO64(body, delta)
			return typ, out, err
		}
		return typ, body, nil
	})
}

// rewriteContainer walks each child box of body, applies fn, and
// re-emits the result. parentName is used in error messages only.
//
// fn returns the (possibly modified) box type and body. The box
// type is permitted to change so callers can promote stco→co64 in
// a future revision.
func rewriteContainer(body []byte, parentName string,
	fn func(typ FourCC, body []byte) (FourCC, []byte, error)) ([]byte, error) {

	var out bytes.Buffer
	pos := 0
	for pos < len(body) {
		size, typ, childBody, err := splitChild(body, pos)
		if err != nil {
			return nil, fmt.Errorf("mp4: walking %s: %w", parentName, err)
		}
		newType, newBody, err := fn(typ, childBody)
		if err != nil {
			return nil, fmt.Errorf("mp4: in %s/%s: %w", parentName, typ, err)
		}
		if err := writeBox(&out, newType, newBody); err != nil {
			return nil, err
		}
		pos += int(size)
	}
	return out.Bytes(), nil
}

// patchOrPromoteSTCO patches an stco body by delta. When
// promoteToCo64 is false, returns ErrStcoOverflow if any value
// would leave the 32-bit range. When promoteToCo64 is true, the
// returned body is in co64 format (8-byte entries) and becameCo64
// is true so the caller can rewrite the box type.
func patchOrPromoteSTCO(body []byte, delta int64, promoteToCo64 bool) (newBody []byte, becameCo64 bool, err error) {
	if len(body) < 8 {
		return nil, false, errors.New("mp4: stco body too short")
	}
	count := binary.BigEndian.Uint32(body[4:8])
	expected := 8 + int(count)*4
	if len(body) < expected {
		return nil, false, fmt.Errorf("mp4: stco truncated: have %d, want %d", len(body), expected)
	}
	if promoteToCo64 {
		out := make([]byte, 8+int(count)*8)
		copy(out[:8], body[:8])
		for i := uint32(0); i < count; i++ {
			srcOff := 8 + 4*int(i)
			dstOff := 8 + 8*int(i)
			v := int64(binary.BigEndian.Uint32(body[srcOff : srcOff+4]))
			nv := v + delta
			if nv < 0 {
				return nil, false, fmt.Errorf("mp4: chunk offset %d underflow on promotion", i)
			}
			binary.BigEndian.PutUint64(out[dstOff:dstOff+8], uint64(nv))
		}
		return out, true, nil
	}
	out := make([]byte, len(body))
	copy(out[:8], body[:8])
	for i := uint32(0); i < count; i++ {
		off := 8 + 4*int(i)
		v := int64(binary.BigEndian.Uint32(body[off : off+4]))
		nv := v + delta
		if nv < 0 || nv > 1<<32-1 {
			return nil, false, ErrStcoOverflow
		}
		binary.BigEndian.PutUint32(out[off:off+4], uint32(nv))
	}
	return out, false, nil
}

// patchCO64 returns a copy of a co64 body with every entry shifted
// by delta.
func patchCO64(body []byte, delta int64) ([]byte, error) {
	if len(body) < 8 {
		return nil, errors.New("mp4: co64 body too short")
	}
	count := binary.BigEndian.Uint32(body[4:8])
	expected := 8 + int(count)*8
	if len(body) < expected {
		return nil, fmt.Errorf("mp4: co64 truncated: have %d, want %d", len(body), expected)
	}
	out := make([]byte, len(body))
	copy(out[:8], body[:8])
	for i := uint32(0); i < count; i++ {
		off := 8 + 8*int(i)
		v := int64(binary.BigEndian.Uint64(body[off : off+8]))
		nv := v + delta
		if nv < 0 {
			return nil, fmt.Errorf("mp4: co64 entry %d underflow", i)
		}
		binary.BigEndian.PutUint64(out[off:off+8], uint64(nv))
	}
	return out, nil
}

// rewriteFile performs a Tier 2/3 full rewrite of path with the
// new moov in place. mdat is reproduced as-is (only its absolute
// position changes); stco/co64 entries are patched in moov so they
// continue to point at the right bytes. When patching would push a
// 32-bit chunk offset past 2^32-1, every stco in the file is
// promoted to co64 and the rewrite is retried.
//
// Layout strategy:
//   - mdat-before-moov: trivial. Old layout: ftyp ... mdat ... moov [...].
//     New layout: same, with moov rebuilt at end. Audio bytes do not
//     move, so chunkDelta = 0 and stco needs no patch.
//   - mdat-after-moov: chunkDelta = newMoovSize - oldMoovSize. mdat
//     shifts by chunkDelta; stco/co64 must be patched by chunkDelta.
//   - When fragmented (mvex/moof) is detected, this function returns
//     ErrFragmentedUnsupport without touching the file.
func (f *File) rewriteFile(path string) error {
	if f.fragmented {
		return ErrFragmentedUnsupport
	}
	src, err := readAllTopLevelBoxes(path)
	if err != nil {
		return err
	}
	moovIdx := -1
	mdatIdx := -1
	for i, b := range src.boxes {
		if b.typ.Equal("moov") {
			moovIdx = i
		}
		if b.typ.Equal("mdat") {
			mdatIdx = i
		}
	}
	if moovIdx < 0 {
		return ErrNoMoov
	}

	// mdat-before-moov: chunk offsets do not change.
	if mdatIdx < 0 || mdatIdx < moovIdx {
		newMoov, err := f.patchMoovForRewrite(0, false)
		if err != nil {
			return err
		}
		return src.writeWithMoovReplaced(path, newMoov)
	}

	// mdat-after-moov: probe to compute chunkDelta, then patch.
	// chunkDelta is the change in moov body size; the surrounding
	// 8-byte moov header is constant on both sides so it cancels.
	probe, err := f.patchMoovForRewrite(0, false)
	if err != nil {
		return err
	}
	chunkDelta := int64(len(probe)) - int64(len(f.rawMoov))

	newMoov, err := f.patchMoovForRewrite(chunkDelta, false)
	if err == nil {
		return src.writeWithMoovReplaced(path, newMoov)
	}
	if !errors.Is(err, ErrStcoOverflow) {
		return err
	}

	// Auto-promote: every stco becomes co64 (which grows moov by
	// 4 bytes per entry), so recompute the delta before patching.
	probe2, err := f.patchMoovForRewrite(0, true)
	if err != nil {
		return err
	}
	chunkDelta2 := int64(len(probe2)) - int64(len(f.rawMoov))
	newMoov, err = f.patchMoovForRewrite(chunkDelta2, true)
	if err != nil {
		return err
	}
	return src.writeWithMoovReplaced(path, newMoov)
}

// rawTopLevel records the raw bytes of every top-level box so
// rewriteFile can re-emit them in order.
type rawTopLevel struct {
	typ  FourCC
	body []byte
	hdr  []byte // 8 or 16 raw header bytes (preserves largesize boxes verbatim)
}

type rawTopLevelSet struct {
	srcPath string
	boxes   []rawTopLevel
	moovIdx int
}

func readAllTopLevelBoxes(path string) (*rawTopLevelSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	tops, err := scanTopLevel(f, info.Size())
	if err != nil {
		return nil, err
	}
	out := &rawTopLevelSet{srcPath: path, moovIdx: -1}
	for i, b := range tops {
		hdr := make([]byte, b.HeaderSize)
		if _, err := f.ReadAt(hdr, b.BodyOffset-int64(b.HeaderSize)); err != nil {
			return nil, err
		}
		body, err := readBoxBody(f, b)
		if err != nil {
			return nil, err
		}
		out.boxes = append(out.boxes, rawTopLevel{typ: b.Type, body: body, hdr: hdr})
		if b.Type.Equal("moov") {
			out.moovIdx = i
		}
	}
	return out, nil
}

func (s *rawTopLevelSet) writeWithMoovReplaced(path string, newMoov []byte) error {
	if s.moovIdx < 0 {
		return ErrNoMoov
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

	for i, b := range s.boxes {
		if i == s.moovIdx {
			if err := writeBox(tmp, fourCC("moov"), newMoov); err != nil {
				cleanup()
				return err
			}
			continue
		}
		if _, err := tmp.Write(b.hdr); err != nil {
			cleanup()
			return err
		}
		if _, err := tmp.Write(b.body); err != nil {
			cleanup()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

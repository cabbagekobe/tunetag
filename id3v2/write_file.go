package id3v2

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFile writes t into path, replacing any existing ID3v2 tag at
// the start of the file. The audio body following the existing tag
// is preserved unchanged.
//
// When the new tag (header + frames + Padding) fits within the
// bytes occupied by the previous tag, it is written in place and
// the padding is grown to keep the audio offset stable. Otherwise
// the file is rewritten to a temporary file in the same directory
// and renamed atomically.
func (t *Tag) WriteFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = src.Close()
		}
	}()

	existing, err := scanExistingTagSize(src)
	if err != nil {
		return err
	}

	framesSize, err := t.framesEncodedSize()
	if err != nil {
		return err
	}
	minRequired := uint32(HeaderSize) + framesSize

	// In-place is possible whenever the new frames (plus header) fit
	// within the bytes occupied by the previous tag. When that holds,
	// the leftover bytes are written as padding so the audio offset
	// stays stable regardless of t.Padding. t.Padding only matters
	// for the full-rewrite path, where it sets the padding budget for
	// the freshly written tag.
	if minRequired <= existing {
		closed = true
		_ = src.Close()
		availPad := int(existing) - HeaderSize - int(framesSize)
		return overwriteInPlace(path, t, availPad, existing)
	}
	// Full rewrite: read body from after the existing tag and copy
	// into a freshly written file with the new tag prepended.
	if _, err := src.Seek(int64(existing), io.SeekStart); err != nil {
		return err
	}
	// rewriteWithBody closes src once the audio body has been copied
	// to the temp file, so the rename can succeed on Windows where
	// renaming over an open file is not permitted.
	closed = true
	return rewriteWithBody(path, src, t)
}

func scanExistingTagSize(f *os.File) (uint32, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() < HeaderSize {
		return 0, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	h, err := readHeader(f)
	if err != nil {
		if errors.Is(err, ErrNoTag) {
			return 0, nil
		}
		return 0, err
	}
	total := uint32(HeaderSize) + h.Size
	if h.Flags&FlagFooter != 0 {
		total += HeaderSize
	}
	return total, nil
}

func overwriteInPlace(path string, t *Tag, pad int, expected uint32) (err error) {
	var buf bytes.Buffer
	if err := t.encodeWithPadding(&buf, pad); err != nil {
		return err
	}
	if uint32(buf.Len()) != expected {
		return fmt.Errorf("id3v2: in-place size %d != expected %d", buf.Len(), expected)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(buf.Bytes())
	return err
}

// rewriteWithBody builds a new file (tag bytes + audio body from
// src) at a sibling temp path and renames it over path. src is
// closed before the rename so Windows — which refuses to rename over
// an open file — succeeds.
func rewriteWithBody(path string, src *os.File, t *Tag) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-*.tmp")
	if err != nil {
		_ = src.Close()
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if err := t.Encode(tmp); err != nil {
		cleanup()
		_ = src.Close()
		return err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		cleanup()
		_ = src.Close()
		return err
	}
	// Close the source BEFORE the rename. Windows refuses to rename
	// over a file that is still open in any handle.
	if err := src.Close(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

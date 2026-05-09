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
			src.Close()
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
		src.Close()
		availPad := int(existing) - HeaderSize - int(framesSize)
		return overwriteInPlace(path, t, availPad, existing)
	}
	// Full rewrite: read body from after the existing tag and copy
	// into a freshly written file with the new tag prepended.
	if _, err := src.Seek(int64(existing), io.SeekStart); err != nil {
		return err
	}
	if err := rewriteWithBody(path, src, t); err != nil {
		return err
	}
	closed = true
	return src.Close()
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

func overwriteInPlace(path string, t *Tag, pad int, expected uint32) error {
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
	defer f.Close()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(buf.Bytes())
	return err
}

func rewriteWithBody(path string, body io.Reader, t *Tag) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	if err := t.Encode(tmp); err != nil {
		cleanup()
		return err
	}
	if _, err := io.Copy(tmp, body); err != nil {
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
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

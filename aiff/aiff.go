// Package aiff reads and writes AIFF (FORM/AIFF) and AIFC
// (FORM/AIFC) file metadata.
//
// Two metadata containers are recognised:
//
//   - The classic AIFF text chunks: NAME (title), AUTH (author),
//     "(c) " (copyright), and ANNO (annotation). Multiple ANNO
//     chunks may exist; this package concatenates them with
//     newlines on read and emits a single ANNO on write.
//
//   - "ID3 " chunks containing an embedded ID3v2 tag. The body is
//     parsed via the id3v2 subpackage. Note the trailing space:
//     AIFF uses "ID3 " (uppercase), while WAV uses "id3 "
//     (lowercase).
//
// All other top-level chunks (COMM, SSND, FVER, MARK, …) are
// preserved byte-for-byte. Their bytes are NOT loaded into memory:
// Read records each chunk's offset and size and WriteFile streams
// the bytes from the original source, so tagging a large file costs
// only the metadata's worth of memory. Consequently the source must
// remain readable — and unmodified — until WriteFile is done: keep
// the ReadSeeker passed to Read open, or use ReadFile, which
// remembers the path and reopens it on write. A source that shrinks
// or disappears makes WriteFile fail cleanly; an in-place rewrite
// of the same size is not detected. WriteFile rebuilds the FORM
// size field.
//
// AIFF uses big-endian sizes (unlike WAV's little-endian RIFF).
//
// A *File is not safe for concurrent use.
package aiff

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cabbagekobe/tunetag/id3v2"
)

// Errors returned by this package.
var (
	// ErrNoAIFF is returned by Read when the input does not begin
	// with "FORM" + (any 4 bytes) + "AIFF" or "AIFC".
	ErrNoAIFF = errors.New("aiff: missing FORM/AIFF marker")
)

const (
	chunkFORM = "FORM"
	formAIFF  = "AIFF"
	formAIFC  = "AIFC"

	ChunkNAME      = "NAME" // Title
	ChunkAUTH      = "AUTH" // Author / artist
	ChunkCopyright = "(c) " // Copyright (note trailing space)
	ChunkANNO      = "ANNO" // Annotation / comment (multi-instance allowed)
	ChunkID3       = "ID3 " // Embedded ID3v2 tag (uppercase, trailing space)
)

// File is the parsed metadata + chunk layout of an AIFF / AIFC file.
type File struct {
	// FormType is "AIFF" or "AIFC". WriteFile re-emits whichever
	// was on disk; preserve it unchanged unless you really mean
	// to flip the container variant.
	FormType string

	// Text holds the AIFF text chunks indexed by chunk ID
	// (ChunkNAME, ChunkAUTH, ChunkCopyright). ANNO is collected
	// separately because the spec allows multiples.
	Text map[string]string

	// Annotations holds every ANNO chunk in stream order.
	Annotations []string

	// ID3 is the parsed embedded ID3v2 tag, or nil if the file
	// had no "ID3 " chunk.
	ID3 *id3v2.Tag

	// chunks is every top-level chunk in stream order. Metadata
	// chunks are stored as placeholders so their position is
	// preserved on write; non-metadata chunks record the
	// offset/size of their bytes in the source stream.
	chunks []chunk

	// src is the stream raw chunk bodies are copied from at
	// WriteFile time. Read keeps the caller's ReadSeeker; ReadFile
	// records srcPath instead (and leaves src nil) so the handle
	// need not stay open — WriteFile reopens the path.
	src     io.ReadSeeker
	srcPath string
}

// chunk is one top-level chunk inside the FORM wrapper. Raw chunks
// parsed from a stream carry offset/size into the source (body is
// nil); chunks synthesised at encode time carry their bytes in body.
type chunk struct {
	id     string
	body   []byte // synthesised bytes; nil when the chunk lives in the source stream
	offset int64  // body start in the source stream (valid when body is nil)
	size   uint32 // body length in the source stream (valid when body is nil)
	kind   chunkKind
}

type chunkKind uint8

const (
	chunkRaw chunkKind = iota
	chunkText
	chunkAnno
	chunkID3v2
)

// Read parses the metadata region of an AIFF / AIFC file. Audio
// chunks are neither decoded nor buffered — only their offset/size
// is recorded — so rs must remain open, readable, and unmodified
// until any WriteFile call is done.
func Read(rs io.ReadSeeker) (*File, error) {
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var hdr [12]byte
	if _, err := io.ReadFull(rs, hdr[:]); err != nil {
		return nil, fmt.Errorf("aiff: short header: %w", err)
	}
	if string(hdr[0:4]) != chunkFORM {
		return nil, ErrNoAIFF
	}
	form := string(hdr[8:12])
	if form != formAIFF && form != formAIFC {
		return nil, ErrNoAIFF
	}

	f := &File{FormType: form, Text: map[string]string{}, src: rs}
	for {
		var ch [8]byte
		_, err := io.ReadFull(rs, ch[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, fmt.Errorf("aiff: read chunk header: %w", err)
		}
		id := string(ch[0:4])
		size := binary.BigEndian.Uint32(ch[4:8])
		// Bound the read against the remaining file size so a
		// malformed (or hostile) chunk header claiming size=4 GiB
		// doesn't force a 4 GiB allocation on the metadata paths
		// below (or a silent seek past end-of-file).
		pos, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if int64(size) > end-pos {
			return nil, fmt.Errorf("aiff: chunk %q declared size %d exceeds remaining file (%d bytes)", id, size, end-pos)
		}

		// Only metadata chunks are materialised. Everything else —
		// notably the audio SSND chunk, which dominates the file —
		// is skipped over and remembered by offset/size so WriteFile
		// can stream it from the source later.
		switch id {
		case ChunkNAME, ChunkAUTH, ChunkCopyright, ChunkANNO, ChunkID3:
			body := make([]byte, size)
			if _, err := io.ReadFull(rs, body); err != nil {
				return nil, fmt.Errorf("aiff: chunk %q short body (%d bytes): %w", id, size, err)
			}
			switch id {
			case ChunkANNO:
				f.Annotations = append(f.Annotations, stripTrailingNUL(string(body)))
				f.chunks = append(f.chunks, chunk{id: id, kind: chunkAnno})
			case ChunkID3:
				t, err := id3v2.Read(bytes.NewReader(body))
				if err != nil {
					return nil, fmt.Errorf("aiff: ID3 chunk: %w", err)
				}
				f.ID3 = t
				f.chunks = append(f.chunks, chunk{id: id, kind: chunkID3v2})
			default:
				f.Text[id] = stripTrailingNUL(string(body))
				f.chunks = append(f.chunks, chunk{id: id, kind: chunkText})
			}
		default:
			if _, err := rs.Seek(pos+int64(size), io.SeekStart); err != nil {
				return nil, err
			}
			f.chunks = append(f.chunks, chunk{id: id, offset: pos, size: size, kind: chunkRaw})
		}
		// AIFF chunks are word-aligned: odd size → 1 pad byte. Skip
		// it if present (a seek past end-of-file is harmless — the
		// next header read just hits EOF).
		if size%2 == 1 {
			if _, err := rs.Seek(1, io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("aiff: skip pad after %q: %w", id, err)
			}
		}
	}
	return f, nil
}

// ReadFile is a convenience wrapper around Read. The returned File
// remembers path (rather than holding the handle open) and reopens
// it to stream the audio chunks on WriteFile.
func ReadFile(path string) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	f, err := Read(fh)
	if err != nil {
		return nil, err
	}
	f.src = nil
	f.srcPath = path
	return f, nil
}

// WriteFile rewrites path with the current chunk layout. The audio
// chunks are streamed from the source (the ReadSeeker given to
// Read, or a reopen of the ReadFile path); only text / annotation /
// ID3 chunks are regenerated. Empty Text entries, no annotations,
// and a nil ID3 cause the corresponding chunks to be dropped.
func (f *File) WriteFile(path string) error {
	src, closeSrc, err := f.source()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-aiff-*.tmp")
	if err != nil {
		closeSrc()
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	err = f.encodeTo(tmp, src)
	// The source is only read during encodeTo; close it before the
	// rename below — Windows refuses to replace a file that still
	// has an open handle.
	closeSrc()
	if err != nil {
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

// source returns the stream raw chunk bodies are copied from: a
// fresh handle on the remembered path (ReadFile), or the caller's
// ReadSeeker (Read). It is nil — with a no-op closer — for a File
// built by hand with no stream-backed chunks.
func (f *File) source() (io.ReadSeeker, func(), error) {
	if f.srcPath != "" {
		fh, err := os.Open(f.srcPath)
		if err != nil {
			return nil, nil, fmt.Errorf("aiff: reopen source: %w", err)
		}
		return fh, func() { _ = fh.Close() }, nil
	}
	return f.src, func() {}, nil
}

// encodeTo writes the full FORM/AIFF byte stream to w, drawing raw
// chunk bodies from src.
func (f *File) encodeTo(w io.Writer, src io.ReadSeeker) error {
	if f.FormType == "" {
		f.FormType = formAIFF
	}
	if f.FormType != formAIFF && f.FormType != formAIFC {
		return fmt.Errorf("aiff: invalid FormType %q", f.FormType)
	}

	// Materialise chunk list. Annotation placeholders only emit
	// once each, with the first one drawing all current
	// annotations; subsequent ANNO placeholders are dropped so
	// the count matches f.Annotations.
	annoEmitted := 0
	saw := map[string]bool{}
	emitted := make([]chunk, 0, len(f.chunks)+4)
	for _, c := range f.chunks {
		switch c.kind {
		case chunkText:
			saw[c.id] = true
			val, ok := f.Text[c.id]
			if !ok || val == "" {
				continue
			}
			emitted = append(emitted, chunk{id: c.id, body: []byte(val), kind: chunkRaw})
		case chunkAnno:
			saw[ChunkANNO] = true
			if annoEmitted < len(f.Annotations) {
				emitted = append(emitted, chunk{id: ChunkANNO, body: []byte(f.Annotations[annoEmitted]), kind: chunkRaw})
				annoEmitted++
			}
		case chunkID3v2:
			saw[ChunkID3] = true
			if f.ID3 == nil {
				continue
			}
			var buf bytes.Buffer
			if err := f.ID3.Encode(&buf); err != nil {
				return fmt.Errorf("aiff: encode ID3 chunk: %w", err)
			}
			emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
		default:
			emitted = append(emitted, c)
		}
	}
	// Append any text chunks added since Read but with no
	// placeholder in chunks.
	for _, id := range []string{ChunkNAME, ChunkAUTH, ChunkCopyright} {
		if saw[id] {
			continue
		}
		if v, ok := f.Text[id]; ok && v != "" {
			emitted = append(emitted, chunk{id: id, body: []byte(v), kind: chunkRaw})
		}
	}
	// Append any extra annotations beyond the original placeholder count.
	for ; annoEmitted < len(f.Annotations); annoEmitted++ {
		emitted = append(emitted, chunk{id: ChunkANNO, body: []byte(f.Annotations[annoEmitted]), kind: chunkRaw})
	}
	if !saw[ChunkID3] && f.ID3 != nil {
		var buf bytes.Buffer
		if err := f.ID3.Encode(&buf); err != nil {
			return fmt.Errorf("aiff: encode ID3 chunk: %w", err)
		}
		emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
	}

	// The FORM size field precedes the payload, so total the chunk
	// sizes arithmetically before streaming anything.
	innerLen := int64(4) // form type
	for _, c := range emitted {
		if len(c.id) != 4 {
			return fmt.Errorf("aiff: chunk id %q is not 4 bytes", c.id)
		}
		n := chunkBodyLen(c)
		innerLen += 8 + n + n%2
	}
	if uint64(innerLen) > uint64(^uint32(0)) {
		return errors.New("aiff: encoded body exceeds 4 GiB")
	}

	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString(chunkFORM)
	_ = binary.Write(bw, binary.BigEndian, uint32(innerLen))
	_, _ = bw.WriteString(f.FormType)
	for _, c := range emitted {
		n := chunkBodyLen(c)
		_, _ = bw.WriteString(c.id)
		_ = binary.Write(bw, binary.BigEndian, uint32(n))
		if c.body != nil || c.size == 0 {
			_, _ = bw.Write(c.body)
		} else {
			if src == nil {
				return fmt.Errorf("aiff: chunk %q needs the source stream, which is no longer available", c.id)
			}
			if _, err := src.Seek(c.offset, io.SeekStart); err != nil {
				return fmt.Errorf("aiff: seek source for chunk %q: %w", c.id, err)
			}
			if _, err := io.CopyN(bw, src, int64(c.size)); err != nil {
				return fmt.Errorf("aiff: copy chunk %q from source: %w", c.id, err)
			}
		}
		if n%2 == 1 {
			_ = bw.WriteByte(0)
		}
	}
	return bw.Flush()
}

// chunkBodyLen is the on-disk body length of c (excluding header
// and alignment pad).
func chunkBodyLen(c chunk) int64 {
	if c.body != nil {
		return int64(len(c.body))
	}
	return int64(c.size)
}

// --- accessors -------------------------------------------------

// Title returns the title: ID3v2 first, else the NAME chunk.
func (f *File) Title() string { return f.pick(id3v2GetTitle, ChunkNAME) }

// Artist returns the artist: ID3v2 first, else the AUTH chunk.
func (f *File) Artist() string { return f.pick(id3v2GetArtist, ChunkAUTH) }

// Album returns the album from ID3v2 only. AIFF has no canonical
// album text chunk.
func (f *File) Album() string {
	if f.ID3 != nil {
		return f.ID3.Album()
	}
	return ""
}

// AlbumArtist returns the album artist from ID3v2 only.
func (f *File) AlbumArtist() string {
	if f.ID3 != nil {
		return f.ID3.AlbumArtist()
	}
	return ""
}

// Composer returns the composer from ID3v2 only.
func (f *File) Composer() string {
	if f.ID3 != nil {
		return f.ID3.Composer()
	}
	return ""
}

// Genre returns the genre from ID3v2 only.
func (f *File) Genre() string {
	if f.ID3 != nil {
		return f.ID3.Genre()
	}
	return ""
}

// Comment returns ID3v2 COMM if present, otherwise the
// concatenated ANNO chunks (joined with newlines).
func (f *File) Comment() string {
	if f.ID3 != nil {
		if s := f.ID3.Comment(); s != "" {
			return s
		}
	}
	return strings.Join(f.Annotations, "\n")
}

// Year returns the ID3v2 year if present, else 0. AIFF has no
// canonical year text chunk.
func (f *File) Year() int {
	if f.ID3 != nil {
		return f.ID3.Year()
	}
	return 0
}

// TrackNumber returns the (number, total) pair from ID3v2 only.
func (f *File) TrackNumber() (n, total int) {
	if f.ID3 != nil {
		return f.ID3.TrackNumber()
	}
	return 0, 0
}

// DiscNumber returns the (number, total) pair from ID3v2 only.
func (f *File) DiscNumber() (n, total int) {
	if f.ID3 != nil {
		return f.ID3.DiscNumber()
	}
	return 0, 0
}

// Pictures returns the embedded ID3v2 APIC frames; nil if no ID3
// chunk is present.
func (f *File) Pictures() []*id3v2.PictureFrame {
	if f.ID3 == nil {
		return nil
	}
	return f.ID3.PictureFrames()
}

// SetTitle sets the NAME chunk.
func (f *File) SetTitle(s string) { f.setText(ChunkNAME, s) }

// SetAuthor sets the AUTH chunk (AIFF's notion of "artist").
func (f *File) SetAuthor(s string) { f.setText(ChunkAUTH, s) }

// SetCopyright sets the "(c) " chunk.
func (f *File) SetCopyright(s string) { f.setText(ChunkCopyright, s) }

func (f *File) setText(id, s string) {
	if f.Text == nil {
		f.Text = map[string]string{}
	}
	if s == "" {
		delete(f.Text, id)
		return
	}
	f.Text[id] = s
}

func (f *File) pick(fromID3 func(*id3v2.Tag) string, textID string) string {
	if f.ID3 != nil {
		if s := fromID3(f.ID3); s != "" {
			return s
		}
	}
	return f.Text[textID]
}

func stripTrailingNUL(s string) string {
	return strings.TrimRight(s, "\x00")
}

func id3v2GetTitle(t *id3v2.Tag) string  { return t.Title() }
func id3v2GetArtist(t *id3v2.Tag) string { return t.Artist() }

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
// preserved byte-for-byte. WriteFile rewrites the file from the
// in-memory chunk list and rebuilds the FORM size field.
//
// AIFF uses big-endian sizes (unlike WAV's little-endian RIFF).
//
// A *File is not safe for concurrent use.
package aiff

import (
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
	// preserved on write; non-metadata chunks carry raw bytes.
	chunks []chunk
}

type chunk struct {
	id   string
	body []byte
	kind chunkKind
}

type chunkKind uint8

const (
	chunkRaw chunkKind = iota
	chunkText
	chunkAnno
	chunkID3v2
)

// Read parses the metadata region of an AIFF / AIFC file. Audio
// chunks are not decoded; their bytes are preserved.
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

	f := &File{FormType: form, Text: map[string]string{}}
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
		// Bound the allocation against the remaining file size so
		// a malformed (or hostile) chunk header claiming size=4 GiB
		// doesn't force a 4 GiB allocation before the subsequent
		// ReadFull fails.
		pos, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if int64(size) > end-pos {
			return nil, fmt.Errorf("aiff: chunk %q declared size %d exceeds remaining file (%d bytes)", id, size, end-pos)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(rs, body); err != nil {
			return nil, fmt.Errorf("aiff: chunk %q short body (%d bytes): %w", id, size, err)
		}
		// AIFF chunks are word-aligned: odd size → 1 pad byte.
		if size%2 == 1 {
			var pad [1]byte
			if _, err := io.ReadFull(rs, pad[:]); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("aiff: read pad after %q: %w", id, err)
			}
		}
		switch id {
		case ChunkNAME, ChunkAUTH, ChunkCopyright:
			f.Text[id] = stripTrailingNUL(string(body))
			f.chunks = append(f.chunks, chunk{id: id, kind: chunkText})
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
			f.chunks = append(f.chunks, chunk{id: id, body: body, kind: chunkRaw})
		}
	}
	return f, nil
}

// ReadFile is a convenience wrapper around Read.
func ReadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Read(f)
}

// WriteFile rewrites path with the current chunk layout. The
// audio chunks are preserved; only text / annotation / ID3 chunks
// are regenerated. Empty Text entries, no annotations, and a nil
// ID3 cause the corresponding chunks to be dropped.
func (f *File) WriteFile(path string) error {
	body, err := f.encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-aiff-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
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
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (f *File) encode() ([]byte, error) {
	if f.FormType == "" {
		f.FormType = formAIFF
	}
	if f.FormType != formAIFF && f.FormType != formAIFC {
		return nil, fmt.Errorf("aiff: invalid FormType %q", f.FormType)
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
				return nil, fmt.Errorf("aiff: encode ID3 chunk: %w", err)
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
			return nil, fmt.Errorf("aiff: encode ID3 chunk: %w", err)
		}
		emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
	}

	// Build payload (everything after FORM size + form type).
	var inner bytes.Buffer
	inner.WriteString(f.FormType)
	for _, c := range emitted {
		if len(c.id) != 4 {
			return nil, fmt.Errorf("aiff: chunk id %q is not 4 bytes", c.id)
		}
		inner.WriteString(c.id)
		_ = binary.Write(&inner, binary.BigEndian, uint32(len(c.body)))
		inner.Write(c.body)
		if len(c.body)%2 == 1 {
			inner.WriteByte(0)
		}
	}
	if inner.Len() > int(^uint32(0)) {
		return nil, errors.New("aiff: encoded body exceeds 4 GiB")
	}
	var out bytes.Buffer
	out.WriteString(chunkFORM)
	_ = binary.Write(&out, binary.BigEndian, uint32(inner.Len()))
	out.Write(inner.Bytes())
	return out.Bytes(), nil
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

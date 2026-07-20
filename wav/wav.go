// Package wav reads and writes WAV (RIFF/WAVE) file metadata.
//
// Two metadata containers are recognised:
//
//   - "LIST" chunks of type "INFO" — the classic RIFF INFO tags
//     (INAM, IART, IPRD, ICRD, IGNR, ICMT, ITRK, …). Values are
//     stored as NUL-terminated strings, conventionally CP1252 /
//     latin-1 but increasingly UTF-8 in modern writers; this
//     package treats them as UTF-8 and round-trips bytes unchanged.
//
//   - "id3 " chunks containing an embedded ID3v2 tag (the Adobe /
//     Wavelab convention). The body is parsed via the id3v2
//     subpackage.
//
// All other top-level chunks ("fmt ", "data", "fact", "JUNK", …)
// are preserved byte-for-byte. Their bytes are NOT loaded into
// memory: Read records each chunk's offset and size and WriteFile
// streams the bytes from the original source, so tagging a
// multi-hundred-MB recording costs only the metadata's worth of
// memory. Consequently the source must remain readable — and
// unmodified — until WriteFile is done: keep the ReadSeeker
// passed to Read open, or use ReadFile, which remembers the path
// and reopens it on write. A source that shrinks or disappears
// makes WriteFile fail cleanly; an in-place rewrite of the same
// size is not detected.
// Any chunk ordering produced by Read is faithfully restored.
//
// 64-bit RIFF (RF64 / BW64) is detected and rejected with
// ErrRF64Unsupported; the spec's 64-bit size table (ds64) is not
// implemented here.
//
// A *File is not safe for concurrent use.
package wav

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
	// ErrNoWAV is returned by Read when the input does not begin
	// with "RIFF" + (any 4 bytes) + "WAVE".
	ErrNoWAV = errors.New("wav: missing RIFF/WAVE marker")

	// ErrRF64Unsupported is returned when the input begins with
	// "RF64" or "BW64" — the 64-bit RIFF variants. These need a
	// "ds64" chunk to recover the true sizes; tunetag does not
	// implement that yet and refuses to guess.
	ErrRF64Unsupported = errors.New("wav: RF64 / BW64 (64-bit RIFF) is not supported")

	// ErrInvalidChunk is returned when a chunk header runs past
	// end-of-stream or declares a negative size.
	ErrInvalidChunk = errors.New("wav: invalid chunk header")
)

// Chunk IDs used inside a WAVE file. Exported so callers building
// files by hand (e.g. tests) can reference them without typos.
const (
	chunkRIFF = "RIFF"
	chunkRF64 = "RF64"
	chunkBW64 = "BW64"
	waveType  = "WAVE"

	ChunkLIST = "LIST"
	ChunkINFO = "INFO"
	ChunkID3  = "id3 " // trailing space is part of the FOURCC
)

// Common LIST/INFO sub-chunk FOURCCs. These follow the canonical
// RIFF INFO names defined by Microsoft and used by most taggers.
const (
	InfoTitle      = "INAM" // Track title
	InfoArtist     = "IART" // Artist
	InfoAlbum      = "IPRD" // Product / album
	InfoDate       = "ICRD" // Creation date — typically YYYY-MM-DD or YYYY
	InfoGenre      = "IGNR" // Genre
	InfoComment    = "ICMT" // Comment
	InfoTrack      = "ITRK" // Track number — non-standard but widespread
	InfoComposer   = "IMUS" // Composer — non-standard but used
	InfoCopyright  = "ICOP" // Copyright
	InfoSoftware   = "ISFT" // Software / encoder name
	InfoEngineer   = "IENG" // Engineer
	InfoTechnician = "ITCH" // Technician
)

// File is the parsed metadata + chunk layout of a WAV file. The
// audio bytes inside "data" (and every non-metadata chunk) are
// preserved by reference — offset and size into the source — and
// streamed back out by WriteFile.
type File struct {
	// Info holds LIST/INFO entries in stream order. Use Info
	// directly to add, mutate, or delete entries; the helpers
	// Title()/SetTitle()/… below are convenience wrappers.
	Info []InfoItem

	// ID3 is the embedded id3 chunk's parsed tag, or nil if the
	// file had no id3 chunk. Mutate Frames directly to edit; pass
	// nil to drop the chunk on the next WriteFile.
	ID3 *id3v2.Tag

	// chunks is every top-level chunk in stream order. LIST/INFO
	// and id3 chunks are stored as placeholder entries so their
	// position is preserved on write; other chunks record the
	// offset/size of their bytes in the source stream.
	chunks []chunk

	// src is the stream raw chunk bodies are copied from at
	// WriteFile time. Read keeps the caller's ReadSeeker; ReadFile
	// records srcPath instead (and leaves src nil) so the handle
	// need not stay open — WriteFile reopens the path.
	src     io.ReadSeeker
	srcPath string
}

// InfoItem is one entry inside a LIST/INFO chunk: a 4-character ID
// (e.g. "INAM") and its NUL-terminated value. Trailing NULs are
// stripped on Read; the writer re-pads as required.
type InfoItem struct {
	ID    string // exactly 4 ASCII bytes; consumers should use the Info* constants
	Value string
}

// chunk is one top-level chunk inside the RIFF wrapper. Raw chunks
// parsed from a stream carry offset/size into the source (body is
// nil); chunks synthesised at encode time carry their bytes in
// body. The kind discriminator lets the writer regenerate LIST/INFO
// and id3 layouts from File.Info / File.ID3.
type chunk struct {
	id     string // 4 ASCII bytes
	body   []byte // synthesised bytes; nil when the chunk lives in the source stream
	offset int64  // body start in the source stream (valid when body is nil)
	size   uint32 // body length in the source stream (valid when body is nil)
	kind   chunkKind
}

type chunkKind uint8

const (
	chunkRaw      chunkKind = iota // verbatim bytes in body
	chunkInfoList                  // LIST/INFO; regenerated from File.Info
	chunkID3v2                     // id3 ; regenerated from File.ID3
)

// Read parses the metadata region (and remembers the full chunk
// layout) of a WAV file from rs. Audio chunks are neither decoded
// nor buffered — only their offset/size is recorded — so rs must
// remain open, readable, and unmodified until any WriteFile call
// is done.
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
		return nil, fmt.Errorf("wav: short header: %w", err)
	}
	switch string(hdr[0:4]) {
	case chunkRIFF:
		// fall through
	case chunkRF64, chunkBW64:
		return nil, ErrRF64Unsupported
	default:
		return nil, ErrNoWAV
	}
	if string(hdr[8:12]) != waveType {
		return nil, ErrNoWAV
	}

	f := &File{src: rs}
	for {
		var ch [8]byte
		_, err := io.ReadFull(rs, ch[:])
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A truncated trailer is common on poorly-written files.
			// Stop parsing rather than refusing the file outright.
			if errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, fmt.Errorf("wav: read chunk header: %w", err)
		}
		id := string(ch[0:4])
		size := binary.LittleEndian.Uint32(ch[4:8])
		// Bound the read: a chunk that claims to be larger than the
		// remaining file cannot be valid. Without this check a
		// malformed (or hostile) file with size=4 GiB would make
		// the metadata paths below allocate 4 GiB (and the seek
		// paths silently run past end-of-file).
		pos, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if int64(size) > end-pos {
			return nil, fmt.Errorf("wav: chunk %q declared size %d exceeds remaining file (%d bytes)", id, size, end-pos)
		}

		// Only metadata chunks are materialised. Everything else —
		// notably the audio "data" chunk, which dominates the file —
		// is skipped over and remembered by offset/size so WriteFile
		// can stream it from the source later.
		switch {
		case id == ChunkLIST && size >= 4:
			var typ [4]byte
			if _, err := io.ReadFull(rs, typ[:]); err != nil {
				return nil, fmt.Errorf("wav: chunk %q short body (%d bytes): %w", id, size, err)
			}
			if string(typ[:]) != ChunkINFO {
				// A non-INFO LIST (e.g. adtl) is preserved raw; the
				// recorded range covers the whole body including the
				// 4 type bytes just consumed.
				if _, err := rs.Seek(pos+int64(size), io.SeekStart); err != nil {
					return nil, err
				}
				f.chunks = append(f.chunks, chunk{id: id, offset: pos, size: size, kind: chunkRaw})
				break
			}
			body := make([]byte, size-4)
			if _, err := io.ReadFull(rs, body); err != nil {
				return nil, fmt.Errorf("wav: chunk %q short body (%d bytes): %w", id, size, err)
			}
			items, err := parseInfo(body)
			if err != nil {
				return nil, fmt.Errorf("wav: LIST/INFO: %w", err)
			}
			f.Info = append(f.Info, items...)
			f.chunks = append(f.chunks, chunk{id: ChunkLIST, kind: chunkInfoList})
		case id == ChunkID3:
			body := make([]byte, size)
			if _, err := io.ReadFull(rs, body); err != nil {
				return nil, fmt.Errorf("wav: chunk %q short body (%d bytes): %w", id, size, err)
			}
			t, err := id3v2.Read(bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("wav: id3 chunk: %w", err)
			}
			f.ID3 = t
			f.chunks = append(f.chunks, chunk{id: ChunkID3, kind: chunkID3v2})
		default:
			if _, err := rs.Seek(pos+int64(size), io.SeekStart); err != nil {
				return nil, err
			}
			f.chunks = append(f.chunks, chunk{id: id, offset: pos, size: size, kind: chunkRaw})
		}
		// RIFF chunks are word-aligned: an odd size is followed by
		// one pad byte. Skip it if present (a seek past end-of-file
		// is harmless — the next header read just hits EOF).
		if size%2 == 1 {
			if _, err := rs.Seek(1, io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("wav: skip pad after %q: %w", id, err)
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

// parseInfo splits the body of a LIST/INFO chunk into its
// sub-chunks. body is everything after the leading "INFO" tag.
func parseInfo(body []byte) ([]InfoItem, error) {
	var out []InfoItem
	for i := 0; i+8 <= len(body); {
		id := string(body[i : i+4])
		size := int(binary.LittleEndian.Uint32(body[i+4 : i+8]))
		i += 8
		if size < 0 || i+size > len(body) {
			return nil, fmt.Errorf("INFO sub-chunk %q size %d overflows", id, size)
		}
		val := body[i : i+size]
		// Values are conventionally NUL-terminated; strip any
		// trailing NULs the writer added for word alignment.
		val = bytes.TrimRight(val, "\x00")
		out = append(out, InfoItem{ID: id, Value: string(val)})
		i += size
		if size%2 == 1 && i < len(body) {
			i++ // word-alignment pad
		}
	}
	return out, nil
}

// encodeInfo encodes File.Info into the body bytes that belong
// inside a LIST chunk (i.e. starting with the "INFO" type tag).
func (f *File) encodeInfo() []byte {
	var buf bytes.Buffer
	buf.WriteString(ChunkINFO)
	for _, it := range f.Info {
		if len(it.ID) != 4 {
			continue
		}
		// NUL-terminate; the spec calls for a trailing NUL on
		// every INFO string, and many parsers depend on it.
		val := []byte(it.Value)
		val = append(val, 0)
		buf.WriteString(it.ID)
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(val)))
		buf.Write(val)
		if len(val)%2 == 1 {
			buf.WriteByte(0) // word-alignment pad
		}
	}
	return buf.Bytes()
}

// WriteFile rewrites path with the current chunk layout. The
// caller's audio bytes are streamed from the source (the ReadSeeker
// given to Read, or a reopen of the ReadFile path); only LIST/INFO
// and id3 chunks are regenerated from f.Info and f.ID3.
//
// When f.Info is empty the LIST chunk is omitted. When f.ID3 is
// nil the id3 chunk is omitted. If either is set but no
// placeholder exists in the original chunk list (e.g. a file that
// gained tags after Read), the missing chunk is appended at the
// end so it sits after the audio data, which is the most
// compatible position for players that pre-buffer "data".
func (f *File) WriteFile(path string) error {
	src, closeSrc, err := f.source()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-wav-*.tmp")
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
			return nil, nil, fmt.Errorf("wav: reopen source: %w", err)
		}
		return fh, func() { _ = fh.Close() }, nil
	}
	return f.src, func() {}, nil
}

// encodeTo writes the full RIFF/WAVE byte stream to w, drawing raw
// chunk bodies from src.
func (f *File) encodeTo(w io.Writer, src io.ReadSeeker) error {
	// Materialise the chunk list, substituting placeholders with
	// real bytes and dropping placeholders that have nothing to
	// emit (empty Info or nil ID3).
	emitted := make([]chunk, 0, len(f.chunks)+2)
	sawInfo, sawID3 := false, false
	for _, c := range f.chunks {
		switch c.kind {
		case chunkInfoList:
			sawInfo = true
			if len(f.Info) == 0 {
				continue
			}
			emitted = append(emitted, chunk{id: ChunkLIST, body: f.encodeInfo(), kind: chunkRaw})
		case chunkID3v2:
			sawID3 = true
			if f.ID3 == nil {
				continue
			}
			var buf bytes.Buffer
			if err := f.ID3.Encode(&buf); err != nil {
				return fmt.Errorf("wav: encode id3 chunk: %w", err)
			}
			emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
		default:
			emitted = append(emitted, c)
		}
	}
	if !sawInfo && len(f.Info) > 0 {
		emitted = append(emitted, chunk{id: ChunkLIST, body: f.encodeInfo(), kind: chunkRaw})
	}
	if !sawID3 && f.ID3 != nil {
		var buf bytes.Buffer
		if err := f.ID3.Encode(&buf); err != nil {
			return fmt.Errorf("wav: encode id3 chunk: %w", err)
		}
		emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
	}

	// The RIFF size field precedes the payload, so total the chunk
	// sizes arithmetically before streaming anything.
	innerLen := int64(4) // "WAVE"
	for _, c := range emitted {
		if len(c.id) != 4 {
			return fmt.Errorf("wav: chunk id %q is not 4 bytes", c.id)
		}
		n := chunkBodyLen(c)
		innerLen += 8 + n + n%2
	}
	if uint64(innerLen) > uint64(^uint32(0)) {
		return errors.New("wav: encoded body exceeds 4 GiB; RF64 required")
	}

	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString(chunkRIFF)
	_ = binary.Write(bw, binary.LittleEndian, uint32(innerLen))
	_, _ = bw.WriteString(waveType)
	for _, c := range emitted {
		n := chunkBodyLen(c)
		_, _ = bw.WriteString(c.id)
		_ = binary.Write(bw, binary.LittleEndian, uint32(n))
		if c.body != nil || c.size == 0 {
			_, _ = bw.Write(c.body)
		} else {
			if src == nil {
				return fmt.Errorf("wav: chunk %q needs the source stream, which is no longer available", c.id)
			}
			if _, err := src.Seek(c.offset, io.SeekStart); err != nil {
				return fmt.Errorf("wav: seek source for chunk %q: %w", c.id, err)
			}
			if _, err := io.CopyN(bw, src, int64(c.size)); err != nil {
				return fmt.Errorf("wav: copy chunk %q from source: %w", c.id, err)
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

// --- Convenience accessors -------------------------------------

// findInfo returns the index of the first InfoItem whose ID
// matches id (case-sensitive). -1 if absent.
func (f *File) findInfo(id string) int {
	for i, it := range f.Info {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// InfoValue returns the first LIST/INFO value for id, or "".
func (f *File) InfoValue(id string) string {
	if i := f.findInfo(id); i >= 0 {
		return f.Info[i].Value
	}
	return ""
}

// SetInfo sets (or replaces) the first LIST/INFO entry for id.
// Passing an empty value removes the entry.
func (f *File) SetInfo(id, value string) {
	if len(id) != 4 {
		return
	}
	idx := f.findInfo(id)
	if value == "" {
		if idx >= 0 {
			f.Info = append(f.Info[:idx], f.Info[idx+1:]...)
		}
		return
	}
	if idx >= 0 {
		f.Info[idx].Value = value
		return
	}
	f.Info = append(f.Info, InfoItem{ID: id, Value: value})
}

// Title returns the most-informative title available: the ID3v2
// frame if present, otherwise the LIST/INFO INAM entry.
func (f *File) Title() string { return f.pick(id3v2GetTitle, InfoTitle) }

// Artist returns the artist: ID3v2 first, else IART.
func (f *File) Artist() string { return f.pick(id3v2GetArtist, InfoArtist) }

// Album returns the album: ID3v2 first, else IPRD.
func (f *File) Album() string { return f.pick(id3v2GetAlbum, InfoAlbum) }

// AlbumArtist returns the album artist from the ID3v2 tag if
// present. LIST/INFO has no canonical equivalent.
func (f *File) AlbumArtist() string {
	if f.ID3 != nil {
		return f.ID3.AlbumArtist()
	}
	return ""
}

// Composer returns the composer: ID3v2 first, else IMUS.
func (f *File) Composer() string { return f.pick(id3v2GetComposer, InfoComposer) }

// Genre returns the genre: ID3v2 first, else IGNR.
func (f *File) Genre() string { return f.pick(id3v2GetGenre, InfoGenre) }

// Comment returns the comment: ID3v2 first, else ICMT.
func (f *File) Comment() string { return f.pick(id3v2GetComment, InfoComment) }

// Year returns the 4-digit year from ID3v2 if present, otherwise
// the leading 4 digits of ICRD (which may be YYYY or YYYY-MM-DD).
func (f *File) Year() int {
	if f.ID3 != nil {
		if y := f.ID3.Year(); y != 0 {
			return y
		}
	}
	s := f.InfoValue(InfoDate)
	if len(s) < 4 {
		return 0
	}
	var y int
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

// TrackNumber returns the (number, total) pair, preferring ID3v2
// over the non-standard ITRK INFO entry.
func (f *File) TrackNumber() (n, total int) {
	if f.ID3 != nil {
		if a, b := f.ID3.TrackNumber(); a != 0 || b != 0 {
			return a, b
		}
	}
	return parseSlashed(f.InfoValue(InfoTrack))
}

// DiscNumber returns the (number, total) pair from ID3v2 only.
// LIST/INFO has no widely-used disc-number key.
func (f *File) DiscNumber() (n, total int) {
	if f.ID3 != nil {
		return f.ID3.DiscNumber()
	}
	return 0, 0
}

// Pictures returns embedded ID3v2 APIC frames. LIST/INFO chunks
// cannot carry images.
func (f *File) Pictures() []*id3v2.PictureFrame {
	if f.ID3 == nil {
		return nil
	}
	return f.ID3.PictureFrames()
}

func (f *File) pick(fromID3 func(*id3v2.Tag) string, infoID string) string {
	if f.ID3 != nil {
		if s := fromID3(f.ID3); s != "" {
			return s
		}
	}
	return f.InfoValue(infoID)
}

// Indirection so the wrappers above stay one-liners.
func id3v2GetTitle(t *id3v2.Tag) string    { return t.Title() }
func id3v2GetArtist(t *id3v2.Tag) string   { return t.Artist() }
func id3v2GetAlbum(t *id3v2.Tag) string    { return t.Album() }
func id3v2GetComposer(t *id3v2.Tag) string { return t.Composer() }
func id3v2GetGenre(t *id3v2.Tag) string    { return t.Genre() }
func id3v2GetComment(t *id3v2.Tag) string  { return t.Comment() }

// parseSlashed parses "n" or "n/total" into the pair (n, total).
// Invalid input returns zeros.
func parseSlashed(s string) (n, total int) {
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, "/", 2)
	n = atoi(parts[0])
	if len(parts) == 2 {
		total = atoi(parts[1])
	}
	return n, total
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

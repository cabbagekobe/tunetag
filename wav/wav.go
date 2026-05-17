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
// are preserved byte-for-byte. WriteFile rewrites the file from
// the in-memory chunk list, so any chunk ordering produced by Read
// is faithfully restored.
//
// 64-bit RIFF (RF64 / BW64) is detected and rejected with
// ErrRF64Unsupported; the spec's 64-bit size table (ds64) is not
// implemented here.
//
// A *File is not safe for concurrent use.
package wav

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
// captured verbatim so they can be re-emitted by WriteFile.
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
	// position is preserved on write; other chunks carry their
	// raw bytes.
	chunks []chunk
}

// InfoItem is one entry inside a LIST/INFO chunk: a 4-character ID
// (e.g. "INAM") and its NUL-terminated value. Trailing NULs are
// stripped on Read; the writer re-pads as required.
type InfoItem struct {
	ID    string // exactly 4 ASCII bytes; consumers should use the Info* constants
	Value string
}

// chunk is one top-level chunk inside the RIFF wrapper. For LIST
// chunks the body holds the bytes after the "INFO" type tag (i.e.
// the concatenated INFO sub-chunks); the kind discriminator lets
// the writer regenerate that layout from File.Info.
type chunk struct {
	id   string // 4 ASCII bytes
	body []byte // raw bytes; for synthetic chunks this is nil
	kind chunkKind
}

type chunkKind uint8

const (
	chunkRaw      chunkKind = iota // verbatim bytes in body
	chunkInfoList                  // LIST/INFO; regenerated from File.Info
	chunkID3v2                     // id3 ; regenerated from File.ID3
)

// Read parses the metadata region (and remembers the full chunk
// layout) of a WAV file from rs. Audio chunks are not decoded;
// their bytes are preserved.
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

	f := &File{}
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
		// Bound the allocation: a chunk that claims to be larger
		// than the remaining file cannot be valid. Without this
		// check a malformed (or hostile) file with size=4 GiB
		// would force a 4 GiB allocation up front before the
		// subsequent ReadFull failed.
		pos, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		if int64(size) > end-pos {
			return nil, fmt.Errorf("wav: chunk %q declared size %d exceeds remaining file (%d bytes)", id, size, end-pos)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(rs, body); err != nil {
			return nil, fmt.Errorf("wav: chunk %q short body (%d bytes): %w", id, size, err)
		}
		// RIFF chunks are word-aligned: an odd size is followed by
		// one pad byte. Skip it if present (EOF mid-pad is fine).
		if size%2 == 1 {
			var pad [1]byte
			if _, err := io.ReadFull(rs, pad[:]); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("wav: read pad after %q: %w", id, err)
			}
		}
		switch {
		case id == ChunkLIST && len(body) >= 4 && string(body[0:4]) == ChunkINFO:
			items, err := parseInfo(body[4:])
			if err != nil {
				return nil, fmt.Errorf("wav: LIST/INFO: %w", err)
			}
			f.Info = append(f.Info, items...)
			f.chunks = append(f.chunks, chunk{id: ChunkLIST, kind: chunkInfoList})
		case id == ChunkID3:
			t, err := id3v2.Read(bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("wav: id3 chunk: %w", err)
			}
			f.ID3 = t
			f.chunks = append(f.chunks, chunk{id: ChunkID3, kind: chunkID3v2})
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
	defer f.Close()
	return Read(f)
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
// caller's audio bytes are preserved; only LIST/INFO and id3
// chunks are regenerated from f.Info and f.ID3 respectively.
//
// When f.Info is empty the LIST chunk is omitted. When f.ID3 is
// nil the id3 chunk is omitted. If either is set but no
// placeholder exists in the original chunk list (e.g. a file that
// gained tags after Read), the missing chunk is appended at the
// end so it sits after the audio data, which is the most
// compatible position for players that pre-buffer "data".
func (f *File) WriteFile(path string) error {
	body, err := f.encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tunetag-wav-*.tmp")
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
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func (f *File) encode() ([]byte, error) {
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
				return nil, fmt.Errorf("wav: encode id3 chunk: %w", err)
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
			return nil, fmt.Errorf("wav: encode id3 chunk: %w", err)
		}
		emitted = append(emitted, chunk{id: ChunkID3, body: buf.Bytes(), kind: chunkRaw})
	}

	// Build payload (everything after RIFF size + WAVE).
	var inner bytes.Buffer
	inner.WriteString(waveType)
	for _, c := range emitted {
		if len(c.id) != 4 {
			return nil, fmt.Errorf("wav: chunk id %q is not 4 bytes", c.id)
		}
		inner.WriteString(c.id)
		_ = binary.Write(&inner, binary.LittleEndian, uint32(len(c.body)))
		inner.Write(c.body)
		if len(c.body)%2 == 1 {
			inner.WriteByte(0)
		}
	}
	// RIFF wrapper: "RIFF" + uint32 size + WAVE-prefixed payload.
	// The size field counts everything after itself.
	if inner.Len() > int(^uint32(0)) {
		return nil, errors.New("wav: encoded body exceeds 4 GiB; RF64 required")
	}
	var out bytes.Buffer
	out.WriteString(chunkRIFF)
	_ = binary.Write(&out, binary.LittleEndian, uint32(inner.Len()))
	out.Write(inner.Bytes())
	return out.Bytes(), nil
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

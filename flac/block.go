package flac

import (
	"errors"
	"fmt"
	"io"
)

// Block types per the FLAC format specification.
const (
	BlockStreamInfo    uint8 = 0
	BlockPadding       uint8 = 1
	BlockApplication   uint8 = 2
	BlockSeekTable     uint8 = 3
	BlockVorbisComment uint8 = 4
	BlockCueSheet      uint8 = 5
	BlockPicture       uint8 = 6
)

// MaxBlockSize is the maximum payload length of a single FLAC
// metadata block (24 bits = 16 MiB - 1).
const MaxBlockSize = 1<<24 - 1

// Block is one FLAC metadata block (excluding the 4-byte header).
// Implementations must round-trip through Encode without loss.
type Block interface {
	// Type is the FLAC block-type byte (low 7 bits of the header).
	Type() uint8
	// Encode returns the block payload (header excluded).
	Encode() ([]byte, error)
}

// RawBlock preserves the original bytes of a block whose payload
// this package does not parse (STREAMINFO, SEEKTABLE, CUESHEET,
// APPLICATION, unknown types). Re-emitting RawBlock is byte-perfect.
type RawBlock struct {
	BlockType uint8
	Body      []byte
}

func (r *RawBlock) Type() uint8             { return r.BlockType }
func (r *RawBlock) Encode() ([]byte, error) { return r.Body, nil }

// PaddingBlock is the explicit zero-padding block. Its size is the
// number of zero bytes to emit, exclusive of the 4-byte header.
type PaddingBlock struct {
	Size int
}

func (p *PaddingBlock) Type() uint8 { return BlockPadding }

func (p *PaddingBlock) Encode() ([]byte, error) {
	if p.Size < 0 {
		return nil, errors.New("flac: padding block has negative size")
	}
	if p.Size > MaxBlockSize {
		return nil, fmt.Errorf("flac: padding block size %d exceeds max %d", p.Size, MaxBlockSize)
	}
	return make([]byte, p.Size), nil
}

// readBlockHeader returns the block type, last-block flag, and the
// 24-bit payload length.
func readBlockHeader(r io.Reader) (blockType uint8, last bool, size uint32, err error) {
	var b [4]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return 0, false, 0, err
	}
	blockType = b[0] & 0x7F
	last = b[0]&0x80 != 0
	size = uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return blockType, last, size, nil
}

func writeBlockHeader(w io.Writer, blockType uint8, last bool, size uint32) error {
	if size > MaxBlockSize {
		return fmt.Errorf("flac: block size %d exceeds 24-bit max", size)
	}
	if blockType > 127 {
		return fmt.Errorf("flac: block type %d > 127", blockType)
	}
	var b [4]byte
	b[0] = blockType & 0x7F
	if last {
		b[0] |= 0x80
	}
	b[1] = byte(size >> 16)
	b[2] = byte(size >> 8)
	b[3] = byte(size)
	_, err := w.Write(b[:])
	return err
}

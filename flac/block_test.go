package flac

import (
	"bytes"
	"testing"
)

func TestBlock_OversizedPaddingRejected(t *testing.T) {
	p := &PaddingBlock{Size: MaxBlockSize + 1}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: padding exceeds 24-bit max")
	}
}

func TestBlock_NegativePaddingRejected(t *testing.T) {
	p := &PaddingBlock{Size: -1}
	if _, err := p.Encode(); err == nil {
		t.Fatal("expected error: negative padding")
	}
}

func TestWriteBlockHeader_TypeOutOfRange(t *testing.T) {
	if err := writeBlockHeader(&bytes.Buffer{}, 200, false, 0); err == nil {
		t.Fatal("expected error: block type > 127")
	}
}

func TestWriteBlockHeader_SizeOutOfRange(t *testing.T) {
	if err := writeBlockHeader(&bytes.Buffer{}, BlockStreamInfo, false, MaxBlockSize+1); err == nil {
		t.Fatal("expected error: size exceeds 24-bit max")
	}
}

func TestWriteBlockHeader_LastFlagSetCorrectly(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBlockHeader(&buf, BlockStreamInfo, true, 34); err != nil {
		t.Fatal(err)
	}
	hdr := buf.Bytes()
	if hdr[0]&0x80 == 0 {
		t.Errorf("last flag not set: % X", hdr)
	}
	if hdr[0]&0x7F != BlockStreamInfo {
		t.Errorf("block type = %d, want %d", hdr[0]&0x7F, BlockStreamInfo)
	}
}

func TestRawBlock_TypeAndEncode(t *testing.T) {
	r := &RawBlock{BlockType: 99, Body: []byte("hello")}
	if r.Type() != 99 {
		t.Errorf("Type = %d", r.Type())
	}
	b, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, r.Body) {
		t.Errorf("Encode mismatch")
	}
}

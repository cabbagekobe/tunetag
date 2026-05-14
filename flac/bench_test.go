package flac

import (
	"bytes"
	"testing"
)

func buildBenchFLAC(numComments int, picSize int) []byte {
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	vc := &VorbisComment{Vendor: "tunetag-bench"}
	for i := 0; i < numComments; i++ {
		vc.Add("TITLE", "Lorem ipsum dolor sit amet")
	}
	pic := &Picture{
		PictureType: 3, MIME: "image/jpeg",
		Description: "cover", Data: make([]byte, picSize),
	}
	pad := &PaddingBlock{Size: 1024}
	f := &File{Blocks: []Block{si, vc, pic, pad}}
	body, err := f.encodeMetadata()
	if err != nil {
		panic(err)
	}
	full := append(Magic[:], body...)
	full = append(full, []byte("audio")...)
	return full
}

func BenchmarkRead_SmallFLAC(b *testing.B) {
	body := buildBenchFLAC(16, 4096)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Read(bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead_LargePicture(b *testing.B) {
	body := buildBenchFLAC(4, 1<<20) // 1 MiB picture
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Read(bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeMetadata_SmallFLAC(b *testing.B) {
	si := &RawBlock{BlockType: BlockStreamInfo, Body: make([]byte, 34)}
	vc := &VorbisComment{Vendor: "v"}
	for i := 0; i < 16; i++ {
		vc.Add("ARTIST", "Bench Artist")
	}
	pad := &PaddingBlock{Size: 1024}
	f := &File{Blocks: []Block{si, vc, pad}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.encodeMetadata(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseVorbisComment(b *testing.B) {
	in := &VorbisComment{Vendor: "tunetag"}
	for i := 0; i < 32; i++ {
		in.Add("TITLE", "Lorem ipsum dolor sit amet")
	}
	body, _ := in.Encode()
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parseVorbisComment(body); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVorbisComment_Get(b *testing.B) {
	vc := &VorbisComment{Vendor: "v"}
	for i := 0; i < 64; i++ {
		vc.Add("ARTIST", "X")
	}
	vc.Add("TITLE", "T")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vc.First("TITLE")
	}
}

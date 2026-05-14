package id3v2

import (
	"bytes"
	"testing"
)

// buildBenchTag returns a tag with N typical frames so each Encode
// pass exercises the text-frame, encoding-selection, and synchsafe
// paths together.
func buildBenchTag(n int, v Version) *Tag {
	tag := &Tag{Version: v, Padding: 0}
	tag.SetTitle("ベンチマークタイトル")
	tag.SetArtist("Benchmark Artist")
	tag.SetAlbum("Bench Album")
	for i := 0; i < n; i++ {
		tag.Frames = append(tag.Frames, &TextFrame{
			FrameID: "TIT2", Encoding: EncUTF8,
			Text: []string{"Lorem ipsum dolor sit amet"},
		})
		tag.Frames = append(tag.Frames, &CommentFrame{
			Encoding: EncUTF8, Language: "eng",
			Description: "desc", Text: "comment payload",
		})
	}
	return tag
}

func BenchmarkEncode_V24_SmallTag(b *testing.B) {
	tag := buildBenchTag(4, V24)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := tag.Encode(&buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode_V23_WithUTF16(b *testing.B) {
	tag := buildBenchTag(8, V23)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := tag.Encode(&buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead_V24_SmallTag(b *testing.B) {
	tag := buildBenchTag(4, V24)
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		b.Fatal(err)
	}
	body := buf.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Read(bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead_WithLargeAPIC(b *testing.B) {
	tag := buildBenchTag(2, V24)
	tag.Frames = append(tag.Frames, &PictureFrame{
		Encoding: EncUTF8, MIME: "image/jpeg",
		PictureType: 3, Data: make([]byte, 1<<20), // 1 MiB
	})
	var buf bytes.Buffer
	if err := tag.Encode(&buf); err != nil {
		b.Fatal(err)
	}
	body := buf.Bytes()
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Read(bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSynchsafe_EncodeDecode(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var v uint32
	for i := 0; i < b.N; i++ {
		v = uint32(i) & MaxSynchsafe
		enc, err := encodeSynchsafe(v)
		if err != nil {
			b.Fatal(err)
		}
		if decodeSynchsafe(enc[:]) != v {
			b.Fatal("mismatch")
		}
	}
}

func BenchmarkUnsync_Encode_1KiB(b *testing.B) {
	data := make([]byte, 1024)
	// Sprinkle 0xFF bytes so the unsync code path actually fires.
	for i := 0; i < len(data); i += 8 {
		data[i] = 0xFF
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = unsyncEncode(data)
	}
}

func BenchmarkUnsync_Decode_1KiB(b *testing.B) {
	src := make([]byte, 1024)
	for i := 0; i < len(src); i += 8 {
		src[i] = 0xFF
	}
	enc := unsyncEncode(src)
	b.SetBytes(int64(len(enc)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = unsyncDecode(enc)
	}
}

func BenchmarkEncodeString_UTF8(b *testing.B) {
	s := "Lorem ipsum dolor sit amet, consectetur adipiscing elit"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encodeString(EncUTF8, s, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeString_UTF16(b *testing.B) {
	s := "Lorem ipsum dolor sit amet, consectetur adipiscing elit 日本語"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encodeString(EncUTF16, s, true); err != nil {
			b.Fatal(err)
		}
	}
}

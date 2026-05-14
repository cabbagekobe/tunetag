package id3v1

import (
	"bytes"
	"testing"
)

func BenchmarkEncode(b *testing.B) {
	tag := &Tag{
		Title:  "Bench Title",
		Artist: "Bench Artist",
		Album:  "Bench Album",
		Year:   "2026",
		Track:  7,
		Genre:  17,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := tag.Encode(&buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRead(b *testing.B) {
	tag := &Tag{
		Title: "T", Artist: "A", Album: "Al", Year: "2026", Track: 1, Genre: 17,
	}
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

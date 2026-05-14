package main

import (
	"fmt"
	"os"

	"github.com/cabbagekobe/tunetag"
)

// detect opens path and returns the auto-detected container format.
func detect(path string) (tunetag.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return tunetag.FormatUnknown, err
	}
	defer f.Close()
	return tunetag.Detect(f)
}

// parseSlash decodes "N" or "N/M" into (n, total).
func parseSlash(s string) (n, total int) {
	for i, c := range s {
		if c == '/' {
			fmt.Sscanf(s[:i], "%d", &n)
			fmt.Sscanf(s[i+1:], "%d", &total)
			return
		}
	}
	fmt.Sscanf(s, "%d", &n)
	return n, 0
}

// guessMIME sniffs the image MIME type from JPEG / PNG magic bytes.
func guessMIME(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && string(data[0:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	}
	return "application/octet-stream"
}

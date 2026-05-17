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

// guessMIME sniffs the image MIME type from common magic bytes,
// falling back to a generic octet-stream string when the bytes
// don't match any recognised image format. Delegates to
// [tunetag.SniffImageMIME] for the actual detection so the CLI
// and the library stay in lockstep.
func guessMIME(data []byte) string {
	if mime := tunetag.SniffImageMIME(data); mime != "" {
		return mime
	}
	return "application/octet-stream"
}

// tunetag is a command-line driver for the tunetag library.
//
// Usage:
//
//	tunetag print  <file>
//	tunetag dump   <file>
//	tunetag set    <file> [--title=...] [--artist=...] [--album=...] [--year=YYYY] [--genre=...] [--track=N[/M]] [--disc=N[/M]]
//	tunetag strip  <file>
//	tunetag cover  <file> (--extract <out>) | (--set <in>)
//
// The set command auto-selects the underlying writer (id3v2 / flac /
// mp4) based on the file's container.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var err error
	switch cmd {
	case "print":
		err = cmdPrint(args)
	case "dump":
		err = cmdDump(args)
	case "set":
		err = cmdSet(args)
	case "strip":
		err = cmdStrip(args)
	case "cover":
		err = cmdCover(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "tunetag: unknown command %q\n", cmd)
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "tunetag: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  tunetag print  <file>")
	fmt.Fprintln(os.Stderr, "  tunetag dump   <file>")
	fmt.Fprintln(os.Stderr, "  tunetag set    <file> [--title=...] [--artist=...] [--album=...] [--year=YYYY] [--genre=...] [--track=N[/M]] [--disc=N[/M]]")
	fmt.Fprintln(os.Stderr, "  tunetag strip  <file>")
	fmt.Fprintln(os.Stderr, "  tunetag cover  <file> (--extract <out>) | (--set <in>)")
	os.Exit(2)
}

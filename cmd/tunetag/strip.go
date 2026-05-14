package main

import (
	"fmt"

	"github.com/cabbagekobe/tunetag"
)

func cmdStrip(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("strip: exactly one file argument required")
	}
	return tunetag.Strip(args[0])
}

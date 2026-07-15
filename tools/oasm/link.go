package main

import (
	"fmt"
	"os"
	"strings"
)

// floppy geometry: 80 cylinders * 2 heads * 18 sectors * 512 bytes = 1.44MiB
const floppySize = 80 * 2 * 18 * 512

func cmdLink(args []string) error {
	var outFile string
	type piece struct {
		path   string
		offset int
	}
	var pieces []piece

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-o" {
			i++
			if i >= len(args) {
				return fmt.Errorf("-o needs an argument")
			}
			outFile = args[i]
			continue
		}
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag %q", a)
		}
		// file.bin@OFFSET  (OFFSET optional, defaults to 0 for the first piece)
		path := a
		off := 0
		if at := strings.LastIndexByte(a, '@'); at >= 0 {
			path = a[:at]
			n, ok := parseNum(a[at+1:])
			if !ok {
				return fmt.Errorf("bad offset in %q", a)
			}
			off = n
		}
		pieces = append(pieces, piece{path: path, offset: off})
	}

	if outFile == "" {
		return fmt.Errorf("link: -o <image.img> is required")
	}
	if len(pieces) == 0 {
		return fmt.Errorf("link: no input binaries")
	}

	image := make([]byte, floppySize)
	for _, p := range pieces {
		data, err := os.ReadFile(p.path)
		if err != nil {
			return err
		}
		if p.offset < 0 || p.offset+len(data) > floppySize {
			return fmt.Errorf("%s: does not fit at offset %d (size %d, image %d)",
				p.path, p.offset, len(data), floppySize)
		}
		copy(image[p.offset:], data)
		c, h, s := offToCHS(p.offset)
		fmt.Printf("  placed %-20s @ 0x%06x (%d bytes)  C=%d H=%d S=%d\n",
			p.path, p.offset, len(data), c, h, s)
	}

	if err := os.WriteFile(outFile, image, 0644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", outFile, floppySize)
	return nil
}

// offToCHS converts a byte offset into cylinder/head/sector (informational).
func offToCHS(off int) (c, h, s int) {
	sector := off / 512
	s = sector % 18
	sector /= 18
	h = sector % 2
	c = sector / 2
	return
}

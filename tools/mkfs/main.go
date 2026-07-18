// mkfs — build an OFS1 filesystem image for OctOS.
//
// OFS1 layout (see playGround/src/kernel.c), all little-endian:
//   superblock (LBA sb)   : "OFS1", entry count (2), next-free data LBA (2)
//   directory  (LBA sb+1) : entries of 16 bytes: name[12], start LBA (2), size (2)
//   data       (LBA sb+2+): each file a contiguous run of 512-byte sectors
//
// The emitted image begins at the superblock (its byte 0 == LBA sb), so the
// caller places it into the floppy at byte offset sb*512 (e.g. oasm link
// fs.img@65536 for sb=128). Directory start-LBAs are absolute, matching how the
// kernel's fs_read resolves them.
//
// Usage:
//   mkfs -o out.img -sb 128 name1=file1 [name2=file2 ...]
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	sectorSize = 512
	direntSize = 16
	nameLen    = 12
	dirMax     = 32
)

func main() {
	out := flag.String("o", "", "output image file")
	sb := flag.Int("sb", 128, "superblock LBA")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mkfs -o out.img -sb 128 name=file [name=file ...]")
		os.Exit(2)
	}
	if flag.NArg() > dirMax {
		fatal(fmt.Sprintf("too many files: %d (max %d)", flag.NArg(), dirMax))
	}

	superblock := make([]byte, sectorSize)
	copy(superblock, "OFS1")
	directory := make([]byte, sectorSize)
	var data []byte

	dataLBA := *sb + 2 // first data sector
	for i, spec := range flag.Args() {
		name, path, ok := strings.Cut(spec, "=")
		if !ok {
			fatal("bad file spec (want name=path): " + spec)
		}
		if len(name) > nameLen {
			fatal(fmt.Sprintf("name %q longer than %d bytes", name, nameLen))
		}
		body, err := os.ReadFile(path)
		if err != nil {
			fatal(err.Error())
		}
		nsect := (len(body) + sectorSize - 1) / sectorSize
		padded := make([]byte, nsect*sectorSize)
		copy(padded, body)
		data = append(data, padded...)

		e := directory[i*direntSize:]
		copy(e[:nameLen], name) // NUL-padded (directory is zero-initialised)
		put16(e[12:], dataLBA)
		put16(e[14:], len(body))
		dataLBA += nsect
	}
	put16(superblock[4:], flag.NArg()) // entry count
	put16(superblock[6:], dataLBA)     // next free data LBA

	img := append(append(superblock, directory...), data...)
	if err := os.WriteFile(*out, img, 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("mkfs: wrote %s (%d files, %d bytes, data LBA %d..%d)\n",
		*out, flag.NArg(), len(img), *sb+2, dataLBA-1)
}

func put16(b []byte, v int) {
	b[0] = byte(v & 0xff)
	b[1] = byte((v >> 8) & 0xff)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "mkfs: "+msg)
	os.Exit(1)
}

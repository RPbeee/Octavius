package main

import (
	"log"
	"math/rand"
	"os"
)

const (
	floppy_CMD  = 0x10 //0x01 READ	0x02 WRITE	0x03 RESET
	floppy_ARG  = 0x11 //MSBは常に1にすること	C,H,S
	floppy_DATA = 0x12
	floppy_STAT = 0x13 //b0:busy	b1:dataready	b2:error	b3:writeprotect
)

var floppy_map [80 * 2 * 18 * 512]uint8
var args []uint //Cilinder[0-79], Head[0-1], Sector[0-17]
var waitTime uint
var headbyte uint

func floppyInit() {
	readImg()
}

func floppyTick() {
	//fmt.Println("args:", len(args))
	if len(args) == 3 && waitTime > 0 {
		waitTime--
	}
	switch ioport[floppy_CMD] {
	case 0x01:
		// READ
		ioport[floppy_STAT] |= 0x01
		if ioport[floppy_ARG] != 0 && len(args) < 2 {
			args = append(args, uint(ioport[floppy_ARG]&0x7f))
		} else if len(args) == 2 {
			waitTime = 5 + uint(rand.Intn(20))
			args = append(args, uint(ioport[floppy_ARG]&0x7f))
		}
		if len(args) == 3 && waitTime == 0 {
			ioport[floppy_STAT] |= 0x02
			ioport[floppy_DATA] = floppy_map[(((args[0]*2+args[1])*18+args[2])*512)+headbyte]
		}
	case 0x02:
		// WRITE
		if ioport[floppy_STAT]&0x08 != 0x0 {
			ioport[floppy_STAT] |= 0x04
		} else {
			ioport[floppy_STAT] |= 0x01
			if ioport[floppy_ARG] != 0 && len(args) < 2 {
				args = append(args, uint(ioport[floppy_ARG]&0x7f))
			} else if len(args) == 2 {
				args = append(args, uint(ioport[floppy_ARG]&0x7f))
				waitTime = 5 + uint(rand.Intn(20))
			}
			if len(args) == 3 && waitTime == 0 {
				ioport[floppy_STAT] |= 0x02
			}
		}
	case 0x03:
		// RESET
		ioport[floppy_STAT] = 0x00
		ioport[floppy_CMD] = 0x00
		args = []uint{}
		headbyte = 0
	}
}

func updateFloppyIO() {
	if len(args) == 3 && waitTime == 0 && headbyte < 511 {
		if ioport[floppy_CMD] == 0x02 {
			floppy_map[(((args[0]*2+args[1])*18+args[2])*512)+headbyte] = ioport[floppy_DATA]
		}
		headbyte++
	} else {
		ioport[floppy_STAT] |= 0x04 //Error
	}
}

func readImg() {
	f, err := os.Open("floppy.img")
	if err != nil {
		log.Println("Failed to open floppy.img. Creating...")
		resetImg()
		f, err = os.Open("floppy.img")
		if err != nil {
			panic("Failed to open floppy.img after creating.")
		}
	}
	defer f.Close()
	image := make([]byte, 80*2*18*512)
	size, err := f.Read(image)
	if err != nil {
		panic(err)
	}
	log.Println("Size:", size)
	if size != 80*2*18*512 {
		panic("File size is not 1.44MiB")
	}
	floppy_map = [80 * 2 * 18 * 512]uint8(image)
}

func saveImg() {
	f, err := os.Create("floppy.img")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	count, err := f.Write(floppy_map[:])
	if err != nil {
		panic(err)
	}
	if count != 80*2*18*512 {
		panic("Image size is not 1.44MiB")
	}
}

func resetImg() {
	f, err := os.Create("floppy.img")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	empty := make([]byte, 80*2*18*512)
	count, err := f.Write(empty[:])
	if err != nil {
		panic(err)
	}
	if count != 80*2*18*512 {
		panic("Image size is not 1.44MiB")
	}
}

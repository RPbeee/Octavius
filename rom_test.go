package main

import "testing"

// TestBootROMReadsFullSector runs the built-in boot ROM against a floppy whose
// sector 0 holds a known pattern and checks that all 512 bytes arrive at
// 0x7c00. Guards the CMP-leaves-CF / INC+JC hazard in the ROM's copy loop
// (havetofix.md #1).
func TestBootROMReadsFullSector(t *testing.T) {
	statsReg = 0
	reg = [12]uint8{}
	mem = [MemSize]uint8{}
	ioport = [256]uint8{}
	halting = false
	args = []uint{}
	waitTime = 0
	headbyte = 0
	reg[cs] = 0xff
	// Re-burn the ROM exactly as reset() does, without touching floppy.img.
	copy(mem[uint(InstLength)*0xff00/0x04:], bootROM)

	for i := range 512 {
		floppy_map[i] = uint8(i % 251) // non-repeating across the 256B halves
	}

	for i := 0; i < 200000; i++ {
		if pc16() == 0x7c00 {
			for j := 0; j < 512; j++ {
				if mem[0x7c00+j] != uint8(j%251) {
					t.Fatalf("mem[0x%04x] = %#02x, want %#02x (copy stopped early)", 0x7c00+j, mem[0x7c00+j], uint8(j%251))
				}
			}
			return
		}
		if !halting {
			tick()
		}
		floppyTick()
		if !halting {
			advancePC()
		}
	}
	t.Fatalf("ROM never jumped to 0x7c00; stuck at %04x", pc16())
}

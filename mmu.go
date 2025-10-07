package main

import "log"

func readMemory(address uint16, length uint8) []uint8 {
	mmuEnabled := false
	if mmuEnabled {
		// MMU ON
	} else {
		// MMU OFF
		if length > 0xff-uint8(address) {
			// over segment hangup (temporary)
			log.Fatal("Offset overflow (readMem)")
		}
		return mem[uint(address>>8)*0x100+uint(address&0xff) : uint(address>>8)*0x100+uint(address&0xff)+uint(length)]
	}
	return []uint8{0x0}
}

func writeMemory(address uint16, data []uint8) {
	mmuEnabled := false
	if mmuEnabled {
		// MMU ON
	} else {
		// MMU OFF
		if len(data) > 0xff-int(address&0xff) {
			// over segment hangup (temporary)
			log.Fatal("Offset overflow (writeMem)")
		}
		copy(mem[uint(address>>8)*0x100+uint(address&0xff):uint(address>>8)*0x100+uint(address&0xff)+uint(len(data))], data)
	}
}

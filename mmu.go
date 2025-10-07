package main

func readMemory(address uint16, length uint8) []uint8 {
	mmuEnabled := true
	if mmuEnabled {
		// MMU ON
	} else {
		// MMU OFF
		return mem[uint(address>>8)*0x100+uint(address&0xff) : uint(address>>8)*0x100+uint(address&0xff)+uint(length)]
	}
	return []uint8{0x0}
}

func writeMemory(address uint16, data []uint8) {
	mmuEnabled := true
	if mmuEnabled {
		// MMU ON
	} else {
		// MMU OFF
		copy(mem[uint(address>>8)*0x100+uint(address&0xff):uint(address>>8)*0x100+uint(address&0xff)+uint(len(data))], data)
	}
}

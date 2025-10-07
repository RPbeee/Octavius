package main

func push(inst []uint8) {
	// PUSH
	switch {
	case inst[1] < 0x0c:
		//reg
		reg[sp]--
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), []uint8{reg[inst[1]]})
	case inst[1] == 0x0c:
		reg[sp]--
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1))
	case inst[1] == 0x0d:
		reg[sp]--
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1))
	case inst[1] == 0x0e:
		reg[sp]--
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1))
	case inst[1] == 0x0f:
		reg[sp]--
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), []uint8{inst[2]})
	}
}

func pop(inst []uint8) {
	// POP
	switch {
	case inst[1] < 0x0c:
		//reg
		reg[inst[1]] = readMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), 1)[0]
		reg[sp]++
	case inst[1] == 0x0c:
		writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), readMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), 1))
		reg[sp]++
	case inst[1] == 0x0d:
		writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), readMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), 1))
		reg[sp]++
	case inst[1] == 0x0e:
		writeMemory(uint16(reg[ds])*0x100+uint16(inst[2]), readMemory(uint16(reg[ss])*0x100+uint16(reg[sp]), 1))
		reg[sp]++
	}
}

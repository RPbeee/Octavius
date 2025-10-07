package main

func push(inst []uint8) {
	// PUSH
	switch {
	case inst[1] < 0x0c:
		//reg
		reg[sp]--
		mem[uint(reg[ss])*0x100+uint(reg[sp])] = reg[inst[1]]
	case inst[1] == 0x0c:
		reg[sp]--
		mem[uint(reg[ss])*0x100+uint(reg[sp])] = mem[uint(reg[ds])*0x100+uint(reg[bx])]
	case inst[1] == 0x0d:
		reg[sp]--
		mem[uint(reg[ss])*0x100+uint(reg[sp])] = mem[uint(reg[ss])*0x100+uint(reg[bp])]
	case inst[1] == 0x0e:
		reg[sp]--
		mem[uint(reg[ss])*0x100+uint(reg[sp])] = mem[uint(reg[ds])*0x100+uint(inst[2])]
	case inst[1] == 0x0f:
		reg[sp]--
		mem[uint(reg[ss])*0x100+uint(reg[sp])] = inst[2]
	}
}

func pop(inst []uint8) {
	// POP
	switch {
	case inst[1] < 0x0c:
		//reg
		reg[inst[1]] = mem[uint(reg[ss])*0x100+uint(reg[sp])]
		reg[sp]++
	case inst[1] == 0x0c:
		mem[uint(reg[ds])*0x100+uint(reg[bx])] = mem[uint(reg[ss])*0x100+uint(reg[sp])]
		reg[sp]++
	case inst[1] == 0x0d:
		mem[uint(reg[ss])*0x100+uint(reg[bp])] = mem[uint(reg[ss])*0x100+uint(reg[sp])]
		reg[sp]++
	case inst[1] == 0x0e:
		mem[uint(reg[ds])*0x100+uint(inst[2])] = mem[uint(reg[ss])*0x100+uint(reg[sp])]
		reg[sp]++
	}
}

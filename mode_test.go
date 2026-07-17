package main

import "testing"

// TestInterruptModeSwitch: a hardware interrupt taken in USER mode must
// dispatch the handler in KERNEL mode, and IRET must drop back to USER mode
// (the mode is carried in bit4 of the pushed flags byte).
func TestInterruptModeSwitch(t *testing.T) {
	statsReg = 0
	reg = [12]uint8{}
	mem = [MemSize]uint8{}
	ioport = [256]uint8{}
	irq = [2]uint64{}
	halting = false
	idtReg, ptReg = 0, 0

	// IRQ1 vector -> handler at 0x90:00
	mem[0x02] = 0x90
	mem[0x03] = 0x00
	// user code at 0x2000: endless NOPs (mem is already zero = NOP)
	// handler at 0x9000: IRET
	mem[0x9000] = 0x22
	// stack
	reg[ss] = 0x30
	reg[sp] = 0x00
	reg[cs] = 0x20
	reg[ip] = 0x00
	statsReg = 0x05 // interrupts on + USER mode

	irq[0] |= 0x02 // raise timer IRQ

	// one cycle: NOP executes, then interrupt() dispatches
	tick()
	interrupt()
	advancePC()

	if statsReg&0x04 != 0 {
		t.Fatalf("after dispatch: statsReg = %#02x, want KERNEL mode (bit2=0)", statsReg)
	}
	if pc16() != 0x9000 {
		t.Fatalf("after dispatch: pc = %#04x, want handler 0x9000", pc16())
	}
	// pushed flags (top of stack) must carry the old mode in bit4
	if mem[uint(reg[ss])*0x100+uint(reg[sp])]&0x10 == 0 {
		t.Fatalf("pushed flags = %#02x, want bit4 (USER) set", mem[uint(reg[ss])*0x100+uint(reg[sp])])
	}

	// execute the IRET
	tick()
	interrupt()
	advancePC()

	if statsReg&0x04 == 0 {
		t.Fatalf("after IRET: statsReg = %#02x, want USER mode restored", statsReg)
	}
	if statsReg&0x01 == 0 {
		t.Fatalf("after IRET: interrupts not re-enabled")
	}
	if reg[flag]&0x10 != 0 {
		t.Fatalf("after IRET: mode bit leaked into flags: %#02x", reg[flag])
	}
	if pc16()>>8 != 0x20 {
		t.Fatalf("after IRET: pc = %#04x, want back in user code at 0x20xx", pc16())
	}
}

// TestUserReadOfKernelPageFaults: with the MMU on, a USER-mode read of a
// page without the User bit must raise exception 0x7e and not return data.
func TestUserReadOfKernelPageFaults(t *testing.T) {
	statsReg = 0
	reg = [12]uint8{}
	mem = [MemSize]uint8{}
	ioport = [256]uint8{}
	irq = [2]uint64{}
	halting = false
	idtReg = 0

	// page table at 0x40/0x41: identity map, all pages kernel except 0x20
	ptReg = 0x40
	for i := 0; i < 256; i++ {
		mem[0x4000+i] = 0x0f
		mem[0x4100+i] = uint8(i)
	}
	mem[0x4000+0x20] = 0x1f // user code page

	// user code at 0x2000: MOV ax, [ds:bx] with ds=0x50 (kernel page)
	reg[ds] = 0x50
	reg[bx] = 0x00
	mem[0x2000] = 0x01 // MOV
	mem[0x2001] = 0x01 // dest ax
	mem[0x2002] = 0x0c // src [ds:bx]
	reg[cs] = 0x20
	statsReg = 0x06 // MMU on + USER mode (interrupts off so nothing dispatches)

	tick()

	if irq[1]&(1<<(0x7e-0x40)) == 0 {
		t.Fatalf("irq[1] = %#x, want permission-violation exception 0x7e raised", irq[1])
	}
}

// TestInterruptFromKernelStaysKernel: the pre-change behavior must be intact —
// dispatch from KERNEL mode returns to KERNEL mode.
func TestInterruptFromKernelStaysKernel(t *testing.T) {
	statsReg = 0
	reg = [12]uint8{}
	mem = [MemSize]uint8{}
	ioport = [256]uint8{}
	irq = [2]uint64{}
	halting = false
	idtReg, ptReg = 0, 0

	mem[0x02] = 0x90
	mem[0x03] = 0x00
	mem[0x9000] = 0x22 // IRET
	reg[ss] = 0x30
	reg[cs] = 0x20
	statsReg = 0x01 // interrupts on, KERNEL

	irq[0] |= 0x02
	tick()
	interrupt()
	advancePC()
	tick() // IRET
	interrupt()
	advancePC()

	if statsReg&0x04 != 0 {
		t.Fatalf("after IRET: statsReg = %#02x, want KERNEL mode kept", statsReg)
	}
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runAsm assembles an oasm source with the real assembler, loads it at
// 0x7c00, and runs the CPU for `steps` iterations with the same per-step
// order as the main loop (timer included). Callers inspect mem/reg after.
func runAsm(t *testing.T, asmSrc string, steps int) {
	t.Helper()
	dir := t.TempDir()
	sfile := filepath.Join(dir, "p.s")
	bfile := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(sfile, []byte(asmSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./tools/oasm", "asm", sfile, "-o", bfile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("oasm: %v\n%s", err, out)
	}
	bin, err := os.ReadFile(bfile)
	if err != nil {
		t.Fatal(err)
	}

	statsReg, reg, mem, ioport = 0, [12]uint8{}, [MemSize]uint8{}, [256]uint8{}
	irq = [2]uint64{}
	idtReg, ptReg = 0, 0
	timerCount = 0
	halting = false
	copy(mem[0x7c00:], bin)
	reg[cs], reg[ip] = 0x7c, 0x00

	for i := 0; i < steps; i++ {
		if !halting {
			tick()
		}
		timerTick()
		interrupt()
		if !halting {
			advancePC()
		}
	}
}

// The timer raises IRQ 1 every `period` ticks once interrupts are enabled
// with STS; the handler counts firings into a VRAM cell.
func TestTimerInterrupt(t *testing.T) {
	const steps = 10000
	const period = 64
	runAsm(t, `
    .org 0x7c00
    ; IDT is at 0x0000 (idtReg=0): HW IRQ 1 vector = seg@0x02, off@0x03
    MOV ds, 0x00
    MOV ax, high(handler)
    MOV [0x02], ax
    MOV ax, low(handler)
    MOV [0x03], ax
    ; a stack for the dispatch pushes
    MOV ss, 0x70
    MOV sp, 0x00
    ; timer period = 64 ticks
    MOV ax, 64
    OUT 0x14, ax
    MOV ax, 0
    OUT 0x15, ax
    STS 1               ; enable interrupts
spin:
    JMP rel spin
handler:
    MOV ds, 0xfb
    MOV ax, [0x00]
    ADD 1
    MOV [0x00], ax      ; VRAM cell 0 = number of timer interrupts
    IRET
`, steps)

	got := int(mem[0xfb00])
	want := steps / period
	if got < want-3 || got > want {
		t.Fatalf("timer interrupts = %d, want about %d", got, want)
	}
}

// HLT must sleep until the timer interrupt wakes the CPU, and IRET must
// resume execution after the HLT.
func TestTimerWakesHLT(t *testing.T) {
	runAsm(t, `
    .org 0x7c00
    MOV ds, 0x00
    MOV ax, high(handler)
    MOV [0x02], ax
    MOV ax, low(handler)
    MOV [0x03], ax
    MOV ss, 0x70
    MOV sp, 0x00
    MOV ax, 32
    OUT 0x14, ax
    MOV ax, 0
    OUT 0x15, ax
    STS 1
    HLT                 ; sleep until the timer fires
    MOV ds, 0xfb        ; woken: leave a marker
    MOV ax, 0x55
    MOV [0x10], ax
end:
    JMP rel end
handler:
    IRET
`, 2000)

	if mem[0xfb10] != 0x55 {
		t.Fatalf("CPU did not resume after HLT (marker=%02x)", mem[0xfb10])
	}
}

// STS is privileged: in user mode it must raise 0x7f instead of executing.
func TestSTSPrivileged(t *testing.T) {
	runAsm(t, `
    .org 0x7c00
    STS 0b100           ; drop to user mode
    STS 0               ; must be refused (still user mode)
spin:
    JMP rel spin
`, 50)

	if statsReg>>2&1 != 1 {
		t.Fatalf("user-mode STS was executed (statsReg=%02x)", statsReg)
	}
	if irq[1]&0x8000000000000000 == 0 {
		t.Fatalf("privileged-instruction interrupt (0x7f) was not raised")
	}
}

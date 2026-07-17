package main

// Programmable interval timer. The 16-bit period (in ticks = executed main
// loop iterations) is set through two I/O ports; every `period` ticks the
// timer raises hardware IRQ 1 (irq[0] bit 1). A period of 0 disables it.
//
// Like the other devices, the timer only *raises* the IRQ line; dispatching
// still requires interrupts to be enabled (statsReg bit 0, see STS/IRET).

const (
	timer_LO = 0x14 // write: period low byte
	timer_HI = 0x15 // write: period high byte
)

var timerCount uint

func timerTick() {
	period := uint(ioport[timer_HI])*0x100 + uint(ioport[timer_LO])
	if period == 0 {
		timerCount = 0
		return
	}
	timerCount++
	if timerCount >= period {
		timerCount = 0
		irq[0] |= 0x02 // hardware IRQ 1
	}
}

// outPort writes an I/O port and runs the device side effects: the floppy
// DATA cursor advances, and writing either timer period byte restarts the
// count (standard PIT behavior — lets the OS re-arm a full time slice).
func outPort(port, val uint8) {
	ioport[port] = val
	switch port {
	case floppy_DATA:
		updateFloppyIO()
	case timer_LO, timer_HI:
		timerCount = 0
	}
}

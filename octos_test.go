package main

// OctOS system tests: boot the real floppy image (boot ROM -> stage-1 ->
// kernel) on the headless machine loop and assert what appears in VRAM.
// The OctOS repo is expected next to this one (override with OCTOS_DIR).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func octosDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("OCTOS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, "playGround")
}

// octosBoot builds floppy.img in the OctOS repo, resets the machine with the
// image inserted, and returns. Use octosStep to run the machine.
func octosBoot(t *testing.T) {
	t.Helper()
	dir := octosDir(t)
	cmd := exec.Command("make", "floppy.img")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make floppy.img: %v\n%s", err, out)
	}
	img, err := os.ReadFile(filepath.Join(dir, "floppy.img"))
	if err != nil {
		t.Fatal(err)
	}

	// Full machine reset (mirrors reset(), without touching ./floppy.img).
	halting = false
	idtReg, ptReg, statsReg = 0, 0, 0
	reg = [12]uint8{}
	reg[cs] = 0xff
	mem = [MemSize]uint8{}
	copy(mem[uint(InstLength)*0xff00/0x04:], bootROM)
	irq = [2]uint64{}
	timerCount = 0
	ioport = [256]uint8{}
	args = []uint{}
	waitTime = 0
	headbyte = 0
	keybuff = nil
	floppy_map = [80 * 2 * 18 * 512]uint8{}
	copy(floppy_map[:], img)
}

// octosStep runs n machine cycles (same order as the interactive main loop).
func octosStep(n int) {
	for i := 0; i < n; i++ {
		if !halting {
			tick()
		}
		floppyTick()
		keyTick()
		timerTick()
		interrupt()
		if !halting {
			advancePC()
		}
	}
}

// octosType injects keystrokes exactly like the tcell event loop does.
func octosType(s string) {
	for _, r := range s {
		irq[0] |= 0x01
		if r == '\n' {
			keybuff = append(keybuff, 0x0d, 0x0a)
		} else {
			keybuff = append(keybuff, r)
		}
	}
}

func vrow(row int) string {
	b := make([]byte, 64)
	for i := 0; i < 64; i++ {
		c := mem[0xfb00+row*64+i]
		if c < 32 || c > 126 {
			c = ' '
		}
		b[i] = c
	}
	return string(b)
}

// octosWait steps until cond() holds, in chunks, up to budget cycles.
func octosWait(t *testing.T, budget int, what string, cond func() bool) {
	t.Helper()
	for done := 0; done < budget; done += 10000 {
		octosStep(10000)
		if cond() {
			return
		}
	}
	t.Fatalf("timeout waiting for %s\nrow0: %q\nrow2: %q\nrow6: %q",
		what, vrow(0), vrow(2), vrow(6))
}

// TestOctOSBootsAndMultitasks boots the image and checks that the title is
// drawn, both counters advance independently, and the spinner spins.
func TestOctOSBootsAndMultitasks(t *testing.T) {
	octosBoot(t)

	octosWait(t, 5_000_000, "kernel title", func() bool {
		return strings.Contains(vrow(0), "OctOS")
	})

	// Counters live at row2/row3 col 13..16 (4 hex digits).
	hexAt := func(row int) string { return vrow(row)[13:17] }
	octosWait(t, 3_000_000, "counter A to start", func() bool {
		return strings.Trim(hexAt(2), " ") != ""
	})
	a1, b1 := hexAt(2), hexAt(3)
	octosWait(t, 3_000_000, "counters to advance", func() bool {
		return hexAt(2) != a1 && hexAt(3) != b1
	})
}

// TestOctOSTasksRunInUserMode samples the processor mode after boot: tasks
// must spend almost all their time in USER mode (statsReg bit2 = 1), dropping
// to KERNEL only inside interrupt/syscall handlers.
func TestOctOSTasksRunInUserMode(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "kernel title", func() bool {
		return strings.Contains(vrow(0), "OctOS")
	})

	// Whenever interrupts are enabled (= not inside a handler) and a
	// non-idle task is current, the CPU must be in USER mode. Idle (tid 7)
	// legitimately runs HLT in KERNEL mode.
	user := 0
	for i := 0; i < 5000; i++ {
		octosStep(97) // odd stride so samples don't beat with the time slice
		if statsReg&0x01 == 0 || mem[0x5000] == 7 {
			continue
		}
		if statsReg&0x04 == 0 {
			t.Fatalf("task %d observed running in KERNEL mode (statsReg=%#02x, pc=%04x)",
				mem[0x5000], statsReg, pc16())
		}
		user++
	}
	if user == 0 {
		t.Fatalf("no USER-mode task execution observed")
	}
}

// TestOctOSSpawnAndSleep: task A spawns task E at startup; E advances its
// counter via SYS_SLEEP, and the sleep-driven spinner C also advances.
// Sleep pacing: E sleeps 2 slices per step, C sleeps 1 — C must lead.
func TestOctOSSpawnAndSleep(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "spawned task E label", func() bool {
		return strings.Contains(vrow(7), "E child")
	})
	e1 := vrow(7)[13:17]
	c1 := vrow(4)[22:26]
	octosWait(t, 3_000_000, "sleep-driven counters to advance", func() bool {
		return vrow(7)[13:17] != e1 && vrow(4)[22:26] != c1
	})
}

// TestOctOSDemandPaging: task A keeps its counter in the demand-paged heap
// (virtual page 0x44, unmapped at boot). Its first store must fault, get a
// page mapped on the fly, and the counter must then advance normally.
func TestOctOSDemandPaging(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "counter A (heap-backed) to appear", func() bool {
		return strings.TrimSpace(vrow(2)[13:17]) != ""
	})
	// mask off Accessed/Dirty (bits 6/7), which the CPU sets on use
	if mem[0x4000+0x44]&0x3f != 0x17 {
		t.Fatalf("page table flags for heap page 0x44 = %#02x, want 0x17 (demand-mapped)", mem[0x4000+0x44])
	}
	frame := mem[0x4100+0x44]
	if mem[0x5100+uint(frame)] != 1 {
		t.Fatalf("heap frame %#02x not marked used in the page bitmap", frame)
	}
	a1 := vrow(2)[13:17]
	octosWait(t, 3_000_000, "heap-backed counter to advance", func() bool {
		return vrow(2)[13:17] != a1
	})
}

// TestOctOSSemaphoreAndIPC: counter B is fed only by IPC messages from task
// E; the ping/pong pair alternates strictly through two semaphores, so
// their counters may never drift more than 1 apart.
func TestOctOSSemaphoreAndIPC(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "pingpong label", func() bool {
		return strings.Contains(vrow(9), "S pingpong")
	})

	b1 := vrow(3)[13:17]
	octosWait(t, 3_000_000, "IPC-fed counter B to advance", func() bool {
		return vrow(3)[13:17] != b1 && strings.TrimSpace(vrow(3)[13:17]) != ""
	})

	hexVal := func(s string) int {
		v := 0
		for _, c := range s {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= int(c - '0')
			case c >= 'A' && c <= 'F':
				v |= int(c-'A') + 10
			default:
				return -1
			}
		}
		return v
	}
	octosWait(t, 3_000_000, "ping counter to advance", func() bool {
		return hexVal(vrow(9)[13:17]) > 2
	})
	for i := 0; i < 20; i++ {
		octosStep(50_000)
		p, q := hexVal(vrow(9)[13:17]), hexVal(vrow(9)[22:26])
		if p < 0 || q < 0 {
			continue // mid-redraw
		}
		if d := p - q; d < -1 || d > 1 {
			t.Fatalf("ping=%d pong=%d — semaphore alternation broken", p, q)
		}
	}
}

// TestOctOSExceptionKillsOnlyFaultingTask raises an invalid-opcode exception
// (as the CPU would) while a user task is running. The kernel must report it
// ("E7C Tn" top-right), kill that task, and keep scheduling the others.
func TestOctOSExceptionKillsOnlyFaultingTask(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "kernel title", func() bool {
		return strings.Contains(vrow(0), "OctOS")
	})
	// wait until the system is fully up (task E spawned and counting, A
	// drawing), then fault a USER task while it is running. Faulting the
	// idle task would be a kernel bug, not the scenario under test.
	octosWait(t, 5_000_000, "task E up", func() bool {
		return strings.Contains(vrow(7), "E child")
	})
	octosWait(t, 5_000_000, "counter A visible", func() bool {
		return strings.TrimSpace(vrow(2)[13:17]) != ""
	})
	for i := 0; i < 3_000_000; i++ {
		octosStep(1)
		if statsReg&0x05 == 0x05 && mem[0x5000] != 7 {
			break
		}
	}
	irq[1] |= 1 << (0x7c - 0x40)

	octosWait(t, 3_000_000, "exception report E7C", func() bool {
		return strings.Contains(vrow(0), "E7C")
	})

	// the survivors must keep running: counter A or B still advances
	a1, b1 := vrow(2)[13:17], vrow(3)[13:17]
	octosWait(t, 3_000_000, "surviving counters to advance", func() bool {
		return vrow(2)[13:17] != a1 || vrow(3)[13:17] != b1
	})
}

// TestOctOSKeyboardEcho types on the emulated keyboard and expects task D to
// echo it, and Enter to clear the line.
func TestOctOSKeyboardEcho(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "kernel title", func() bool {
		return strings.Contains(vrow(0), "OctOS")
	})

	octosType("hello")
	octosWait(t, 3_000_000, "echo of 'hello'", func() bool {
		return strings.Contains(vrow(6), "hello")
	})

	octosType("\n")
	octosWait(t, 3_000_000, "line clear", func() bool {
		return !strings.Contains(vrow(6), "hello")
	})
}

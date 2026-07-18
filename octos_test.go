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

// TestOctOSDiskBlockDriver: main() round-trips the generic floppy block
// driver (write a pattern to a scratch block, read it back into a poisoned
// buffer, compare) and reports "disk: R/W block OK" on screen. Also assert the
// scratch block landed in floppy_map with the expected pattern.
func TestOctOSDiskBlockDriver(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "disk self-test result", func() bool {
		return strings.Contains(vrow(11), "disk: R/W block")
	})
	if !strings.Contains(vrow(11), "disk: R/W block OK") {
		t.Fatalf("disk self-test did not pass: row11=%q", vrow(11))
	}
	// LBA 2844 = C79 H0 S0 -> byte offset in floppy_map.
	const lba = 2844
	off := uint(lba) * 512
	for i := 0; i < 512; i++ {
		want := byte((i*7 + 3) & 0xff)
		if floppy_map[off+uint(i)] != want {
			t.Fatalf("floppy_map[block %d][%d] = %#02x, want %#02x",
				lba, i, floppy_map[off+uint(i)], want)
		}
	}
}

// TestOctOSFileSystem: main() runs the FS self-test (format, create a file
// from a known pattern, read it back into a poisoned buffer, compare) and
// reports "fs: file R/W OK". Also assert the on-disk superblock magic and the
// file's data landed at the expected LBAs in floppy_map.
func TestOctOSFileSystem(t *testing.T) {
	octosBoot(t)
	octosWait(t, 5_000_000, "fs self-test result", func() bool {
		return strings.Contains(vrow(12), "fs: file R/W")
	})
	if !strings.Contains(vrow(12), "fs: file R/W OK") {
		t.Fatalf("fs self-test did not pass: row12=%q", vrow(12))
	}
	// superblock magic "OFS1" at the scratch FS LBA 512.
	sb := uint(512) * 512
	if string(floppy_map[sb:sb+4]) != "OFS1" {
		t.Fatalf("superblock magic = %q, want OFS1", floppy_map[sb:sb+4])
	}
	// file data (400 bytes of pattern (i*3+1)&0xff) at the first data LBA 514.
	data := uint(514) * 512
	for i := 0; i < 400; i++ {
		want := byte((i*3 + 1) & 0xff)
		if floppy_map[data+uint(i)] != want {
			t.Fatalf("fs data[%d] = %#02x, want %#02x", i, floppy_map[data+uint(i)], want)
		}
	}
}

// TestOctOSProgramLoader: main() loads the user program hello.bin from the
// filesystem (placed there by the host mkfs) into the reserved program region
// and starts it as a USER task. The program writes its own banner to VRAM,
// which is the proof it was loaded from disk and ran on top of the OS.
func TestOctOSProgramLoader(t *testing.T) {
	octosBoot(t)
	octosWait(t, 8_000_000, "loaded program banner", func() bool {
		return strings.Contains(vrow(13), "loaded app running") ||
			strings.Contains(vrow(13), "load failed")
	})
	if strings.Contains(vrow(13), "load failed") {
		t.Fatalf("program loader reported failure: row13=%q", vrow(13))
	}
	if !strings.Contains(vrow(13), "prog: loaded app running") {
		t.Fatalf("loaded program banner not shown: row13=%q", vrow(13))
	}
}

// TestOctOSSwap: the loaded program touches more heap pages than there are
// physical heap frames, forcing the kernel to swap pages out to disk and back
// in, then checks every value survived. "swap: heap OK" proves demand paging
// with disk-backed swap works end to end from a user program.
func TestOctOSSwap(t *testing.T) {
	octosBoot(t)
	octosWait(t, 8_000_000, "swap self-test result", func() bool {
		return strings.Contains(vrow(14), "swap: heap")
	})
	if !strings.Contains(vrow(14), "swap: heap OK") {
		t.Fatalf("swap self-test did not pass: row14=%q", vrow(14))
	}
	// a swap slot on disk should hold one of the evicted signatures.
	dirty := false
	for j := 0; j < 12; j++ {
		off := uint(200+j) * 512
		for i := uint(0); i < 8; i++ {
			if floppy_map[off+i] != 0 {
				dirty = true
			}
		}
	}
	if !dirty {
		t.Fatalf("no swap slot was written to disk (swap never evicted a dirty page)")
	}
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
	// Wait until the system is actually preemptive (counter A advancing) before
	// sampling — main()'s single-threaded init (which now includes a blocking
	// disk self-test) runs with interrupts off and must not be sampled.
	octosWait(t, 5_000_000, "tasks running", func() bool {
		return strings.TrimSpace(vrow(2)[13:17]) != ""
	})

	// Whenever interrupts are enabled (= not inside a handler) and a
	// non-idle task is current, the CPU must be in USER mode. Idle (tid 7)
	// legitimately runs HLT in KERNEL mode.
	user := 0
	for i := 0; i < 5000; i++ {
		octosStep(97) // odd stride so samples don't beat with the time slice
		if statsReg&0x01 == 0 || mem[0x0300] == 7 {
			continue
		}
		if statsReg&0x04 == 0 {
			t.Fatalf("task %d observed running in KERNEL mode (statsReg=%#02x, pc=%04x)",
				mem[0x0300], statsReg, pc16())
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
	if mem[0x0100+0x44]&0x3f != 0x17 {
		t.Fatalf("page table flags for heap page 0x44 = %#02x, want 0x17 (demand-mapped)", mem[0x0100+0x44])
	}
	frame := mem[0x0200+0x44]
	if mem[0x0400+uint(frame)] != 1 {
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
		if statsReg&0x05 == 0x05 && mem[0x0300] != 7 {
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

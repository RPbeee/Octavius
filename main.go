package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gdamore/tcell"
)

const InstLength uint8 = 4 //Instruction Length
const MemSize uint = 64 * 1024

const displayWidth = 64
const displayHeight = 16

const (
	ip   = 0x0 // Program Counter
	ax   = 0x1
	bx   = 0x2
	cx   = 0x3
	dx   = 0x4
	bp   = 0x5
	sp   = 0x6
	cs   = 0x7
	ss   = 0x8
	ds   = 0x9
	di   = 0xa
	flag = 0xb
)

var idtReg uint8
var ptReg uint8
var statsReg uint8
var reg [12]uint8
var mem [MemSize]uint8
var ioport [256]uint8

var halting bool
var now time.Time
var freq float64

func main() {
	reset()
	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		log.Fatal(err)
	}
	defer func() {
		screen.Fini()
		if r := recover(); r != nil {
			log.Fatalf("Fatal error: %v", r)
		}
	}()

	// Styles
	styleBorder := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Background(tcell.ColorBlack)
	styleTitle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDarkCyan)
	styleNormal := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	styleLabel := tcell.StyleDefault.Foreground(tcell.ColorLightBlue).Background(tcell.ColorBlack)
	styleValue := tcell.StyleDefault.Foreground(tcell.ColorYellow).Background(tcell.ColorBlack)
	styleDisplay := tcell.StyleDefault.Foreground(tcell.ColorLime).Background(tcell.ColorBlack)

	keyCh := make(chan rune, 10)
	go func() { //keyboard
		for {
			ev := screen.PollEvent()
			switch tev := ev.(type) {
			case *tcell.EventKey:
				if tev.Key() == tcell.KeyEscape {
					close(keyCh)
					//Shutdown
					saveImg()
					screen.Fini() // Ensure terminal is restored on exit
					os.Exit(0)
				} else if r := tev.Rune(); r >= 32 && r <= 127 {
					irq[0] |= 0x01
					keybuff = append(keybuff, r)
				} else if tev.Key() == tcell.KeyEnter {
					irq[0] |= 0x01
					keybuff = append(keybuff, 0x0d, 0x0a)
				} else if r := tev.Rune(); tev.Key() == tcell.KeyRune && r == ' ' {
					irq[0] |= 0x01
					keybuff = append(keybuff, 0x20)
				}
			case *tcell.EventResize:
				screen.Sync()
			}
		}
	}()
	for {
		screen.Clear()
		if ((float64(time.Now().UnixMicro()) - float64(now.UnixMicro())) / 1000000.0) != 0 {
			freq = 1 / ((float64(time.Now().UnixMicro()) - float64(now.UnixMicro())) / 1000000.0)
		}

		width, _ := screen.Size()

		// Layout decision (Side-by-side if terminal is wide enough, stacked otherwise)
		var dispX, dispY, dbgX, dbgY int
		var useLayout bool // true: Side-by-side, false: Stacked
		if width >= 105 {
			useLayout = true
			dbgX = 1
			dbgY = 1
			dispX = 37
			dispY = 1
		} else {
			useLayout = false
			dispX = 1
			dispY = 1
			dbgX = 1
			dbgY = 19
		}

		// 1. Draw Display (64x16 -> 66x18 with border)
		drawBox(screen, dispX, dispY, displayWidth+2, displayHeight+2, "VRAM DISPLAY (64x16)", styleBorder, styleTitle)
		for y := 0; y < displayHeight; y++ {
			for x := 0; x < displayWidth; x++ {
				addr := 0xfb00 + y*displayWidth + x
				ch := ' '
				if addr < len(mem) {
					ch = rune(mem[addr])
					if ch < 32 || ch > 126 {
						ch = '.'
					}
				}
				screen.SetContent(dispX+1+x, dispY+1+y, ch, nil, styleDisplay)
			}
		}

		// 2. Draw Debug Panel
		dbgW := 34
		dbgH := 18
		if !useLayout {
			dbgW = 66
			dbgH = 11
		}

		drawBox(screen, dbgX, dbgY, dbgW, dbgH, "CPU DEBUG STATUS", styleBorder, styleTitle)

		if useLayout {
			// Side-by-Side Layout
			drawString(screen, dbgX+2, dbgY+2, "Time:", styleLabel)
			drawString(screen, dbgX+8, dbgY+2, now.Format("15:04:05"), styleValue)
			drawString(screen, dbgX+18, dbgY+2, "Freq:", styleLabel)
			drawString(screen, dbgX+24, dbgY+2, fmt.Sprintf("%.2fHz", freq), styleValue)

			drawString(screen, dbgX+2, dbgY+4, "Registers:", styleLabel)
			drawString(screen, dbgX+3, dbgY+5, fmt.Sprintf("AX: %02X  BX: %02X  CX: %02X  DX: %02X", reg[ax], reg[bx], reg[cx], reg[dx]), styleValue)
			drawString(screen, dbgX+3, dbgY+6, fmt.Sprintf("SP: %02X  BP: %02X  DI: %02X  FL: %02X", reg[sp], reg[bp], reg[di], reg[flag]), styleValue)
			drawString(screen, dbgX+3, dbgY+7, fmt.Sprintf("CS: %02X  DS: %02X  SS: %02X  IP: %02X", reg[cs], reg[ds], reg[ss], reg[ip]), styleValue)

			physAddr := uint(reg[cs])*0x100 + uint(reg[ip])
			drawString(screen, dbgX+2, dbgY+9, "CS:IP (Phys):", styleLabel)
			drawString(screen, dbgX+16, dbgY+9, fmt.Sprintf("%02X:%02X (%04X)", reg[cs], reg[ip], physAddr), styleValue)

			drawString(screen, dbgX+2, dbgY+11, "Code Dump (CS:IP):", styleLabel)
			baseAddr := uint(reg[cs])*0x100 + uint(reg[ip])
			for i := 0; i < 4; i++ {
				addr := baseAddr + uint(i*4)
				b := make([]byte, 4)
				for j := 0; j < 4; j++ {
					a := addr + uint(j)
					if a < uint(len(mem)) {
						b[j] = mem[a]
					}
				}
				dumpStr := fmt.Sprintf(" %04X: %02X %02X %02X %02X", addr&0xFFFF, b[0], b[1], b[2], b[3])
				drawString(screen, dbgX+2, dbgY+12+i, dumpStr, styleNormal)
			}
		} else {
			// Stacked Layout
			physAddr := uint(reg[cs])*0x100 + uint(reg[ip])
			drawString(screen, dbgX+2, dbgY+2, "Time:", styleLabel)
			drawString(screen, dbgX+8, dbgY+2, now.Format("15:04:05"), styleValue)
			drawString(screen, dbgX+18, dbgY+2, "Freq:", styleLabel)
			drawString(screen, dbgX+24, dbgY+2, fmt.Sprintf("%.2fHz", freq), styleValue)
			drawString(screen, dbgX+38, dbgY+2, "CS:IP:", styleLabel)
			drawString(screen, dbgX+45, dbgY+2, fmt.Sprintf("%02X:%02X (%04X)", reg[cs], reg[ip], physAddr), styleValue)

			drawString(screen, dbgX+2, dbgY+4, fmt.Sprintf("AX:%02X BX:%02X CX:%02X DX:%02X SP:%02X BP:%02X DI:%02X FL:%02X",
				reg[ax], reg[bx], reg[cx], reg[dx], reg[sp], reg[bp], reg[di], reg[flag]), styleValue)
			drawString(screen, dbgX+2, dbgY+5, fmt.Sprintf("CS:%02X DS:%02X SS:%02X IP:%02X",
				reg[cs], reg[ds], reg[ss], reg[ip]), styleValue)

			drawString(screen, dbgX+2, dbgY+7, "Code Dump:", styleLabel)
			baseAddr := uint(reg[cs])*0x100 + uint(reg[ip])
			dumpLine := ""
			for i := 0; i < 4; i++ {
				addr := baseAddr + uint(i*4)
				b := make([]byte, 4)
				for j := 0; j < 4; j++ {
					a := addr + uint(j)
					if a < uint(len(mem)) {
						b[j] = mem[a]
					}
				}
				dumpLine += fmt.Sprintf(" %04X:%02X%02X%02X%02X", addr&0xFFFF, b[0], b[1], b[2], b[3])
			}
			drawString(screen, dbgX+13, dbgY+7, dumpLine, styleNormal)
		}

		screen.Show()
		now = time.Now()

		tick()
		floppyTick()
		keyTick()
		interrupt()

		reg[ip] += InstLength
		time.Sleep(2 * time.Millisecond)
	}
}

func reset() { //Resets all the data
	halting = false
	idtReg = 0x00
	ptReg = 0x00
	statsReg = 0x00
	reg = [12]uint8{}
	reg[cs] = 0xff
	mem = [MemSize]uint8{}
	copy(mem[uint(InstLength)*0xff00/0x04:], []uint8{
		0x01, 0x02, 0x0f, 0x01, //Bootloaderloader
		0x01, 0x01, 0x0f, 0x80, //120byte
		0x01, 0x09, 0x0f, 0x7c,
		0x17, 0x10, 0x02, 0x00,
		0x17, 0x11, 0x01, 0x00, //0C
		0x17, 0x11, 0x01, 0x00, //0H
		0x17, 0x11, 0x01, 0x00, //0S
		0x16, 0x02, 0x13, 0x00, //Stats
		0x0d, 0x02, 0x0f, 0x03, //
		0x0f, 0x08, 0x00, 0x00, //JZ +8
		0x0e, 0x0f, 0xf4, 0x00, //LOOPBACK -12
		0x01, 0x02, 0x0f, 0x00, //BX=0
		0x01, 0x0b, 0x0f, 0x00, //FLAG CLEAR
		0x16, 0x01, 0x12, 0x00, //READ
		0x01, 0x0c, 0x01, 0x00, //MOV AX to mem 0x7c00--
		0x15, 0x02, 0x00, 0x00, //INC BX
		0x18, 0x08, 0x00, 0x00, //JC +8
		0x0e, 0x0f, 0xf0, 0x00, //LOOPBACK -16
		0x15, 0x03, 0x00, 0x00, //INC CX
		0x0d, 0x03, 0x0f, 0x02, //CMP CX == 2
		0x0f, 0x0c, 0x00, 0x00, //JZ +12
		0x15, 0x09, 0x00, 0x00, //INC DS
		0x0e, 0x0f, 0xd8, 0x00, //LOOPBACK -36
		//512B Copy complete
		0x17, 0x10, 0x03, 0x00, //RESET Floppy
		0x01, 0x01, 0x0f, 0x00,
		0x01, 0x02, 0x01, 0x00,
		0x01, 0x03, 0x01, 0x00,
		0x01, 0x09, 0x01, 0x00,
		0x01, 0x0b, 0x01, 0x00, //Clear Registers
		0x0e, 0x2f, 0x7c, 0x00, //Jump to 0x7c00
	})
	irq = [2]uint64{}
	floppyInit()
}

func tick() {
	if isExecutable(reg[cs]) {
		instruction := readMemory(uint16(reg[cs])*0x100+uint16(reg[ip]), InstLength)
		if len(instruction) == int(InstLength) {
			decode(instruction)
			return
		}
	}
	// 実行権限がない、または読み込みに失敗したティックは命令を実行せずに直後のinterrupt()に処理を任せる
}

func decode(inst []uint8) {
	switch inst[0] {
	case 0x0:
		//NOP
	case 0x1:
		// MOV
		switch {
		case inst[1] < 0x0c:
			// to Register
			switch {
			case inst[2] < 0x0c:
				reg[inst[1]] = reg[inst[2]]
			case inst[2] == 0x0c:
				reg[inst[1]] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			case inst[2] == 0x0d:
				reg[inst[1]] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			case inst[2] == 0x0e:
				reg[inst[1]] = readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			case inst[2] == 0x0f:
				reg[inst[1]] = inst[3]
			case 0x10 <= inst[2] && inst[2] < 0x1c:
				if 0x10 <= inst[3] && inst[3] < 0x1c {
					reg[inst[1]] = readMemory(uint16(reg[inst[2]-0x10])*0x100+uint16(reg[inst[3]-0x10]), 1)[0]
				}
			case 0x20 <= inst[2] && inst[2] < 0x2c:
				reg[inst[1]] = readMemory(uint16(reg[inst[2]-0x20])*0x100+uint16(inst[3]), 1)[0]
			}
		case inst[1] == 0x0c:
			// to MEM
			switch {
			case inst[2] < 0x0c:
				// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = reg[inst[2]]
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{reg[inst[2]]})
			case inst[2] == 0x0f:
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{inst[3]})
			}
		case inst[1] == 0x0d:
			// to MEM (stack)
			switch {
			case inst[2] < 0x0c:
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{reg[inst[2]]})
			case inst[2] == 0x0f:
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{inst[3]})
			}
		case inst[1] == 0x0e:
			// to MEM (immediate address)
			switch {
			case inst[2] < 0x0c:
				writeMemory(uint16(reg[ds])*0x100+uint16(inst[3]), []uint8{reg[inst[2]]})
			}
		case 0x10 <= inst[1] && inst[1] < 0x1c:
			//mem[segreg:addrreg]
			switch {
			case 0x10 <= inst[2] && inst[2] < 0x1c:
				if inst[3] < 0x0c {
					writeMemory(uint16(reg[inst[1]-0x10])*0x100+uint16(reg[inst[2]-0x10]), []uint8{reg[inst[3]]})
				}
			}
		case 0x20 <= inst[1] && inst[1] < 0x2c:
			writeMemory(uint16(reg[inst[1]-0x20])*0x100+uint16(inst[3]), []uint8{reg[inst[2]]})
		}
	case 0x2:
		// ADD
		switch {
		case inst[1] < 0x0c:
			// add Register
			if uint(reg[ax])+uint(reg[inst[1]]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += reg[inst[1]]
		case inst[1] == 0x0c:
			// add MEM
			if uint(reg[ax])+uint(readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
		case inst[1] == 0x0d:
			// add MEM (stack)
			if uint(reg[ax])+uint(readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
		case inst[1] == 0x0e:
			// add MEM (immediate address)
			if uint(reg[ax])+uint(readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]
		case inst[1] == 0x0f:
			// add immd
			if uint(reg[ax])+uint(inst[2]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += inst[2]
		case inst[1] == 0xf0:
			if uint(reg[ax])+uint(readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0]) > 255 {
				reg[flag] |= 0b10
			}
			reg[ax] += readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0]
		}
		if reg[ax] == 0 {
			reg[flag] |= 0b1
		}
	case 0x3:
		// NOT
		switch {
		case inst[1] < 0x0c:
			//register
			reg[inst[1]] = ^reg[inst[1]]
		case inst[1] == 0x0c:
			//ds* 0x100+bx
			// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = ^mem[uint16(reg[ds])*0x100+uint16(reg[bx])]
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{^readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]})
		case inst[1] == 0x0d:
			//ss* 0x100+bp
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{^readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]})
		case inst[1] == 0x0e:
			//immd
			// mem[uint16(reg[ds])*0x100+uint16(inst[2])] = ^mem[uint16(reg[ds])*0x100+uint16(inst[2])]
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[2]), []uint8{^readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]})
		case inst[1] == 0xf0:
			//mem[immd]
			// mem[uint16(inst[2])*0x100+uint16(inst[3])] = ^mem[uint16(inst[2])*0x100+uint16(inst[3])]
			writeMemory(uint16(inst[2])*0x100+uint16(inst[3]), []uint8{^readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0]})
		}
	case 0x4:
		// OR
		switch {
		case inst[1] < 0x0c:
			//register
			switch {
			case inst[2] < 0x0c:
				//register
				reg[inst[1]] = reg[inst[1]] | reg[inst[2]]
			case inst[2] == 0x0c:
				//ds* 0x100+bx
				reg[inst[1]] = reg[inst[1]] | readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			case inst[2] == 0x0d:
				//ss* 0x100+bp
				reg[inst[1]] = reg[inst[1]] | readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			case inst[2] == 0x0e:
				//immd addr
				reg[inst[1]] = reg[inst[1]] | readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			case inst[2] == 0x0f:
				//immd
				reg[inst[1]] = reg[inst[1]] | inst[3]
			}
		case inst[1] == 0x0c:
			//ds* 0x100+bx
			switch {
			case inst[2] < 0x0c:
				//register
				// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = mem[uint16(reg[ds])*0x100+uint16(reg[bx])] | reg[inst[2]]
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] | reg[inst[2]]})
			case inst[2] == 0x0f:
				//immd
				// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = mem[uint16(reg[ds])*0x100+uint16(reg[bx])] | inst[3]
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] | inst[3]})
			}
		case inst[1] == 0x0d:
			//ss* 0x100+bp
			switch {
			case inst[2] < 0x0c:
				//register
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] | reg[inst[2]]})
			case inst[2] == 0x0f:
				//immd
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] | inst[3]})
			}
		}
	case 0x5:
		// AND
		switch {
		case inst[1] < 0x0c:
			//register
			switch {
			case inst[2] < 0x0c:
				reg[inst[1]] = reg[inst[1]] & reg[inst[2]]
			case inst[2] == 0x0c:
				reg[inst[1]] = reg[inst[1]] & readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			case inst[2] == 0x0d:
				reg[inst[1]] = reg[inst[1]] & readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			case inst[2] == 0x0e:
				reg[inst[1]] = reg[inst[1]] & readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			case inst[2] == 0x0f:
				reg[inst[1]] = reg[inst[1]] & inst[3]
			}
		case inst[1] == 0x0c:
			switch {
			case inst[2] < 0x0c:
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] & reg[inst[2]]})
			case inst[2] == 0x0f:
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] & inst[3]})
			}
		case inst[1] == 0x0d:
			switch {
			case inst[2] < 0x0c:
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] & reg[inst[2]]})
			case inst[2] == 0x0f:
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] & inst[3]})
			}
		}
	case 0x6:
		// XOR
		switch {
		case inst[1] < 0x0c:
			switch {
			case inst[2] < 0x0c:
				reg[inst[1]] = reg[inst[1]] ^ reg[inst[2]]
			case inst[2] == 0x0c:
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			case inst[2] == 0x0d:
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			case inst[2] == 0x0e:
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			case inst[2] == 0x0f:
				reg[inst[1]] = reg[inst[1]] ^ inst[3]
			}
		case inst[1] == 0x0c:
			switch {
			case inst[2] < 0x0c:
			case inst[2] < 0x0c:
				//register
				reg[inst[1]] = reg[inst[1]] ^ reg[inst[2]]
			case inst[2] == 0x0c:
				//ds* 0x100+bx
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			case inst[2] == 0x0d:
				//ss* 0x100+bp
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			case inst[2] == 0x0e:
				//immd addr
				reg[inst[1]] = reg[inst[1]] ^ readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			case inst[2] == 0x0f:
				//immd
				reg[inst[1]] = reg[inst[1]] ^ inst[3]
			}
		case inst[1] == 0x0c:
			//ds* 0x100+bx
			switch {
			case inst[2] < 0x0c:
				//register
				// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = mem[uint16(reg[ds])*0x100+uint16(reg[bx])] ^ reg[inst[2]]
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] ^ reg[inst[2]]})
			case inst[2] == 0x0f:
				//immd
				// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = mem[uint16(reg[ds])*0x100+uint16(reg[bx])] ^ inst[3]
				writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] ^ inst[3]})
			}
		case inst[1] == 0x0d:
			//ss* 0x100+bp
			switch {
			case inst[2] < 0x0c:
				//register
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] ^ reg[inst[2]]})
			case inst[2] == 0x0f:
				//immd
				writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] ^ inst[3]})
			}
		}
	case 0x7:
		// SHL
		switch {
		case inst[1] < 0x0c:
			//register
			if (reg[inst[1]]<<inst[2]-1)>>7&1 == 1 {
				//Carry
				reg[flag] |= 0b10
			}
			reg[inst[1]] = reg[inst[1]] << inst[2]
		case inst[1] == 0x0c:
			//ds* 0x100+bx
			if (readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]<<inst[2]-1)>>7&1 == 1 {
				//Carry
				reg[flag] |= 0b10
			}
			// mem[uint16(reg[ds])*0x100+uint16(reg[bx])] = mem[uint16(reg[ds])*0x100+uint16(reg[bx])] << inst[2]
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] << inst[2]})
		case inst[1] == 0x0d:
			//ss* 0x100+bp
			if (readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]<<inst[2]-1)>>7&1 == 1 {
				//Carry
				reg[flag] |= 0b10
			}
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] << inst[2]})
		case inst[1] == 0x0e:
			//immd
			if (readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]<<inst[2]-1)>>7&1 == 1 {
				//Carry
				reg[flag] |= 0b10
			}
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[3]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0] << inst[2]})
		}
	case 0x8:
		// SHR
		switch {
		case inst[1] < 0x0c:
			//register
			reg[inst[1]] = reg[inst[1]] >> inst[2]
		case inst[1] == 0x0c:
			//ds* 0x100+bx
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] >> inst[2]})
		case inst[1] == 0x0d:
			//ss* 0x100+bp
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] >> inst[2]})
		case inst[1] == 0x0e:
			//immd
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[3]), []uint8{readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0] >> inst[2]})
		}
	case 0x9:
		// ROL
		switch {
		case inst[1] < 0x0c:
			tmp := reg[inst[1]]
			reg[inst[1]] <<= (inst[2] % 8)
			reg[inst[1]] |= (tmp >> (8 - inst[2]%8))
		case inst[1] == 0x0c:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{(tmp << (inst[2] % 8)) | (tmp >> (8 - inst[2]%8))})
		case inst[1] == 0x0d:
			tmp := readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{(tmp << (inst[2] % 8)) | (tmp >> (8 - inst[2]%8))})
		case inst[1] == 0x0e:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[3]), []uint8{(tmp << (inst[2] % 8)) | (tmp >> (8 - inst[2]%8))})
		}
	case 0xa:
		// ROR
		switch {
		case inst[1] < 0x0c:
			tmp := reg[inst[1]]
			reg[inst[1]] >>= (inst[2] % 8)
			reg[inst[1]] |= (tmp << (8 - inst[2]%8))
		case inst[1] == 0x0c:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{(tmp >> (inst[2] % 8)) | (tmp << (8 - inst[2]%8))})
		case inst[1] == 0x0d:
			tmp := readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{(tmp >> (inst[2] % 8)) | (tmp << (8 - inst[2]%8))})
		case inst[1] == 0x0e:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[3]), []uint8{(tmp >> (inst[2] % 8)) | (tmp << (8 - inst[2]%8))})
		}
	case 0xb:
		push(inst)
	case 0xc:
		pop(inst)
	case 0xd:
		// CMP
		switch {
		case inst[1] < 0x0c:
			switch {
			case inst[2] < 0x0c:
				if reg[inst[1]] == reg[inst[2]] {
					reg[flag] |= 0x1
				} else {
					reg[flag] &= 0xfe
				}
				if reg[inst[1]] < reg[inst[2]] {
					reg[flag] |= 0x2
				} else {
					reg[flag] &= 0xfd
				}
				if (reg[inst[1]]-reg[inst[2]])&0x80 != 0 {
					reg[flag] |= 0x8
				} else {
					reg[flag] &= 0xf7
				}
				if reg[inst[1]]&0x80 != reg[inst[2]]&0x80 && (reg[inst[1]]-reg[inst[2]])&0x80 != reg[inst[1]]&0x80 {
					reg[flag] |= 0x4
				} else {
					reg[flag] &= 0xfb
				}
			case inst[2] == 0x0c:
				if reg[inst[1]] == readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] {
					reg[flag] |= 0x1
				} else {
					reg[flag] &= 0xfe
				}
				if reg[inst[1]] < readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] {
					reg[flag] |= 0x2
				} else {
					reg[flag] &= 0xfd
				}
				if (reg[inst[1]]-readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0])&0x80 != 0 {
					reg[flag] |= 0x8
				} else {
					reg[flag] &= 0xf7
				}
				if reg[inst[1]]&0x80 != readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]&0x80 && (reg[inst[1]]-readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0])&0x80 != reg[inst[1]]&0x80 {
					reg[flag] |= 0x4
				} else {
					reg[flag] &= 0xfb
				}
			case inst[2] == 0x0d:
				if reg[inst[1]] == readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] {
					reg[flag] |= 0x1
				} else {
					reg[flag] &= 0xfe
				}
				if reg[inst[1]] < readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] {
					reg[flag] |= 0x2
				} else {
					reg[flag] &= 0xfd
				}
				if (reg[inst[1]]-readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0])&0x80 != 0 {
					reg[flag] |= 0x8
				} else {
					reg[flag] &= 0xf7
				}
				if reg[inst[1]]&0x80 != readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]&0x80 && (reg[inst[1]]-readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0])&0x80 != reg[inst[1]]&0x80 {
					reg[flag] |= 0x4
				} else {
					reg[flag] &= 0xfb
				}
			case inst[2] == 0x0e:
				if reg[inst[1]] == readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0] {
					reg[flag] |= 0x1
				} else {
					reg[flag] &= 0xfe
				}
				if reg[inst[1]] < readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0] {
					reg[flag] |= 0x2
				} else {
					reg[flag] &= 0xfd
				}
				if (reg[inst[1]]-readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0])&0x80 != 0 {
					reg[flag] |= 0x8
				} else {
					reg[flag] &= 0xf7
				}
				if reg[inst[1]]&0x80 != readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0]&0x80 && (reg[inst[1]]-readMemory(uint16(reg[ds])*0x100+uint16(inst[3]), 1)[0])&0x80 != reg[inst[1]]&0x80 {
					reg[flag] |= 0x4
				} else {
					reg[flag] &= 0xfb
				}
			case inst[2] == 0x0f:
				if reg[inst[1]] == inst[3] {
					reg[flag] |= 0x1
				} else {
					reg[flag] &= 0xfe
				}
				if reg[inst[1]] < inst[3] {
					reg[flag] |= 0x2
				} else {
					reg[flag] &= 0xfd
				}
				if (reg[inst[1]]-inst[3])&0x80 != 0 {
					reg[flag] |= 0x8
				} else {
					reg[flag] &= 0xf7
				}
				if reg[inst[1]]&0x80 != inst[3]&0x80 && (reg[inst[1]]-inst[3])&0x80 != reg[inst[1]]&0x80 {
					reg[flag] |= 0x4
				} else {
					reg[flag] &= 0xfb
				}
			}
		case inst[1] == 0x0c:
			// mem-immd
			if readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] == inst[2] {
				reg[flag] |= 0x1
			} else {
				reg[flag] &= 0xfe
			}
			if readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] < inst[2] {
				reg[flag] |= 0x2
			} else {
				reg[flag] &= 0xfd
			}
			if (readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]-inst[2])&0x80 != 0 {
				reg[flag] |= 0x8
			} else {
				reg[flag] &= 0xf7
			}
			if readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]&0x80 != inst[2]&0x80 && (readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]-inst[2])&0x80 != readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]&0x80 {
				reg[flag] |= 0x4
			} else {
				reg[flag] &= 0xfb
			}
		case inst[1] == 0x0d:
			// mem-immd
			if readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] == inst[2] {
				reg[flag] |= 0x1
			} else {
				reg[flag] &= 0xfe
			}
			if readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] < inst[2] {
				reg[flag] |= 0x2
			} else {
				reg[flag] &= 0xfd
			}
			if (readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]-inst[2])&0x80 != 0 {
				reg[flag] |= 0x8
			} else {
				reg[flag] &= 0xf7
			}
			if readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]&0x80 != inst[2]&0x80 && (readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]-inst[2])&0x80 != readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]&0x80 {
				reg[flag] |= 0x4
			} else {
				reg[flag] &= 0xfb
			}
		case inst[1] == 0x0e:
			// mem(immd1)-immd2
			if readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0] == inst[3] {
				reg[flag] |= 0x1
			} else {
				reg[flag] &= 0xfe
			}
			if readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0] < inst[3] {
				reg[flag] |= 0x2
			} else {
				reg[flag] &= 0xfd
			}
			if (readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]-inst[3])&0x80 != 0 {
				reg[flag] |= 0x8
			} else {
				reg[flag] &= 0xf7
			}
			if readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]&0x80 != inst[3]&0x80 && (readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]-inst[3])&0x80 != readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]&0x80 {
				reg[flag] |= 0x4
			} else {
				reg[flag] &= 0xfb
			}
		}
	case 0xe:
		// JMP
		switch {
		case inst[1] < 0x0c:
			reg[ip] = reg[inst[1]] - 4
		case inst[1] == 0x0f:
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[2]))) - 4
		case inst[1] == 0x1f:
			reg[ip] = inst[2] - 4
		case inst[1] == 0x2f:
			reg[cs] = inst[2]
			reg[ip] = inst[3] - 4
		case (inst[1] & 0x0f) == 0x0c:
			if inst[1]&0xf0 != 0 {
				reg[cs] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx])+1, 1)[0]
			}
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] - 4
		case (inst[1] & 0x0f) == 0x0d:
			if inst[1]&0xf0 != 0 {
				reg[cs] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp])+1, 1)[0]
			}
			reg[ip] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] - 4
		case (inst[1] & 0x0f) == 0x0e:
			if inst[1]&0xf0 != 0 {
				reg[cs] = readMemory(uint16(reg[ds])*0x100+uint16(inst[2])+1, 1)[0]
			}
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0] - 4
		case inst[1] == 0xf0:
			reg[ip] = readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0] - 4
		case inst[1] == 0xf1:
			reg[cs] = readMemory(uint16(inst[2])*0x100+uint16(inst[3])+1, 1)[0]
			reg[ip] = readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0] - 4
		}
	case 0xf:
		// JZ
		if reg[flag]&0x01 != 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x10:
		// JNZ
		if reg[flag]&0x01 == 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x11:
		// JA
		if reg[flag]&0b11 == 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x12:
		// JBE
		if reg[flag]&0b11 != 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x13:
		// JG
		if reg[flag]&0b1 == 0 && reg[flag]&0b1000>>3 == reg[flag]&0b100>>2 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x14:
		// JLE
		if reg[flag]&0b1 != 0 || reg[flag]&0b1000>>3 != reg[flag]&0b100>>2 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x15:
		// INC
		switch {
		case inst[1] < 0x0c:
			reg[inst[1]]++
			if reg[inst[1]] == 0 {
				//CF
				reg[flag] |= 0b10
			}
		case inst[1] == 0x0c:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] + 1
			writeMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), []uint8{tmp})
			if tmp == 0 {
				//CF
				reg[flag] |= 0b10
			}
		case inst[1] == 0x0d:
			tmp := readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] + 1
			writeMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), []uint8{tmp})
			if tmp == 0 {
				//CF
				reg[flag] |= 0b10
			}
		case inst[1] == 0x0e:
			tmp := readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0] + 1
			writeMemory(uint16(reg[ds])*0x100+uint16(inst[2]), []uint8{tmp})
			if tmp == 0 {
				//CF
				reg[flag] |= 0b10
			}
		}
	case 0x16:
		// IN
		if statsReg>>2&1 == 0 {
			if inst[1] < 0x0c {
				if inst[2] < 0x0c {
					reg[inst[1]] = ioport[reg[inst[2]]]
					switch reg[inst[2]] {
					case floppy_DATA:
						updateFloppyIO()
					case key_DATA:
						updateKeys()
					}
				} else {
					reg[inst[1]] = ioport[inst[2]]
					switch inst[2] {
					case floppy_DATA:
						updateFloppyIO()
					case key_DATA:
						updateKeys()
					}
				}
			}
		} else {
			// not Privileged
			// INTERRUPT
			// 0x7f
			irq[1] |= 0x8000000000000000
		}
	case 0x17:
		// OUT
		if statsReg>>2&1 == 0 {
			if inst[1] < 0x0c {
				if inst[2] < 0x0c {
					ioport[reg[inst[1]]] = reg[inst[2]]
					if reg[inst[1]] == floppy_DATA {
						updateFloppyIO()
					}
				}
			} else {
				if inst[2] < 0x0c {
					ioport[inst[1]] = reg[inst[2]]
					if inst[1] == floppy_DATA {
						updateFloppyIO()
					}
				}
			}
		} else {
			// not Privileged
			// INTERRUPT
			// 0x7f
			irq[1] |= 0x8000000000000000
		}
	case 0x18:
		//JC
		if reg[flag]&0x02 != 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x19:
		//JNC
		if reg[flag]&0x02 == 0 {
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[1]))) - 4
		}
	case 0x20:
		//CALL
		switch {
		case inst[1] < 0x0c:
			//near register
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = reg[inst[1]]
		case 0x10 <= inst[1] && inst[1] < 0x1c:
			//far register
			if 0x10 <= inst[2] && inst[2] < 0x1c {
				push([]uint8{0x00, 0x00, 0x00, 0x00})
				push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
				reg[ip] = reg[inst[2]-0x10]
				reg[cs] = reg[inst[1]-0x10]
			}
		case inst[1] == 0x0c:
			//near	mem	ds
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0] - 4
		case inst[1] == 0x1c:
			//far	mem	ds
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx])+4, 1)[0] - 4
			reg[cs] = readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
		case inst[1] == 0x0d:
			//near  mem ss
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0] - 4
		case inst[1] == 0x1d:
			//far	mem ss
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
			reg[ip] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp])+4, 1)[0] - 4
			reg[cs] = readMemory(uint16(reg[ss])*0x100+uint16(reg[bp]), 1)[0]
		case inst[1] == 0x0e:
			//near	mem imm
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0] - 4
		case inst[1] == 0x1e:
			//far	mem imm
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
			reg[ip] = readMemory(uint16(reg[ds])*0x100+uint16(inst[2])+4, 1)[0] - 4
			reg[cs] = readMemory(uint16(reg[ds])*0x100+uint16(inst[2]), 1)[0]
		case inst[1] == 0x0f:
			//short	imm
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = uint8(int(reg[ip])+int(int8(inst[2]))) - 4
		case inst[1] == 0x1f:
			//near	imm
			push([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip)の値-4,0x00,0x00
			reg[ip] = inst[2] - 4
		case inst[1] == 0x2f:
			//far 	imm
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
			reg[ip] = inst[3] - 4
			reg[cs] = inst[2]
		case inst[1] == 0xf0:
			//near	memimm2
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			reg[ip] = readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0] - 4
		case inst[1] == 0xf1:
			//far 	memimm2
			push([]uint8{0x00, 0x00, 0x00, 0x00})
			push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
			reg[ip] = readMemory(uint16(inst[2])*0x100+uint16(inst[3])+4, 1)[0] - 4
			reg[cs] = readMemory(uint16(inst[2])*0x100+uint16(inst[3]), 1)[0]
		}
	case 0x21:
		//RET
		switch inst[1] {
		case 0x00:
			//near 	return
			pop([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip),0x00,0x00
		case 0x01:
			//far 	return
			pop([]uint8{0x00, 0x07, 0x00, 0x00}) //inst=>0x00,0x07(cs),0x00,0x00
			pop([]uint8{0x00, 0x00, 0x00, 0x00}) //inst=>0x00,0x00(ip),0x00,0x00
		}
	case 0x22:
		//IRET
		pop([]uint8{0x00, 0x0b, 0x00, 0x00}) //POP flags
		pop([]uint8{0x00, 0x07, 0x00, 0x00}) //POP cs
		pop([]uint8{0x00, 0x00, 0x00, 0x00}) //POP ip
	case 0x23:
		//LST
		if statsReg>>2&1 == 0 {
			if inst[2] == 0x0c {
				addr := readMemory(uint16(reg[ds])*0x100+uint16(reg[bx]), 1)[0]
				switch inst[1] {
				case 0x00:
					// Page Table
					ptReg = addr
				case 0x01:
					// Interruption Description Table
					idtReg = addr
				}
			}
		} else {
			// not Privileged
			// INTERRUPT
			// 0x7f
			irq[1] |= 0x8000000000000000
		}
	case 0x24:
		//SYSCALL
		//INTERRUPT 0x70
		irq[1] |= 0x1000000000000
		//
	case 0xff:
		//HLT
		if statsReg>>2&1 == 0 {
			//
		} else {
			// NOT PRIVILEGED
			// 0x7f
			irq[1] |= 0x8000000000000000
		}
	default:
		// 無効な命令(オペランドが無効な場合はまだ定義できてない。)
		//INTERRUPT 0x7c
		irq[1] |= 0x1000000000000000
		//
	}
}

func drawString(screen tcell.Screen, x, y int, str string, style tcell.Style) {
	for i, r := range str {
		screen.SetContent(x+i, y, r, nil, style)
	}
}

func drawBox(screen tcell.Screen, x, y, w, h int, title string, borderStyle, titleStyle tcell.Style) {
	// 角
	screen.SetContent(x, y, '┌', nil, borderStyle)
	screen.SetContent(x+w-1, y, '┐', nil, borderStyle)
	screen.SetContent(x, y+h-1, '└', nil, borderStyle)
	screen.SetContent(x+w-1, y+h-1, '┘', nil, borderStyle)

	// 横線
	for col := x + 1; col < x+w-1; col++ {
		screen.SetContent(col, y, '─', nil, borderStyle)
		screen.SetContent(col, y+h-1, '─', nil, borderStyle)
	}
	// 縦線
	for row := y + 1; row < y+h-1; row++ {
		screen.SetContent(row, x, '│', nil, borderStyle)
		screen.SetContent(row, x+w-1, '│', nil, borderStyle)
	}

	// タイトル
	if title != "" {
		drawString(screen, x+2, y, " "+title+" ", titleStyle)
	}
}

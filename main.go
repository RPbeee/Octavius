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
		log.Fatal(nil)
	}
	defer screen.Fini()
	width, _ := screen.Size()
	leftWidth := width / 2

	debugStyle := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGreen)
	displayStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)

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

		debugLines := []string{
			fmt.Sprintln("時刻:", now.Format("15:04:05")),
			fmt.Sprintf("現在の実参照アドレス:%x\n", (uint(reg[cs])*0x100 + uint(reg[ip]))),
			fmt.Sprintf("レジスタ:%b\n", reg),
			fmt.Sprintln("メモリ:", mem[uint16(reg[cs])*0x100:uint(reg[cs])*0x100+256]),
			fmt.Sprintln("VRAM:", mem[uint16(0xfb)*0x100+0:uint(0xfb)*0x100+256]),
			fmt.Sprintln("CPU freq (Hz):", freq),
		}
		for y, line := range debugLines {
			for x, ch := range line {
				if x >= leftWidth {
					break
				}
				screen.SetContent(x, y, ch, nil, debugStyle)
			}
		}
		for y := 0; y < displayHeight; y++ {
			for x := 0; x < displayWidth; x++ {
				addr := 0xfb00 + y*displayWidth + x
				ch := ' '
				if addr < len(mem) {
					ch = rune(mem[addr])
					if ch < 32 {
						ch = '.'
					}
				}
				screen.SetContent(x+leftWidth, y, ch, nil, displayStyle)
			}
		}
		screen.Show()
		now = time.Now()

		tick()
		floppyTick()
		keyTick()
		interrupt()

		reg[ip] += InstLength
		time.Sleep(200 * time.Millisecond)
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
	copy(mem[:uint(InstLength)*0x2], []uint8{
		0x01, 0x00, //keyboard interrupt vector 0x0100
	})
	copy(mem[0x0100:0x0100+256], []uint8{
		0x0b, 0x01, 0x00, 0x00,
		0x0b, 0x02, 0x00, 0x00,
		0x0b, 0x03, 0x00, 0x00,
		0x0b, 0x04, 0x00, 0x00,
		0x0b, 0x05, 0x00, 0x00,
		0x0b, 0x06, 0x00, 0x00,
		0x0b, 0x08, 0x00, 0x00,
		0x0b, 0x09, 0x00, 0x00,
		0x0b, 0x0a, 0x00, 0x00,
		0x01, 0x01, 0x0f, 0x00,
		0x01, 0x02, 0x0f, 0x00,
		0x01, 0x03, 0x0f, 0x00,
		0x01, 0x04, 0x0f, 0x00,
		0x01, 0x05, 0x0f, 0x00,
		0x01, 0x09, 0x0f, 0x00,
		0x01, 0x0a, 0x0f, 0x00,
		0x16, 0x01, 0x0d, 0x00, //0
		0x01, 0x09, 0x0f, 0xfb, //4
		0x01, 0x02, 0x0e, 0xff, //8
		0x16, 0x03, 0x0c, 0x00, //c
		0x01, 0x0c, 0x03, 0x00, //10
		0x02, 0x0f, 0xff, 0x00, //14
		0x15, 0x02, 0x00, 0x00, //18
		0x0f, 0x08, 0x00, 0x00, //1c
		0x0e, 0x0f, 0xec, 0x00, //20
		0x01, 0x0e, 0x02, 0xff, //24
		0x01, 0x0b, 0x0f, 0x00, //28
		0x0c, 0x0a, 0x00, 0x00,
		0x0c, 0x09, 0x00, 0x00,
		0x0c, 0x08, 0x00, 0x00,
		0x0c, 0x06, 0x00, 0x00,
		0x0c, 0x05, 0x00, 0x00,
		0x0c, 0x04, 0x00, 0x00,
		0x0c, 0x03, 0x00, 0x00,
		0x0c, 0x02, 0x00, 0x00,
		0x0c, 0x01, 0x00, 0x00,
		0x22, 0x00, 0x00, 0x00, //IRET
	})
	/*copy(mem[0xff00:], []uint8{
		0x01, 0x08, 0x0f, 0x40,
		0x01, 0x0b, 0x0f, 0x10, //Allow interrupt
		0x0e, 0x0f, 0xf8, 0x00,
	})*/
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
	/*copy(mem[uint16(InstLength)*0xff00/0x04:], []uint8{
		0x01, 0x0a, 0x0f, 0x02,	//floppy write test
		0x01, 0x04, 0x0f, 0x80,
		0x17, 0x10, 0x0a, 0x00,
		0x17, 0x11, 0x04, 0x00,
		0x17, 0x11, 0x04, 0x00,
		0x17, 0x11, 0x04, 0x00,
		0x16, 0x03, 0x13, 0x00, //loop start
		0x0d, 0x03, 0x0f, 0x03, //read stats
		0x0f, 0x08, 0x00, 0x00, //jumpout
		0x0e, 0x0f, 0xf4, 0x00, //loop end
		0x01, 0x0b, 0x0f, 0x00,
		0x01, 0x0a, 0x0f, 0xff,
		0x17, 0x12, 0x0a, 0x00, //write
		0x15, 0x01, 0x00, 0x00,
		0x18, 0x08, 0x00, 0x00,
		0x0e, 0x0f, 0xf4, 0x00, //loopback
		0xff, 0x00, 0x00, 0x00,
	})*/
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
			if reg[inst[1]]+1 == 0 {
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

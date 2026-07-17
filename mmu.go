package main

func isExecutable(page uint8) bool {
	if statsReg>>1&1 == 1 { //mmu is enabled
		pageFlag := mem[uint(ptReg)*0x100+uint(page)]
		if pageFlag&1 == 1 {
			mem[uint(ptReg)*0x100+uint(page)] |= 0x40 //access flag
			return pageFlag>>3&1 == 1                 //isExecutable
		} else {
			//not valid
			// 0x7d 未マップページ
			ioport[faultPage_PORT] = page
			irq[1] |= 0x2000000000000000
			//
		}
	}
	return true //mmu無効のときは全部許可
}

func readMemory(address uint16, length uint8) []uint8 {
	if uint16(length) > 0x100-address&0xff {
		// over segment hangup (temporary)
		panic("Offset overflow (readMem)")
	}
	if statsReg>>1&1 == 1 { //mmu is enabled
		// MMU ON
		pageFlag := mem[uint(ptReg)*0x100+uint(address>>8)]
		pageAddr := mem[uint(ptReg+1)*0x100+uint(address>>8)]
		if pageFlag&1 == 1 {
			// Valid
			mem[uint(ptReg)*0x100+uint(address>>8)] |= 0x40 //access flag
			if pageFlag>>1&1 == 1 {                         //isReadable
				if pageFlag>>4&1 >= statsReg>>2&1 { //isPrivileged
					return mem[uint(pageAddr)*0x100+uint(address&0xff) : uint(pageAddr)*0x100+uint(address&0xff)+uint(length)]
				} else {
					//not privileged
					//INTERRUPT
					// 0x7e
					ioport[faultPage_PORT] = uint8(address >> 8)
					irq[1] |= 0x4000000000000000
					//割り込みと通常処理が順番な都合
					reg[ip] -= InstLength
				}
			} else {
				//not Readable
				//INTERRUPT
				// 0x7e
				ioport[faultPage_PORT] = uint8(address >> 8)
				irq[1] |= 0x4000000000000000
				//割り込みと通常処理が順番な都合
				reg[ip] -= InstLength
			}
		} else {
			// Invalid
			// INTERRUPT
			// 0x7d
			ioport[faultPage_PORT] = uint8(address >> 8)
			irq[1] |= 0x2000000000000000
			//割り込みと通常処理が順番な都合
			reg[ip] -= InstLength
		}
	} else {
		// MMU OFF
		return mem[uint(address>>8)*0x100+uint(address&0xff) : uint(address>>8)*0x100+uint(address&0xff)+uint(length)]
	}
	// フォルト時はゼロ埋めを返す。ip は巻き戻し済みなので命令はハンドラ後に
	// 再実行される (空スライスを返すと decode 側の [0] 参照で Go が panic する)。
	return make([]uint8, length)
}

func writeMemory(address uint16, data []uint8) {
	if uint16(len(data)) > 0x100-address&0xff {
		// over segment hangup (temporary)
		panic("Offset overflow (writeMem)")
	}
	if statsReg>>1&1 == 1 {
		// MMU ON
		pageFlag := mem[uint(ptReg)*0x100+uint(address>>8)]
		pageAddr := mem[uint(ptReg+1)*0x100+uint(address>>8)]
		if pageFlag&1 == 1 {
			// Valid
			mem[uint(ptReg)*0x100+uint(address>>8)] |= 0x40 //access flag
			if pageFlag>>2&1 == 1 {                         //isWritable
				if pageFlag>>4&1 >= statsReg>>2&1 { //isPrivileged
					mem[uint(ptReg)*0x100+uint(address>>8)] |= 0x80 //dirty flag
					copy(mem[uint(pageAddr)*0x100+uint(address&0xff):uint(pageAddr)*0x100+uint(address&0xff)+uint(len(data))], data)
				} else {
					//not privileged
					//INTERRUPT
					// 0x7e
					ioport[faultPage_PORT] = uint8(address >> 8)
					irq[1] |= 0x4000000000000000
					//割り込みと通常処理が順番な都合
					reg[ip] -= InstLength
				}
			} else {
				//not Writable
				//INTERRUPT
				// 0x7e
				ioport[faultPage_PORT] = uint8(address >> 8)
				irq[1] |= 0x4000000000000000
				//割り込みと通常処理が順番な都合
				reg[ip] -= InstLength
			}
		} else {
			// Invalid
			// INTERRUPT
			// 0x7d
			ioport[faultPage_PORT] = uint8(address >> 8)
			irq[1] |= 0x2000000000000000
			//割り込みと通常処理が順番な都合
			reg[ip] -= InstLength
		}
	} else {
		// MMU OFF
		copy(mem[uint(address>>8)*0x100+uint(address&0xff):uint(address>>8)*0x100+uint(address&0xff)+uint(len(data))], data)
	}
}

// faultPage_PORT: reading I/O port 0x16 yields the virtual page number of
// the most recent MMU fault (0x7d/0x7e) — the OS page-fault handler's "CR2".
const faultPage_PORT = 0x16

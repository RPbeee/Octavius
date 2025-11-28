package main

import "log"

func isExecutable(page uint8) bool {
	if statsReg>>1&1 == 1 { //mmu is enabled
		pageFlag := mem[uint(ptReg)*0x100+uint(page)]
		if pageFlag&1 == 1 {
			mem[uint(ptReg)*0x100+uint(page)] |= 0x40 //access flag
			return pageFlag>>3&1 == 1                 //isExecutable
		} else {
			//not valid
		}
	}
	return true //mmu無効のときは全部許可
}

func readMemory(address uint16, length uint8) []uint8 {
	if length > 0xff-uint8(address) {
		// over segment hangup (temporary)
		log.Fatal("Offset overflow (readMem)")
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
				}
			} else {
				//not Readable
				//INTERRUPT
			}
		} else {
			// Invalid
			// INTERRUPT
		}
	} else {
		// MMU OFF
		return mem[uint(address>>8)*0x100+uint(address&0xff) : uint(address>>8)*0x100+uint(address&0xff)+uint(length)]
	}
	return []uint8{0x0}
}

func writeMemory(address uint16, data []uint8) {
	if len(data) > 0xff-int(address&0xff) {
		// over segment hangup (temporary)
		log.Fatal("Offset overflow (writeMem)")
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
				}
			} else {
				//not Writable
				//INTERRUPT
			}
		} else {
			// Invalid
			// INTERRUPT
		}
	} else {
		// MMU OFF
		copy(mem[uint(address>>8)*0x100+uint(address&0xff):uint(address>>8)*0x100+uint(address&0xff)+uint(len(data))], data)
	}
}

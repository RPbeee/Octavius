package main

import (
	"math"
	"math/bits"
)

var irq [2]uint64

func interrupt() { // こいつのメモリアクセスは割込みベクタだから物理アドレス参照。
	if (irq[0] != 0 || irq[1] != 0) && reg[flag]>>4&1 == 1 {
		//Interrupt
		push([]uint8{0x00, 0x00, 0x00, 0x00}) //PUSH ip
		push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
		push([]uint8{0x00, 0x0b, 0x00, 0x00}) //PUSH flags
		if irq[0] != 0 {
			reg[cs] = mem[2*bits.TrailingZeros64(irq[0])]
			reg[ip] = mem[2*bits.TrailingZeros64(irq[0])+1] - 4
			irq[0] &= uint64(^uint(math.Pow(2.0, float64(bits.TrailingZeros64(irq[0])))))
			reg[flag] &= ^uint8(16)
		} else {
			reg[cs] = mem[2*(bits.TrailingZeros64(irq[1])+64)]
			reg[ip] = mem[2*(bits.TrailingZeros64(irq[1])+64)+1] - 4
			irq[1] &= uint64(^uint(math.Pow(2.0, float64(bits.TrailingZeros64(irq[1])))))
			reg[flag] &= ^uint8(16)
		}
	}
}

package main

import (
	"math"
	"math/bits"
)

var irq [2]uint64

func interrupt() { // こいつのメモリアクセスは割込みベクタだから物理アドレス参照。
	if (irq[0] != 0 || irq[1] != 0) && statsReg&1 == 1 {
		//Interrupt
		halting = false // HLTで停止中でも割り込みを受けたら実行を再開する
		// 現在のプロセッサモード (statsReg bit2) を flags の bit4 に保存して
		// カーネルモードへ遷移する。IRET が bit4 からモードを復元する。
		// (flags bit4-7 は未使用領域。従来コードは flags=0 を積むので互換)
		reg[flag] = reg[flag]&0x0f | (statsReg>>2&1)<<4
		push([]uint8{0x00, 0x00, 0x00, 0x00}) //PUSH ip
		push([]uint8{0x00, 0x07, 0x00, 0x00}) //PUSH cs
		push([]uint8{0x00, 0x0b, 0x00, 0x00}) //PUSH flags
		statsReg &= ^uint8(0x04) // enter KERNEL mode
		// ソフトウェア割り込み (SYSCALL・例外) は実行中の命令に同期しているので、
		// ハードウェア割り込みより先にディスパッチする。逆順だと、同じサイクルで
		// タイマーが先に取られてタスク切替が起き、syscall/フォルトが別タスクの
		// 文脈で処理される競合が起きる。
		if irq[1] != 0 {
			// (従来は cs:ip を直接セットしていたため、末尾の advancePC() で
			//  ベクタ先+4に着地するバグがあった。setNext で正確に着地させる)
			addr := readMemory(uint16(idtReg)+uint16(2*bits.TrailingZeros64(irq[1])+64), 2)
			setNext(uint16(addr[0])*0x100 + uint16(addr[1]))
			irq[1] &= uint64(^uint(math.Pow(2.0, float64(bits.TrailingZeros64(irq[1])))))
			statsReg &= ^uint8(1)
		} else {
			addr := readMemory(uint16(idtReg)+uint16(2*bits.TrailingZeros64(irq[0])), 2)
			setNext(uint16(addr[0])*0x100 + uint16(addr[1]))
			irq[0] &= uint64(^uint(math.Pow(2.0, float64(bits.TrailingZeros64(irq[0])))))
			statsReg &= ^uint8(1)
		}
	}
}

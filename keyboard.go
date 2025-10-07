package main

var keybuff []rune

const (
	key_DATA   = 0x0c
	key_BUFFER = 0x0d
)

func keyTick() {
	if len(keybuff) != 0 {
		ioport[key_DATA] = uint8(keybuff[0])
		ioport[key_BUFFER] = uint8(len(keybuff))
	} else {
		ioport[key_DATA] = 0
		ioport[key_BUFFER] = 0
		irq[0] &= 0xfffffffffffffffe
	}
}

func updateKeys() {
	if len(keybuff) > 0 {
		keybuff = append([]rune{}, keybuff[1:]...)
	}
}

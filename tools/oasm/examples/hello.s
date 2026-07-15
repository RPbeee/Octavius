; hello.s — minimal bootable program for the Octavius emulator.
;
; The ROM loader reads sector 0 (the first 512 bytes of floppy.img) to
; physical 0x7c00 (CS=0x7c) and jumps there. So boot code is org'd at 0x7c00.
;
; Build:
;   oasm asm  hello.s -o hello.bin
;   oasm link -o floppy.img hello.bin@0
;   (run the emulator; it loads floppy.img)

    .org 0x7c00

VRAM = 0xfb              ; video text RAM segment (0xfb00..0xfeff, 64x16)

    MOV ds, VRAM         ; point ds at the text framebuffer
    MOV ax, 'H'
    MOV [ds:0], ax
    MOV ax, 'e'
    MOV [ds:1], ax
    MOV ax, 'l'
    MOV [ds:2], ax
    MOV ax, 'l'
    MOV [ds:3], ax
    MOV ax, 'o'
    MOV [ds:4], ax
    MOV ax, '!'
    MOV [ds:5], ax
done:
    HLT

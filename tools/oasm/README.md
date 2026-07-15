# oasm — Octavius Assembler & Linker

An Intel-syntax assembler and image linker for the Octavius CPU emulator.
Every instruction assembles to **exactly 4 bytes**: `[opcode, op1, op2, op3]`.

## Build

```sh
go build -o oasm ./tools/oasm
```

## Usage

```sh
# assemble source -> flat binary
oasm asm  input.s [-o output.bin] [-org N]

# combine binaries into a 1.44MiB bootable floppy image
oasm link -o image.img file.bin@OFFSET [file.bin@OFFSET ...]
```

- `-org N` sets the base address used for label arithmetic (default `0`).
  A `.org` directive in the source does the same and overrides it.
- In `link`, `@OFFSET` is a **byte** offset into the image. The ROM loader
  boots **sector 0** (the first 512 bytes), loading it to `0x7c00`, so a boot
  program goes at `@0`. Sector *S* of cylinder *C*, head *H* is at
  `((C*2 + H)*18 + S) * 512`.

### Quick start

```sh
oasm asm  tools/oasm/examples/hello.s -o hello.bin
oasm link -o floppy.img hello.bin@0
# then run the emulator (it reads floppy.img)
```

## Syntax

- One instruction per line, Intel order: `MNEMONIC dest, src`.
- Comments start with `;`.
- Labels: `name:` (may share a line with an instruction).
- Numbers: `0x1f` (hex), `0b1010` (binary), `42` (decimal), `'A'` (char,
  supports `\n \r \t \0`).
- Constants: `NAME = VALUE`.
- Current address: `$`, with `$+N` / `$-N`.

### Directives

| Directive        | Meaning                                             |
| ---------------- | --------------------------------------------------- |
| `.org N`         | set assembly origin (affects label values)          |
| `db a, b, "str"` | emit raw bytes; strings are inlined byte-for-byte   |
| `NAME = VALUE`   | define a constant                                   |

Symbol helpers usable in expressions: `low(x)`/`off(x)` (low byte),
`high(x)`/`seg(x)` (high byte).

### Registers

`ip ax bx cx dx bp sp cs ss ds di flag`

### Operand forms

| Written              | Meaning                                    |
| -------------------- | ------------------------------------------ |
| `ax`                 | register                                   |
| `42`, `0x2a`, `'A'`  | immediate                                  |
| `[bx]`               | memory at `ds:bx`                          |
| `[bp]`               | memory at `ss:bp`                          |
| `[0x10]`             | memory at `ds:0x10` (8-bit offset)         |
| `[ds:bx]`            | memory at `<seg reg>:<addr reg>`           |
| `[ds:0x10]`          | memory at `<seg reg>:<imm offset>`         |
| `abs[0x7c00]`        | absolute 16-bit memory `mem[0x7c00]`       |
| `far ...`            | far variant of a jump/memory operand       |

### Instruction set

```
NOP
MOV  dest, src
ADD  src                 ; ax += src
NOT  src
OR   a, b                ; a |= b   (same for AND, XOR)
SHL  src, count          ; also SHR, ROL, ROR
PUSH src        POP dest
CMP  a, b
INC  src
IN   reg, port           ; port is a register or immediate (>=0x0c)
OUT  port, reg
CALL target      RET [0|1]      ; RET 1 = far
JMP  target                     ; see jump forms
JZ JNZ JA JBE JG JLE JC JNC  target   ; conditional, relative
IRET      SYSCALL      HLT
LST 0|1, [bx]            ; 0=page table, 1=IDT
```

### Jump / call targets

| Form                | Encoding / meaning                               |
| ------------------- | ------------------------------------------------ |
| `JMP ax`            | near, address = register                         |
| `JMP label`         | near, absolute offset (same segment)             |
| `JMP rel label`     | near, PC-relative (`±127`, position-independent) |
| `JMP far 0x7c, 0`   | far, `cs=0x7c, ip=0`                             |
| `JMP far ds, bx`    | far, from register pair                          |
| `JMP [bx]`          | indirect via `ds:bx` (`far [bx]` adds segment)   |
| `JMP [0x10]`        | indirect via `ds:0x10`                           |
| `JMP abs[0x0500]`   | indirect via absolute `mem[0x0500]`              |

Conditional jumps are always relative: `JZ label` (to a label) or a raw
displacement like `JZ rel $+8`.

## Example

`examples/hello.s` prints `Hello!` to the video text RAM and halts. The
assembler is validated against the emulator's own hard-coded bootloader:
re-assembling it produces byte-identical output.

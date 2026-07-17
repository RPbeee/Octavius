# Octavius CPU 仕様書

自作8bit CPUエミュレータ「Octavius」の命令セット・メモリマップ・割り込み仕様のリファレンス。
実装 (`main.go` / `mmu.go` / `interrupt.go` / `instructions.go` / `keyboard.go` / `floppy.go`) から
起こした「今どう動いているか」の記録。

フロッピー/ブートローダ周りの詳細な挙動・タイミング・既知の罠は [`floppy.md`](./floppy.md) を参照。

## Overview

- **8bit CPU**、命令長は固定 **4バイト** (`opcode`, `operand1`, `operand2`, `operand3`)。
- アドレス空間は64KB (`MemSize = 64*1024`) だが、レジスタは全て8bitのため
  **セグメント:オフセット方式**でアクセスする (`物理アドレス = Segment*0x100 + Offset`)。
- 汎用レジスタ12本 + システムレジスタ3本 (IDTR / PTR / StatsReg)。
- 電源投入時は物理アドレス `0xff00` (`CS:IP = 0xff:0x00`) から実行開始。ブートROMの
  実例は [`floppy.md` §4](./floppy.md#4-組み込みブートローダ-rom正典の実例) 参照。

## 目次

- [MemMap](#memmap)
- [Registers](#registers)
- [Flags](#flags)
- [Status Register](#status-register)
- [MMU](#mmu)
- [Interruptions](#interruptions)
- [Instructions](#instructions)
- [Notes / Known Quirks](#notes--known-quirks)

## MemMap

| 領域 | 用途 |
| :-- | :-- |
| `0x0000`–`0x00ff` | Interrupt vector (default; `idtReg=0x00` 時) |
| `0x0100`–`0x7bff` | Free |
| `0x7c00`–`0x7dff` | Bootsector loading point |
| `0x7e00`–`0xfaff` | Free |
| `0xfb00`–`0xfeff` | Video Text RAM (64x16 = 1024Byte) |
| `0xff00`–`0xffff` | 組み込みブートROM (`reset()` が焼き込む) |

## Registers

```js
ip:   0x0 // Program Counter
ax:   0x1
bx:   0x2
cx:   0x3
dx:   0x4
bp:   0x5
sp:   0x6
cs:   0x7
ss:   0x8
ds:   0x9
di:   0xa
flag: 0xb
```

### Other Registers

| レジスタ | 意味 |
| :-- | :-- |
| IDTR (`idtReg`) | 割り込みベクタ位置レジスタ。MMU ONの時は仮想セグメントとして扱われる |
| StatsReg (`statsReg`) | ステータス・レジスタ。[Status Register](#status-register) 参照 |
| PTR (`ptReg`) | ページテーブル位置レジスタ。常に物理セグメントを指す |

## Flags

`reg[flag]` (0x0b) のビット構成。**`CMP` 以外は基本的にフラグを立てるだけで、クリアはしない**
(詳細は [Notes](#notes--known-quirks) 参照)。

```js
0:    Zero      (ZF)
1:    Carry     (CF)
2:    Overflow  (OF)
3:    Sign      (SF)
4~7:  NULL
```

## Status Register

`statsReg` のビット構成。リセット直後は `0x00` (割り込み無効・MMU無効・特権モード)。
書き込みは `STS` 命令 (`0x25`、👑特権) で行う。割り込みディスパッチ時に bit0 は
自動クリアされ、`IRET` が自動で再セットする。

**プロセッサモードの自動遷移**: 割り込みディスパッチ時、現在のモード (bit2) が
push される flags の **bit4** に保存され、CPU は KERNEL モードに遷移する (bit2 クリア)。
`IRET` は pop した flags の bit4 から bit2 を復元し、bit4 は flags から除去される。
新規タスクの初期スタックに `flags = 0x10` を積んで IRET すれば USER モードで開始できる。

```js
0:    Interrupt OK        (1=割り込み有効)
1:    MMU ON/OFF          (1=ON)
2:    Processor_MODE      (0:KERNEL/1:USER)
3~7:  NULL
```

## MMU

MMU有効時 (`statsReg` bit1 = 1) は `ptReg` を先頭とする2セグメントでページ変換を行う。
無効時は物理アドレスがそのまま使われる (全アクセス許可)。

### `[ptReg]` セグメント: フラグテーブル

仮想ページ番号 (アドレス上位8bit) をインデックスとする1バイト/エントリのテーブル。

|ビット|名前|0の時|1の時|
|-|-|-|-|
|0|Valid|物理メモリ上にマップされていない|マップされている|
|1|Read|読み込み不可|読み込み可能|
|2|Write|書き込み不可|書き込み可能|
|3|Execute|実行不可|実行可能|
|4|User|カーネル領域(特権が必要)|ユーザー領域|
|5|NULL|||
|6|Accessed|最近アクセスされていない|直近でアクセスされた(自動セット)|
|7|Dirty|データ未更新|書き込みで自動セットされる|

- 特権チェックは `flag>>4 >= statsReg>>2` (ページがKERNEL領域かつ現在USERモードなら
  違反、それ以外は許可)。
- Read/Write/Executeいずれも不許可時、および未マップ(Valid=0)時は例外を送出する
  ([Interruptions](#interruptions) の `0x7d`/`0x7e` 参照)。

### `[ptReg+1]` セグメント: アドレステーブル (物理ページフレームテーブル)

同じ仮想ページ番号をインデックスとする1バイト/エントリのテーブル。各バイトは
対応する **物理ページ番号 (0x00–0xff)** を保持する。

```
物理アドレス = AddrTable[仮想アドレス >> 8] * 0x100 + (仮想アドレス & 0xff)
```

## Interruptions

割り込みは2本の64bitビットマップで管理される: `irq[0]` = ハードウェア割り込み、
`irq[1]` = ソフトウェア割り込み/例外。統一された割り込みID (`0x00`–`0x7f`) との対応は:

- ハードウェア (`irq[0]` のビット *n*) → ID = `n`
- ソフトウェア (`irq[1]` のビット *n*) → ID = `0x40 + n`

`statsReg` bit0 (Interrupt OK) が1のときのみディスパッチされる。**ソフトウェア割り込み
(`irq[1]`: SYSCALL・例外) は実行中の命令に同期しているため、ハードウェア割り込みより
優先してディスパッチされる**。各ビットマップ内では最下位ビット優先
(`bits.TrailingZeros64`)。ディスパッチ時は `IP` → `CS` → `Flags` の順でスタックに
pushしてからハンドラへジャンプし (`setNext` 経由でベクタ先に正確に着地する)、
`statsReg` bit0 をクリアする。MMU フォルト (0x7d/0x7e) は `ip` を1命令ぶん巻き戻して
からディスパッチされるので、ハンドラがページをマップして `IRET` すればフォルトした
命令が再実行される (デマンドページングの土台。フォルトページ番号はポート `0x16`)。

|割り込みID (0x00~0x7f)|割り込み内容|発生元|
|-|-|-|
|`0x00`|キーボード入力|`keyboard.go` (キー入力時に `irq[0]` bit0)|
|`0x01`|タイマー|`timer.go` (ポート `0x14/0x15` で設定した周期ごとに `irq[0]` bit1)|
|`0x70`|システムコール|`SYSCALL` 命令 (`0x24`)|
|`0x7b`|ゼロ除算|*(除算命令未実装のため現状発火しない)*|
|`0x7c`|無効な命令|未定義オペコードをデコードした場合|
|`0x7d`|未マップページ|MMU: ページがValid=0|
|`0x7e`|メモリ権限違反|MMU: Read/Write/Privilege違反|
|`0x7f`|不正な特権命令|👑命令をUSERモードで実行した場合|

### Timer

`timer.go` のプログラマブル・インターバル・タイマー。ポート `0x14` (下位) /
`0x15` (上位) に16bit周期を書くと、その tick 数 (メインループ1周 = 1命令 = 1 tick)
ごとに IRQ `0x01` を上げる。周期 0 で停止 (リセット直後は停止)。**どちらかの周期
ポートに書き込むとカウントが 0 にリセットされる** (実PIT同様の再アーム。OSは
タスク切替時に書き込むことで、選んだタスクにフルタイムスライスを保証できる —
これをしないとスリープ系syscallの連鎖がフリーランのタイマーと位相ロックして
特定タスクが飢餓する)。他のデバイスと
同じく IRQ線を上げるだけなので、実際のディスパッチには `STS 1` で割り込みを
有効化しておく必要がある。典型的な使い方:

```asm
    MOV ds, 0x00            ; IDT (idtReg=0): IRQ1 のベクタは seg@0x02, off@0x03
    MOV ax, high(handler)
    MOV [0x02], ax
    MOV ax, low(handler)
    MOV [0x03], ax
    MOV ax, 64
    OUT 0x14, ax            ; 周期 = 64 tick
    MOV ax, 0
    OUT 0x15, ax
    STS 1                   ; 割り込み有効化
```

ハンドラは `IRET` で復帰する (割り込み許可は IRET が自動で再セット)。
回帰テストは `timer_test.go`。

## Instructions

### Addressing Modes

命令は `[opcode, b1, b2, b3]` の4バイト固定長。どのバイトが「モードバイト」で
どのバイトが「データバイト」になるかは命令ごとに異なる (`MOV`/`OR`/`AND`/`XOR`/`CMP` の
ような2オペランド命令はモードが `b2`、データが `b3` の1バイトのみ。`ADD`/`NOT`/`SHL`/
`JMP`/`CALL` のような単一ソース命令はモードが `b1`、データは `b2`–`b3` の最大2バイト
使える)。下表の「n+1バイト目」はモードバイトの直後を指す。

**8-bit Addressing**
| Value | Target | Description |
| :--- | :--- | :--- |
| `0x00`~`0x0b` | Registers | General purpose registers |
| `0x0c` | Memory | `DataSegment * 256 + BX` <br> `mem[uint(reg[ds])*0x100+uint(reg[bx])]` |
| `0x0d` | Memory | `StackSegment * 256 + BasePointer` <br> `mem[uint(reg[ss])*0x100+uint(reg[bp])]` |
| `0x0e` | Memory | `DataSegment * 256 + (n+1バイト目)` <br> `mem[uint(reg[ds])*0x100+uint(data)]` |
| `0x0f` | Immediate | Immediate data (Source only) |
| `0xf0` | Memory | `Mem[Immd1*256+Immd2]` (n+1, n+2バイト目を使用) |
| `0xf1` | Memory | `Mem[Immd1*256+Immd2] : Mem[Immd1*256+Immd2+1]` (CS:IPペア用) |

**16-bit Addressing**
| Value | Target | Description |
| :--- | :--- | :--- |
| `0x10`~`0x1b` | Registers | Segment:Address (レジスタペア) |
| `0x20`~`0x2b` | Reg + Imm | Segreg + Addrimm |

### Instruction Set

👑 = **Kernel Only**。USERモード (`statsReg` bit2 = 1) で実行すると `0x7f` 例外が発生し、
命令自体は実行されない。

| Opcode | Mnemonic | Syntax | Description |
| :--- | :--- | :--- | :--- |
| `0x00` | **NOP** | `NOP [NULL] [NULL] [NULL]` | No operation. |
| `0x01` | **MOV** | `MOV Dest Source [NULL/DATA/ADDR]` | Move data between registers/memory/segment:reg pairs. |
| `0x02` | **ADD** | `ADD Source [DATA/ADDR] [NULL]` | `AX ← AX + Source`. Sets CF on unsigned overflow (>255), ZF if the resulting AX is 0. |
| `0x03` | **NOT** | `NOT Source [NULL/DATA/ADDR] [NULL]` | Bitwise NOT, written back in place. |
| `0x04` | **OR** | `OR SourceA SourceB [NULL/DATA/ADDR]` | Bitwise OR. `SourceA <= SourceA OR SourceB`. Max 1 Immediate. |
| `0x05` | **AND** | `AND SourceA SourceB [NULL/DATA/ADDR]` | Bitwise AND. Same syntax as OR. |
| `0x06` | **XOR** | `XOR SourceA SourceB [NULL/DATA/ADDR]` | Bitwise XOR. Same syntax as OR. |
| `0x07` | **SHL** | `SHL Source Shift-Num [NULL/ADDR]` | Shift Left. Sets CF to the bit shifted out of bit7. |
| `0x08` | **SHR** | `SHR Source Shift-Num [NULL/ADDR]` | Shift Right. **Does not touch any flag.** |
| `0x09` | **ROL** | `ROL Source Roll-Num [NULL/ADDR]` | Rotate Left (mod 8). No flags affected. |
| `0x0a` | **ROR** | `ROR Source Roll-Num [NULL/ADDR]` | Rotate Right (mod 8). No flags affected. |
| `0x0b` | **PUSH** | `PUSH Source [ADDR/DATA] [NULL]` | `SP--`; store Source at `SS:SP`. |
| `0x0c` | **POP** | `POP Dest [ADDR] [NULL]` | Load `SS:SP` into Dest; `SP++`. |
| `0x0d` | **CMP** | `CMP Source1 [Source2/ADDR/IMMD] [IMMD]` | Sets ZF/CF/SF/OF from `Source1 - Source2`. The only instruction that also **clears** flags explicitly. |
| `0x0e` | **JMP** | `JMP Mode+Source ...` | Jump to address. See [Jump Modes](#jump-modes-0x0e-jmp) below. |
| `0x0f` | **JZ** | `JZ [OFFSET(+-)]` | Jump if Zero (`ZF=1`). |
| `0x10` | **JNZ** | `JNZ [OFFSET(+-)]` | Jump if Not Zero (`ZF=0`). |
| `0x11` | **JA** | `JA [OFFSET(+-)]` | Jump if Above, unsigned (`CF=0 and ZF=0`). |
| `0x12` | **JBE** | `JBE [OFFSET(+-)]` | Jump if Below-or-Equal, unsigned (`CF=1 or ZF=1`). |
| `0x13` | **JG** | `JG [OFFSET(+-)]` | Jump if Greater, signed (`ZF=0 and SF=OF`). |
| `0x14` | **JLE** | `JLE [OFFSET(+-)]` | Jump if Less-or-Equal, signed (`ZF=1 or SF≠OF`). |
| `0x15` | **INC** | `INC Source [NULL/ADDR] [NULL]` | Increment by 1 (wraps `0xff→0x00`). Sets CF on wraparound; does not touch ZF. |
| `0x16` | 👑**IN** | `IN Dest [Register/IOPort]` | Input from port. See [I/O Ports](#io-ports-0x16-in) below. |
| `0x17` | 👑**OUT** | `OUT [Register/IOPort] [Source]` | Output to port. |
| `0x18` | **JC** | `JC [OFFSET(+-)]` | Jump if Carry (`CF=1`). |
| `0x19` | **JNC** | `JNC [OFFSET(+-)]` | Jump if Not Carry (`CF=0`). |
| `0x20` | **CALL** | `CALL Source1 [Source2/Segment/ADDR] [ADDR]` | Call subroutine. Always pushes return `IP` then `CS` (`CS` ends up on top), regardless of near/far form. See [Call Addressing](#call-addressing-0x20-call). |
| `0x21` | **RET** | `RET [near, far]` | Pops `CS` then `IP`. The operand is accepted but ignored (kept for backward compatibility) — always matches whatever `CALL` form was used. **Execution resumes at popped `CS:IP` + 4** (see note below). |
| `0x22` | **IRET** | `IRET [NULL] [NULL] [NULL]` | Pops `Flags`, then `CS`, then `IP`, and **re-enables interrupts** (`statsReg` bit0 ← 1, mirroring the auto-clear at dispatch). **Execution resumes at popped `CS:IP` + 4** (see note below). |
| `0x23` | 👑**LST** | `LST [TABLE TYPE] [0x0e] [NULL]` | Load system table. See [System Tables](#system-tables-0x23-lst) below. |
| `0x24` | **SYSCALL** | `SYSCALL [NULL] [NULL] [NULL]` | Raises interrupt `0x70`. Not privileged — this is the intended USER→KERNEL call path. |
| `0x25` | 👑**STS** | `STS Source [DATA] [NULL]` | Write `statsReg` from a register (`inst[1] < 0x0c`) or an immediate (`inst[1] = 0x0f`, value in `inst[2]`). The only way to enable interrupts (bit0), turn the MMU on (bit1), or drop to USER mode (bit2). |
| `0xFF` | 👑**HLT** | `HLT [NULL] [NULL] [NULL]` | Stop instruction fetch until the next interrupt wakes the CPU. |

### Jump Modes (`0x0e` JMP)
Syntax: `JMP Mode+Source [NULL/ADDR/OFFSET/SEGMENT/IMMDADDR/REGISTER2] [NULL/IMMDADDR]`
(`[Mode4bit] + [Source4bit]`)

| Mode | Segment | Address |
| :--- | :--- | :--- |
| `0x0f` | Current | `IP + Immediate` (relative, signed 8-bit) |
| `0x1f` | Current | `Immediate` |
| `0x2f` | Immediate | `Immediate` |

**JMP Addressing** (`inst[1]` value)
- `0x00`~`0x0b`: Segment=Now, Address=`Register`
- `0x0c`: Segment=Now, Address=`Mem[DS*256+BX]`
- `0x1c`: Segment=`Mem[DS*256+BX+1]`, Address=`Mem[DS*256+BX]`
- `0x0d`: Segment=Now, Address=`Mem[SS*256+BP]`
- `0x1d`: Segment=`Mem[SS*256+BP+1]`, Address=`Mem[SS*256+BP]`
- `0x0e`: Segment=Now, Address=`Mem[DS*256+Immd]`
- `0x1e`: Segment=`Mem[DS*256+Immd+1]`, Address=`Mem[DS*256+Immd]`
- `0x10`~`0x1b` (+ 2nd operand `0x10`~`0x1b`): Segment=`reg[inst1-0x10]`, Address=`reg[inst2-0x10]` (far register pair)
- `0xf0`: Segment=Now, Address=`Mem[Immd1*256+Immd2]`
- `0xf1`: Segment=`Mem[Immd1*256+Immd2+1]`, Address=`Mem[Immd1*256+Immd2]`

### RET / IRET landing (+4)

`RET` / `IRET` は pop した `CS:IP` **そのもの**ではなく、その **+4 バイト**に着地する
（メインループ末尾の `advancePC()` が必ず1命令ぶん加算するため)。

- `CALL` は「`CALL` 命令自身のアドレス」を push するので、`CALL`/`RET` の対では
  ちょうど次の命令に戻り、意識する必要はない。
- **スタックを手組みして `RET`/`IRET` で飛び込む場合**（タスクの初期スタック、
  コンテキストスイッチ等）は、**エントリポイントの4バイト手前**を push すること。
  慣例としてエントリ直前に `NOP` を1個置き、そのアドレス（`t*_ep:` のNOP）を積む。

### Call Addressing (`0x20` CALL)

Operand layout mirrors JMP addressing. Every form pushes the return `CS:IP` before
jumping; `RET` (`0x21`) always pops `CS` then `IP`, so it matches any `CALL` form.

| `inst[1]` | Target |
| :--- | :--- |
| `0x00`~`0x0b` | near register: `IP = reg[inst1]` |
| `0x0c` | near mem: `IP = Mem[DS*256+BX]` |
| `0x1c` | far mem: `IP = Mem[DS*256+BX]`, `CS = Mem[DS*256+BX+1]` |
| `0x0d` | near mem: `IP = Mem[SS*256+BP]` |
| `0x1d` | far mem: `IP = Mem[SS*256+BP]`, `CS = Mem[SS*256+BP+1]` |
| `0x0e` | near mem: `IP = Mem[DS*256+Immd]` |
| `0x1e` | far mem: `IP = Mem[DS*256+Immd]`, `CS = Mem[DS*256+Immd+1]` |
| `0x10`~`0x1b` (+ 2nd operand `0x10`~`0x1b`) | far register pair: `CS = reg[inst1-0x10]`, `IP = reg[inst2-0x10]` |
| `0x0f` | near relative: `IP = CS:IP + int8(Immd)` |
| `0x1f` | near immediate: `IP = Immd` |
| `0x2f` | far immediate: `CS = Immd1`, `IP = Immd2` |
| `0xf0` | near mem: `IP = Mem[Immd1*256+Immd2]` |
| `0xf1` | far mem: `IP = Mem[Immd1*256+Immd2]`, `CS = Mem[Immd1*256+Immd2+1]` |

### I/O Ports (`0x16` IN)
| Port | Name | Description |
| :--- | :--- | :--- |
| `0x00`~`0x0b` | NULL | Reserved |
| `0x0c` | KEYB | CHAR |
| `0x0d` | KEYB | BUFFER NUM |
| `0x10` | FLOPPY | CMD |
| `0x11` | FLOPPY | ARG |
| `0x12` | FLOPPY | DATA |
| `0x13` | FLOPPY | STATS |
| `0x14` | TIMER | Period low byte (ticks)。**書き込みでカウントがリセットされる** |
| `0x15` | TIMER | Period high byte。同上 |
| `0x16` | MMU | read: 直近の MMU フォルト (0x7d/0x7e) の仮想ページ番号 (OSのページフォルトハンドラ用 "CR2") |

Reading/writing the `DATA` port for FLOPPY or KEYB auto-advances that device's internal
cursor (`updateFloppyIO()` / `updateKeys()`). Full protocol, timing model, and gotchas
(ARG triple-write requirement, etc.) are documented in [`floppy.md`](./floppy.md).

### System Tables (`0x23` LST)
**Table Types**:
- `0`: Page Table (→ `ptReg`)
- `1`: Interruption Description Table (→ `idtReg`)

**Description**:
- `[SEGREG:REG2]`から2バイトを取り出してテーブルレジスタに格納する。
- 1バイト目がセグメント、2バイト目がオフセットとして扱われる。
- 現状 `inst[2] == 0x0c` (`Mem[DS*256+BX]`) 経由のみ実装されている。他のアドレッシングは
  何もしない (サイレントno-op)。

## Notes / Known Quirks

- **フラグは基本的に立てるだけ**: `CMP` を除く全命令 (`ADD`/`SHL`/`INC` など) はフラグを
  `|=` でセットするのみで、成立しなくなった条件を自動でクリアしない。前の命令の
  フラグが残ったまま次の条件分岐に影響することがあるので、条件分岐の前には必要に
  応じて明示的にフラグをクリアすること (組み込みブートROMも `MOV flag, 0x00` で
  手動クリアしている)。
- **`SHR`/`ROL`/`ROR` はフラグ無変更**、`SHL`/`INC` はCFのみセット — 対称に見えて
  非対称なので注意。
- 除算命令は未実装。ゼロ除算例外 (`0x7b`) は現状発火しない。
- 未定義オペコード (どの `case` にもマッチしない `inst[0]`) は `0x7c` 例外を送出するが、
  **既知オペコードに対する未定義オペランドの組み合わせは大半が無言のno-op**になる
  (エラーにならず、そのまま次命令へ進む)。

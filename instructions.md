# Specs
## MemMap
|領域|用途|
|-|-|
|0x0000--0x00ff|Interrupt vector (default)|
|0x0100--0x7bff|Free|
|0x7c00--0x7dff|Bootsector loading point|
|0x7e00--0xfaff|Free|
|0xfb00--0xfeff|Video Text RAM (64x16=1024Byte) *(Beta)*|
|0xff00--0xffff|Free|
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
## Flags
```js
0:		Zero
1:		Carry
2:		Overflow
3:		Sign
4~7:	NULL
```
## Status Register
```js
0:		Interrupt OK
1:		MMU ON/OFF
2:		Processor_MODE (0:KERNEL/1:USER)
3~7:	NULL
```
## Other Registers
```js
割り込みベクタ位置レジスタ:  IDTR (idtReg) (MMUがONの時は仮想セグメントだけ記述)
ステータス・レジスタ:       (statsReg)
ページテーブル位置レジスタ:  PTR (ptReg) (物理セグメントだけ記述)
```
## MMU
### **(指定したテーブル開始アドレス) :** フラグテーブル
>|ビット|名前|0の時|1の時|
>|-|-|-|-|
>|0|Valid|物理メモリ上にマップされていない|マップされている|
>|1|Read||
>|2|Write||
>|3|Execute||
>|4|User|カーネル領域|ユーザー領域|
>|5|NULL||
>|6|Accessed|最近利用されている|
>|7|Dirty|データが更新されている|
### **↑の次のセグメント :** アドレステーブル
今は特になし
## Interruptions
|割り込みID (0x00~0x7f)|割り込み内容|
|-|-|
|0x00|キーボード入力|
|0x7f|システムコール|
## Instructions

### Addressing Modes

**8-bit Addressing**
| Value | Target | Description |
| :--- | :--- | :--- |
| `0x00`~`0x0b` | Registers | General purpose registers |
| `0x0c` | Memory | `DataSegment * 256 + BX` <br> `mem[uint(reg[ds])*0x100+uint(reg[bx])]` |
| `0x0d` | Memory | `StackSegment * 256 + BasePointer` <br> `mem[uint(reg[ss])*0x100+uint(reg[bp])]` |
| `0x0e` | Memory | `DataSegment * 256 + 4th byte` <br> `mem[uint(reg[ds])*0x100+uint(inst[3])]` |
| `0x0f` | Immediate | Immediate data (Source only) |
| `0xf0` | Memory | `Mem[Immd1*256+Immd2]` |
| `0xf1` | Memory | `Mem[Immd1*256+Immd2] : Mem[Immd1*256+Immd2+1]` |

**16-bit Addressing**
| Value | Target | Description |
| :--- | :--- | :--- |
| `0x10`~`0x1b` | Registers | Segment:Address |
| `0x20`~`0x2b` | Reg + Imm | Segreg + Addrimm |

### Instruction Set (Kernel Onlyは下付き線)

| Opcode | Mnemonic | Syntax | Description |
| :--- | :--- | :--- | :--- |
| `0x01` | **MOV** | `MOV Dest Source [NULL/DATA/ADDR]` | Move data between registers/memory. |
| `0x02` | **ADD** | `ADD Source [NULL/DATA/ADDR] [NULL]` | Add source to destination. Carry flag set on carry. |
| `0x03` | **NOT** | `NOT Source [NULL/DATA/ADDR] [NULL]` | Bitwise NOT. |
| `0x04` | **OR** | `OR SourceA SourceB [NULL/DATA/ADDR]` | Bitwise OR. `SourceA <= SourceA OR SourceB`. Max 1 Immediate. |
| `0x05` | **AND** | `AND SourceA SourceB [NULL/DATA/ADDR]` | Bitwise AND. Same syntax as OR. |
| `0x06` | **XOR** | `XOR SourceA SourceB [NULL/DATA/ADDR]` | Bitwise XOR. Same syntax as OR. |
| `0x07` | **SHL** | `SHL Source Shift-Num [NULL/ADDR]` | Shift Left. |
| `0x08` | **SHR** | `SHR Source Shift-Num [NULL/ADDR]` | Shift Right. |
| `0x09` | **ROL** | `ROL Source Roll-Num [NULL/ADDR]` | Rotate Left. |
| `0x0a` | **ROR** | `ROR Source Roll-Num [NULL/ADDR]` | Rotate Right. |
| `0x0b` | **PUSH** | `PUSH Source [ADDR/DATA] [NULL]` | Push to stack. |
| `0x0c` | **POP** | `POP Dest [ADDR] [NULL]` | Pop from stack. |
| `0x0d` | **CMP** | `CMP Source1 [Source2/ADDR/IMMD] [IMMD]` | Compare values. |
| `0x0e` | **JMP** | `JMP Mode+Source ...` | Jump to address. See **Jump Modes** below. |
| `0x0f`~`0x14` | **JZ** | `JZ [OFFSET(+-)]` | Jump if Zero. Jumps to `CS:IP+OFFSET`. |
| `0x15` | **INC** | `INC Source [NULL/ADDR] [NULL]` | Increment value. |
| `0x16` | 👑**IN** | `IN Dest [Register/IOPort]` | Input from port. See **I/O Ports** below. |
| `0x17` | 👑**OUT** | `OUT [Register/IOPort] [Source]` | Output to port. |
| `0x18` | **JC** | `JC` | Jump if Carry. |
| `0x19` | **JNC** | `JNC` | Jump if Not Carry. |
| `0x20` | **CALL** | `CALL Source1 [Source2/Segment/ADDR] [ADDR]` | Call subroutine. |
| `0x21` | **RET** | `RET [near, far]` | Return from subroutine. |
| `0x22` | **IRET** | `IRET [NULL] [NULL] [NULL]` | Return from interrupt. |
| `0x23` | 👑**LST** | `LST [TABLE TYPE] [SEGREG] [REG2]` | Load system table. See **System Tables** below. |
| `0x24` | **SYSCALL** | `SYSCALL [NULL] [NUll] [NULL]` | Call system. See **Interruptions** above. |
| `0xFF` | **HLT** | `HLT [NULL] [NULL] [NULL]` | Halt processor. |

### Jump Modes (`0x0e` JMP)
Syntax: `JMP Mode+Source [NULL/ADDR/OFFSET/SEGMENT/IMMDADDR/REGISTER2] [NULL/IMMDADDR]`
(`[Mode4bit] + [Source4bit]`)

| Mode | Segment | Address |
| :--- | :--- | :--- |
| `0x0f` | Current | `IP + Immediate` (relative) |
| `0x1f` | Current | `Immediate` |
| `0x2f` | Immediate | `Immediate` |

**JMP Addressing**
- `0x01`~`0x0b`: Segment=Now, Address=`Register`
- `0x0c`: Segment=Now, Address=`Mem[DS*256+BX]`
- `0x1c`: Segment=`Mem[DS*256+BX+1]`, Address=`Mem[DS*256+BX]`
- `0x0d`: Segment=Now, Address=`Mem[SS*256+BP]`
- `0x1d`: Segment=`Mem[SS*256+BP+1]`, Address=`Mem[SS*256+BP]`
- `0x0e`: Segment=Now, Address=`Mem[DS*256+Immd]`
- `0x1e`: Segment=`Mem[DS*256+Immd+1]`, Address=`Mem[DS*256+Immd]`
- `0xf0`: Segment=Now, Address=`Mem[Immd1*256+Immd2]`
- `0xf1`: Segment=`Mem[Immd1*256+Immd2+1]`, Address=`Mem[Immd1*256+Immd2]`

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

### System Tables (`0x23` LST)
**Table Types**:
- `0`: Page Table
- `1`: Interruption Description Table

**Description**:
- `[SEGREG:REG2]`から2バイトを取り出してテーブルレジスタに格納する。
- 1バイト目がセグメント、2バイト目がオフセットとして扱われる。

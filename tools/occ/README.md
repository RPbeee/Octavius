# occ — a tiny C-subset compiler for Octavius

`occ` compiles a small C-like language to **oasm** assembly, which `oasm` then
assembles and links into a bootable floppy image. It exists so you can write
programs for the Octavius CPU without hand-writing assembly.

Octavius is an **8-bit** machine (all registers are 8 bits; only addresses are
16-bit via `segment:offset`), so this is deliberately a *subset* of C, not
standard C.

## Build

```sh
go build -o occ  ./tools/occ
go build -o oasm ./tools/oasm
```

## Use

```sh
./occ  prog.c -o prog.s        # C subset  -> oasm assembly
./oasm asm  prog.s -o prog.bin # assembly  -> flat binary
./oasm link -o floppy.img prog.bin@0
# run the emulator; it loads floppy.img
```

`occ prog.c` with no `-o` prints the assembly to stdout.
`-org N` sets the code origin (default `0x7c00`, the boot load point) for
programs loaded elsewhere by a custom loader, e.g. `-org 0x8000`.
`-sorg N` sets the runtime **stack** segment (default `0x70` → `ss=0x70`) for
programs that keep a compact low memory map and want the high pages free for
code, e.g. `-sorg 0x05` (OctOS packs its stack at `0x0500`).

## Quick start

`examples/hello.c` writes `Hello!` to the video text RAM and halts.

```sh
./occ  tools/occ/examples/hello.c -o hello.s
./oasm asm  hello.s -o hello.bin
./oasm link -o floppy.img hello.bin@0
```

## The language (v0)

```c
int  add1(int x) { return x + 1; }   // functions with parameters

void main() {
    int i;                 // local variable (8-bit)
    i = 0;
    while (i < 6) {        // while
        if (i == 3) {      // if / else
            putc(i, '*');
        } else {
            putc(i, 'A' + i);
        }
        i = i + 1;
    }
}
```

- **Types**: `int` is **16-bit**, `char` is **8-bit** storage, plus `void`,
  pointers `T*`, arrays `T a[N]`, and `struct`. All arithmetic is computed as
  16-bit (C-style integer promotion); a `char` is zero-extended when read and
  truncated to its low byte when stored.
- **`unsigned` / `signed`**: `unsigned int` and `unsigned char` (and bare
  `unsigned`, meaning `unsigned int`) make the relational operators
  `< > <= >=` compare **unsigned**. `signed` is accepted and is the default.
  (Unsignedness is a property of a declared variable; it is not tracked through
  intermediate arithmetic.)
- **`typedef`**: `typedef <type> Name;` introduces a type alias usable wherever
  a type is expected, e.g. `typedef char* str; str s;`.
- **`enum`**: `enum Tag { A, B = 5, C };` defines integer constants (values
  auto-increment, an `=` sets the next value). An `enum` type is a plain `int`;
  the constants are compile-time integers and may appear in `switch` labels.
- **Structs**: define at top level with `struct Tag { int a; char b; ... };`
  (fields are packed, no alignment padding). Declare `struct Tag v;`,
  `struct Tag *p;`, `struct Tag a[N];`; access fields with `v.a` and `p->a`
  (both lvalues). Struct pointers work as parameters. Whole-struct assignment
  and passing structs by value are not supported (use a pointer).
- **Pointers & arrays**: `int *p; char buf[16];` with `&x`, `*p`, `a[i]`.
  A pointer is a 16-bit **linear address** (segment:offset packed as high:low),
  so it can point anywhere in the 64 KiB space. Pointer arithmetic scales by the
  pointee size (`p + 1` on `int*` advances 2 bytes). Arrays decay to a pointer
  to their first element; array parameters are pointers. Comparisons of pointers
  are unsigned. (Pointer − pointer is not supported.)
- **String literals**: `"text"` is stored in the image as its bytes plus a
  trailing NUL; the expression is a `char*` to those bytes. Escapes `\n \r \t
  \0 \\ \"` are recognised.
- **Initializers**: scalars `int y = 5;`; arrays from a list `int v[3] = {1,2,3};`
  or a string `char s[] = "hi";`. An unsized `[]` takes its length from the
  initializer. Array elements are filled at startup (globals) or at the
  declaration (locals).
- **`sizeof`**: `sizeof(int)`, `sizeof(char)`, `sizeof(T*)`, `sizeof expr`,
  `sizeof(arr)` (the whole array, in bytes). Evaluates to a compile-time
  constant.
- **Globals and locals**: `int x;`, `int y = 5;`, `char c;`, `int v[4];`.
- **Statements**: declaration, assignment, `if` / `else` / `else if`, `while`,
  `do { … } while (cond)`, `for (init; cond; post)`, `switch` (with `case`,
  `default`, fall-through, and `break`), `break`, `continue`, `return`,
  expression statement, `{ … }` blocks.
- **Operators**: `+ - * / % | & ^ << >>`, comparisons `== != < > <= >=`,
  logical `&& ||` (short-circuit), unary `- ~ ! * &`, pre/post `++` / `--`,
  the conditional `?:`, `sizeof`, subscript `[]`,
  member `.` / `->`, assignment `=` and compound assignment
  `+= -= *= /= %= &= |= ^= <<= >>=`, parentheses. (A compound assignment
  `a op= b` evaluates `a` twice, so avoid a side-effecting subscript there.)
  Comparisons yield `1`/`0`. Relational
  `< > <= >=` are **signed** for integers (unsigned for pointers); `==` `!=` are
  sign-agnostic. `*` is 16-bit (mod 65536); `/ %` are **unsigned** 16-bit
  software routines (the CPU has no multiply/divide); dividing by zero yields
  quotient `0`, remainder = the dividend. Shift counts must be constants.
- **Functions**: any number of `int`/`char`/pointer parameters, 16-bit return
  value.
- **Built-ins**:
  - `putc(pos, ch)` writes byte `ch` to video text RAM cell `pos`
    (segment `0xfb`). This is how you produce visible output.
  - `__syscall(n, arg)` executes the `SYSCALL` instruction with call number
    `n` in `cx` and the 16-bit `arg` in `ax:dx`; the expression evaluates to
    the kernel's return value (returned in `ax:dx`). Clobbers `cx`.
  - A **function name used as a value** (not called) evaluates to its 16-bit
    linear address — for spawn-style APIs, e.g. `__syscall(SYS_SPAWN, task_e)`.
    There are no callable function pointers.
  - `__diskcmd(cmd, c, h, s)` submits a floppy command + cylinder/head/sector to
    the disk controller as one hand-emitted burst: `RESET; ARG=0; OUT CMD;
    OUT ARG,c; OUT ARG,h; OUT ARG,s` with no instructions in between (each C/H/S
    gets its MSB set so a zero value still reads as a non-zero ARG). The floppy
    ARG port has no handshake — it is re-sampled every CPU tick while a command
    is active — so three separate `__out` calls would duplicate values; this
    builtin is the one thing the driver can't express as ordinary `__out`s.
    `cmd` is 1=READ, 2=WRITE. Evaluates to nothing. Streaming the 512 data bytes
    and polling `STAT` are then plain `__in`/`__out` in ordinary code.
  - `__out(port, value)` writes the low byte of `value` to I/O `port`, and
    `__in(port)` reads a byte from `port` (zero-extended to 16-bit). A constant
    `port` uses the immediate instruction form. These are the raw `OUT`/`IN`
    instructions — device drivers (floppy, keyboard, …) are written on top of
    them in ordinary `occ` code, e.g. in a header. Arbitrary memory-mapped
    addresses need no built-in: assign the linear address to a pointer
    (`char *v = 0xfb00; *v = ch;`).
- **Comments**: `//` and `/* … */`.

## Preprocessor

A minimal preprocessor runs before parsing:

```c
#include "defs.h"      // splice in another file (path is relative to this one)

#define VRAM  0xfb     // object-like macro: VRAM is replaced by 0xfb
#define STAR  '*'
#define WIDTH (8 + 8)  // the replacement can be several tokens
#define ADD(a, b) ((a) + (b))   // function-like macro

#ifdef DEBUG
#define TRACE(x) putc(0, x)
#else
#define TRACE(x)
#endif

void main() { putc(0, STAR); putc(1, ADD(2, 3) + '0'); }
```

- `#define NAME tokens…` — object-like macro: every later use of `NAME` is
  replaced by its tokens (macros in the replacement expand too; self-reference
  is left alone).
- `#define NAME(a, b) tokens…` — function-like macro. `(` must follow the name
  immediately; arguments are fully macro-expanded before substitution. (Because
  the lexer discards whitespace, an object-like macro whose replacement is a
  parenthesised identifier list, e.g. `#define G (x)`, is misread as
  function-like — write such values without the leading `(` touching the name,
  as in `#define WIDTH (8 + 8)`, which is safe.) `#` / `##` are not supported.
- `#undef NAME` — removes a macro definition.
- `#ifdef NAME` / `#ifndef NAME` / `#else` / `#endif` — conditional compilation,
  nestable. (`#if` with an expression and `#elif` are not supported.)
- `#include "file"` — inlines another file, resolved relative to the including
  file. Include cycles are detected and rejected. `#include` is spliced before
  the conditional pass, so it is **not** suppressed by an enclosing `#ifdef`.
- Directives must be on their own line. (Line numbers in error messages are
  relative to the fully-included source.)

## Deliberate limits (things v0 does *not* do)

These come from the hardware, and are the natural next things to add:

- **Shift counts must be constants** (`x << 3`, not `x << n`).
- **`/ %` are unsigned**; `*` keeps only the low 16 bits. All three are software
  routines the compiler adds only when used.
- **No multi-dimensional arrays, pointer − pointer, unions, whole-struct
  assignment, or struct-by-value parameters** yet.
- **Recursion is supported but shallow.** Parameters and locals are statically
  allocated (one slot per name), so a call to a function that can reach itself in
  the call graph (direct or mutual recursion) saves that function's whole frame
  on the hardware stack and restores it on return. Because `sp` is 8-bit, the
  stack is a single **256-byte** segment shared with expression temporaries and
  return addresses, so only a modest recursion depth fits before it overflows
  (there is no overflow check). One consequence of the static slots: taking
  `&local` and passing it *into* a deeper activation that writes through it does
  not propagate back (the slot is saved/restored around the call). Ordinary
  by-value recursion (factorial, Fibonacci, tree walks) works.
- **Variables share one 256-byte data segment** (`int`/pointer take 2 bytes,
  `char` 1, an array N×element); the top 16 bytes are reserved for runtime
  scratch, leaving ~240 bytes. Pointers can still *point* anywhere in the 64 KiB
  address space — only the variables themselves live in this segment.
- **Boot size**: the ROM loader reads only sector 0 (512 bytes) to `0x7c00`, so
  a single-sector program must fit there. 16-bit code plus the math helpers is
  larger, so bigger programs need a multi-sector loader on the floppy.

## How it compiles (memory model)

- Values are computed as **16 bits** in `ax` (low) : `dx` (high). `char` is a
  1-byte store/1-byte zero-extended load.
- Variables are statically allocated in data segment `0x10` (`ds:imm`); the
  16-bit math helpers use scratch at `0xf0`–`0xff`. There is no runtime stack
  frame for locals, so all functions' locals must fit in the 240-byte segment
  below the scratch. To make that budget go far, occ **overlays** frames by the
  call graph: two functions that can never be live simultaneously (neither
  reaches the other) share the same bytes. Each frame is placed just above the
  deepest chain of callers that can reach it (a longest-path walk of the call
  DAG; recursive back-edges are skipped and handled by the save/restore below),
  so the total is the longest call path, not the sum of all frames. Functions
  entered only from asm (interrupt handlers) and `main` root at the base and
  overlay with each other. Overflow is reported as "out of data memory".
- The stack is at `ss=0x70` (overridable with `-sorg`), `sp=0xff`; `CALL`/`RET` and expression temporaries
  use it (each 16-bit temporary is two 1-byte pushes). A call to a recursive
  function also pushes that function's frame bytes (`PUSH [addr]`) before the
  call and pops them back (`POP [addr]`) after, so a nested activation can reuse
  the same static slots. Non-recursive calls skip this and write arguments
  straight into the parameter slots.
- A pointer value is the 16-bit linear address `segment*256 + offset`. Because
  the segment granularity (256) and offset width (8 bits) line up exactly, the
  16-bit accumulator *is* the pointer, `p + i` is plain 16-bit addition that
  crosses segment boundaries correctly, and `&x` is a compile-time constant.
  Dereferencing loads the segment into `ds` and the offset into `bx`
  (`[ds:bx]`), then restores `ds`.
- Code is `org`'d at `0x7c00` (the boot load point). A prologue sets up
  `ss`/`sp`/`ds`, initialises globals, `CALL main`, then `HLT`. 16-bit
  arithmetic, compares, shifts, multiply/divide, and pointer load/store are
  emitted as `__*16` / `__load*` / `__store*` subroutines after the `HLT`.
- `if` / `while` / `for` and `&&` / `||` invert the condition and branch over a
  near `JMP`, so the relative conditional jumps never overflow their ±127 range.

Regression tests that build a program and run it on the emulator live in
`occ_run_test.go` at the repository root (`go test -run TestOcc ./`).

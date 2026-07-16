package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runProgram compiles a C-subset source with occ, assembles it with oasm,
// loads the flat binary at 0x7c00, and single-steps the CPU until it reaches
// a HLT. It returns nothing; callers inspect package globals (mem/reg).
func runProgram(t *testing.T, csrc string) {
	t.Helper()
	dir := t.TempDir()
	cfile := filepath.Join(dir, "p.c")
	sfile := filepath.Join(dir, "p.s")
	bfile := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(cfile, []byte(csrc), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("go", "run", "./tools/occ", cfile, "-o", sfile)
	run("go", "run", "./tools/oasm", "asm", sfile, "-o", bfile)

	bin, err := os.ReadFile(bfile)
	if err != nil {
		t.Fatal(err)
	}

	// Fresh machine state: MMU off, program loaded at 0x7c00, cs:ip = 0x7c:0.
	statsReg = 0
	reg = [12]uint8{}
	mem = [MemSize]uint8{}
	ioport = [256]uint8{}
	copy(mem[0x7c00:], bin)
	reg[cs] = 0x7c
	reg[ip] = 0x00

	for i := 0; i < 100000; i++ {
		inst := mem[uint(reg[cs])*0x100+uint(reg[ip]):]
		if len(inst) > 0 && inst[0] == 0xff { // HLT
			return
		}
		tick()
		advancePC()
	}
	t.Fatal("program did not halt within the step budget")
}

func vram(i int) byte { return mem[0xfb00+i] }

func TestOccHello(t *testing.T) {
	runProgram(t, `
void main() {
    putc(0, 'H');
    putc(1, 'i');
    putc(2, '!');
}
`)
	if got := string([]byte{vram(0), vram(1), vram(2)}); got != "Hi!" {
		t.Fatalf("VRAM = %q, want %q", got, "Hi!")
	}
}

func TestOccControlFlow(t *testing.T) {
	// Loop with if/else, a function call, comparisons, arithmetic.
	// Writes 'A','B','C','*','E','F' to cells 0..5 (index 3 becomes '*').
	runProgram(t, `
int add1(int x) { return x + 1; }

void main() {
    int i;
    int c;
    i = 0;
    c = 'A';
    while (i < 6) {
        if (i == 3) {
            putc(i, '*');
        } else {
            putc(i, c);
        }
        c = add1(c);
        i = i + 1;
    }
}
`)
	want := "ABC*EF"
	got := ""
	for i := 0; i < 6; i++ {
		got += string(rune(vram(i)))
	}
	if got != want {
		t.Fatalf("VRAM = %q, want %q", got, want)
	}
}

func TestOccForLogicalSigned(t *testing.T) {
	runProgram(t, `
void main() {
    int i;
    int sum;
    sum = 0;
    for (i = 0; i < 5; i = i + 1) {
        sum = sum + i;               // 0+1+2+3+4 = 10
    }
    putc(0, sum);                    // 10
    putc(1, (2 < 3) && (4 < 5));     // 1
    putc(2, (2 < 3) && (5 < 4));     // 0
    putc(3, (9 < 4) || (1 < 2));     // 1
    putc(4, (0 - 1) < 0);            // signed: -1 < 0  -> 1
    putc(5, (0 - 1) < 5);            // signed: -1 < 5  -> 1
}
`)
	want := []byte{10, 1, 0, 1, 1, 1}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d, want %d", i, got, w)
		}
	}
}

func TestOccShortCircuit(t *testing.T) {
	// The right operand must NOT run when the left already decides the result:
	// mark cells 0 and 1 only if the (never-taken) rhs side effect fires.
	runProgram(t, `
int touch(int n) { putc(n, 9); return 1; }

void main() {
    putc(0, 0);
    putc(1, 0);
    if (0 && touch(0)) { }   // touch(0) must be skipped -> cell 0 stays 0
    if (1 || touch(1)) { }   // touch(1) must be skipped -> cell 1 stays 0
}
`)
	if vram(0) != 0 {
		t.Fatalf("&& evaluated its rhs (cell0=%d)", vram(0))
	}
	if vram(1) != 0 {
		t.Fatalf("|| evaluated its rhs (cell1=%d)", vram(1))
	}
}

func TestOccStructs(t *testing.T) {
	runProgram(t, `
struct Point { int x; int y; };

void main() {
    struct Point p;
    struct Point *pp;
    p.x = 300;               // 0x012c
    p.y = 7;
    pp = &p;
    putc(0, pp->x);          // 0x2c
    putc(1, pp->x >> 8);     // 0x01
    putc(2, p.y);            // 7
    pp->y = 42;              // store through ->
    putc(3, p.y);            // 42
    putc(4, sizeof(struct Point));  // 4
}
`)
	want := []byte{0x2c, 0x01, 7, 42, 4}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d, want %d", i, got, w)
		}
	}
}

func TestOccElseIfBreakContinue(t *testing.T) {
	runProgram(t, `
int classify(int n) {
    if (n < 0)       return 0;
    else if (n == 0) return 1;
    else if (n < 10) return 2;
    else             return 3;
}

void main() {
    int i;
    int sum;
    sum = 0;
    for (i = 0; i < 10; i = i + 1) {
        if (i == 3) continue;   // skip 3
        if (i == 7) break;      // stop before 7
        sum = sum + i;          // 0+1+2+4+5+6 = 18
    }
    putc(0, sum);               // 18
    putc(1, classify(-5));      // 0
    putc(2, classify(0));       // 1
    putc(3, classify(5));       // 2
    putc(4, classify(99));      // 3
    // continue in a while loop
    i = 0;
    int cnt;
    cnt = 0;
    while (i < 5) {
        i = i + 1;
        if (i == 2) continue;
        cnt = cnt + 1;          // counts 1,3,4,5 -> 4
    }
    putc(5, cnt);               // 4
}
`)
	want := []byte{18, 0, 1, 2, 3, 4}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d, want %d", i, got, w)
		}
	}
}

func TestOccStringsSizeofInit(t *testing.T) {
	runProgram(t, `
char msg[] = "Hi!";          // inferred length 4 (incl NUL)
int nums[3] = {10, 20, 30};

void puts(char *s) {
    int i;
    for (i = 0; s[i] != 0; i = i + 1) {
        putc(i, s[i]);
    }
}

void main() {
    puts("Hey");             // string literal decays to char*
    putc(8, sizeof(int));    // 2
    putc(9, sizeof(msg));    // 4 (whole array)
    putc(10, sizeof(nums));  // 6
    putc(11, nums[2]);       // 30
    putc(12, msg[1]);        // 'i'
}
`)
	// "Hey" written to cells 0..2
	if got := string([]byte{vram(0), vram(1), vram(2)}); got != "Hey" {
		t.Fatalf("string literal output = %q, want %q", got, "Hey")
	}
	checks := map[int]byte{8: 2, 9: 4, 10: 6, 11: 30, 12: 'i'}
	for cell, w := range checks {
		if got := vram(cell); got != w {
			t.Fatalf("cell %d = %d, want %d", cell, got, w)
		}
	}
}

func TestOccPointersArrays(t *testing.T) {
	runProgram(t, `
char buf[8];
void main() {
    char *p;
    int i;
    p = buf;
    for (i = 0; i < 6; i = i + 1) {
        *p = 'A' + i;       // store through pointer
        p = p + 1;          // char* arithmetic (+1 byte)
    }
    for (i = 0; i < 6; i = i + 1) {
        putc(i, buf[i]);    // read back via indexing
    }
}
`)
	want := "ABCDEF"
	got := ""
	for i := 0; i < 6; i++ {
		got += string(rune(vram(i)))
	}
	if got != want {
		t.Fatalf("VRAM = %q, want %q", got, want)
	}
}

func TestOccIntArrayPointer(t *testing.T) {
	// int arrays: element stride is 2 bytes; pointer arithmetic must scale.
	runProgram(t, `
void main() {
    int a[3];
    int *q;
    a[0] = 1000;
    a[1] = 2000;
    a[2] = a[0] + a[1];      // 3000 = 0x0bb8
    q = a;
    putc(0, *(q + 2));       // 0xb8
    putc(1, *(q + 2) >> 8);  // 0x0b
    putc(2, a[1]);           // 2000 low = 0xd0
    putc(3, a[1] >> 8);      // 0x07
    // &a[1] - &a[0] pointer difference via addresses (unsigned compare)
    putc(4, (&a[1]) > (&a[0]));  // 1
}
`)
	want := []byte{0xb8, 0x0b, 0xd0, 0x07, 1}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = 0x%02x, want 0x%02x", i, got, w)
		}
	}
}

func TestOcc16Bit(t *testing.T) {
	// int is 16-bit; observe both bytes via putc(low) / putc(high).
	runProgram(t, `
void main() {
    int a;
    int b;
    a = 300;                 // 0x012c
    putc(0, a);              // low  = 0x2c
    putc(1, a >> 8);         // high = 0x01
    b = 200 * 3;             // 600 = 0x0258
    putc(2, b);              // 0x58
    putc(3, b >> 8);         // 0x02
    b = 1000 - 1;            // 999 = 0x03e7
    putc(4, b);              // 0xe7
    putc(5, b >> 8);         // 0x03
}
`)
	want := []byte{0x2c, 0x01, 0x58, 0x02, 0xe7, 0x03}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = 0x%02x, want 0x%02x", i, got, w)
		}
	}
}

func TestOcc16MulDivSigned(t *testing.T) {
	runProgram(t, `
char lo;
char hi;
void main() {
    int q;
    q = 60000 / 250;         // 240 = 0xf0
    putc(0, q);              // 0xf0
    putc(1, q >> 8);         // 0x00
    q = 12345 % 1000;        // 345 = 0x0159
    putc(2, q);              // 0x59
    putc(3, q >> 8);         // 0x01
    putc(4, (100 * 100) >> 8);  // 10000=0x2710 -> high 0x27
    // signed comparison on 16-bit: -1 (0xffff) < 1 is true
    putc(5, ((0 - 1) < 1) + 48); // '1'
}
`)
	want := []byte{0xf0, 0x00, 0x59, 0x01, 0x27, '1'}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = 0x%02x, want 0x%02x", i, got, w)
		}
	}
}

func TestOccCharTruncates(t *testing.T) {
	// A char stores only the low byte; reading it zero-extends.
	runProgram(t, `
void main() {
    char c;
    int i;
    c = 300;         // stored as 300 & 0xff = 44
    i = c;           // zero-extended back to 44
    putc(0, i);      // 44
    putc(1, i >> 8); // 0
}
`)
	if vram(0) != 44 || vram(1) != 0 {
		t.Fatalf("char truncation: cells = %d,%d want 44,0", vram(0), vram(1))
	}
}

func TestOccMulDiv(t *testing.T) {
	// Software multiply/divide/modulo (unsigned, 8-bit).
	runProgram(t, `
void main() {
    putc(0, 6 * 7);      // 42
    putc(1, 100 / 7);    // 14
    putc(2, 100 % 7);    // 2
    putc(3, 3 * 4 * 5);  // 60
    putc(4, 255 / 16);   // 15
    putc(5, 9 % 5);      // 4
}
`)
	want := []byte{42, 14, 2, 60, 15, 4}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d, want %d", i, got, w)
		}
	}
}

func TestOccPreprocessor(t *testing.T) {
	// #include pulls in a helper file; #define provides object-like macros.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "defs.h"), []byte(`
#define BASE 0x40
int ident(int x) { return x; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfile := filepath.Join(dir, "p.c")
	sfile := filepath.Join(dir, "p.s")
	bfile := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(cfile, []byte(`
#include "defs.h"
#define FIRST 'H'
#define STAR  '*'

void main() {
    putc(ident(0), FIRST);
    putc(1, STAR);
    putc(2, BASE + 5);   // 0x45 = 'E'
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	run("go", "run", "./tools/occ", cfile, "-o", sfile)
	run("go", "run", "./tools/oasm", "asm", sfile, "-o", bfile)
	bin, err := os.ReadFile(bfile)
	if err != nil {
		t.Fatal(err)
	}
	statsReg, reg, mem, ioport = 0, [12]uint8{}, [MemSize]uint8{}, [256]uint8{}
	copy(mem[0x7c00:], bin)
	reg[cs], reg[ip] = 0x7c, 0x00
	for i := 0; i < 100000; i++ {
		inst := mem[uint(reg[cs])*0x100+uint(reg[ip]):]
		if len(inst) > 0 && inst[0] == 0xff {
			break
		}
		tick()
		advancePC()
	}
	if got := string([]byte{vram(0), vram(1), vram(2)}); got != "H*E" {
		t.Fatalf("VRAM = %q, want %q", got, "H*E")
	}
}

func TestOccArithmetic(t *testing.T) {
	// Exercise -, comparisons, bitwise ops, shifts. Store results in VRAM.
	runProgram(t, `
void main() {
    int a;
    a = 10 - 3;          // 7
    putc(0, a + 48);     // '7'
    putc(1, (2 < 5) + 48);   // '1'
    putc(2, (5 < 2) + 48);   // '0'
    putc(3, (1 << 3) + 48);  // 8 -> '8'
    putc(4, (6 & 3) + 48);   // 2 -> '2'
    putc(5, (6 | 1) + 48);   // 7 -> '7'
}
`)
	want := "710827"
	got := ""
	for i := 0; i < 6; i++ {
		got += string(rune(vram(i)))
	}
	if got != want {
		t.Fatalf("VRAM = %q, want %q", got, want)
	}
}

func TestOccIncDecCompound(t *testing.T) {
	// ++/-- (prefix and postfix, value semantics), compound assignment, and
	// pointer ++ scaling by the pointee size.
	runProgram(t, `
void main() {
    int i;
    int s;
    int arr[3];
    int *r;

    i = 5;
    i++;          // 6
    ++i;          // 7
    i--;          // 6
    putc(0, i);   // 6

    s = 2;
    s += 3;       // 5
    s *= 4;       // 20
    s -= 1;       // 19
    s /= 2;       // 9
    s %= 4;       // 1
    s <<= 3;      // 8
    s |= 1;       // 9
    putc(1, s);   // 9

    i = 10;
    putc(2, i++); // stores old value 10, i -> 11
    putc(3, i);   // 11
    putc(4, ++i); // pre: i -> 12, stores 12

    arr[0] = 40; arr[1] = 41; arr[2] = 42;
    r = arr;
    r++;          // int* advances 2 bytes -> &arr[1]
    putc(5, *r);  // 41
}
`)
	want := []byte{6, 9, 10, 11, 12, 41}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d, want %d", i, got, w)
		}
	}
}

func TestOccTernarySwitch(t *testing.T) {
	// Ternary, switch with fall-through/break/default, and do-while.
	runProgram(t, `
void main() {
    int i;
    int s;
    int p;

    // do-while accumulates 0+1+2+3+4 = 10
    i = 0; s = 0;
    do {
        s += i;
        i++;
    } while (i < 5);
    putc(0, s);   // 10

    p = (s > 5) ? 100 : 200;   // 10 > 5 -> 100
    putc(1, p);   // 100

    switch (s) {           // s == 10
        case 1:  putc(2, 1); break;
        case 10: putc(2, 55);   // fall through
        case 9:  putc(3, 66); break;
        default: putc(2, 99);
    }

    switch (p) {           // p == 100, no case -> default
        case 1: putc(4, 7); break;
        default: putc(4, 88);
    }

    // continue inside a loop nested in a switch still targets the loop
    switch (s) {
        case 10:
            for (i = 0; i < 4; i++) {
                if (i == 2) continue;
                putc(5 + i, 'A' + i);
            }
            break;
    }
}
`)
	// cells: 0=10, 1=100, 2=55, 3=66, 4=88, then loop writes A,B,(skip),D
	checks := map[int]byte{0: 10, 1: 100, 2: 55, 3: 66, 4: 88, 5: 'A', 6: 'B', 8: 'D'}
	for cell, w := range checks {
		if got := vram(cell); got != w {
			t.Fatalf("cell %d = %d, want %d", cell, got, w)
		}
	}
	if got := vram(7); got != 0 {
		t.Fatalf("cell 7 = %d, want 0 (continue should skip i==2)", got)
	}
}

func TestOccTypedefEnum(t *testing.T) {
	// typedef (scalar and pointer), enum (implicit and explicit values).
	runProgram(t, `
typedef int myint;
typedef char* str;

enum Color { RED, GREEN, BLUE };        // 0, 1, 2
enum Flags { A = 1, B = 4, C, D = 10 }; // 1, 4, 5, 10

char msg[] = "Zy";

void main() {
    myint n;
    str s;

    n = GREEN;         // 1
    putc(0, n + 48);   // '1'
    putc(1, BLUE + 48);// 2 -> '2'
    putc(2, C + 48);   // 5 -> '5'
    putc(3, D + 48);   // 10 -> ':'

    s = msg;           // char* typedef
    putc(4, *s);       // 'Z'
    putc(5, s[1]);     // 'y'

    switch (D) {       // enum constant in a case dispatch
        case 10: putc(6, 'X'); break;
        default: putc(6, '?');
    }
}
`)
	want := []byte{'1', '2', '5', ':', 'Z', 'y', 'X'}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d (%q), want %d (%q)", i, got, rune(got), w, rune(w))
		}
	}
}

func TestOccUnsigned(t *testing.T) {
	// A large value has its high bit set: as signed it is negative, as unsigned
	// it is large. The relational operator must respect the declared signedness.
	runProgram(t, `
void main() {
    int si;
    unsigned int ui;

    si = 0x8000;       // -32768 signed, 32768 unsigned
    ui = 0x8000;

    // signed: 0x8000 < 1  is true (negative < positive)
    putc(0, (si < 1) + 48);   // '1'
    // unsigned: 0x8000 < 1  is false (big < 1)
    putc(1, (ui < 1) + 48);   // '0'
}
`)
	if vram(0) != '1' {
		t.Fatalf("signed compare: cell0 = %q, want '1'", rune(vram(0)))
	}
	if vram(1) != '0' {
		t.Fatalf("unsigned compare: cell1 = %q, want '0'", rune(vram(1)))
	}
}

func TestOccMacros(t *testing.T) {
	// Function-like macros (with nested-paren args and macro-in-arg), object-like
	// macros in a replacement, #undef, and #ifdef/#ifndef/#else/#endif.
	runProgram(t, `
#define VRAM 0
#define ADD(a, b) ((a) + (b))
#define DBL(x) ADD(x, x)
#define BASE 'A'

#ifdef FEATURE_X
#define STEP 100
#else
#define STEP 1
#endif

#ifndef MISSING
#define OK 1
#endif

void main() {
    putc(VRAM + 0, BASE + ADD(1, 2));   // 'A' + 3 = 'D'
    putc(VRAM + 1, BASE + DBL(3));      // 'A' + 6 = 'G'
    putc(VRAM + 2, '0' + STEP);         // FEATURE_X undefined -> STEP=1 -> '1'
    putc(VRAM + 3, '0' + OK);           // '1'
    putc(VRAM + 4, '0' + ADD(DBL(2), 1)); // (4)+(1)=5 -> '5'
}

#define BASE 'Z'
#undef BASE
`)
	want := []byte{'D', 'G', '1', '1', '5'}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %d (%q), want %d (%q)", i, got, rune(got), w, rune(w))
		}
	}
}

func TestOccIfdefDefined(t *testing.T) {
	// When the guard macro IS defined, the #ifdef branch (not #else) is taken.
	runProgram(t, `
#define FEATURE_X
#ifdef FEATURE_X
#define STEP 7
#else
#define STEP 0
#endif
void main() {
    putc(0, '0' + STEP);   // '7'
}
`)
	if got := vram(0); got != '7' {
		t.Fatalf("cell 0 = %q, want '7'", rune(got))
	}
}

func TestOccRecursionFactorial(t *testing.T) {
	// Direct recursion: fact(5) = 120. Requires each activation's `n` to survive
	// the recursive call.
	runProgram(t, `
int fact(int n) {
    if (n <= 1) return 1;
    return n * fact(n - 1);
}
void main() {
    putc(0, fact(5));   // 120
}
`)
	if got := vram(0); got != 120 {
		t.Fatalf("fact(5) = %d, want 120", got)
	}
}

func TestOccRecursionFib(t *testing.T) {
	// Two recursive calls per frame, and a local that must survive both.
	runProgram(t, `
int fib(int n) {
    int a;
    int b;
    if (n < 2) return n;
    a = fib(n - 1);
    b = fib(n - 2);
    return a + b;
}
void main() {
    putc(0, fib(10));   // 55
    putc(1, fib(7));    // 13
}
`)
	if got := vram(0); got != 55 {
		t.Fatalf("fib(10) = %d, want 55", got)
	}
	if got := vram(1); got != 13 {
		t.Fatalf("fib(7) = %d, want 13", got)
	}
}

func TestOccMutualRecursion(t *testing.T) {
	// Mutual recursion (a cycle through two functions).
	runProgram(t, `
int isodd(int n);
int iseven(int n) {
    if (n == 0) return 1;
    return isodd(n - 1);
}
int isodd(int n) {
    if (n == 0) return 0;
    return iseven(n - 1);
}
void main() {
    putc(0, iseven(8) + '0');   // 1 -> '1'
    putc(1, iseven(7) + '0');   // 0 -> '0'
    putc(2, isodd(5) + '0');    // 1 -> '1'
}
`)
	want := []byte{'1', '0', '1'}
	for i, w := range want {
		if got := vram(i); got != w {
			t.Fatalf("cell %d = %q, want %q", i, rune(got), rune(w))
		}
	}
}

func TestOccPortIO(t *testing.T) {
	// __out writes a byte to an I/O port; __in reads it back. Uses scratch ports
	// with no attached device, so the value simply round-trips through ioport[].
	runProgram(t, `
void main() {
    int p;
    __out(0x50, 42);         // constant-port form
    putc(0, __in(0x50));     // 42
    p = 0x51;
    __out(p, 99);            // register-port form
    putc(1, __in(p));        // 99
}
`)
	if got := vram(0); got != 42 {
		t.Fatalf("port 0x50 round-trip = %d, want 42", got)
	}
	if got := vram(1); got != 99 {
		t.Fatalf("port 0x51 round-trip = %d, want 99", got)
	}
}

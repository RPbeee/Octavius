package main

import "fmt"

// encodeError is returned when a mnemonic/operand combination is not representable.
func encErr(mn string, msg string) error {
	return fmt.Errorf("%s: %s", mn, msg)
}

func b(v int) uint8 { return uint8(v & 0xff) }

// encode turns one instruction (mnemonic + operands) into its 4 bytes.
// `curAddr` is the address of THIS instruction (used for relative jumps).
func encode(mn string, ops []operand, curAddr int) ([]uint8, error) {
	switch mn {
	case "NOP":
		return []uint8{0x00, 0x00, 0x00, 0x00}, nil

	case "MOV":
		return encMov(ops)

	case "ADD":
		return encAccum(0x02, ops) // ADD src -> ax += src
	case "NOT":
		return encUnaryMem(0x03, ops)

	case "OR":
		return encLogic(0x04, ops)
	case "AND":
		return encLogic(0x05, ops)
	case "XOR":
		return encLogic(0x06, ops)

	case "SHL":
		return encShift(0x07, ops)
	case "SHR":
		return encShift(0x08, ops)
	case "ROL":
		return encShift(0x09, ops)
	case "ROR":
		return encShift(0x0a, ops)

	case "PUSH":
		return encPush(ops)
	case "POP":
		return encPop(ops)

	case "CMP":
		return encCmp(ops)

	case "JMP":
		return encJmp(ops, curAddr)
	case "CALL":
		return encJmp2(0x20, ops, curAddr)

	case "JZ":
		return encCond(0x0f, ops, curAddr)
	case "JNZ":
		return encCond(0x10, ops, curAddr)
	case "JA":
		return encCond(0x11, ops, curAddr)
	case "JBE":
		return encCond(0x12, ops, curAddr)
	case "JG":
		return encCond(0x13, ops, curAddr)
	case "JLE":
		return encCond(0x14, ops, curAddr)
	case "JC":
		return encCond(0x18, ops, curAddr)
	case "JNC":
		return encCond(0x19, ops, curAddr)

	case "INC":
		return encUnaryMem(0x15, ops)

	case "IN":
		return encIn(ops)
	case "OUT":
		return encOut(ops)

	case "RET":
		if len(ops) == 0 {
			return []uint8{0x21, 0x00, 0x00, 0x00}, nil
		}
		if ops[0].kind == kImm {
			return []uint8{0x21, b(ops[0].imm), 0x00, 0x00}, nil
		}
		return nil, encErr("RET", "expected optional near/far (0/1)")
	case "IRET":
		return []uint8{0x22, 0x00, 0x00, 0x00}, nil
	case "SYSCALL":
		return []uint8{0x24, 0x00, 0x00, 0x00}, nil
	case "HLT":
		return []uint8{0xff, 0x00, 0x00, 0x00}, nil

	case "LST":
		return encLst(ops)
	}
	return nil, fmt.Errorf("unknown mnemonic %q", mn)
}

func need(mn string, ops []operand, n int) error {
	if len(ops) != n {
		return encErr(mn, fmt.Sprintf("expected %d operand(s), got %d", n, len(ops)))
	}
	return nil
}

// MOV dest, src
func encMov(ops []operand) ([]uint8, error) {
	if err := need("MOV", ops, 2); err != nil {
		return nil, err
	}
	d, s := ops[0], ops[1]
	switch d.kind {
	case kReg:
		switch s.kind {
		case kReg:
			return []uint8{0x01, d.regNum, s.regNum, 0}, nil
		case kMemBX:
			return []uint8{0x01, d.regNum, 0x0c, 0}, nil
		case kMemBP:
			return []uint8{0x01, d.regNum, 0x0d, 0}, nil
		case kMemDSImm:
			return []uint8{0x01, d.regNum, 0x0e, b(s.imm)}, nil
		case kImm:
			return []uint8{0x01, d.regNum, 0x0f, b(s.imm)}, nil
		case kMemSegReg:
			return []uint8{0x01, d.regNum, 0x10 + s.segNum, 0x10 + s.regNum}, nil
		case kMemSegImm:
			return []uint8{0x01, d.regNum, 0x20 + s.segNum, b(s.imm)}, nil
		}
	case kMemBX:
		switch s.kind {
		case kReg:
			return []uint8{0x01, 0x0c, s.regNum, 0}, nil
		case kImm:
			return []uint8{0x01, 0x0c, 0x0f, b(s.imm)}, nil
		}
	case kMemBP:
		switch s.kind {
		case kReg:
			return []uint8{0x01, 0x0d, s.regNum, 0}, nil
		case kImm:
			return []uint8{0x01, 0x0d, 0x0f, b(s.imm)}, nil
		}
	case kMemDSImm:
		if s.kind == kReg {
			return []uint8{0x01, 0x0e, s.regNum, b(d.imm)}, nil
		}
	case kMemSegReg:
		if s.kind == kReg {
			return []uint8{0x01, 0x10 + d.segNum, 0x10 + d.regNum, s.regNum}, nil
		}
	case kMemSegImm:
		if s.kind == kReg {
			return []uint8{0x01, 0x20 + d.segNum, s.regNum, b(d.imm)}, nil
		}
	}
	return nil, encErr("MOV", "unsupported operand combination")
}

// ADD src  (immediate/mem land in inst[2])
func encAccum(op uint8, ops []operand) ([]uint8, error) {
	if err := need("ADD", ops, 1); err != nil {
		return nil, err
	}
	s := ops[0]
	switch s.kind {
	case kReg:
		return []uint8{op, s.regNum, 0, 0}, nil
	case kMemBX:
		return []uint8{op, 0x0c, 0, 0}, nil
	case kMemBP:
		return []uint8{op, 0x0d, 0, 0}, nil
	case kMemDSImm:
		return []uint8{op, 0x0e, b(s.imm), 0}, nil
	case kImm:
		return []uint8{op, 0x0f, b(s.imm), 0}, nil
	case kMemAbs:
		return []uint8{op, 0xf0, b(s.imm >> 8), b(s.imm)}, nil
	}
	return nil, encErr("ADD", "unsupported operand")
}

// NOT/INC src
func encUnaryMem(op uint8, ops []operand) ([]uint8, error) {
	if err := need("op", ops, 1); err != nil {
		return nil, err
	}
	s := ops[0]
	switch s.kind {
	case kReg:
		return []uint8{op, s.regNum, 0, 0}, nil
	case kMemBX:
		return []uint8{op, 0x0c, 0, 0}, nil
	case kMemBP:
		return []uint8{op, 0x0d, 0, 0}, nil
	case kMemDSImm:
		return []uint8{op, 0x0e, b(s.imm), 0}, nil
	case kMemAbs:
		if op == 0x03 { // NOT supports abs
			return []uint8{op, 0xf0, b(s.imm >> 8), b(s.imm)}, nil
		}
	}
	return nil, encErr("unary", "unsupported operand")
}

// OR/AND/XOR a, b
func encLogic(op uint8, ops []operand) ([]uint8, error) {
	if err := need("logic", ops, 2); err != nil {
		return nil, err
	}
	a, s := ops[0], ops[1]
	switch a.kind {
	case kReg:
		switch s.kind {
		case kReg:
			return []uint8{op, a.regNum, s.regNum, 0}, nil
		case kMemBX:
			return []uint8{op, a.regNum, 0x0c, 0}, nil
		case kMemBP:
			return []uint8{op, a.regNum, 0x0d, 0}, nil
		case kMemDSImm:
			return []uint8{op, a.regNum, 0x0e, b(s.imm)}, nil
		case kImm:
			return []uint8{op, a.regNum, 0x0f, b(s.imm)}, nil
		}
	case kMemBX:
		switch s.kind {
		case kReg:
			return []uint8{op, 0x0c, s.regNum, 0}, nil
		case kImm:
			return []uint8{op, 0x0c, 0x0f, b(s.imm)}, nil
		}
	case kMemBP:
		switch s.kind {
		case kReg:
			return []uint8{op, 0x0d, s.regNum, 0}, nil
		case kImm:
			return []uint8{op, 0x0d, 0x0f, b(s.imm)}, nil
		}
	}
	return nil, encErr("logic", "unsupported operand combination")
}

// SHL/SHR/ROL/ROR src, count  (count in inst[2])
func encShift(op uint8, ops []operand) ([]uint8, error) {
	if err := need("shift", ops, 2); err != nil {
		return nil, err
	}
	s, c := ops[0], ops[1]
	if c.kind != kImm {
		return nil, encErr("shift", "count must be immediate")
	}
	switch s.kind {
	case kReg:
		return []uint8{op, s.regNum, b(c.imm), 0}, nil
	case kMemBX:
		return []uint8{op, 0x0c, b(c.imm), 0}, nil
	case kMemBP:
		return []uint8{op, 0x0d, b(c.imm), 0}, nil
	case kMemDSImm:
		return []uint8{op, 0x0e, b(c.imm), b(s.imm)}, nil
	}
	return nil, encErr("shift", "unsupported operand")
}

func encPush(ops []operand) ([]uint8, error) {
	if err := need("PUSH", ops, 1); err != nil {
		return nil, err
	}
	s := ops[0]
	switch s.kind {
	case kReg:
		return []uint8{0x0b, s.regNum, 0, 0}, nil
	case kMemBX:
		return []uint8{0x0b, 0x0c, 0, 0}, nil
	case kMemBP:
		return []uint8{0x0b, 0x0d, 0, 0}, nil
	case kMemDSImm:
		return []uint8{0x0b, 0x0e, b(s.imm), 0}, nil
	case kImm:
		return []uint8{0x0b, 0x0f, b(s.imm), 0}, nil
	}
	return nil, encErr("PUSH", "unsupported operand")
}

func encPop(ops []operand) ([]uint8, error) {
	if err := need("POP", ops, 1); err != nil {
		return nil, err
	}
	s := ops[0]
	switch s.kind {
	case kReg:
		return []uint8{0x0c, s.regNum, 0, 0}, nil
	case kMemBX:
		return []uint8{0x0c, 0x0c, 0, 0}, nil
	case kMemBP:
		return []uint8{0x0c, 0x0d, 0, 0}, nil
	case kMemDSImm:
		return []uint8{0x0c, 0x0e, b(s.imm), 0}, nil
	}
	return nil, encErr("POP", "unsupported operand")
}

// CMP a, b
func encCmp(ops []operand) ([]uint8, error) {
	if err := need("CMP", ops, 2); err != nil {
		return nil, err
	}
	a, s := ops[0], ops[1]
	switch a.kind {
	case kReg:
		switch s.kind {
		case kReg:
			return []uint8{0x0d, a.regNum, s.regNum, 0}, nil
		case kMemBX:
			return []uint8{0x0d, a.regNum, 0x0c, 0}, nil
		case kMemBP:
			return []uint8{0x0d, a.regNum, 0x0d, 0}, nil
		case kMemDSImm:
			return []uint8{0x0d, a.regNum, 0x0e, b(s.imm)}, nil
		case kImm:
			return []uint8{0x0d, a.regNum, 0x0f, b(s.imm)}, nil
		}
	case kMemBX:
		if s.kind == kImm {
			return []uint8{0x0d, 0x0c, b(s.imm), 0}, nil
		}
	case kMemBP:
		if s.kind == kImm {
			return []uint8{0x0d, 0x0d, b(s.imm), 0}, nil
		}
	case kMemDSImm:
		if s.kind == kImm {
			return []uint8{0x0d, 0x0e, b(a.imm), b(s.imm)}, nil
		}
	}
	return nil, encErr("CMP", "unsupported operand combination")
}

// conditional / short relative jumps: offset relative to THIS instruction.
func encCond(op uint8, ops []operand, curAddr int) ([]uint8, error) {
	if err := need("Jcc", ops, 1); err != nil {
		return nil, err
	}
	off, err := relOffset(ops[0], curAddr)
	if err != nil {
		return nil, err
	}
	return []uint8{op, b(off), 0, 0}, nil
}

// relOffset computes a signed 8-bit offset from curAddr to the operand target.
// A symbolic operand (label or $-expression) is an absolute address, so the
// delta is target-curAddr. A raw numeric literal (e.g. "JZ +8") is already the
// relative offset and is used verbatim — matching the raw bytes in the ISA.
func relOffset(o operand, curAddr int) (int, error) {
	if o.kind != kImm {
		return 0, encErr("rel", "target must be a label/immediate")
	}
	d := o.imm
	if o.symbolic {
		d = o.imm - curAddr
	}
	if d < -128 || d > 127 {
		return 0, encErr("rel", fmt.Sprintf("relative offset %d out of range (-128..127)", d))
	}
	return d, nil
}

func encIn(ops []operand) ([]uint8, error) {
	if err := need("IN", ops, 2); err != nil {
		return nil, err
	}
	d, p := ops[0], ops[1]
	if d.kind != kReg {
		return nil, encErr("IN", "destination must be a register")
	}
	switch p.kind {
	case kReg:
		return []uint8{0x16, d.regNum, p.regNum, 0}, nil
	case kImm:
		return []uint8{0x16, d.regNum, b(p.imm), 0}, nil
	}
	return nil, encErr("IN", "port must be register or immediate")
}

func encOut(ops []operand) ([]uint8, error) {
	if err := need("OUT", ops, 2); err != nil {
		return nil, err
	}
	p, s := ops[0], ops[1]
	if s.kind != kReg {
		return nil, encErr("OUT", "source must be a register")
	}
	switch p.kind {
	case kReg:
		return []uint8{0x17, p.regNum, s.regNum, 0}, nil
	case kImm:
		return []uint8{0x17, b(p.imm), s.regNum, 0}, nil
	}
	return nil, encErr("OUT", "port must be register or immediate")
}

// LST PAGE|IDT, [bx]
func encLst(ops []operand) ([]uint8, error) {
	if err := need("LST", ops, 2); err != nil {
		return nil, err
	}
	t, m := ops[0], ops[1]
	if t.kind != kImm {
		return nil, encErr("LST", "table type must be 0(PAGE) or 1(IDT)")
	}
	if m.kind != kMemBX {
		return nil, encErr("LST", "source must be [bx]")
	}
	return []uint8{0x23, b(t.imm), 0x0c, 0}, nil
}

// JMP with all modes (near/far/relative/register/memory).
func encJmp(ops []operand, curAddr int) ([]uint8, error) {
	return encJmp2(0x0e, ops, curAddr)
}

// encJmp2 handles both JMP (0x0e) and CALL (0x20), which share the same mode map.
func encJmp2(op uint8, ops []operand, curAddr int) ([]uint8, error) {
	mn := "JMP"
	if op == 0x20 {
		mn = "CALL"
	}
	// two-operand far register form: JMP [seg:addr]-style is handled below;
	// "seg:off" immediate far form is 1 operand written as far imm:imm.
	if len(ops) == 1 {
		o := ops[0]
		// explicit relative form: JMP rel <label/disp> -> mode 0x0f
		if o.rel {
			if o.kind != kImm {
				return nil, encErr(mn, "rel form needs a label or displacement")
			}
			off, err := relOffset(o, curAddr)
			if err != nil {
				return nil, err
			}
			return []uint8{op, 0x0f, b(off), 0}, nil
		}
		switch o.kind {
		case kReg:
			if o.far {
				return nil, encErr(mn, "far register form needs two registers: far seg, addr")
			}
			return []uint8{op, o.regNum, 0, 0}, nil
		case kMemBX:
			if o.far {
				return []uint8{op, 0x1c, 0, 0}, nil
			}
			return []uint8{op, 0x0c, 0, 0}, nil
		case kMemBP:
			if o.far {
				return []uint8{op, 0x1d, 0, 0}, nil
			}
			return []uint8{op, 0x0d, 0, 0}, nil
		case kMemDSImm:
			if o.far {
				return []uint8{op, 0x1e, b(o.imm), 0}, nil
			}
			return []uint8{op, 0x0e, b(o.imm), 0}, nil
		case kMemAbs:
			if o.far {
				return []uint8{op, 0xf1, b(o.imm >> 8), b(o.imm)}, nil
			}
			return []uint8{op, 0xf0, b(o.imm >> 8), b(o.imm)}, nil
		case kImm:
			// Jump/call to an absolute address. If the target lies in the
			// same 256-byte segment as this instruction, a near jump (mode
			// 1f: low byte only, cs unchanged) is enough. Otherwise emit a
			// far jump (mode 2f) that also loads cs, so control transfers
			// across segment boundaries correctly.
			if (o.imm >> 8) == (curAddr >> 8) {
				return []uint8{op, 0x1f, b(o.imm), 0}, nil
			}
			return []uint8{op, 0x2f, b(o.imm >> 8), b(o.imm)}, nil
		}
	}
	// far seg:off  ->  JMP far <seg>, <off>
	if len(ops) == 2 && ops[0].far && ops[0].kind == kImm && ops[1].kind == kImm {
		return []uint8{op, 0x2f, b(ops[0].imm), b(ops[1].imm)}, nil
	}
	// far register pair: JMP far <segreg>, <addrreg>
	if len(ops) == 2 && ops[0].far && ops[0].kind == kReg && ops[1].kind == kReg {
		return []uint8{op, 0x10 + ops[0].regNum, 0x10 + ops[1].regNum, 0}, nil
	}
	// relative: JMP rel <disp>  (explicit relative)
	return nil, encErr(mn, "unsupported jump/call form")
}

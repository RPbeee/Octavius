package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Register name -> number (0x00-0x0b), matching the emulator.
var regNames = map[string]uint8{
	"ip": 0x0, "ax": 0x1, "bx": 0x2, "cx": 0x3, "dx": 0x4,
	"bp": 0x5, "sp": 0x6, "cs": 0x7, "ss": 0x8, "ds": 0x9,
	"di": 0xa, "flag": 0xb,
}

type opKind int

const (
	kReg       opKind = iota // a register            -> regNum
	kImm                     // immediate / label     -> imm (8-bit) ; wantSeg for far
	kMemBX                   // [bx]        (ds:bx)   mode 0x0c
	kMemBP                   // [bp]        (ss:bp)   mode 0x0d
	kMemDSImm                // [imm8]      (ds:imm)  mode 0x0e
	kMemSegReg               // [seg:reg]             mode 0x10+seg / 0x10+reg
	kMemSegImm               // [seg:imm8]            mode 0x20+seg , imm
	kMemAbs                  // abs[imm16]            mode 0xf0/0xf1
)

type operand struct {
	kind     opKind
	regNum   uint8  // for kReg / kMemSegReg (address reg)
	segNum   uint8  // for kMemSegReg / kMemSegImm
	imm      int    // resolved immediate value (may be a full 16-bit for kMemAbs)
	label    string // unresolved symbol; resolved in pass 2
	far      bool   // "far" prefix on a memory/jump operand
	rel      bool   // "rel" prefix on a jump operand (force relative mode)
	symbolic bool   // imm came from a label/symbol (an absolute address), not a raw literal
}

// parseOperand turns one textual operand into an operand struct.
// Symbol resolution is deferred: if a value is a label it is stored in .label.
func parseOperand(tok string, syms map[string]int) (operand, error) {
	tok = strings.TrimSpace(tok)
	far := false
	rel := false
	if strings.HasPrefix(strings.ToLower(tok), "far ") {
		far = true
		tok = strings.TrimSpace(tok[4:])
	} else if strings.HasPrefix(strings.ToLower(tok), "rel ") {
		rel = true
		tok = strings.TrimSpace(tok[4:])
	}

	// Memory operand [...]
	if strings.HasPrefix(tok, "[") && strings.HasSuffix(tok, "]") {
		inner := strings.TrimSpace(tok[1 : len(tok)-1])
		low := strings.ToLower(inner)

		if low == "bx" {
			return operand{kind: kMemBX, far: far}, nil
		}
		if low == "bp" {
			return operand{kind: kMemBP, far: far}, nil
		}
		// [seg:xxx]
		if i := strings.Index(inner, ":"); i >= 0 {
			segTok := strings.ToLower(strings.TrimSpace(inner[:i]))
			addrTok := strings.TrimSpace(inner[i+1:])
			seg, ok := regNames[segTok]
			if !ok {
				return operand{}, fmt.Errorf("invalid segment %q", segTok)
			}
			if r, ok := regNames[strings.ToLower(addrTok)]; ok {
				return operand{kind: kMemSegReg, segNum: seg, regNum: r, far: far}, nil
			}
			v, err := resolveVal(addrTok, syms)
			if err != nil {
				return operand{}, err
			}
			if v.label != "" {
				return operand{kind: kMemSegImm, segNum: seg, label: v.label, far: far}, nil
			}
			return operand{kind: kMemSegImm, segNum: seg, imm: v.imm, far: far}, nil
		}
		// abs[...] handled by caller via "abs" prefix; here bare [imm]
		v, err := resolveVal(inner, syms)
		if err != nil {
			return operand{}, err
		}
		return operand{kind: kMemDSImm, imm: v.imm, label: v.label, far: far}, nil
	}

	// abs[imm16]  (absolute 16-bit memory)
	if strings.HasPrefix(strings.ToLower(tok), "abs[") && strings.HasSuffix(tok, "]") {
		inner := tok[4 : len(tok)-1]
		v, err := resolveVal(inner, syms)
		if err != nil {
			return operand{}, err
		}
		return operand{kind: kMemAbs, imm: v.imm, label: v.label, far: far}, nil
	}

	// Register
	if r, ok := regNames[strings.ToLower(tok)]; ok {
		return operand{kind: kReg, regNum: r, far: far, rel: rel}, nil
	}

	// Immediate / label
	v, err := resolveVal(tok, syms)
	if err != nil {
		return operand{}, err
	}
	return operand{kind: kImm, imm: v.imm, label: v.label, far: far, rel: rel, symbolic: v.symbolic}, nil
}

type valRef struct {
	imm      int
	label    string
	symbolic bool // resolved from a symbol name (absolute address) rather than a literal
}

// resolveVal parses a numeric literal, char literal, or (deferred) symbol.
func resolveVal(tok string, syms map[string]int) (valRef, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return valRef{}, fmt.Errorf("empty value")
	}
	// char literal 'A'
	if len(tok) >= 3 && tok[0] == '\'' && tok[len(tok)-1] == '\'' {
		s := tok[1 : len(tok)-1]
		if s == "\\n" {
			return valRef{imm: '\n'}, nil
		}
		if s == "\\r" {
			return valRef{imm: '\r'}, nil
		}
		if s == "\\0" {
			return valRef{imm: 0}, nil
		}
		if len(s) == 1 {
			return valRef{imm: int(s[0])}, nil
		}
		return valRef{}, fmt.Errorf("bad char literal %q", tok)
	}
	// low(sym) / high(sym) helpers
	for _, fn := range []string{"low", "high", "off", "seg"} {
		if strings.HasPrefix(strings.ToLower(tok), fn+"(") && strings.HasSuffix(tok, ")") {
			inner := tok[len(fn)+1 : len(tok)-1]
			v, err := resolveVal(inner, syms)
			if err != nil {
				return valRef{}, err
			}
			if v.label != "" {
				return valRef{}, fmt.Errorf("%s() needs a resolved value here", fn)
			}
			switch fn {
			case "low", "off":
				return valRef{imm: v.imm & 0xff}, nil
			case "high", "seg":
				return valRef{imm: (v.imm >> 8) & 0xff}, nil
			}
		}
	}
	// "$" = current address, "$+N" / "$-N" = current address +/- N.
	// The current address is injected into syms under the key "$".
	if strings.HasPrefix(tok, "$") {
		cur, ok := syms["$"]
		if !ok {
			return valRef{}, fmt.Errorf("'$' used but current address unknown")
		}
		rest := strings.TrimSpace(tok[1:])
		if rest == "" {
			return valRef{imm: cur, symbolic: true}, nil
		}
		n, ok := parseNum(rest)
		if !ok {
			return valRef{}, fmt.Errorf("bad $ expression %q", tok)
		}
		return valRef{imm: cur + n, symbolic: true}, nil
	}
	// numeric?
	if n, ok := parseNum(tok); ok {
		return valRef{imm: n}, nil
	}
	// symbol: resolve now if known, else defer
	if v, ok := syms[tok]; ok {
		return valRef{imm: v, symbolic: true}, nil
	}
	return valRef{label: tok}, nil
}

func parseNum(tok string) (int, bool) {
	tok = strings.TrimSpace(tok)
	neg := false
	if strings.HasPrefix(tok, "-") {
		neg = true
		tok = tok[1:]
	} else if strings.HasPrefix(tok, "+") {
		tok = tok[1:]
	}
	var n int64
	var err error
	switch {
	case strings.HasPrefix(tok, "0x"), strings.HasPrefix(tok, "0X"):
		n, err = strconv.ParseInt(tok[2:], 16, 64)
	case strings.HasPrefix(tok, "0b"), strings.HasPrefix(tok, "0B"):
		n, err = strconv.ParseInt(tok[2:], 2, 64)
	default:
		if _, e := strconv.Atoi(tok); e != nil {
			return 0, false
		}
		n, err = strconv.ParseInt(tok, 10, 64)
	}
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return int(n), true
}

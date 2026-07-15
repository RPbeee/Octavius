// oasm — assembler + linker for the Octavius CPU emulator.
//
//	oasm asm  <in.s> [-o out.bin] [-org N]
//	oasm link -o floppy.img file.bin@OFFSET [file.bin@OFFSET ...]
//
// Every instruction assembles to exactly 4 bytes. See README.md for syntax.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "asm":
		if err := cmdAsm(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "link":
		if err := cmdLink(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `oasm — Octavius assembler & linker

Usage:
  oasm asm  <input.s> [-o output.bin] [-org N]
      Assemble Intel-style source into a flat binary.
      -org N   base address for label math (default 0; use 0x7c00 for boot code)

  oasm link -o <image.img> <file.bin@OFFSET> [<file.bin@OFFSET> ...]
      Combine binaries into a 1.44MiB bootable floppy image.
      OFFSET is a byte offset into the image (e.g. boot.bin@0, kernel.bin@512).
      Sector S of cylinder C, head H lives at ((C*2+H)*18+S)*512.
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "oasm: "+err.Error())
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// asm subcommand
// ---------------------------------------------------------------------------

func cmdAsm(args []string) error {
	var inFile, outFile string
	org := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			i++
			if i >= len(args) {
				return fmt.Errorf("-o needs an argument")
			}
			outFile = args[i]
		case "-org":
			i++
			if i >= len(args) {
				return fmt.Errorf("-org needs an argument")
			}
			n, ok := parseNum(args[i])
			if !ok {
				return fmt.Errorf("bad -org value %q", args[i])
			}
			org = n
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag %q", args[i])
			}
			inFile = args[i]
		}
	}
	if inFile == "" {
		return fmt.Errorf("no input file")
	}
	if outFile == "" {
		outFile = strings.TrimSuffix(inFile, ".s") + ".bin"
	}

	src, err := os.ReadFile(inFile)
	if err != nil {
		return err
	}
	out, base, err := assemble(string(src), org)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outFile, out, 0644); err != nil {
		return err
	}
	fmt.Printf("assembled %s -> %s (%d bytes, base=0x%04x)\n", inFile, outFile, len(out), base)
	return nil
}

// a parsed source line kept between the two passes.
type line struct {
	num  int    // 1-based source line number
	addr int    // address assigned in pass 1
	kind string // "inst" or "data"
	mn   string // mnemonic (upper) for inst
	ops  string // raw operand text for inst
	data []dataItem
}

type dataItem struct {
	str  string // non-empty => raw string bytes
	expr string // else => a single-byte expression
}

func assemble(src string, org int) ([]byte, int, error) {
	syms := map[string]int{}
	var lines []line
	addr := org

	// ---- Pass 1: layout + collect symbols ----
	for i, raw := range strings.Split(src, "\n") {
		ln := stripComment(raw)
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		// leading labels ("name:" possibly several / inline before an instruction)
		for {
			if c := strings.IndexByte(ln, ':'); c >= 0 && isLabelName(ln[:c]) {
				name := strings.TrimSpace(ln[:c])
				if _, dup := syms[name]; dup {
					return nil, 0, fmt.Errorf("line %d: duplicate label %q", i+1, name)
				}
				syms[name] = addr
				ln = strings.TrimSpace(ln[c+1:])
				if ln == "" {
					break
				}
				continue
			}
			break
		}
		if ln == "" {
			continue
		}

		// directives
		fields := strings.Fields(ln)
		head := strings.ToLower(fields[0])
		switch head {
		case ".org":
			if len(fields) != 2 {
				return nil, 0, fmt.Errorf("line %d: .org needs one value", i+1)
			}
			n, ok := parseNum(fields[1])
			if !ok {
				return nil, 0, fmt.Errorf("line %d: bad .org value", i+1)
			}
			addr = n
			org = n
			continue
		case "equ", ".equ":
			// NAME equ VALUE  (also allow ".equ NAME VALUE")
			return nil, 0, fmt.Errorf("line %d: use 'NAME = VALUE' for constants", i+1)
		case "db", ".db":
			items, err := splitDataItems(strings.TrimSpace(ln[len(fields[0]):]))
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: %v", i+1, err)
			}
			sz := 0
			for _, it := range items {
				if it.str != "" {
					sz += len(it.str)
				} else {
					sz++
				}
			}
			lines = append(lines, line{num: i + 1, addr: addr, kind: "data", data: items})
			addr += sz
			continue
		}

		// constant assignment:  NAME = VALUE
		if len(fields) >= 3 && fields[1] == "=" {
			v, ok := parseNum(fields[2])
			if !ok {
				// allow referencing an earlier symbol
				if sv, ok2 := syms[fields[2]]; ok2 {
					v = sv
				} else {
					return nil, 0, fmt.Errorf("line %d: bad constant value %q", i+1, fields[2])
				}
			}
			syms[fields[0]] = v
			continue
		}

		// instruction
		mn := strings.ToUpper(fields[0])
		ops := strings.TrimSpace(ln[len(fields[0]):])
		lines = append(lines, line{num: i + 1, addr: addr, kind: "inst", mn: mn, ops: ops})
		addr += 4
	}

	// ---- Pass 2: encode ----
	var out []byte
	base := org
	// Determine the true base (first emitted address) to size the buffer.
	if len(lines) > 0 {
		base = lines[0].addr
	}
	for _, l := range lines {
		// pad gaps (e.g. after .org jumps) with zeros
		want := l.addr - base
		for len(out) < want {
			out = append(out, 0)
		}
		switch l.kind {
		case "data":
			bytes, err := encodeData(l.data, syms)
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: %v", l.num, err)
			}
			out = append(out, bytes...)
		case "inst":
			syms["$"] = l.addr
			ops, err := parseOperands(l.ops, syms)
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: %v", l.num, err)
			}
			enc, err := encode(l.mn, ops, l.addr)
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: %v", l.num, err)
			}
			out = append(out, enc...)
		}
	}
	return out, base, nil
}

func encodeData(items []dataItem, syms map[string]int) ([]byte, error) {
	var out []byte
	for _, it := range items {
		if it.str != "" {
			out = append(out, []byte(it.str)...)
			continue
		}
		v, err := resolveVal(it.expr, syms)
		if err != nil {
			return nil, err
		}
		if v.label != "" {
			return nil, fmt.Errorf("undefined symbol %q", v.label)
		}
		out = append(out, byte(v.imm&0xff))
	}
	return out, nil
}

// parseOperands splits an operand string on top-level commas (ignoring commas
// inside [...]) and parses each into an operand.
func parseOperands(s string, syms map[string]int) ([]operand, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	var ops []operand
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		o, err := parseOperand(p, syms)
		if err != nil {
			return nil, err
		}
		if o.label != "" {
			return nil, fmt.Errorf("undefined symbol %q", o.label)
		}
		ops = append(ops, o)
	}
	return ops, nil
}

func stripComment(s string) string {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		return s[:i]
	}
	return s
}

func isLabelName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t[]:,") {
		return false
	}
	// don't treat "ds:bx" style or numeric as a label
	if _, ok := parseNum(s); ok {
		return false
	}
	return true
}

// splitDataItems parses the arguments of a db directive into items,
// respecting quoted strings.
func splitDataItems(s string) ([]dataItem, error) {
	var items []dataItem
	i := 0
	for i < len(s) {
		// skip whitespace and commas
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '"' {
			j := i + 1
			var sb strings.Builder
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					switch s[j] {
					case 'n':
						sb.WriteByte('\n')
					case 'r':
						sb.WriteByte('\r')
					case 't':
						sb.WriteByte('\t')
					case '0':
						sb.WriteByte(0)
					case '\\':
						sb.WriteByte('\\')
					case '"':
						sb.WriteByte('"')
					default:
						sb.WriteByte(s[j])
					}
				} else {
					sb.WriteByte(s[j])
				}
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("unterminated string")
			}
			if sb.Len() > 0 {
				items = append(items, dataItem{str: sb.String()})
			}
			i = j + 1
			continue
		}
		// bare token until comma
		j := i
		for j < len(s) && s[j] != ',' {
			j++
		}
		tok := strings.TrimSpace(s[i:j])
		if tok != "" {
			items = append(items, dataItem{expr: tok})
		}
		i = j
	}
	return items, nil
}

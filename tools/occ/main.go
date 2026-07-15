// occ — a tiny C-subset compiler for the Octavius CPU.
//
//	occ <in.c> [-o out.s]
//
// It emits oasm (Intel-syntax) assembly, which you then assemble and link:
//
//	go build -o occ  ./tools/occ
//	go build -o oasm ./tools/oasm
//	./occ  prog.c -o prog.s
//	./oasm asm  prog.s -o prog.bin
//	./oasm link -o floppy.img prog.bin@0
//
// The Octavius machine is an 8-bit CPU, so this is deliberately a *subset* of
// C, not standard C. See tools/occ/README.md for the language and its limits.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	in, out := "", ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				fatal("-o needs an argument")
			}
			i++
			out = args[i]
		default:
			if in != "" {
				fatal("only one input file is supported")
			}
			in = args[i]
		}
	}
	if in == "" {
		fmt.Fprintln(os.Stderr, "usage: occ <in.c> [-o out.s]")
		os.Exit(2)
	}
	toks := lexFile(in)
	p := &parser{toks: toks}
	prog := p.parseProgram()
	asm := gen(prog)

	if out == "" {
		fmt.Print(asm)
		return
	}
	if err := os.WriteFile(out, []byte(asm), 0o644); err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "occ: "+msg)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tStr   // string literal, decoded bytes in .s
	tPunct // operators and punctuation, value in .s
)

type token struct {
	kind tokKind
	s    string
	n    int
	line int
}

var keywords = map[string]bool{
	"int": true, "char": true, "void": true, "struct": true,
	"if": true, "else": true, "while": true, "for": true, "return": true,
	"break": true, "continue": true, "sizeof": true,
}

// scanTokens tokenizes src into raw tokens. It does NOT interpret preprocessor
// directives (it emits `#` as an ordinary token) and does NOT append a tEOF;
// the preprocessor handles both. startLine is the 1-based line of the first
// character.
func scanTokens(src string, startLine int) []token {
	var toks []token
	line := startLine
	i := 0
	n := len(src)
	push := func(k tokKind, s string, num int) {
		toks = append(toks, token{kind: k, s: s, n: num, line: line})
	}
	isIdentStart := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }

	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '\'':
			// char literal, supports a few escapes
			i++
			if i >= n {
				lexErr(line, "unterminated char literal")
			}
			var v int
			if src[i] == '\\' {
				i++
				switch src[i] {
				case 'n':
					v = '\n'
				case 'r':
					v = '\r'
				case 't':
					v = '\t'
				case '0':
					v = 0
				case '\\':
					v = '\\'
				case '\'':
					v = '\''
				default:
					v = int(src[i])
				}
			} else {
				v = int(src[i])
			}
			i++
			if i >= n || src[i] != '\'' {
				lexErr(line, "unterminated char literal")
			}
			i++
			push(tNum, "", v&0xff)
		case c == '"':
			// string literal, same escapes as char literals
			i++
			var sb []byte
			for i < n && src[i] != '"' {
				if src[i] == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n':
						sb = append(sb, '\n')
					case 'r':
						sb = append(sb, '\r')
					case 't':
						sb = append(sb, '\t')
					case '0':
						sb = append(sb, 0)
					default:
						sb = append(sb, src[i])
					}
				} else {
					if src[i] == '\n' {
						line++
					}
					sb = append(sb, src[i])
				}
				i++
			}
			if i >= n {
				lexErr(line, "unterminated string literal")
			}
			i++ // closing quote
			push(tStr, string(sb), 0)
		case isDigit(c):
			j := i
			base := 10
			if c == '0' && i+1 < n && (src[i+1] == 'x' || src[i+1] == 'X') {
				base = 16
				j = i + 2
			} else if c == '0' && i+1 < n && (src[i+1] == 'b' || src[i+1] == 'B') {
				base = 2
				j = i + 2
			}
			start := j
			for j < n && isHexish(src[j]) {
				j++
			}
			val := 0
			for _, ch := range src[start:j] {
				val = val*base + digitVal(byte(ch))
			}
			if j == start {
				val = 0 // lone "0"
			}
			i = j
			push(tNum, "", val)
		case isIdentStart(c):
			j := i
			for j < n && (isIdentStart(src[j]) || isDigit(src[j])) {
				j++
			}
			push(tIdent, src[i:j], 0)
			i = j
		default:
			// multi-char operators first
			two := ""
			if i+1 < n {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "<<", ">>", "&&", "||", "->":
				push(tPunct, two, 0)
				i += 2
				continue
			}
			switch c {
			case '#',
				'+', '-', '*', '/', '%', '&', '|', '^', '~', '!',
				'<', '>', '=', '(', ')', '{', '}', '[', ']', '.', ';', ',':
				push(tPunct, string(c), 0)
				i++
			default:
				lexErr(line, fmt.Sprintf("unexpected character %q", string(c)))
			}
		}
	}
	return toks
}

// ---------------------------------------------------------------------------
// Preprocessor: #include "file" and object-like #define NAME value
// ---------------------------------------------------------------------------

// lexFile reads path, resolves #include directives, tokenizes, then expands
// #define macros, producing the token stream the parser consumes.
func lexFile(path string) []token {
	text := assemble(path, nil)
	return preprocess(scanTokens(text, 1))
}

// assemble returns the source of path with every `#include "f"` line replaced
// by the assembled contents of f (resolved relative to the including file).
// #define lines are left in place for the token pass to handle.
func assemble(path string, stack []string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		fatal(err.Error())
	}
	for _, p := range stack {
		if p == abs {
			fatal("#include cycle through " + path)
		}
	}
	stack = append(stack, abs)
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err.Error())
	}
	dir := filepath.Dir(abs)
	var out strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#include") {
			rest := strings.TrimSpace(trimmed[len("#include"):])
			if len(rest) < 2 || rest[0] != '"' {
				fatal(`bad #include (use #include "file"): ` + trimmed)
			}
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				fatal("unterminated #include: " + trimmed)
			}
			out.WriteString(assemble(filepath.Join(dir, rest[1:1+end]), stack))
			out.WriteByte('\n')
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// preprocess consumes raw tokens, records object-like #define macros, expands
// macro uses, drops directive lines, and appends the final tEOF.
func preprocess(raw []token) []token {
	defines := map[string][]token{}
	var out []token
	for i := 0; i < len(raw); {
		t := raw[i]
		// A directive is a `#` that begins its line.
		if t.s == "#" && (i == 0 || raw[i-1].line != t.line) {
			j := i + 1
			var dir []token
			for j < len(raw) && raw[j].line == t.line {
				dir = append(dir, raw[j])
				j++
			}
			handleDirective(dir, defines, t.line)
			i = j
			continue
		}
		if t.kind == tIdent {
			for _, e := range expandIdent(t.s, defines, map[string]bool{}) {
				e.line = t.line
				out = append(out, e)
			}
		} else {
			out = append(out, t)
		}
		i++
	}
	endLine := 1
	if len(raw) > 0 {
		endLine = raw[len(raw)-1].line
	}
	return append(out, token{kind: tEOF, line: endLine})
}

func handleDirective(dir []token, defines map[string][]token, line int) {
	if len(dir) == 0 {
		fatal(fmt.Sprintf("line %d: empty preprocessor directive", line))
	}
	switch dir[0].s {
	case "define":
		if len(dir) < 2 || dir[1].kind != tIdent {
			fatal(fmt.Sprintf("line %d: #define needs a name", line))
		}
		defines[dir[1].s] = dir[2:] // remaining tokens are the replacement
	case "include":
		fatal(fmt.Sprintf("line %d: #include must be on its own line", line))
	default:
		fatal(fmt.Sprintf("line %d: unknown directive #%s", line, dir[0].s))
	}
}

// expandIdent replaces a macro name with its (recursively expanded) tokens.
// active guards against infinite expansion of self- or mutually-referential
// macros.
func expandIdent(name string, defines map[string][]token, active map[string]bool) []token {
	rep, ok := defines[name]
	if !ok || active[name] {
		return []token{{kind: tIdent, s: name}}
	}
	active[name] = true
	var out []token
	for _, t := range rep {
		if t.kind == tIdent {
			out = append(out, expandIdent(t.s, defines, active)...)
		} else {
			out = append(out, t)
		}
	}
	delete(active, name)
	return out
}

func isHexish(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}
func lexErr(line int, msg string) {
	fatal(fmt.Sprintf("line %d: %s", line, msg))
}

// ---------------------------------------------------------------------------
// AST
// ---------------------------------------------------------------------------

type program struct {
	globals []*varDecl
	funcs   []*funcDecl
}

// ctype is a source-level type. Values are computed as 16-bit; a pointer is a
// 16-bit linear address (segment:offset packed as high:low), so its size is 2.
type ctype struct {
	kind   string  // "char", "int", "void", "ptr", "array", "struct"
	elem   *ctype  // for ptr/array
	n      int     // element count for array
	tag    string  // struct tag name
	fields []field // struct members (with computed offsets)
}

type field struct {
	name string
	typ  *ctype
	off  int // byte offset within the struct
}

var (
	tChar = &ctype{kind: "char"}
	tInt  = &ctype{kind: "int"}
	tVoid = &ctype{kind: "void"}
)

func ptrTo(t *ctype) *ctype          { return &ctype{kind: "ptr", elem: t} }
func arrayOf(n int, t *ctype) *ctype { return &ctype{kind: "array", elem: t, n: n} }

func baseType(name string) *ctype {
	switch name {
	case "char":
		return tChar
	case "void":
		return tVoid
	default:
		return tInt
	}
}

func (t *ctype) isPtr() bool   { return t.kind == "ptr" }
func (t *ctype) isArray() bool { return t.kind == "array" }

// sizeOf is the storage size in bytes.
func sizeOf(t *ctype) int {
	switch t.kind {
	case "char":
		return 1
	case "array":
		return t.n * sizeOf(t.elem)
	case "struct":
		total := 0
		for _, f := range t.fields {
			total += sizeOf(f.typ)
		}
		return total
	default: // int, ptr
		return 2
	}
}

// decay converts an array type to a pointer-to-element in value contexts.
func decay(t *ctype) *ctype {
	if t.isArray() {
		return ptrTo(t.elem)
	}
	return t
}

type varDecl struct {
	name string
	typ  *ctype
	init *node // may be nil
}

type param struct {
	name string
	typ  *ctype
}

type funcDecl struct {
	name   string
	ret    *ctype
	params []param
	body   []*node
}

// node is a tagged union for both statements and expressions.
type node struct {
	kind string
	// expressions
	num   int
	name  string
	op    string
	str   string // decoded bytes for a "str" literal
	arrow bool   // member access via -> (true) vs . (false)
	typ   *ctype // declared type for a "decl" statement
	lhs   *node
	rhs   *node
	args  []*node
	// statements
	init *node   // for return/expr/decl; also the init clause of a for
	cond *node   // if/while/for
	post *node   // for post-expression
	then []*node // if-then / while-body / for-body
	els  []*node // if-else
	line int
}

// ---------------------------------------------------------------------------
// Parser (recursive descent + precedence climbing)
// ---------------------------------------------------------------------------

type parser struct {
	toks    []token
	pos     int
	structs map[string]*ctype // struct tag -> type
}

// structType returns the (possibly forward-declared) type for a struct tag.
func (p *parser) structType(tag string) *ctype {
	if p.structs == nil {
		p.structs = map[string]*ctype{}
	}
	t := p.structs[tag]
	if t == nil {
		t = &ctype{kind: "struct", tag: tag}
		p.structs[tag] = t
	}
	return t
}

// parseBaseType parses a base type: `struct Tag` or int/char/void.
func (p *parser) parseBaseType() *ctype {
	if p.at("struct") {
		p.next()
		tag := p.next()
		if tag.kind != tIdent {
			p.err("expected struct tag")
		}
		return p.structType(tag.s)
	}
	return baseType(p.next().s)
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) at(s string) bool {
	t := p.peek()
	return (t.kind == tPunct || t.kind == tIdent) && t.s == s
}
func (p *parser) accept(s string) bool {
	if p.at(s) {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expect(s string) token {
	if !p.at(s) {
		p.err(fmt.Sprintf("expected %q, got %q", s, p.peek().s))
	}
	return p.next()
}
func (p *parser) err(msg string) {
	fatal(fmt.Sprintf("line %d: %s", p.peek().line, msg))
}

func isTypeName(s string) bool {
	return s == "int" || s == "char" || s == "void" || s == "struct"
}

func (p *parser) parseProgram() *program {
	prog := &program{}
	for p.peek().kind != tEOF {
		// A `struct Tag { ... };` definition at top level.
		if p.at("struct") && p.toks[p.pos+2].kind == tPunct && p.toks[p.pos+2].s == "{" {
			p.parseStructDef()
			continue
		}
		// type name
		if p.peek().kind != tIdent || !isTypeName(p.peek().s) {
			p.err("expected a type (int/char/void/struct) at top level")
		}
		t := p.parseStars(p.parseBaseType())
		nameTok := p.next()
		if nameTok.kind != tIdent {
			p.err("expected identifier after type")
		}
		if p.at("(") {
			prog.funcs = append(prog.funcs, p.parseFunc(nameTok.s, t))
		} else {
			// global variable (possibly an array)
			t = p.parseArraySuffix(t)
			vd := &varDecl{name: nameTok.s, typ: t}
			if p.accept("=") {
				vd.init = p.parseInitializer()
			}
			inferArraySize(t, vd.init)
			p.expect(";")
			prog.globals = append(prog.globals, vd)
		}
	}
	return prog
}

// parseStructDef parses `struct Tag { type name; ... };` and records the type,
// computing each field's byte offset (no alignment padding — the machine is
// byte-addressable).
func (p *parser) parseStructDef() {
	p.expect("struct")
	tag := p.next()
	if tag.kind != tIdent {
		p.err("expected struct tag")
	}
	st := p.structType(tag.s)
	if len(st.fields) > 0 {
		p.err("struct " + tag.s + " redefined")
	}
	p.expect("{")
	off := 0
	for !p.at("}") {
		ft := p.parseStars(p.parseBaseType())
		fn := p.next()
		if fn.kind != tIdent {
			p.err("expected field name")
		}
		ft = p.parseArraySuffix(ft)
		p.expect(";")
		st.fields = append(st.fields, field{name: fn.s, typ: ft, off: off})
		off += sizeOf(ft)
	}
	p.expect("}")
	p.expect(";")
}

// parseStars consumes any number of leading `*` and wraps the type in pointers.
func (p *parser) parseStars(t *ctype) *ctype {
	for p.accept("*") {
		t = ptrTo(t)
	}
	return t
}

// parseArraySuffix consumes an optional `[N]` (or `[]`, whose size -1 is
// inferred from an initializer) and wraps the type in an array.
func (p *parser) parseArraySuffix(t *ctype) *ctype {
	if p.accept("[") {
		if p.accept("]") {
			return arrayOf(-1, t) // size inferred from initializer
		}
		nt := p.next()
		if nt.kind != tNum {
			p.err("array size must be a constant number")
		}
		p.expect("]")
		t = arrayOf(nt.n, t)
	}
	return t
}

// parseInitializer parses the right-hand side of `= …` in a declaration: a
// brace list `{a, b, …}` or a single expression (which may be a string literal).
func (p *parser) parseInitializer() *node {
	if p.at("{") {
		line := p.peek().line
		p.next()
		nd := &node{kind: "initlist", line: line}
		for !p.at("}") {
			nd.args = append(nd.args, p.parseExpr())
			if !p.accept(",") {
				break
			}
		}
		p.expect("}")
		return nd
	}
	return p.parseExpr()
}

// inferArraySize fills in an unsized array's length from its initializer.
func inferArraySize(t *ctype, init *node) {
	if !t.isArray() || t.n >= 0 {
		return
	}
	if init == nil {
		fatal("array declared with [] needs an initializer")
	}
	switch init.kind {
	case "str":
		t.n = len(init.str) + 1 // room for the trailing NUL
	case "initlist":
		t.n = len(init.args)
	default:
		fatal("array declared with [] needs a string or {…} initializer")
	}
}

func (p *parser) parseFunc(name string, ret *ctype) *funcDecl {
	fn := &funcDecl{name: name, ret: ret}
	p.expect("(")
	for !p.at(")") {
		if !(p.peek().kind == tIdent && isTypeName(p.peek().s)) {
			break
		}
		pt := p.parseStars(p.parseBaseType())
		if pt.kind == "void" {
			break
		}
		pn := p.next()
		if pn.kind != tIdent {
			p.err("expected parameter name")
		}
		pt = p.parseArraySuffix(pt)
		if pt.isArray() { // array parameters decay to pointers
			pt = ptrTo(pt.elem)
		}
		fn.params = append(fn.params, param{name: pn.s, typ: pt})
		if !p.accept(",") {
			break
		}
	}
	p.expect(")")
	fn.body = p.parseBlock()
	return fn
}

func (p *parser) parseBlock() []*node {
	p.expect("{")
	var stmts []*node
	for !p.at("}") && p.peek().kind != tEOF {
		stmts = append(stmts, p.parseStmt())
	}
	p.expect("}")
	return stmts
}

func (p *parser) parseStmt() *node {
	line := p.peek().line
	switch {
	case p.peek().kind == tIdent && isTypeName(p.peek().s):
		t := p.parseStars(p.parseBaseType())
		nameTok := p.next()
		if nameTok.kind != tIdent {
			p.err("expected identifier in declaration")
		}
		t = p.parseArraySuffix(t)
		nd := &node{kind: "decl", name: nameTok.s, typ: t, line: line}
		if p.accept("=") {
			nd.init = p.parseInitializer()
		}
		inferArraySize(t, nd.init)
		p.expect(";")
		return nd
	case p.at("if"):
		p.next()
		p.expect("(")
		cond := p.parseExpr()
		p.expect(")")
		then := p.parseStmtOrBlock()
		nd := &node{kind: "if", cond: cond, then: then, line: line}
		if p.accept("else") {
			nd.els = p.parseStmtOrBlock()
		}
		return nd
	case p.at("while"):
		p.next()
		p.expect("(")
		cond := p.parseExpr()
		p.expect(")")
		body := p.parseStmtOrBlock()
		return &node{kind: "while", cond: cond, then: body, line: line}
	case p.at("for"):
		p.next()
		p.expect("(")
		nd := &node{kind: "for", line: line}
		if !p.at(";") { // init clause: a declaration or an expression
			if p.peek().kind == tIdent && isTypeName(p.peek().s) {
				t := p.parseStars(p.parseBaseType())
				nt := p.next()
				if nt.kind != tIdent {
					p.err("expected identifier in for-init")
				}
				t = p.parseArraySuffix(t)
				d := &node{kind: "decl", name: nt.s, typ: t, line: line}
				if p.accept("=") {
					d.init = p.parseExpr()
				}
				nd.init = d
			} else {
				nd.init = &node{kind: "exprstmt", init: p.parseExpr(), line: line}
			}
		}
		p.expect(";")
		if !p.at(";") {
			nd.cond = p.parseExpr()
		}
		p.expect(";")
		if !p.at(")") {
			nd.post = p.parseExpr()
		}
		p.expect(")")
		nd.then = p.parseStmtOrBlock()
		return nd
	case p.at("return"):
		p.next()
		nd := &node{kind: "return", line: line}
		if !p.at(";") {
			nd.init = p.parseExpr()
		}
		p.expect(";")
		return nd
	case p.at("break"):
		p.next()
		p.expect(";")
		return &node{kind: "break", line: line}
	case p.at("continue"):
		p.next()
		p.expect(";")
		return &node{kind: "continue", line: line}
	case p.at("{"):
		return &node{kind: "block", then: p.parseBlock(), line: line}
	default:
		e := p.parseExpr()
		p.expect(";")
		return &node{kind: "exprstmt", init: e, line: line}
	}
}

func (p *parser) parseStmtOrBlock() []*node {
	if p.at("{") {
		return p.parseBlock()
	}
	return []*node{p.parseStmt()}
}

// Precedence climbing. Higher number binds tighter.
var binPrec = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3,
	"<": 4, ">": 4, "<=": 4, ">=": 4,
	"|":  5,
	"^":  6,
	"&":  7,
	"<<": 8, ">>": 8,
	"+": 9, "-": 9,
	"*": 10, "/": 10, "%": 10,
}

func (p *parser) parseExpr() *node { return p.parseAssign() }

func (p *parser) parseAssign() *node {
	left := p.parseBinary(1)
	if p.at("=") {
		line := p.peek().line
		p.next()
		right := p.parseAssign()
		switch left.kind {
		case "var", "deref", "index", "member":
		default:
			fatal(fmt.Sprintf("line %d: left side of = is not assignable", line))
		}
		return &node{kind: "assign", lhs: left, rhs: right, line: line}
	}
	return left
}

func (p *parser) parseBinary(minPrec int) *node {
	left := p.parseUnary()
	for {
		t := p.peek()
		if t.kind != tPunct {
			break
		}
		prec, ok := binPrec[t.s]
		if !ok || prec < minPrec {
			break
		}
		op := t.s
		line := t.line
		p.next()
		right := p.parseBinary(prec + 1)
		left = &node{kind: "binary", op: op, lhs: left, rhs: right, line: line}
	}
	return left
}

func (p *parser) parseUnary() *node {
	t := p.peek()
	if t.kind == tIdent && t.s == "sizeof" {
		p.next()
		// sizeof(type) if a type name follows '('; otherwise sizeof <expr>.
		if p.at("(") && p.toks[p.pos+1].kind == tIdent && isTypeName(p.toks[p.pos+1].s) {
			p.next() // (
			ty := p.parseArraySuffix(p.parseStars(p.parseBaseType()))
			p.expect(")")
			return &node{kind: "num", num: sizeOf(ty), line: t.line}
		}
		return &node{kind: "sizeof", lhs: p.parseUnary(), line: t.line}
	}
	if t.kind == tPunct && (t.s == "-" || t.s == "~" || t.s == "!" || t.s == "+") {
		p.next()
		operand := p.parseUnary()
		if t.s == "+" {
			return operand
		}
		return &node{kind: "unary", op: t.s, lhs: operand, line: t.line}
	}
	if t.kind == tPunct && t.s == "*" { // pointer dereference
		p.next()
		return &node{kind: "deref", lhs: p.parseUnary(), line: t.line}
	}
	if t.kind == tPunct && t.s == "&" { // address-of
		p.next()
		return &node{kind: "addr", lhs: p.parseUnary(), line: t.line}
	}
	return p.parsePostfix()
}

// parsePostfix handles `expr[index]`, `expr.field`, and `expr->field`.
func (p *parser) parsePostfix() *node {
	e := p.parsePrimary()
	for {
		line := p.peek().line
		switch {
		case p.at("["):
			p.next()
			idx := p.parseExpr()
			p.expect("]")
			e = &node{kind: "index", lhs: e, rhs: idx, line: line}
		case p.at(".") || p.at("->"):
			arrow := p.at("->")
			p.next()
			fn := p.next()
			if fn.kind != tIdent {
				p.err("expected field name after '.'/'->'")
			}
			e = &node{kind: "member", lhs: e, name: fn.s, arrow: arrow, line: line}
		default:
			return e
		}
	}
}

func (p *parser) parsePrimary() *node {
	t := p.peek()
	switch {
	case t.kind == tNum:
		p.next()
		return &node{kind: "num", num: t.n, line: t.line}
	case t.kind == tStr:
		p.next()
		return &node{kind: "str", str: t.s, line: t.line}
	case t.kind == tIdent:
		p.next()
		if p.at("(") {
			p.next()
			var args []*node
			for !p.at(")") {
				args = append(args, p.parseExpr())
				if !p.accept(",") {
					break
				}
			}
			p.expect(")")
			return &node{kind: "call", name: t.s, args: args, line: t.line}
		}
		return &node{kind: "var", name: t.s, line: t.line}
	case t.kind == tPunct && t.s == "(":
		p.next()
		e := p.parseExpr()
		p.expect(")")
		return e
	}
	p.err(fmt.Sprintf("unexpected token %q", t.s))
	return nil
}

// ---------------------------------------------------------------------------
// Code generation
// ---------------------------------------------------------------------------

const (
	dataSeg  = 0x10 // variables live at physical 0x1000..0x10ff (ds:imm)
	stackSeg = 0x70 // stack at 0x7000..0x70ff
	vramSeg  = 0xfb // video text RAM segment
	codeOrg  = 0x7c00

	// Runtime scratch occupies the top of the data segment; user variables are
	// allocated below it. Used by the 16-bit multiply/divide helpers.
	rtA   = 0xf0 // 2 bytes
	rtB   = 0xf2 // 2 bytes
	rtP   = 0xf4 // 2 bytes
	rtN   = 0xf6 // 2 bytes
	rtR   = 0xf8 // 2 bytes
	rtC   = 0xfa // 1 byte  (loop counter)
	rtT   = 0xfb // 1 byte  (temp)
	rtTop = 0xf0
)

// scalarWidth is the load/store width (1 or 2 bytes) of a scalar type. char is
// 1 byte; int and pointers are 2.
func scalarWidth(t *ctype) int {
	if t.kind == "char" {
		return 1
	}
	return 2
}

type generator struct {
	b        strings.Builder
	off      map[string]int    // storage key -> byte offset in dataSeg
	typ      map[string]*ctype // storage key -> declared type
	nextOff  int
	labels   int
	fnNames  map[string]bool
	fnRet    map[string]*ctype
	curFn    string
	usesMul  bool // 16-bit multiply helper needed
	usesDiv  bool // 16-bit divide helper needed
	usesAdd  bool // 16-bit add helper needed
	usesSub  bool // 16-bit subtract helper needed
	usesCmp  bool // 16-bit compare helper needed
	usesShl  bool // 16-bit shift-left-by-1 helper needed
	usesShr  bool // 16-bit shift-right-by-1 helper needed
	usesLd8  bool // __load8 needed
	usesLd16 bool // __load16 needed
	usesSt8  bool // __store8 needed
	usesSt16 bool // __store16 needed
	strLits  []strLit
	breakLbl []string // innermost loop's break target (stack)
	contLbl  []string // innermost loop's continue target (stack)
}

// strLit is a string literal to emit as data in the code image; its label's
// linear address becomes the char* value.
type strLit struct {
	label string
	data  []byte
}

func gen(prog *program) string {
	progRef = prog
	g := &generator{
		off:     map[string]int{},
		typ:     map[string]*ctype{},
		fnNames: map[string]bool{},
		fnRet:   map[string]*ctype{},
	}
	for _, f := range prog.funcs {
		g.fnNames[f.name] = true
		g.fnRet[f.name] = f.ret
	}
	if !g.fnNames["main"] {
		fatal("no main() function")
	}

	// Allocate storage. Globals first, then each function's params + locals.
	for _, gv := range prog.globals {
		g.alloc(gv.name, gv.typ)
	}
	for _, f := range prog.funcs {
		for _, pr := range f.params {
			g.alloc(g.qual(f.name, pr.name), pr.typ)
		}
		for _, st := range f.body {
			g.allocLocals(f.name, st)
		}
	}

	g.emit(".org 0x%x", codeOrg)
	g.emit("")
	g.emit("; ---- runtime prologue ----")
	g.emit("MOV ss, 0x%x", stackSeg)
	g.emit("MOV sp, 0xff")
	g.emit("MOV ds, 0x%x", dataSeg)
	// Initialise globals that have initialisers.
	for _, gv := range prog.globals {
		if gv.init != nil {
			g.genVarInit(g.off[gv.name], gv.typ, gv.init)
		}
	}
	g.emit("CALL main")
	g.emit("HLT")
	g.emit("")

	for _, f := range prog.funcs {
		g.genFunc(f)
	}
	g.genRuntime()
	g.genStrings()
	return g.b.String()
}

// genVarInit stores an initializer into the variable at data offset off. Arrays
// are filled element-by-element (they live at known data-segment offsets, so no
// pointer machinery is needed); scalars use a single store.
func (g *generator) genVarInit(off int, t *ctype, init *node) {
	if !t.isArray() {
		g.genExpr(init, "")
		g.storeAt(off, scalarWidth(t))
		return
	}
	esz := sizeOf(t.elem)
	switch init.kind {
	case "str":
		if t.elem.kind != "char" {
			fatal("a string can only initialise a char array")
		}
		data := append([]byte(init.str), 0) // trailing NUL
		for i := 0; i < t.n && i < len(data); i++ {
			g.emit("MOV ax, 0x%x", data[i])
			g.emit("MOV [0x%x], ax", off+i)
		}
	case "initlist":
		if len(init.args) > t.n {
			fatal("too many initializers for array")
		}
		for i, el := range init.args {
			g.genExpr(el, "")
			g.storeAt(off+i*esz, scalarWidth(t.elem))
		}
	default:
		fatal("array initializer must be a string or {…} list")
	}
}

// genStrings emits collected string literals as data after all code.
func (g *generator) genStrings() {
	if len(g.strLits) == 0 {
		return
	}
	g.emit("; ---- string literals ----")
	for _, s := range g.strLits {
		bytes := make([]string, 0, len(s.data)+1)
		for _, b := range s.data {
			bytes = append(bytes, fmt.Sprintf("0x%x", b))
		}
		bytes = append(bytes, "0x0") // NUL terminator
		g.emit("%s: db %s", s.label, strings.Join(bytes, ", "))
	}
}

// genRuntime emits the 16-bit software helpers the program uses. All work on a
// 16-bit accumulator held in ax(low):dx(high); a second operand, where needed,
// is in bx(low):cx(high). Helpers clobber di and flag. They are placed after
// HLT and reached only via CALL.
//
//	__add16 : ax:dx += bx:cx
//	__sub16 : ax:dx -= bx:cx
//	__cmp16 : compare ax:dx vs bx:cx (unsigned) -> ax = 0 eq / 1 below / 2 above
//	__shl16 : ax:dx <<= 1        __shr16 : ax:dx >>= 1 (logical)
//	__mul16 : ax:dx *= bx:cx     (mod 65536)
//	__div16 : ax:dx /= bx:cx     -> ax:dx = quotient, bx:cx = remainder (unsigned)
//
// __div16 by zero returns quotient 0, remainder = the dividend.
func (g *generator) genRuntime() {
	// Resolve helper dependencies.
	if g.usesMul {
		g.usesAdd, g.usesShl, g.usesShr = true, true, true
	}
	if g.usesDiv {
		g.usesCmp, g.usesSub, g.usesShl = true, true, true
	}

	if g.usesAdd {
		g.emit("; ---- __add16: ax:dx += bx:cx ----")
		g.emit("__add16:")
		g.emit("MOV flag, 0") // clear carry so it reflects only the low add
		g.emit("ADD bx")      // ax += low
		g.emit("MOV di, ax")  // save low result
		g.emit("MOV ax, dx")  // ax = acc high
		g.emit("JNC __add16_nc")
		g.emit("INC ax") // carry into high
		g.emit("__add16_nc:")
		g.emit("ADD cx")     // += operand high
		g.emit("MOV dx, ax") // store high
		g.emit("MOV ax, di") // restore low
		g.emit("RET")
		g.emit("")
	}
	if g.usesSub {
		g.emit("; ---- __sub16: ax:dx -= bx:cx ----")
		g.emit("__sub16:")
		// negate operand: bx:cx = -(bx:cx)
		g.emit("NOT bx")
		g.emit("NOT cx")
		g.emit("INC bx")
		g.emit("CMP bx, 0")
		g.emit("JNZ __sub16_na")
		g.emit("INC cx") // low wrapped 0xff->0x00 => carry into high
		g.emit("__sub16_na:")
		// add: ax:dx += bx:cx
		g.emit("MOV flag, 0")
		g.emit("ADD bx")
		g.emit("MOV di, ax")
		g.emit("MOV ax, dx")
		g.emit("JNC __sub16_nc")
		g.emit("INC ax")
		g.emit("__sub16_nc:")
		g.emit("ADD cx")
		g.emit("MOV dx, ax")
		g.emit("MOV ax, di")
		g.emit("RET")
		g.emit("")
	}
	if g.usesCmp {
		g.emit("; ---- __cmp16: ax:dx ? bx:cx (unsigned) -> ax 0/1/2 ----")
		g.emit("__cmp16:")
		g.emit("CMP dx, cx") // high bytes
		g.emit("JZ __cmp16_lo")
		g.emit("JC __cmp16_lt") // acc high < op high
		g.emit("MOV ax, 2")
		g.emit("RET")
		g.emit("__cmp16_lo:")
		g.emit("CMP ax, bx") // low bytes
		g.emit("JZ __cmp16_eq")
		g.emit("JC __cmp16_lt")
		g.emit("MOV ax, 2")
		g.emit("RET")
		g.emit("__cmp16_lt:")
		g.emit("MOV ax, 1")
		g.emit("RET")
		g.emit("__cmp16_eq:")
		g.emit("MOV ax, 0")
		g.emit("RET")
		g.emit("")
	}
	if g.usesShl {
		g.emit("; ---- __shl16: ax:dx <<= 1 ----")
		g.emit("__shl16:")
		g.emit("MOV di, ax")
		g.emit("SHR di, 7") // di = old bit7 of low
		g.emit("SHL dx, 1")
		g.emit("OR dx, di")
		g.emit("SHL ax, 1")
		g.emit("RET")
		g.emit("")
	}
	if g.usesShr {
		g.emit("; ---- __shr16: ax:dx >>= 1 (logical) ----")
		g.emit("__shr16:")
		g.emit("MOV di, dx")
		g.emit("AND di, 1")
		g.emit("SHL di, 7") // di = (high&1)<<7
		g.emit("SHR ax, 1")
		g.emit("OR ax, di")
		g.emit("SHR dx, 1")
		g.emit("RET")
		g.emit("")
	}
	if g.usesMul {
		g.emit("; ---- __mul16: ax:dx *= bx:cx (mod 65536) ----")
		g.emit("__mul16:")
		g.emit("MOV [0x%x], ax", rtA) // A = multiplicand
		g.emit("MOV [0x%x], dx", rtA+1)
		g.emit("MOV [0x%x], bx", rtB) // B = multiplier
		g.emit("MOV [0x%x], cx", rtB+1)
		g.emit("MOV ax, 0")
		g.emit("MOV [0x%x], ax", rtP) // product = 0
		g.emit("MOV [0x%x], ax", rtP+1)
		g.emit("MOV [0x%x], ax", rtC) // counter = 0
		g.emit("__mul16_loop:")
		g.emit("MOV ax, [0x%x]", rtB) // test low bit of B
		g.emit("AND ax, 1")
		g.emit("CMP ax, 0")
		g.emit("JNZ __mul16_add")
		g.emit("JMP __mul16_shift")
		g.emit("__mul16_add:")
		g.emit("MOV ax, [0x%x]", rtP)
		g.emit("MOV dx, [0x%x]", rtP+1)
		g.emit("MOV bx, [0x%x]", rtA)
		g.emit("MOV cx, [0x%x]", rtA+1)
		g.emit("CALL __add16") // P += A
		g.emit("MOV [0x%x], ax", rtP)
		g.emit("MOV [0x%x], dx", rtP+1)
		g.emit("__mul16_shift:")
		g.emit("MOV ax, [0x%x]", rtA) // A <<= 1
		g.emit("MOV dx, [0x%x]", rtA+1)
		g.emit("CALL __shl16")
		g.emit("MOV [0x%x], ax", rtA)
		g.emit("MOV [0x%x], dx", rtA+1)
		g.emit("MOV ax, [0x%x]", rtB) // B >>= 1
		g.emit("MOV dx, [0x%x]", rtB+1)
		g.emit("CALL __shr16")
		g.emit("MOV [0x%x], ax", rtB)
		g.emit("MOV [0x%x], dx", rtB+1)
		g.emit("MOV ax, [0x%x]", rtC) // counter++
		g.emit("INC ax")
		g.emit("MOV [0x%x], ax", rtC)
		g.emit("CMP ax, 16")
		g.emit("JZ __mul16_done")
		g.emit("JMP __mul16_loop")
		g.emit("__mul16_done:")
		g.emit("MOV ax, [0x%x]", rtP)
		g.emit("MOV dx, [0x%x]", rtP+1)
		g.emit("RET")
		g.emit("")
	}
	if g.usesDiv {
		g.emit("; ---- __div16: ax:dx /= bx:cx -> quot ax:dx, rem bx:cx ----")
		g.emit("__div16:")
		g.emit("MOV [0x%x], ax", rtN) // N = dividend (becomes quotient)
		g.emit("MOV [0x%x], dx", rtN+1)
		g.emit("MOV [0x%x], bx", rtB) // D = divisor
		g.emit("MOV [0x%x], cx", rtB+1)
		g.emit("MOV ax, 0")
		g.emit("MOV [0x%x], ax", rtR) // R = 0
		g.emit("MOV [0x%x], ax", rtR+1)
		g.emit("MOV [0x%x], ax", rtC) // counter = 0
		g.emit("__div16_loop:")
		// topbit = bit15 of N
		g.emit("MOV ax, [0x%x]", rtN+1)
		g.emit("SHR ax, 7")
		g.emit("MOV [0x%x], ax", rtT) // stash top bit
		// R <<= 1 ; R |= topbit
		g.emit("MOV ax, [0x%x]", rtR)
		g.emit("MOV dx, [0x%x]", rtR+1)
		g.emit("CALL __shl16")
		g.emit("MOV bx, [0x%x]", rtT)
		g.emit("OR ax, bx")
		g.emit("MOV [0x%x], ax", rtR)
		g.emit("MOV [0x%x], dx", rtR+1)
		// N <<= 1
		g.emit("MOV ax, [0x%x]", rtN)
		g.emit("MOV dx, [0x%x]", rtN+1)
		g.emit("CALL __shl16")
		g.emit("MOV [0x%x], ax", rtN)
		g.emit("MOV [0x%x], dx", rtN+1)
		// if R >= D: R -= D; N |= 1
		g.emit("MOV ax, [0x%x]", rtR)
		g.emit("MOV dx, [0x%x]", rtR+1)
		g.emit("MOV bx, [0x%x]", rtB)
		g.emit("MOV cx, [0x%x]", rtB+1)
		g.emit("CALL __cmp16") // ax: 0 eq / 1 R<D / 2 R>D
		g.emit("CMP ax, 1")
		g.emit("JNZ __div16_sub") // not "R<D" -> subtract
		g.emit("JMP __div16_next")
		g.emit("__div16_sub:")
		g.emit("MOV ax, [0x%x]", rtR)
		g.emit("MOV dx, [0x%x]", rtR+1)
		g.emit("MOV bx, [0x%x]", rtB)
		g.emit("MOV cx, [0x%x]", rtB+1)
		g.emit("CALL __sub16") // R -= D
		g.emit("MOV [0x%x], ax", rtR)
		g.emit("MOV [0x%x], dx", rtR+1)
		g.emit("MOV ax, [0x%x]", rtN) // set quotient bit 0
		g.emit("OR ax, 1")
		g.emit("MOV [0x%x], ax", rtN)
		g.emit("__div16_next:")
		g.emit("MOV ax, [0x%x]", rtC)
		g.emit("INC ax")
		g.emit("MOV [0x%x], ax", rtC)
		g.emit("CMP ax, 16")
		g.emit("JZ __div16_done")
		g.emit("JMP __div16_loop")
		g.emit("__div16_done:")
		g.emit("MOV ax, [0x%x]", rtN) // quotient
		g.emit("MOV dx, [0x%x]", rtN+1)
		g.emit("MOV bx, [0x%x]", rtR) // remainder
		g.emit("MOV cx, [0x%x]", rtR+1)
		g.emit("RET")
		g.emit("")
	}

	// Dereference helpers. A pointer is a 16-bit linear address in ax(off):dx(seg).
	if g.usesLd8 {
		g.emit("; ---- __load8: ax:dx (addr) -> ax (byte) ----")
		g.emit("__load8:")
		g.emit("MOV bx, ax")
		g.emit("MOV ds, dx")
		g.emit("MOV ax, [ds:bx]")
		g.emit("MOV ds, 0x%x", dataSeg)
		g.emit("MOV dx, 0")
		g.emit("RET")
		g.emit("")
	}
	if g.usesLd16 {
		g.emit("; ---- __load16: ax:dx (addr) -> ax:dx (word) ----")
		g.emit("__load16:")
		g.emit("MOV bx, ax")
		g.emit("MOV ds, dx")
		g.emit("MOV cx, [ds:bx]") // low byte
		g.emit("INC bx")
		g.emit("CMP bx, 0")
		g.emit("JNZ __load16_hi")
		g.emit("INC ds") // crossed a 256-byte boundary
		g.emit("__load16_hi:")
		g.emit("MOV ax, [ds:bx]") // high byte
		g.emit("MOV dx, ax")
		g.emit("MOV ax, cx")
		g.emit("MOV ds, 0x%x", dataSeg)
		g.emit("RET")
		g.emit("")
	}
	if g.usesSt8 {
		g.emit("; ---- __store8: [rtA]=addr, ax=byte ----")
		g.emit("__store8:")
		g.emit("MOV bx, [0x%x]", rtA)
		g.emit("MOV ds, [0x%x]", rtA+1)
		g.emit("MOV [ds:bx], ax")
		g.emit("MOV ds, 0x%x", dataSeg)
		g.emit("RET")
		g.emit("")
	}
	if g.usesSt16 {
		g.emit("; ---- __store16: [rtA]=addr, ax:dx=word ----")
		g.emit("__store16:")
		g.emit("MOV bx, [0x%x]", rtA)
		g.emit("MOV ds, [0x%x]", rtA+1)
		g.emit("MOV [ds:bx], ax") // low byte
		g.emit("INC bx")
		g.emit("CMP bx, 0")
		g.emit("JNZ __store16_hi")
		g.emit("INC ds")
		g.emit("__store16_hi:")
		g.emit("MOV [ds:bx], dx") // high byte
		g.emit("MOV ds, 0x%x", dataSeg)
		g.emit("RET")
		g.emit("")
	}
}

func (g *generator) alloc(name string, t *ctype) {
	if _, ok := g.off[name]; ok {
		return
	}
	width := sizeOf(t)
	if g.nextOff+width > rtTop {
		fatal("out of data memory (too many variables)")
	}
	g.off[name] = g.nextOff
	g.typ[name] = t
	g.nextOff += width
}

// qual returns the storage key for a name inside a function (params/locals are
// statically allocated per function).
func (g *generator) qual(fn, name string) string { return fn + "::" + name }

func (g *generator) allocLocals(fn string, st *node) {
	if st == nil {
		return
	}
	switch st.kind {
	case "decl":
		g.alloc(g.qual(fn, st.name), st.typ)
	case "if":
		for _, s := range st.then {
			g.allocLocals(fn, s)
		}
		for _, s := range st.els {
			g.allocLocals(fn, s)
		}
	case "while", "block":
		for _, s := range st.then {
			g.allocLocals(fn, s)
		}
	case "for":
		g.allocLocals(fn, st.init) // may declare a loop variable
		for _, s := range st.then {
			g.allocLocals(fn, s)
		}
	}
}

// resolve maps a source name to its (offset, type), preferring function-locals.
func (g *generator) resolve(name string) (off int, t *ctype) {
	key := g.qual(g.curFn, name)
	if o, ok := g.off[key]; ok {
		return o, g.typ[key]
	}
	if o, ok := g.off[name]; ok {
		return o, g.typ[name]
	}
	fatal(fmt.Sprintf("unknown variable %q in %s()", name, g.curFn))
	return 0, nil
}

// loadVar loads a scalar variable into the accumulator ax:dx. A char is
// zero-extended; an int/pointer loads both bytes.
func (g *generator) loadVar(name string) {
	off, t := g.resolve(name)
	g.emit("MOV ax, [0x%x]", off)
	if scalarWidth(t) == 2 {
		g.emit("MOV dx, [0x%x]", off+1)
	} else {
		g.emit("MOV dx, 0")
	}
}

// storeVar writes the accumulator ax:dx into a scalar variable (char truncates).
func (g *generator) storeVar(name string) {
	off, t := g.resolve(name)
	g.storeAt(off, scalarWidth(t))
}

// storeAt writes ax:dx to a fixed data offset of the given width.
func (g *generator) storeAt(off, width int) {
	g.emit("MOV [0x%x], ax", off)
	if width == 2 {
		g.emit("MOV [0x%x], dx", off+1)
	}
}

func (g *generator) emit(format string, a ...any) {
	fmt.Fprintf(&g.b, format+"\n", a...)
}

func (g *generator) label() string {
	g.labels++
	return fmt.Sprintf("_L%d", g.labels)
}

// branchZero emits "if the 16-bit accumulator == 0, jump to target". It folds
// dx into ax first (destroying the accumulator, which a condition test does not
// need afterwards). target may be far: the relative conditional jump only hops
// over a near (unbounded) JMP.
func (g *generator) branchZero(target string) {
	over := g.label()
	g.emit("OR ax, dx") // 16-bit zero iff both bytes zero
	g.emit("CMP ax, 0")
	g.emit("JNZ %s", over) // != 0: fall through past the JMP
	g.emit("JMP %s", target)
	g.emit("%s:", over)
}

// branchNonZero emits "if the accumulator != 0, jump to target" (far-safe).
func (g *generator) branchNonZero(target string) {
	over := g.label()
	g.emit("OR ax, dx")
	g.emit("CMP ax, 0")
	g.emit("JZ %s", over)
	g.emit("JMP %s", target)
	g.emit("%s:", over)
}

func (g *generator) genFunc(f *funcDecl) {
	g.curFn = f.name
	g.emit("; ---- %s ----", f.name)
	g.emit("%s:", f.name)
	end := "_end_" + f.name
	for _, st := range f.body {
		g.genStmt(st, end)
	}
	g.emit("%s:", end)
	if f.ret != nil && f.ret.kind == "char" {
		g.emit("MOV dx, 0") // char return is zero-extended
	}
	g.emit("RET")
	g.emit("")
}

func (g *generator) genStmt(st *node, endLabel string) {
	switch st.kind {
	case "decl":
		if st.init != nil {
			off, _ := g.resolve(st.name)
			g.genVarInit(off, st.typ, st.init)
		}
	case "exprstmt":
		g.genExpr(st.init, "")
	case "assign":
		g.genExpr(st, "")
	case "return":
		if st.init != nil {
			g.genExpr(st.init, "")
		}
		g.emit("JMP %s", endLabel)
	case "block":
		for _, s := range st.then {
			g.genStmt(s, endLabel)
		}
	case "if":
		done := g.label()
		g.genExpr(st.cond, "")
		if len(st.els) > 0 {
			els := g.label()
			g.branchZero(els) // cond == 0 -> else branch
			for _, s := range st.then {
				g.genStmt(s, endLabel)
			}
			g.emit("JMP %s", done)
			g.emit("%s:", els)
			for _, s := range st.els {
				g.genStmt(s, endLabel)
			}
		} else {
			g.branchZero(done) // cond == 0 -> skip
			for _, s := range st.then {
				g.genStmt(s, endLabel)
			}
		}
		g.emit("%s:", done)
	case "while":
		top := g.label()
		done := g.label()
		g.pushLoop(done, top) // continue re-tests the condition
		g.emit("%s:", top)
		g.genExpr(st.cond, "")
		g.branchZero(done) // cond == 0 -> exit
		for _, s := range st.then {
			g.genStmt(s, endLabel)
		}
		g.emit("JMP %s", top)
		g.emit("%s:", done)
		g.popLoop()
	case "for":
		if st.init != nil {
			g.genStmt(st.init, endLabel)
		}
		top := g.label()
		cont := g.label()
		done := g.label()
		g.pushLoop(done, cont) // continue runs the post-expression
		g.emit("%s:", top)
		if st.cond != nil {
			g.genExpr(st.cond, "")
			g.branchZero(done) // cond == 0 -> exit
		}
		for _, s := range st.then {
			g.genStmt(s, endLabel)
		}
		g.emit("%s:", cont)
		if st.post != nil {
			g.genExpr(st.post, "")
		}
		g.emit("JMP %s", top)
		g.emit("%s:", done)
		g.popLoop()
	case "break":
		if len(g.breakLbl) == 0 {
			fatal(fmt.Sprintf("line %d: break outside a loop", st.line))
		}
		g.emit("JMP %s", g.breakLbl[len(g.breakLbl)-1])
	case "continue":
		if len(g.contLbl) == 0 {
			fatal(fmt.Sprintf("line %d: continue outside a loop", st.line))
		}
		g.emit("JMP %s", g.contLbl[len(g.contLbl)-1])
	default:
		fatal("cannot generate statement of kind " + st.kind)
	}
}

func (g *generator) pushLoop(breakL, contL string) {
	g.breakLbl = append(g.breakLbl, breakL)
	g.contLbl = append(g.contLbl, contL)
}

func (g *generator) popLoop() {
	g.breakLbl = g.breakLbl[:len(g.breakLbl)-1]
	g.contLbl = g.contLbl[:len(g.contLbl)-1]
}

// memberInfo resolves a `.`/`->` access to (field offset, field type). For `->`
// the base is a pointer to struct; for `.` the base is a struct lvalue.
func (g *generator) memberInfo(e *node) (off int, ft *ctype) {
	var st *ctype
	if e.arrow {
		pt := g.typeOf(e.lhs)
		if !pt.isPtr() || pt.elem.kind != "struct" {
			fatal(fmt.Sprintf("line %d: '->' needs a pointer to struct", e.line))
		}
		st = pt.elem
	} else {
		st = g.typeOf(e.lhs)
		if st.kind != "struct" {
			fatal(fmt.Sprintf("line %d: '.' needs a struct", e.line))
		}
	}
	for _, f := range st.fields {
		if f.name == e.name {
			return f.off, f.typ
		}
	}
	fatal(fmt.Sprintf("line %d: struct %q has no field %q", e.line, st.tag, e.name))
	return 0, nil
}

// typeOf returns the value type of an expression (arrays decayed to pointers).
func (g *generator) typeOf(e *node) *ctype {
	switch e.kind {
	case "num":
		return tInt
	case "member":
		_, ft := g.memberInfo(e)
		return decay(ft)
	case "var":
		_, t := g.resolve(e.name)
		return decay(t)
	case "deref":
		pt := g.typeOf(e.lhs)
		if !pt.isPtr() {
			fatal(fmt.Sprintf("line %d: cannot dereference a non-pointer", e.line))
		}
		return decay(pt.elem)
	case "addr":
		return ptrTo(g.lvalueType(e.lhs))
	case "index":
		bt := g.typeOf(e.lhs)
		if !bt.isPtr() {
			fatal(fmt.Sprintf("line %d: cannot index a non-pointer/array", e.line))
		}
		return decay(bt.elem)
	case "assign":
		return g.lvalueType(e.lhs)
	case "str":
		return ptrTo(tChar)
	case "sizeof":
		return tInt
	case "call":
		if r := g.fnRet[e.name]; r != nil {
			return r
		}
		return tInt
	case "binary":
		lt, rt := g.typeOf(e.lhs), g.typeOf(e.rhs)
		switch e.op {
		case "+":
			if lt.isPtr() {
				return lt
			}
			if rt.isPtr() {
				return rt
			}
		case "-":
			if lt.isPtr() && !rt.isPtr() {
				return lt
			}
		}
		return tInt
	case "unary":
		return tInt
	}
	return tInt
}

// lvalueType is the (un-decayed) type of an addressable expression.
func (g *generator) lvalueType(e *node) *ctype {
	switch e.kind {
	case "var":
		_, t := g.resolve(e.name)
		return t
	case "deref":
		pt := g.typeOf(e.lhs)
		if !pt.isPtr() {
			fatal(fmt.Sprintf("line %d: cannot dereference a non-pointer", e.line))
		}
		return pt.elem
	case "index":
		bt := g.typeOf(e.lhs)
		if !bt.isPtr() {
			fatal(fmt.Sprintf("line %d: cannot index a non-pointer/array", e.line))
		}
		return bt.elem
	case "member":
		_, ft := g.memberInfo(e)
		return ft
	}
	fatal(fmt.Sprintf("line %d: expression is not an lvalue", e.line))
	return nil
}

// genAddr leaves the 16-bit linear address of an lvalue in ax:dx.
func (g *generator) genAddr(e *node) {
	switch e.kind {
	case "var":
		off, _ := g.resolve(e.name)
		addr := dataSeg*0x100 + off
		g.emit("MOV ax, 0x%x", addr&0xff)
		g.emit("MOV dx, 0x%x", (addr>>8)&0xff)
	case "deref":
		g.genExpr(e.lhs, "") // the pointer's value *is* the address
	case "index":
		g.genElemAddr(e.lhs, e.rhs)
	case "member":
		off, _ := g.memberInfo(e)
		if e.arrow {
			g.genExpr(e.lhs, "") // pointer value = struct base address
		} else {
			g.genAddr(e.lhs) // address of the struct lvalue
		}
		g.genAddConst(off) // + field offset
	default:
		fatal(fmt.Sprintf("line %d: cannot take the address of this expression", e.line))
	}
}

// genAddConst adds a constant to the 16-bit accumulator (a field/element offset).
func (g *generator) genAddConst(off int) {
	if off == 0 {
		return
	}
	g.emit("MOV bx, 0x%x", off&0xff)
	g.emit("MOV cx, 0x%x", (off>>8)&0xff)
	g.usesAdd = true
	g.emit("CALL __add16")
}

// genElemAddr computes &base[idx] into ax:dx: base(decayed) + idx*elemSize.
func (g *generator) genElemAddr(base, idx *node) {
	bt := g.typeOf(base)
	if !bt.isPtr() {
		fatal(fmt.Sprintf("line %d: cannot index a non-pointer/array", base.line))
	}
	sz := sizeOf(bt.elem)
	g.genExpr(base, "") // decayed base address (or pointer value)
	g.emit("PUSH ax")
	g.emit("PUSH dx")
	g.genExpr(idx, "") // index
	if sz == 2 {
		g.usesShl = true
		g.emit("CALL __shl16") // *2
	}
	g.emit("POP cx")
	g.emit("POP bx")
	g.usesAdd = true
	g.emit("CALL __add16") // base + idx*sz
}

// loadThrough loads a value of the given width from the address in ax:dx.
func (g *generator) loadThrough(width int) {
	if width == 1 {
		g.usesLd8 = true
		g.emit("CALL __load8")
	} else {
		g.usesLd16 = true
		g.emit("CALL __load16")
	}
}

// genStore evaluates rhs and stores it through the address of lvalue lhs.
func (g *generator) genStore(lhs, rhs *node) {
	lt := g.lvalueType(lhs)
	if lt.isArray() || lt.kind == "struct" {
		fatal(fmt.Sprintf("line %d: cannot assign to a whole array or struct", lhs.line))
	}
	width := scalarWidth(lt)
	g.genExpr(rhs, "")
	g.emit("PUSH ax")
	g.emit("PUSH dx")
	g.genAddr(lhs)
	g.emit("MOV [0x%x], ax", rtA) // stash address for the store helper
	g.emit("MOV [0x%x], dx", rtA+1)
	g.emit("POP dx") // restore value (high then low)
	g.emit("POP ax")
	if width == 1 {
		g.usesSt8 = true
		g.emit("CALL __store8")
	} else {
		g.usesSt16 = true
		g.emit("CALL __store16")
	}
}

// sizeofOf returns sizeof(e) in bytes. A variable uses its declared (un-decayed)
// type, so sizeof(array) is the whole array, not a pointer.
func (g *generator) sizeofOf(e *node) int {
	if e.kind == "var" {
		_, t := g.resolve(e.name)
		return sizeOf(t)
	}
	return sizeOf(g.typeOf(e))
}

// genLoad evaluates a `deref`/`index`/`member` lvalue: it computes the address
// and loads the scalar there — unless the lvalue is an array (decays to its
// address) or a struct (used by address for further member access).
func (g *generator) genLoad(e *node) {
	lt := g.lvalueType(e)
	g.genAddr(e)
	if lt.isArray() || lt.kind == "struct" {
		return
	}
	g.loadThrough(scalarWidth(lt))
}

// genExpr evaluates an expression, leaving the 16-bit result in ax(low):dx(high).
func (g *generator) genExpr(e *node, _ string) {
	switch e.kind {
	case "num":
		v := e.num & 0xffff
		g.emit("MOV ax, 0x%x", v&0xff)
		g.emit("MOV dx, 0x%x", (v>>8)&0xff)
	case "str":
		// The value is a char* = the linear address of the emitted string data.
		lab := fmt.Sprintf("_str%d", len(g.strLits)+1)
		g.strLits = append(g.strLits, strLit{label: lab, data: []byte(e.str)})
		g.emit("MOV ax, low(%s)", lab)
		g.emit("MOV dx, high(%s)", lab)
	case "sizeof":
		sz := g.sizeofOf(e.lhs)
		g.emit("MOV ax, 0x%x", sz&0xff)
		g.emit("MOV dx, 0x%x", (sz>>8)&0xff)
	case "var":
		if _, t := g.resolve(e.name); t.isArray() {
			g.genAddr(e) // an array decays to the address of its first element
		} else {
			g.loadVar(e.name)
		}
	case "addr":
		g.genAddr(e.lhs)
	case "deref", "index", "member":
		g.genLoad(e)
	case "assign":
		if e.lhs.kind == "var" {
			if _, t := g.resolve(e.lhs.name); !t.isArray() && t.kind != "struct" {
				g.genExpr(e.rhs, "")
				g.storeVar(e.lhs.name)
				return
			}
		}
		g.genStore(e.lhs, e.rhs)
	case "unary":
		g.genExpr(e.lhs, "")
		switch e.op {
		case "-": // two's complement negate of ax:dx
			over := g.label()
			g.emit("NOT ax")
			g.emit("NOT dx")
			g.emit("INC ax")
			g.emit("CMP ax, 0")
			g.emit("JNZ %s", over) // no carry into high
			g.emit("INC dx")
			g.emit("%s:", over)
		case "~":
			g.emit("NOT ax")
			g.emit("NOT dx")
		case "!":
			keep := g.label()
			g.emit("OR ax, dx") // 16-bit zero test
			g.emit("CMP ax, 0")
			g.emit("MOV ax, 1")
			g.emit("JZ %s", keep) // was zero -> !x == 1
			g.emit("MOV ax, 0")
			g.emit("%s:", keep)
			g.emit("MOV dx, 0")
		}
	case "binary":
		g.genBinary(e)
	case "call":
		g.genCall(e)
	default:
		fatal("cannot generate expression of kind " + e.kind)
	}
}

func (g *generator) genBinary(e *node) {
	// Short-circuit logical operators evaluate the right side conditionally.
	if e.op == "&&" || e.op == "||" {
		g.genLogical(e)
		return
	}
	// Pointer arithmetic scales the integer side by the pointee size.
	if (e.op == "+" || e.op == "-") && (g.typeOf(e.lhs).isPtr() || g.typeOf(e.rhs).isPtr()) {
		g.genPtrArith(e)
		return
	}
	// Shifts need an immediate count (hardware/oasm limitation).
	if e.op == "<<" || e.op == ">>" {
		if e.rhs.kind != "num" {
			fatal(fmt.Sprintf("line %d: shift count must be a constant", e.line))
		}
		g.genExpr(e.lhs, "")
		n := e.rhs.num & 0xff
		if n >= 16 {
			g.emit("MOV ax, 0")
			g.emit("MOV dx, 0")
			return
		}
		helper := "__shl16"
		if e.op == ">>" {
			helper = "__shr16"
			g.usesShr = true
		} else {
			g.usesShl = true
		}
		for k := 0; k < n; k++ {
			g.emit("CALL %s", helper)
		}
		return
	}
	// Evaluate rhs first, stash both bytes, then lhs, so the accumulator holds
	// lhs (ax:dx) and the operand is in bx(low):cx(high).
	g.genExpr(e.rhs, "")
	g.emit("PUSH ax")
	g.emit("PUSH dx")
	g.genExpr(e.lhs, "")
	g.emit("POP cx")
	g.emit("POP bx")

	switch e.op {
	case "+":
		g.usesAdd = true
		g.emit("CALL __add16")
	case "-":
		g.usesSub = true
		g.emit("CALL __sub16")
	case "|":
		g.emit("OR ax, bx")
		g.emit("OR dx, cx")
	case "&":
		g.emit("AND ax, bx")
		g.emit("AND dx, cx")
	case "^":
		g.emit("XOR ax, bx")
		g.emit("XOR dx, cx")
	case "*":
		g.usesMul = true
		g.emit("CALL __mul16")
	case "/":
		g.usesDiv = true
		g.emit("CALL __div16") // quotient in ax:dx
	case "%":
		g.usesDiv = true
		g.emit("CALL __div16")
		g.emit("MOV ax, bx") // remainder is in bx:cx
		g.emit("MOV dx, cx")
	case "==", "!=", "<", ">", "<=", ">=":
		// Pointer comparisons are unsigned; integer relationals are signed.
		signedRel := !(g.typeOf(e.lhs).isPtr() || g.typeOf(e.rhs).isPtr())
		g.genCompare(e.op, signedRel)
	default:
		fatal("unknown binary operator " + e.op)
	}
}

// genPtrArith handles ptr±int (and int+ptr), scaling the integer by the pointee
// size. Result is a pointer (16-bit address) in ax:dx.
func (g *generator) genPtrArith(e *node) {
	lt, rt := g.typeOf(e.lhs), g.typeOf(e.rhs)
	if lt.isPtr() && rt.isPtr() {
		fatal(fmt.Sprintf("line %d: pointer-pointer arithmetic is not supported", e.line))
	}
	ptrExpr, intExpr, pt := e.lhs, e.rhs, lt
	if rt.isPtr() { // int + ptr
		if e.op == "-" {
			fatal(fmt.Sprintf("line %d: cannot subtract a pointer from an integer", e.line))
		}
		ptrExpr, intExpr, pt = e.rhs, e.lhs, rt
	}
	sz := sizeOf(pt.elem)

	if e.op == "-" { // ptr - int: keep the pointer in the accumulator
		g.genExpr(intExpr, "")
		if sz == 2 {
			g.usesShl = true
			g.emit("CALL __shl16")
		}
		g.emit("PUSH ax")
		g.emit("PUSH dx")
		g.genExpr(ptrExpr, "")
		g.emit("POP cx")
		g.emit("POP bx")
		g.usesSub = true
		g.emit("CALL __sub16")
		return
	}
	// ptr + int (or int + ptr): scaled index in the accumulator, add pointer.
	g.genExpr(ptrExpr, "")
	g.emit("PUSH ax")
	g.emit("PUSH dx")
	g.genExpr(intExpr, "")
	if sz == 2 {
		g.usesShl = true
		g.emit("CALL __shl16")
	}
	g.emit("POP cx")
	g.emit("POP bx")
	g.usesAdd = true
	g.emit("CALL __add16")
}

// genLogical emits short-circuit && / ||, normalising the result to 0/1 in ax.
func (g *generator) genLogical(e *node) {
	done := g.label()
	if e.op == "&&" {
		setFalse := g.label()
		g.genExpr(e.lhs, "")
		g.branchZero(setFalse) // lhs == 0 -> whole thing is false
		g.genExpr(e.rhs, "")
		g.branchZero(setFalse)
		g.emit("MOV ax, 1")
		g.emit("JMP %s", done)
		g.emit("%s:", setFalse)
		g.emit("MOV ax, 0")
	} else { // ||
		setTrue := g.label()
		g.genExpr(e.lhs, "")
		g.branchNonZero(setTrue) // lhs != 0 -> whole thing is true
		g.genExpr(e.rhs, "")
		g.branchNonZero(setTrue)
		g.emit("MOV ax, 0")
		g.emit("JMP %s", done)
		g.emit("%s:", setTrue)
		g.emit("MOV ax, 1")
	}
	g.emit("%s:", done)
	g.emit("MOV dx, 0") // normalise the boolean to a 16-bit 0/1
}

// genCompare assumes the accumulator holds lhs (ax:dx) and the operand is in
// bx:cx. It leaves a 16-bit 0/1 in ax:dx. Relational operators are signed (like
// C's int); == and != are sign-agnostic. The unsigned __cmp16 handles signed
// order after biasing both high bytes by 0x80 (flipping the sign bit).
func (g *generator) genCompare(op string, signedRel bool) {
	g.usesCmp = true
	relational := op == "<" || op == ">" || op == "<=" || op == ">="
	if relational && signedRel {
		g.emit("XOR dx, 0x80") // bias so unsigned compare == signed compare
		g.emit("XOR cx, 0x80")
	}
	g.emit("CALL __cmp16") // ax = 0 eq / 1 lhs<rhs / 2 lhs>rhs
	switch op {
	case "==":
		g.matchResult(0, true)
	case "!=":
		g.matchResult(0, false)
	case "<":
		g.matchResult(1, true)
	case ">":
		g.matchResult(2, true)
	case "<=":
		g.matchResult(2, false) // not greater
	case ">=":
		g.matchResult(1, false) // not less
	}
}

// matchResult turns the __cmp16 code in ax (0/1/2) into a 16-bit boolean: if
// eq, "1 when ax==k else 0"; otherwise "1 when ax!=k else 0".
func (g *generator) matchResult(k int, eq bool) {
	keep := g.label()
	g.emit("CMP ax, %d", k)
	if eq {
		g.emit("MOV ax, 1")
		g.emit("JZ %s", keep)
		g.emit("MOV ax, 0")
	} else {
		g.emit("MOV ax, 0")
		g.emit("JZ %s", keep)
		g.emit("MOV ax, 1")
	}
	g.emit("%s:", keep)
	g.emit("MOV dx, 0")
}

func (g *generator) genCall(e *node) {
	// Built-in: putc(pos, ch) writes ch to VRAM cell pos.
	if e.name == "putc" {
		if len(e.args) != 2 {
			fatal(fmt.Sprintf("line %d: putc expects (pos, ch)", e.line))
		}
		g.genExpr(e.args[1], "") // ch -> ax
		g.emit("PUSH ax")
		g.genExpr(e.args[0], "") // pos -> ax
		g.emit("MOV bx, ax")
		g.emit("POP ax") // ax=ch, bx=pos
		g.emit("MOV ds, 0x%x", vramSeg)
		g.emit("MOV [ds:bx], ax")
		g.emit("MOV ds, 0x%x", dataSeg)
		return
	}
	if !g.fnNames[e.name] {
		fatal(fmt.Sprintf("line %d: call to unknown function %q", e.line, e.name))
	}
	// Pass arguments by writing into the callee's static parameter slots.
	// (Non-reentrant; see README.)
	fnParams := g.paramKeys(e.name)
	if len(e.args) != len(fnParams) {
		fatal(fmt.Sprintf("line %d: %s expects %d argument(s), got %d",
			e.line, e.name, len(fnParams), len(e.args)))
	}
	for i, a := range e.args {
		g.genExpr(a, "")
		key := fnParams[i]
		g.storeAt(g.off[key], scalarWidth(g.typ[key]))
	}
	g.emit("CALL %s", e.name)
}

func (g *generator) paramKeys(fn string) []string {
	var keys []string
	for _, f := range progRef.funcs {
		if f.name == fn {
			for _, pr := range f.params {
				keys = append(keys, g.qual(fn, pr.name))
			}
		}
	}
	return keys
}

// progRef lets genCall look up callee parameter lists without threading the
// whole program through every helper. Set at the top of gen().
var progRef *program

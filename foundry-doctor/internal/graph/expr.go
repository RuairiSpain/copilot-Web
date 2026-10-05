// Package graph builds the typed local dependency graph (ADR-004, ADR-012) from azure.yaml, the compiled
// ARM template and the selected azd environment. It is lexical only: it scans ARM expressions and
// ${VAR} interpolations to find references, and never evaluates either.
package graph

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Scanner limits. They bound memory and recursion on hostile templates.
const (
	maxExprLen   = 1 << 20 // 1 MiB
	maxExprDepth = 64
)

// ParseError reasons.
const (
	ReasonNotExpression = "not-an-expression"
	ReasonLengthLimit   = "length-limit"
	ReasonDepthLimit    = "depth-limit"
	ReasonUnterminated  = "unterminated-string"
	ReasonUnexpected    = "unexpected-token"
	ReasonUnexpectedEnd = "unexpected-end"
	ReasonTrailing      = "trailing-input"
)

// ParseError reports why an expression could not be scanned and where (byte offset). It never carries the
// expression text, which may hold sensitive values.
type ParseError struct {
	Reason string
	Offset int
}

// Error implements error.
func (e *ParseError) Error() string {
	return "arm expression " + e.Reason + " at offset " + strconv.Itoa(e.Offset)
}

type exprKind uint8

const (
	exprString exprKind = iota
	exprNumber
	exprIdent
	exprCall
	exprMember
	exprIndex
)

// expr is a node of the lexical expression tree.
//   - exprString, exprNumber, exprIdent: text
//   - exprCall: text is the function name, args its arguments
//   - exprMember: base and text (the property)
//   - exprIndex: base and args[0]
type expr struct {
	kind exprKind
	text string
	args []*expr
	base *expr
}

type parser struct {
	s   string
	pos int
	end int
}

// parseExpression scans an ARM expression of the form "[...]".
func parseExpression(s string) (*expr, error) {
	if len(s) > maxExprLen {
		return nil, &ParseError{ReasonLengthLimit, 0}
	}
	if !model.IsARMExpression(s) {
		return nil, &ParseError{ReasonNotExpression, 0}
	}
	p := &parser{s: s, pos: 1, end: len(s) - 1}
	e, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != p.end {
		return nil, &ParseError{ReasonTrailing, p.pos}
	}
	return e, nil
}

func (p *parser) skipWS() {
	for p.pos < p.end {
		switch p.s[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) parseExpr(depth int) (*expr, error) {
	if depth > maxExprDepth {
		return nil, &ParseError{ReasonDepthLimit, p.pos}
	}
	e, err := p.parsePrimary(depth)
	if err != nil {
		return nil, err
	}
	for chain := 0; ; chain++ {
		p.skipWS()
		if p.pos >= p.end {
			return e, nil
		}
		if chain >= maxExprDepth {
			return nil, &ParseError{ReasonDepthLimit, p.pos}
		}
		switch p.s[p.pos] {
		case '.':
			p.pos++
			p.skipWS()
			name, ok := p.ident()
			if !ok {
				return nil, p.unexpected()
			}
			e = &expr{kind: exprMember, base: e, text: name}
		case '[':
			p.pos++
			idx, err := p.parseExpr(depth + 1)
			if err != nil {
				return nil, err
			}
			p.skipWS()
			if p.pos >= p.end || p.s[p.pos] != ']' {
				return nil, p.unexpected()
			}
			p.pos++
			e = &expr{kind: exprIndex, base: e, args: []*expr{idx}}
		default:
			return e, nil
		}
	}
}

func (p *parser) unexpected() error {
	if p.pos >= p.end {
		return &ParseError{ReasonUnexpectedEnd, p.pos}
	}
	return &ParseError{ReasonUnexpected, p.pos}
}

func (p *parser) parsePrimary(depth int) (*expr, error) {
	p.skipWS()
	if p.pos >= p.end {
		return nil, &ParseError{ReasonUnexpectedEnd, p.pos}
	}
	c := p.s[p.pos]
	switch {
	case c == '\'':
		return p.parseString()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	}
	name, ok := p.ident()
	if !ok {
		return nil, p.unexpected()
	}
	p.skipWS()
	if p.pos < p.end && p.s[p.pos] == '(' {
		p.pos++
		call := &expr{kind: exprCall, text: name}
		p.skipWS()
		if p.pos < p.end && p.s[p.pos] == ')' {
			p.pos++
			return call, nil
		}
		for {
			a, err := p.parseExpr(depth + 1)
			if err != nil {
				return nil, err
			}
			call.args = append(call.args, a)
			p.skipWS()
			if p.pos >= p.end {
				return nil, &ParseError{ReasonUnexpectedEnd, p.pos}
			}
			switch p.s[p.pos] {
			case ',':
				p.pos++
			case ')':
				p.pos++
				return call, nil
			default:
				return nil, p.unexpected()
			}
		}
	}
	return &expr{kind: exprIdent, text: name}, nil
}

// parseString reads a single-quoted string; ” is an escaped quote.
func (p *parser) parseString() (*expr, error) {
	start := p.pos
	p.pos++
	var b strings.Builder
	for p.pos < p.end {
		c := p.s[p.pos]
		if c == '\'' {
			if p.pos+1 < p.end && p.s[p.pos+1] == '\'' {
				b.WriteByte('\'')
				p.pos += 2
				continue
			}
			p.pos++
			return &expr{kind: exprString, text: b.String()}, nil
		}
		b.WriteByte(c)
		p.pos++
	}
	return nil, &ParseError{ReasonUnterminated, start}
}

func (p *parser) parseNumber() (*expr, error) {
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
	}
	digits := 0
	for p.pos < p.end && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
		digits++
	}
	if digits == 0 {
		p.pos = start
		return nil, p.unexpected()
	}
	if p.pos+1 < p.end && p.s[p.pos] == '.' && p.s[p.pos+1] >= '0' && p.s[p.pos+1] <= '9' {
		p.pos++
		for p.pos < p.end && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	return &expr{kind: exprNumber, text: p.s[start:p.pos]}, nil
}

func (p *parser) ident() (string, bool) {
	start := p.pos
	for p.pos < p.end {
		r, size := utf8.DecodeRuneInString(p.s[p.pos:p.end])
		if r == utf8.RuneError && size <= 1 {
			break
		}
		if r == '_' || unicode.IsLetter(r) || (p.pos > start && unicode.IsDigit(r)) {
			p.pos += size
			continue
		}
		break
	}
	if p.pos == start {
		return "", false
	}
	return p.s[start:p.pos], true
}

// canonical renders e in a normalised form: no whitespace, lower-case function names and identifiers.
// Expression names compare by this text.
func canonical(e *expr) string {
	switch e.kind {
	case exprString:
		return "'" + strings.ReplaceAll(e.text, "'", "''") + "'"
	case exprNumber:
		return e.text
	case exprIdent:
		return strings.ToLower(e.text)
	case exprCall:
		parts := make([]string, len(e.args))
		for i, a := range e.args {
			parts[i] = canonical(a)
		}
		return strings.ToLower(e.text) + "(" + strings.Join(parts, ",") + ")"
	case exprMember:
		return canonical(e.base) + "." + e.text
	case exprIndex:
		return canonical(e.base) + "[" + canonical(e.args[0]) + "]"
	}
	return ""
}

// fold returns the literal text of e when it is a string, a number or a concat of such values.
func fold(e *expr) (string, bool) {
	switch e.kind {
	case exprString, exprNumber:
		return e.text, true
	case exprCall:
		if !strings.EqualFold(e.text, "concat") || len(e.args) == 0 {
			return "", false
		}
		var b strings.Builder
		for _, a := range e.args {
			s, ok := fold(a)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return b.String(), true
	}
	return "", false
}

// text returns the folded literal when possible, otherwise the canonical text.
func text(e *expr) string {
	if s, ok := fold(e); ok {
		return s
	}
	return canonical(e)
}

// Finding kinds produced by analyzeExpr.
type findKind uint8

const (
	findResource   findKind = iota // key is lower(type)/name
	findNamed                      // a symbolic or plain resource name
	findParameter                  // parameters('name')
	findUnresolved                 // reason says why
)

// Unresolved reasons.
const (
	ReasonMalformedExpression = "malformed-expression"
	ReasonUnsupportedPrefix   = "unsupported-function:"
	ReasonUnresolvedTarget    = "unresolved-target"
	ReasonTargetNotInTemplate = "target-not-in-template"
	ReasonUnknownParameter    = "unknown-parameter"
	ReasonUnknownService      = "unknown-service"
	ReasonMalformedInterp     = "malformed-interpolation"
)

type finding struct {
	kind   findKind
	key    string // findResource: "<lower type>/<names>", findNamed: the name, findParameter: the name
	reason string
}

// analyzeExpr scans s and returns every reference it finds. Recognised functions are resourceId,
// reference, parameters and concat; any other function is reported unresolved.
func analyzeExpr(s string) []finding {
	e, err := parseExpression(s)
	if err != nil {
		return []finding{{kind: findUnresolved, reason: ReasonMalformedExpression}}
	}
	var out []finding
	walkExpr(e, &out)
	return out
}

func walkExpr(e *expr, out *[]finding) {
	switch e.kind {
	case exprMember:
		walkExpr(e.base, out)
		return
	case exprIndex:
		walkExpr(e.base, out)
		walkExpr(e.args[0], out)
		return
	case exprCall:
	default:
		return
	}
	name := strings.ToLower(e.text)
	switch name {
	case "resourceid":
		*out = append(*out, resourceIDFinding(e))
	case "reference":
		if f, ok := referenceFinding(e); ok {
			*out = append(*out, f)
		}
	case "parameters":
		if len(e.args) == 1 {
			if s, ok := fold(e.args[0]); ok {
				*out = append(*out, finding{kind: findParameter, key: s})
				break
			}
		}
		*out = append(*out, finding{kind: findUnresolved, reason: ReasonUnresolvedTarget})
	case "concat":
	default:
		*out = append(*out, finding{kind: findUnresolved, reason: ReasonUnsupportedPrefix + name})
	}
	for _, a := range e.args {
		walkExpr(a, out)
	}
}

// resourceIDFinding reads resourceId([subscriptionId, [resourceGroup,]] type, name...). The type is the first of
// the leading three arguments that is a literal containing "/".
func resourceIDFinding(e *expr) finding {
	unresolved := finding{kind: findUnresolved, reason: ReasonUnresolvedTarget}
	limit := min(3, len(e.args))
	for i := 0; i < limit; i++ {
		t, ok := fold(e.args[i])
		if !ok || !strings.Contains(t, "/") {
			continue
		}
		names := e.args[i+1:]
		if len(names) == 0 {
			return unresolved
		}
		parts := make([]string, len(names))
		for j, n := range names {
			parts[j] = text(n)
		}
		return finding{kind: findResource, key: strings.ToLower(t) + "/" + strings.Join(parts, "/")}
	}
	return unresolved
}

// referenceFinding reads reference(target, ...). A resourceId first argument is reported by its own walk.
func referenceFinding(e *expr) (finding, bool) {
	if len(e.args) == 0 {
		return finding{kind: findUnresolved, reason: ReasonMalformedExpression}, true
	}
	a := e.args[0]
	if a.kind == exprCall && strings.EqualFold(a.text, "resourceid") {
		return finding{}, false
	}
	if s, ok := fold(a); ok {
		return finding{kind: findNamed, key: s}, true
	}
	return finding{kind: findUnresolved, reason: ReasonUnresolvedTarget}, true
}

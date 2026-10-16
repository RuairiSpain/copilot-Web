package armexpr

import (
	"strconv"
	"strings"
)

// Evaluation bounds. They keep a hostile expression from exhausting time or
// memory.
const (
	maxEvalBytes = 64 << 10
	maxEvalDepth = 32
)

// Eval evaluates an ARM expression ("[...]") that consists only of literals
// and the pure functions true, false, not, and, or, equals, if, concat,
// format, toLower, toUpper and empty. It returns a string, bool or int64.
//
// It is not an ARM evaluator. Anything else (a parameter or variable
// reference, resourceGroup(), uniqueString(), a property access, a format
// specifier) returns ok false, and the caller must treat the value as unknown.
// equals compares strings case-insensitively, as ARM does.
func Eval(s string) (any, bool) {
	s = strings.TrimSpace(s)
	if !IsExpression(s) || !strings.HasSuffix(s, "]") || len(s) > maxEvalBytes {
		return nil, false
	}
	p := &evalParser{s: s[1 : len(s)-1]}
	v, ok := p.expr(0)
	if !ok {
		return nil, false
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, false
	}
	return v, true
}

// Simplify returns the plain value of s when Eval can evaluate it: the string
// itself (a leading "[" escaped as "[["), or "[true()]" / "[false()]" for a
// boolean. An integer result and any expression Eval cannot evaluate return s
// unchanged.
func Simplify(s string) string {
	v, ok := Eval(s)
	if !ok {
		return s
	}
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "[") {
			return "[" + x
		}
		return x
	case bool:
		if x {
			return "[true()]"
		}
		return "[false()]"
	}
	return s
}

// SimplifyValue is Simplify for a template property value: it also returns an
// integer result as a float64, the type encoding/json gives a number.
func SimplifyValue(s string) any {
	v, ok := Eval(s)
	if !ok {
		return s
	}
	switch x := v.(type) {
	case string:
		return Simplify(s)
	case bool:
		return x
	case int64:
		return float64(x)
	}
	return s
}

type evalParser struct {
	s string
	i int
}

func (p *evalParser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *evalParser) expr(depth int) (any, bool) {
	if depth > maxEvalDepth {
		return nil, false
	}
	p.skip()
	if p.i >= len(p.s) {
		return nil, false
	}
	c := p.s[p.i]
	switch {
	case c == '\'':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		j := p.i
		if c == '-' {
			j++
		}
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
		}
		n, err := strconv.ParseInt(p.s[p.i:j], 10, 64)
		if err != nil {
			return nil, false
		}
		p.i = j
		return n, true
	case isIdentStart(c):
		j := p.i
		for j < len(p.s) && isIdentOrDot(p.s[j]) && p.s[j] != '.' {
			j++
		}
		name := strings.ToLower(p.s[p.i:j])
		p.i = j
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != '(' {
			return nil, false
		}
		p.i++
		var args []any
		p.skip()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
		} else {
			for {
				v, ok := p.expr(depth + 1)
				if !ok {
					return nil, false
				}
				args = append(args, v)
				p.skip()
				if p.i >= len(p.s) {
					return nil, false
				}
				if p.s[p.i] == ',' {
					p.i++
					continue
				}
				if p.s[p.i] == ')' {
					p.i++
					break
				}
				return nil, false
			}
		}
		// A property or index access on the result is not evaluated.
		if p.i < len(p.s) && (p.s[p.i] == '.' || p.s[p.i] == '[') {
			return nil, false
		}
		return applyFn(name, args)
	}
	return nil, false
}

func (p *evalParser) str() (any, bool) {
	var b strings.Builder
	p.i++
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\'' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
				b.WriteByte('\'')
				p.i += 2
				continue
			}
			p.i++
			return b.String(), true
		}
		b.WriteByte(c)
		p.i++
	}
	return nil, false
}

func applyFn(name string, args []any) (any, bool) {
	switch name {
	case "true", "false":
		if len(args) != 0 {
			return nil, false
		}
		return name == "true", true
	case "not":
		if len(args) != 1 {
			return nil, false
		}
		b, ok := args[0].(bool)
		return !b, ok
	case "and", "or":
		if len(args) < 2 {
			return nil, false
		}
		res := name == "and"
		for _, a := range args {
			b, ok := a.(bool)
			if !ok {
				return nil, false
			}
			if name == "and" {
				res = res && b
			} else {
				res = res || b
			}
		}
		return res, true
	case "equals":
		if len(args) != 2 {
			return nil, false
		}
		if a, ok := args[0].(string); ok {
			b, ok := args[1].(string)
			return ok && strings.EqualFold(a, b), true
		}
		return args[0] == args[1], true
	case "if":
		if len(args) != 3 {
			return nil, false
		}
		c, ok := args[0].(bool)
		if !ok {
			return nil, false
		}
		if c {
			return args[1], true
		}
		return args[2], true
	case "concat":
		if len(args) == 0 {
			return nil, false
		}
		var b strings.Builder
		for _, a := range args {
			t, ok := text(a)
			if !ok {
				return nil, false
			}
			b.WriteString(t)
		}
		return b.String(), true
	case "tolower", "toupper":
		if len(args) != 1 {
			return nil, false
		}
		t, ok := args[0].(string)
		if !ok {
			return nil, false
		}
		if name == "tolower" {
			return strings.ToLower(t), true
		}
		return strings.ToUpper(t), true
	case "empty":
		if len(args) != 1 {
			return nil, false
		}
		t, ok := args[0].(string)
		return t == "", ok
	case "format":
		return formatFn(args)
	}
	return nil, false
}

func text(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int64:
		return strconv.FormatInt(x, 10), true
	}
	return "", false
}

// formatFn implements format() for plain {n} placeholders and the {{ }}
// escapes. A format specifier or a non-text argument is not evaluated.
func formatFn(args []any) (any, bool) {
	if len(args) < 1 {
		return nil, false
	}
	f, ok := args[0].(string)
	if !ok {
		return nil, false
	}
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		switch c := f[i]; c {
		case '{':
			if i+1 < len(f) && f[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			j := strings.IndexByte(f[i:], '}')
			if j < 0 {
				return nil, false
			}
			n, err := strconv.Atoi(f[i+1 : i+j])
			if err != nil || n < 0 || n+1 >= len(args) {
				return nil, false
			}
			t, ok := text(args[n+1])
			if !ok {
				return nil, false
			}
			b.WriteString(t)
			i += j
		case '}':
			if i+1 < len(f) && f[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			return nil, false
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

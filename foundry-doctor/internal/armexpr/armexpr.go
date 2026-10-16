// Package armexpr is the one place the doctor reads ARM template language
// expressions ("[format('ai-{0}', variables('t'))]") as they appear in compiled
// Bicep. It is not a Bicep parser and not an ARM evaluator: it splits a
// function call into arguments, extracts resource type and name segments from
// resourceId-style calls, and compares two references conservatively.
//
// The comparison has three outcomes. Two references are Same only when every
// name segment is a literal equal to the other or an expression with identical
// normalised text. They are Different only when a literal segment, or the
// literal start of a format() expression, provably differs. Everything else is
// Unknown, and rules must treat Unknown as "cannot correlate": a skip, never a
// pass or a fail.
package armexpr

import (
	"regexp"
	"strings"
)

// IsExpression reports whether s is an ARM language expression: it starts with
// "[" and not "[[" (the escape for a literal bracket).
func IsExpression(s string) bool {
	return strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "[[")
}

// Normalize returns the canonical comparison text of an expression: outer
// brackets removed, whitespace outside string literals removed and the text
// lower-cased (ARM resource names and types compare case-insensitively).
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if IsExpression(s) && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if !inQuote && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			continue
		}
		b.WriteByte(c)
	}
	return strings.ToLower(b.String())
}

// Call is a parsed function call such as format('a-{0}', x).
type Call struct {
	Fn   string   // lower-cased function name
	Args []string // raw argument texts, trimmed
	// Suffix is the text after the closing parenthesis (".id", ".subnets[0].id").
	Suffix string
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

// ParseCall parses "[fn(args)suffix]" or "fn(args)suffix".
func ParseCall(expr string) (Call, bool) {
	s := strings.TrimSpace(expr)
	if IsExpression(s) {
		if !strings.HasSuffix(s, "]") {
			return Call{}, false
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	id := identRe.FindString(s)
	if id == "" || len(s) <= len(id) || s[len(id)] != '(' {
		return Call{}, false
	}
	end := matchParen(s, len(id))
	if end < 0 {
		return Call{}, false
	}
	args, ok := SplitArgs(s[len(id)+1 : end])
	if !ok {
		return Call{}, false
	}
	return Call{Fn: strings.ToLower(id), Args: args, Suffix: strings.TrimSpace(s[end+1:])}, true
}

// matchParen returns the index of the parenthesis closing the one at open, or -1.
func matchParen(s string, open int) int {
	depth := 0
	inQuote := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// SplitArgs splits a call body at top-level commas.
func SplitArgs(body string) ([]string, bool) {
	var out []string
	depth, start := 0, 0
	inQuote := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '\'' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch c {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	if inQuote || depth != 0 {
		return nil, false
	}
	if last := strings.TrimSpace(body[start:]); last != "" || len(out) > 0 {
		out = append(out, last)
	}
	return out, true
}

// Unquote returns the value of a single-quoted ARM string literal.
func Unquote(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || arg[0] != '\'' || arg[len(arg)-1] != '\'' {
		return "", false
	}
	inner := arg[1 : len(arg)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\'' {
			if i+1 < len(inner) && inner[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return "", false
		}
		b.WriteByte(inner[i])
	}
	return b.String(), true
}

// Fold returns the literal value of the two all-literal shapes older
// templates use for child resource names and references:
// format('{0}/{1}', 'a', 'b') becomes "a/b" and resourceId('type', 'a', 'b')
// becomes "a/b" (the type is dropped, as the model has always done). Any
// other string, including a call with a non-literal argument, is returned
// unchanged with ok false.
func Fold(s string) (string, bool) {
	if !IsExpression(s) {
		return s, false
	}
	c, ok := ParseCall(s)
	if !ok || c.Suffix != "" {
		return s, false
	}
	lits := make([]string, len(c.Args))
	for i, a := range c.Args {
		v, ok := Unquote(a)
		if !ok {
			return s, false
		}
		lits[i] = v
	}
	switch c.Fn {
	case "format":
		if len(lits) < 1 {
			return s, false
		}
		for _, ch := range lits[0] {
			if !(ch == '/' || (ch >= '0' && ch <= '9') || ch == '{' || ch == '}') {
				return s, false
			}
		}
		out := lits[0]
		for i, v := range lits[1:] {
			out = strings.ReplaceAll(out, "{"+itoa(i)+"}", v)
		}
		if strings.ContainsAny(out, "{}") {
			return s, false
		}
		return out, true
	case "resourceid":
		if len(lits) < 2 {
			return s, false
		}
		return strings.Join(lits[1:], "/"), true
	}
	return s, false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

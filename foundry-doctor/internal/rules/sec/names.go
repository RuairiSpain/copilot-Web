package sec

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// This file reads ARM names and resource IDs lexically. It evaluates nothing: a function call that it
// does not recognise is kept as an opaque, canonical token, and two tokens match only when their
// canonical text is identical. It is not a Bicep or ARM expression parser.

// splitCall parses "[name(a, b, ...)]" into the lower-case function name and its raw arguments.
func splitCall(s string) (string, []string, bool) {
	if !model.IsARMExpression(s) {
		return "", nil, false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	open := strings.IndexByte(inner, '(')
	if open <= 0 || !strings.HasSuffix(inner, ")") {
		return "", nil, false
	}
	name := strings.TrimSpace(inner[:open])
	for _, c := range name {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", nil, false
		}
	}
	args, ok := splitArgs(inner[open+1 : len(inner)-1])
	if !ok {
		return "", nil, false
	}
	return strings.ToLower(name), args, true
}

// splitArgs splits on top-level commas, honouring quotes (” is an escaped quote) and brackets.
func splitArgs(s string) ([]string, bool) {
	var out []string
	depth, start, inQuote := 0, 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inQuote = false
				}
			}
		case c == '\'':
			inQuote = true
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
			if depth < 0 {
				return nil, false
			}
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if inQuote || depth != 0 {
		return nil, false
	}
	if last := strings.TrimSpace(s[start:]); last != "" || len(out) > 0 {
		out = append(out, last)
	}
	return out, true
}

// quoted returns the text of a single-quoted ARM string literal argument.
func quoted(a string) (string, bool) {
	a = strings.TrimSpace(a)
	if len(a) < 2 || a[0] != '\'' || a[len(a)-1] != '\'' {
		return "", false
	}
	body := a[1 : len(a)-1]
	if strings.Contains(strings.ReplaceAll(body, "''", ""), "'") {
		return "", false
	}
	return strings.ReplaceAll(body, "''", "'"), true
}

// canon renders an argument in a comparable form: no whitespace outside quotes, lower case.
func canon(a string) string {
	var b strings.Builder
	inQuote := false
	for _, r := range a {
		switch {
		case r == '\'':
			inQuote = !inQuote
			b.WriteRune(r)
		case inQuote:
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
		default:
			b.WriteString(strings.ToLower(string(r)))
		}
	}
	return b.String()
}

// piece renders one name argument: a literal as its lower-case text, anything else as "{canonical}".
func piece(a string) string {
	if s, ok := quoted(a); ok {
		return strings.ToLower(s)
	}
	return "{" + canon(a) + "}"
}

// nameSegments returns the "/"-separated segments of an ARM resource name, lower-cased. Expression
// parts that are not literals appear as "{canonical}". A literal name and the same name written as a
// format() with literal arguments give the same segments.
func nameSegments(name string) []string {
	if name == "" {
		return nil
	}
	if !model.IsARMExpression(name) {
		return strings.Split(strings.ToLower(name), "/")
	}
	fn, args, ok := splitCall(name)
	if ok && fn == "format" && len(args) >= 1 {
		if f, lit := quoted(args[0]); lit {
			var b strings.Builder
			for i := 0; i < len(f); i++ {
				if f[i] != '{' {
					b.WriteByte(f[i])
					continue
				}
				j := strings.IndexByte(f[i:], '}')
				n, good := 0, j > 1
				if good {
					for _, d := range f[i+1 : i+j] {
						if d < '0' || d > '9' {
							good = false
							break
						}
						n = n*10 + int(d-'0')
					}
				}
				if !good || n+1 >= len(args) {
					return []string{"{" + canon(name[1:len(name)-1]) + "}"}
				}
				b.WriteString(piece(args[n+1]))
				i += j
			}
			return strings.Split(strings.ToLower(b.String()), "/")
		}
	}
	return []string{"{" + canon(name[1:len(name)-1]) + "}"}
}

// lastName returns the last segment of a resource name when it is a plain literal.
func lastName(name string) string {
	segs := nameSegments(name)
	if len(segs) == 0 {
		return ""
	}
	l := segs[len(segs)-1]
	if strings.Contains(l, "{") {
		return ""
	}
	return l
}

// identityKey is "<lower type>|<segments>" for a resource, or "" when the name cannot be read.
func identityKey(typ, name string) string {
	segs := nameSegments(name)
	if len(segs) == 0 || typ == "" {
		return ""
	}
	return strings.ToLower(typ) + "|" + strings.Join(segs, "/")
}

// idKey reads a resource ID value (a literal ARM ID or a resourceId() call) into an identity key.
// It returns "" when the value is neither, for example a bare parameter reference.
func idKey(v string) string {
	if !model.IsARMExpression(v) {
		const marker = "/providers/"
		i := strings.Index(strings.ToLower(v), marker)
		if i < 0 {
			return ""
		}
		parts := strings.Split(v[i+len(marker):], "/")
		if len(parts) < 3 || len(parts)%2 == 0 {
			return ""
		}
		typ, names := parts[0], make([]string, 0, len(parts)/2)
		for k := 1; k < len(parts); k += 2 {
			typ += "/" + parts[k]
			names = append(names, parts[k+1])
		}
		return strings.ToLower(typ) + "|" + strings.ToLower(strings.Join(names, "/"))
	}
	fn, args, ok := splitCall(v)
	if !ok || fn != "resourceid" {
		return ""
	}
	for i := 0; i < len(args) && i < 3; i++ {
		if t, lit := quoted(args[i]); lit && strings.Contains(t, "/") {
			if i+1 >= len(args) {
				return ""
			}
			names := make([]string, 0, len(args)-i-1)
			for _, a := range args[i+1:] {
				names = append(names, piece(a))
			}
			return strings.ToLower(t) + "|" + strings.Join(names, "/")
		}
	}
	return ""
}

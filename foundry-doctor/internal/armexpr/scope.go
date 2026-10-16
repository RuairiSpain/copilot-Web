package armexpr

import (
	"regexp"
	"strings"
)

// ScopeSep separates a parameter or variable name from the deployment path of
// the nested template it is local to: parameters('name@main/search1').
//
// A compiled Bicep module is a nested ARM template with its own parameters and
// variables. The text parameters('name') in two different modules names two
// unrelated values, so text equality must not be read as value equality across
// templates. armmodel therefore rewrites every module-local reference it cannot
// substitute to carry the deployment path, which makes the normalised text of
// two different scopes differ and Compare return Unknown between them.
const ScopeSep = "@"

// ScopedName returns name qualified with the deployment path scope. An empty
// scope (the root template) returns name unchanged.
func ScopedName(name, scope string) string {
	if scope == "" {
		return name
	}
	return name + ScopeSep + scope
}

var scopedRefRe = regexp.MustCompile(`(?i)((?:parameters|variables)\(\s*'[^'@]*)@[^']*'`)

// StripScope removes the deployment-path qualification that armmodel adds to
// parameters('x@path') and variables('x@path') references, for display and for
// name heuristics. Text without qualification is returned unchanged.
func StripScope(s string) string {
	if !strings.Contains(s, ScopeSep) {
		return s
	}
	return scopedRefRe.ReplaceAllString(s, "$1'")
}

// Ref kinds passed to a RewriteRefs callback.
const (
	RefParameter = "parameters"
	RefVariable  = "variables"
)

// RewriteRefs replaces each parameters('x') and variables('x') call in the ARM
// expression s. f receives the kind (RefParameter or RefVariable) and name and
// returns replacement expression text (without brackets) and true, or false to
// keep the call as it is. A string that is not an expression is returned
// unchanged. When the result is a single string literal the plain literal is
// returned (with a leading "[" escaped as "[[").
func RewriteRefs(s string, f func(kind, name string) (string, bool)) string {
	if !IsExpression(s) || !strings.HasSuffix(s, "]") {
		return s
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	changed := false
	inQuote := false
	for i := 0; i < len(inner); {
		c := inner[i]
		if c == '\'' {
			inQuote = !inQuote
			b.WriteByte(c)
			i++
			continue
		}
		if !inQuote && isIdentStart(c) && (i == 0 || !isIdentOrDot(inner[i-1])) {
			if kind, name, n, ok := matchRefCall(inner[i:]); ok {
				if repl, ok := f(kind, name); ok {
					rest := inner[i+n:]
					if !(strings.HasPrefix(repl, "'") && (strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "["))) {
						b.WriteString(repl)
						i += n
						changed = true
						continue
					}
				}
			}
		}
		b.WriteByte(c)
		i++
	}
	if !changed {
		return s
	}
	out := b.String()
	if v, ok := Unquote(out); ok {
		if strings.HasPrefix(v, "[") {
			return "[" + v
		}
		return v
	}
	return "[" + out + "]"
}

func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }

func isIdentOrDot(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.'
}

// matchRefCall matches parameters('x') or variables('x') at the start of s and
// returns its length.
func matchRefCall(s string) (kind, name string, n int, ok bool) {
	for _, k := range []string{RefParameter, RefVariable} {
		if len(s) < len(k) || !strings.EqualFold(s[:len(k)], k) {
			continue
		}
		i := skipSpace(s, len(k))
		if i >= len(s) || s[i] != '(' {
			continue
		}
		i = skipSpace(s, i+1)
		if i >= len(s) || s[i] != '\'' {
			continue
		}
		end := strings.IndexByte(s[i+1:], '\'')
		if end < 0 {
			continue
		}
		name = s[i+1 : i+1+end]
		i = skipSpace(s, i+1+end+1)
		if i >= len(s) || s[i] != ')' {
			continue
		}
		return k, name, i + 1, true
	}
	return "", "", 0, false
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// QuoteLiteral renders s as an ARM single-quoted string literal.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ExprText returns the expression text (no brackets) that yields the string
// value v: the inner text of an expression, or a quoted literal.
func ExprText(v string) string {
	if IsExpression(v) && strings.HasSuffix(v, "]") {
		return v[1 : len(v)-1]
	}
	if strings.HasPrefix(v, "[[") {
		v = v[1:]
	}
	return QuoteLiteral(v)
}

// CompareValues compares two scalar template values such as the Region of two
// resources. Two literals compare case-insensitively. Two expressions are Same
// only when their normalised text is equal, which armmodel guarantees means the
// same value only within one deployment scope (module-local references carry
// their scope, see ScopeSep). A literal against an expression, or two different
// expressions, is Unknown. Empty values are Unknown.
func CompareValues(a, b string) Result {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return Unknown
	}
	ae, be := IsExpression(a), IsExpression(b)
	switch {
	case !ae && !be:
		if strings.EqualFold(a, b) {
			return Same
		}
		return Different
	case ae && be:
		if Normalize(a) == Normalize(b) {
			return Same
		}
	}
	return Unknown
}

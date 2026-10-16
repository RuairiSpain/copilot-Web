package ops

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The helpers in this file read compiled ARM expression text. They are a narrow
// duplicate of the helpers in the cost and rel rule packages (no shared package
// by design). They never evaluate an expression; they only compare text.

// isExpr reports whether s is an ARM expression: it starts with "[" and is not
// the "[[" escape for a literal leading bracket.
func isExpr(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[")
}

// exprBody removes the outer brackets of an expression.
func exprBody(s string) string {
	t := strings.TrimSpace(s)
	if isExpr(t) && strings.HasSuffix(t, "]") {
		return strings.TrimSpace(t[1 : len(t)-1])
	}
	return t
}

// normExpr lower-cases an expression body and removes whitespace outside quotes.
func normExpr(s string) string {
	var b strings.Builder
	inQuote := false
	for _, r := range exprBody(s) {
		if r == '\'' {
			inQuote = !inQuote
		}
		if !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r') {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// splitCall splits "fn(a, b(c, d), 'e,f')" into "fn" and its top-level
// arguments. ok is false when s is not a single function call.
func splitCall(s string) (fn string, args []string, ok bool) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return "", nil, false
	}
	fn = strings.TrimSpace(s[:open])
	inner := s[open+1 : len(s)-1]
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(inner); i++ {
		switch c := inner[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
			if depth < 0 {
				return "", nil, false
			}
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(inner[start:i]))
			start = i + 1
		}
	}
	if depth != 0 || inQuote {
		return "", nil, false
	}
	if last := strings.TrimSpace(inner[start:]); last != "" || len(args) > 0 {
		args = append(args, last)
	}
	return fn, args, true
}

// quoted returns the content of a single-quoted ARM string literal.
func quoted(arg string) (string, bool) {
	a := strings.TrimSpace(arg)
	if len(a) >= 2 && a[0] == '\'' && a[len(a)-1] == '\'' && !strings.Contains(a[1:len(a)-1], "'") {
		return a[1 : len(a)-1], true
	}
	return "", false
}

type deployState int

const (
	stateLive deployState = iota
	stateAbsent
	stateUncertain
)

// stateOf classifies a resource using the Conditional/Condition/Looped fields.
// A literal-false condition means the resource is not deployed; any other
// condition, or a copy loop, leaves deployment uncertain.
func stateOf(r sdk.ARMResource) deployState {
	if r.Conditional {
		switch normExpr(r.Condition) {
		case "false", "false()":
			return stateAbsent
		case "true", "true()":
			if !r.Looped {
				return stateLive
			}
		}
		return stateUncertain
	}
	if r.Looped {
		return stateUncertain
	}
	return stateLive
}

// view splits the model's resources by deployment state.
type view struct {
	live []sdk.ARMResource
	unc  []sdk.ARMResource
}

func newView(in *sdk.Input) *view {
	v := &view{}
	if in == nil || in.ARM == nil {
		return v
	}
	for _, r := range in.ARM.Resources() {
		switch stateOf(r) {
		case stateLive:
			v.live = append(v.live, r)
		case stateUncertain:
			v.unc = append(v.unc, r)
		}
	}
	return v
}

func typeIn(typ string, types []string) bool {
	for _, t := range types {
		if strings.EqualFold(t, typ) {
			return true
		}
	}
	return false
}

// of returns the live resources of the given types. Uncertain resources of those
// types are recorded on o as notes so that the rule cannot report a clean pass.
func (v *view) of(o *outcome, types ...string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range v.live {
		if typeIn(r.Type, types) {
			out = append(out, r)
		}
	}
	for _, r := range v.unc {
		if typeIn(r.Type, types) {
			o.notes = append(o.notes, r.Name+": conditional or looped deployment cannot be resolved offline")
		}
	}
	return out
}

// hasUncertain reports whether an uncertain resource of the given types exists.
func (v *view) hasUncertain(types ...string) bool {
	for _, r := range v.unc {
		if typeIn(r.Type, types) {
			return true
		}
	}
	return false
}

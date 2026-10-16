package armmodel

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// maxVariableDepth bounds the chain of variables that reference variables.
const maxVariableDepth = 16

// rootBindings computes the text each non-secure root parameter is replaced
// by: the parameter-file value when there is one, otherwise (only when a
// parameter file exists) the literal Bicep default, marked default-derived.
// A secure parameter, an expression default and an unresolved file value get
// no binding, so parameters('x') stays an expression and compares as unknown.
func rootBindings(t *bicep.Template, opt Options) map[string]binding {
	out := map[string]binding{}
	if len(opt.Parameters) == 0 && !opt.ParametersProvided {
		return out
	}
	given := make(map[string]any, len(opt.Parameters))
	for k, v := range opt.Parameters {
		given[strings.ToLower(k)] = v
	}
	for _, p := range t.Parameters {
		if p.Secure {
			continue
		}
		key := strings.ToLower(p.Name)
		if v, set := given[key]; set {
			if text, ok := paramText(p.Type, v); ok && len(text) <= MaxBindingBytes {
				out[key] = binding{text: text}
			}
			continue
		}
		if !opt.ParametersProvided || len(p.Default) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(p.Default))
		dec.UseNumber()
		var d any
		if dec.Decode(&d) != nil {
			continue
		}
		if text, ok := paramText(p.Type, d); ok && len(text) <= MaxBindingBytes {
			out[key] = binding{text: text, defaults: []string{p.Name}}
		}
	}
	return out
}

// paramText renders a parameter value of ARM type typ as expression text. Only
// string, int and bool parameters are rendered; a string that starts with "["
// would be read as an expression and is not rendered.
func paramText(typ string, v any) (string, bool) {
	switch strings.ToLower(typ) {
	case "string":
		s, ok := v.(string)
		if !ok || strings.HasPrefix(s, "[") {
			return "", false
		}
		return armexpr.QuoteLiteral(s), true
	case "bool":
		switch x := v.(type) {
		case bool:
			return boolText(x), true
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true":
				return "true()", true
			case "false":
				return "false()", true
			}
		}
	case "int":
		var s string
		switch x := v.(type) {
		case json.Number:
			s = x.String()
		case string:
			s = strings.TrimSpace(x)
		case float64:
			if x == float64(int64(x)) {
				s = strconv.FormatInt(int64(x), 10)
			}
		case int:
			s = strconv.Itoa(x)
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return strconv.FormatInt(n, 10), true
		}
	}
	return "", false
}

func boolText(b bool) string {
	if b {
		return "true()"
	}
	return "false()"
}

// injector returns the root template's expression rewriter: it replaces
// parameters('x') with its binding and evaluates what became literal. It
// records the default-derived parameters it uses in bud.
func injector(bound map[string]binding, bud *budget) func(string) string {
	if len(bound) == 0 {
		return func(s string) string { return s }
	}
	return func(s string) string {
		used := 0
		out := armexpr.RewriteRefs(s, func(kind, name string) (string, bool) {
			if kind != armexpr.RefParameter {
				return "", false
			}
			b, ok := bound[strings.ToLower(name)]
			if !ok || used+len(b.text) > MaxExprBytes {
				return "", false
			}
			used += len(b.text)
			bud.note(b.defaults...)
			return b.text, true
		})
		if out != s {
			out = armexpr.Simplify(out)
		}
		return out
	}
}

// union returns the sorted set union of a and b, nil when both are empty.
func union(a, b []string) []string {
	if len(a)+len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// addMeta merges the per-resource detail of res into the model.
func (m *Model) addMeta(res sdk.ARMResource, tagsExpr bool, defaults []string) {
	if !tagsExpr && len(defaults) == 0 {
		return
	}
	k := resourceKey(res)
	cur := m.meta[k]
	cur.tagsExpr = cur.tagsExpr || tagsExpr
	cur.defaults = union(cur.defaults, defaults)
	m.meta[k] = cur
}

// Variable returns the value of the ROOT template variable name. The value is
// the compiled literal (string, bool, float64, map[string]any, []any), or the
// result of substituting the root parameter values and the other variables
// into the compiled expression. A string result can still be an ARM
// expression, for example "[subscriptionResourceId('Microsoft.Authorization/
// roleDefinitions', '8e3af657-...')]", when its text no longer refers to a
// parameter or variable: such a value is as resolved as the template allows,
// and armexpr reads it (RoleDefinitionGUID, ClassifyScope).
//
// ok is false when the variable does not exist, was dropped by the size
// bound, or still depends on a parameter without a value or on a variable that
// cannot be resolved. Variables of module templates are not exposed.
// Parameter-file values are used; literal defaults are not applied to a
// variable (defaults reach a variable only through Options.ParametersProvided,
// like every other expression).
func (m *Model) Variable(name string) (any, bool) {
	if m == nil {
		return nil, false
	}
	return m.variable(strings.ToLower(name), 0)
}

func (m *Model) variable(key string, depth int) (any, bool) {
	raw, ok := m.vars[key]
	if !ok || depth > maxVariableDepth {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	return m.resolveValue(v, depth)
}

func (m *Model) resolveValue(v any, depth int) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case bool:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		return m.resolveString(x, depth)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r, ok := m.resolveValue(e, depth)
			if !ok {
				return nil, false
			}
			out[k] = r
		}
		return out, true
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, ok := m.resolveValue(e, depth)
			if !ok {
				return nil, false
			}
			out[i] = r
		}
		return out, true
	}
	return nil, false
}

func (m *Model) resolveString(s string, depth int) (any, bool) {
	if !armexpr.IsExpression(s) {
		if strings.HasPrefix(s, "[[") {
			return s[1:], true
		}
		return s, true
	}
	txt := s
	if m.varTx != nil {
		txt = m.varTx(txt)
	}
	failed := false
	txt = armexpr.RewriteRefs(txt, func(kind, name string) (string, bool) {
		if kind != armexpr.RefVariable {
			return "", false
		}
		r, ok := m.variable(strings.ToLower(name), depth+1)
		if !ok {
			failed = true
			return "", false
		}
		switch y := r.(type) {
		case string:
			t := armexpr.ExprText(y)
			if len(t) > MaxBindingBytes {
				failed = true
				return "", false
			}
			return t, true
		case bool:
			return boolText(y), true
		case float64:
			if y == float64(int64(y)) {
				return strconv.FormatInt(int64(y), 10), true
			}
		}
		failed = true
		return "", false
	})
	if failed {
		return nil, false
	}
	if armexpr.IsExpression(txt) {
		// Whatever reference remains makes the value unknown.
		armexpr.RewriteRefs(txt, func(string, string) (string, bool) {
			failed = true
			return "", false
		})
		if failed {
			return nil, false
		}
		txt = armexpr.Simplify(txt)
	}
	if !armexpr.IsExpression(txt) {
		if strings.HasPrefix(txt, "[[") {
			return txt[1:], true
		}
		return txt, true
	}
	if v, ok := armexpr.Eval(txt); ok {
		if n, isInt := v.(int64); isInt {
			return float64(n), true
		}
		return v, true
	}
	return normalizeLiteralRef(txt), true
}

package model

import (
	"strconv"
	"strings"
)

// ValueState is the three-state result of reading a template value.
type ValueState int

// ValueState values. A rule must treat Unresolved as "cannot tell", never as pass or fail.
const (
	Absent     ValueState = iota // the path does not exist in the template
	Literal                      // the value is known
	Unresolved                   // the value exists but depends on a parameter, expression or runtime value
)

// String returns the state name.
func (s ValueState) String() string {
	switch s {
	case Literal:
		return "literal"
	case Unresolved:
		return "unresolved"
	}
	return "absent"
}

// Value is a template value read through Resource.Get.
type Value struct {
	State ValueState
	// Lit is the decoded JSON value (string, float64, bool, nil, []any, map[string]any) when State is Literal.
	Lit any
	// Reason says why the value is Unresolved, for example "arm-expression".
	Reason string
}

// IsLiteral reports whether the value is known.
func (v Value) IsLiteral() bool { return v.State == Literal }

// String returns the literal as a string, and false if it is not a literal string.
func (v Value) String() (string, bool) {
	s, ok := v.Lit.(string)
	return s, ok && v.State == Literal
}

// Bool returns the literal as a bool, and false if it is not a literal bool.
func (v Value) Bool() (b bool, ok bool) {
	b, ok = v.Lit.(bool)
	return b, ok && v.State == Literal
}

// Number returns the literal as a float64, and false if it is not a literal number.
func (v Value) Number() (f float64, ok bool) {
	f, ok = v.Lit.(float64)
	return f, ok && v.State == Literal
}

// IsARMExpression reports whether s is an ARM template expression ("[...]" but not the escaped "[[...]").
func IsARMExpression(s string) bool {
	return len(s) >= 2 && strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") && !strings.HasPrefix(s, "[[")
}

// ResourceMeta carries identity inside the compiled document and the flags rules need to decide how
// far to trust a finding. The Bicep normaliser fills it; it is zero for hand-built resources.
type ResourceMeta struct {
	Pointer     string   // JSON pointer into the compiled document, for example /resources/acct
	ModuleChain []string // labels of the enclosing module deployments, outermost first
	Looped      bool     // the resource itself has a top-level copy: the instance count is unknown
	Conditional bool     // the resource itself has a condition
	// MayNotDeploy is true when the resource or any enclosing module is conditional.
	MayNotDeploy bool
	// MultiInstance is true when the resource or any enclosing module is looped.
	MultiInstance bool
	// ConditionValue is a Literal bool only when the condition is a known boolean; Absent when
	// the resource is not conditional.
	ConditionValue Value
	// Confidence is "likely" for resources declared in the entry file (file known, line not) and
	// "uncertain" inside a module (the module file cannot be proven).
	Confidence string
}

// Resource is one resource of the compiled template.
type Resource struct {
	Type       string
	Name       string // the resource name as written; may be an expression
	APIVersion string
	Symbolic   string // symbolic name when the template uses them
	Body       map[string]any
	DependsOn  []string
	Location   Pos // best-effort source position in the Bicep file; zero if unknown
	File       string
	Meta       ResourceMeta
}

// Get reads a dotted path such as "properties.networkAcls.defaultAction". Array items use [n]
// ("properties.ipRules[0].value"). Key matching is case-sensitive. It returns Absent when a segment
// is missing, and Unresolved (reason "arm-expression") when the value, or any container on the way,
// is an ARM expression. A literal object or array is returned as Literal.
func (r Resource) Get(path string) Value {
	var cur any = r.Body
	for _, seg := range strings.Split(path, ".") {
		key, idx := seg, []int(nil)
		if i := strings.Index(seg, "["); i >= 0 {
			key = seg[:i]
			for _, part := range strings.Split(strings.TrimSuffix(seg[i+1:], "]"), "][") {
				n, err := strconv.Atoi(part)
				if err != nil || n < 0 {
					return Value{}
				}
				idx = append(idx, n)
			}
		}
		if s, ok := cur.(string); ok && IsARMExpression(s) {
			return Value{State: Unresolved, Reason: "arm-expression"}
		}
		if key != "" {
			m, ok := cur.(map[string]any)
			if !ok {
				return Value{}
			}
			if cur, ok = m[key]; !ok {
				return Value{}
			}
		}
		for _, n := range idx {
			if s, ok := cur.(string); ok && IsARMExpression(s) {
				return Value{State: Unresolved, Reason: "arm-expression"}
			}
			a, ok := cur.([]any)
			if !ok || n >= len(a) {
				return Value{}
			}
			cur = a[n]
		}
	}
	if s, ok := cur.(string); ok && IsARMExpression(s) {
		return Value{State: Unresolved, Reason: "arm-expression"}
	}
	return Value{State: Literal, Lit: cur}
}

// Output is a template output. Its value is never carried; rules need only the name and whether it is secure.
type Output struct {
	Name   string
	Type   string
	Secure bool
}

// ARMTemplate is the normalised compiled template.
type ARMTemplate struct {
	File       string // source module, relative
	Resources  []Resource
	Outputs    []Output
	Parameters []string
}

// ResourcesOfType returns resources whose type equals t, compared case-insensitively as ARM does.
func (t *ARMTemplate) ResourcesOfType(typ string) []Resource {
	if t == nil {
		return nil
	}
	var out []Resource
	for _, r := range t.Resources {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, r)
		}
	}
	return out
}

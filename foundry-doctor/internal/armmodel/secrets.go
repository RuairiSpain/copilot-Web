package armmodel

import (
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

// OutputDetail is one root-template output with its raw value text. Value is
// an ARM expression ("[parameters('x')]") or literal text; a non-string JSON
// value is its trimmed JSON text.
type OutputDetail struct {
	Name  string
	Type  string
	Value string
}

// secretFacts is what FND-SEC-014 needs from the root template: the output
// values, which parameters are secure and the variable values (so an output
// that reads a variable can be followed to the parameter or list*() call).
type secretFacts struct {
	outputs      []OutputDetail
	secureParams []string
	// vars is keyed by lower-case variable name; the value is the raw JSON
	// text of the variable, which is scanned as expression text.
	vars map[string]string
}

func secretFactsOf(t *bicep.Template) secretFacts {
	var f secretFacts
	for _, o := range t.Outputs {
		f.outputs = append(f.outputs, OutputDetail{Name: o.Name, Type: o.Type, Value: o.Value})
	}
	for _, p := range t.Parameters {
		if p.Secure {
			f.secureParams = append(f.secureParams, p.Name)
		}
	}
	sort.Strings(f.secureParams)
	f.vars = map[string]string{}
	for _, v := range t.Variables {
		f.vars[strings.ToLower(v.Name)] = string(v.Value)
	}
	return f
}

// OutputDetails returns the root template outputs with their value text, in
// template order. It is nil for a nil model.
func (m *Model) OutputDetails() []OutputDetail {
	if m == nil {
		return nil
	}
	return append([]OutputDetail(nil), m.secrets.outputs...)
}

// SecureParams returns the names of the root parameters declared
// securestring or secureObject, sorted.
func (m *Model) SecureParams() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.secrets.secureParams...)
}

// VariableText returns the raw JSON text of the root variable name
// (case-insensitive), and whether it exists.
func (m *Model) VariableText(name string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m.secrets.vars[strings.ToLower(name)]
	return v, ok
}

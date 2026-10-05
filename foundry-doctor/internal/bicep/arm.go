package bicep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const maxModuleDepth = 16

// Template is a normalised ARM template.
type Template struct {
	LanguageVersion string
	Parameters      []Parameter
	Resources       []Resource
	Outputs         []Output
}

// Parameter is a template parameter; Secure is true for securestring/secureObject.
type Parameter struct {
	Name   string
	Type   string
	Secure bool
}

// Output is a template output. Value is the raw ARM expression or literal text.
type Output struct {
	Name  string
	Type  string
	Value string
}

// Copy describes a loop (`for`) over a resource.
type Copy struct {
	Name  string
	Count string
}

// Resource is a normalised ARM resource.
type Resource struct {
	// Pointer is an RFC 6901 JSON pointer into the root ARM document.
	Pointer      string
	SymbolicName string // from the symbolic-name form, or copy.name when looped
	Type         string
	APIVersion   string
	Name         string
	Condition    string // raw condition expression; "" when unconditional
	Conditional  bool
	Copy         *Copy
	DependsOn    []string
	Properties   json.RawMessage
	// Nested is set for Microsoft.Resources/deployments with an inlined
	// template (a Bicep module).
	Nested *Template
}

// IsModule reports whether the resource is an inlined module deployment.
func (r Resource) IsModule() bool { return r.Nested != nil }

// Guards returns the human-readable guards (condition/copy) for evidence.
func (r Resource) Guards() []string {
	var g []string
	if r.Conditional {
		g = append(g, "condition: "+r.Condition)
	}
	if r.Copy != nil {
		g = append(g, "copy: "+r.Copy.Name)
	}
	return g
}

type rawResource struct {
	Type       string          `json:"type"`
	APIVersion string          `json:"apiVersion"`
	Name       json.RawMessage `json:"name"`
	Condition  json.RawMessage `json:"condition"`
	Copy       *struct {
		Name  string          `json:"name"`
		Count json.RawMessage `json:"count"`
	} `json:"copy"`
	DependsOn  []string        `json:"dependsOn"`
	Properties json.RawMessage `json:"properties"`
}

type rawTemplate struct {
	LanguageVersion string                     `json:"languageVersion"`
	Parameters      map[string]json.RawMessage `json:"parameters"`
	Resources       json.RawMessage            `json:"resources"`
	Outputs         map[string]struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	} `json:"outputs"`
}

// ParseARM normalises ARM JSON, supporting both the default array form of
// `resources` and the symbolic-name object form (languageVersion 2.0).
func ParseARM(data []byte) (*Template, error) {
	return parseTemplate(data, "", 0)
}

func rawString(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(r))
}

func parseTemplate(data []byte, base string, depth int) (*Template, error) {
	if depth > maxModuleDepth {
		return nil, toolErr(KindAdapter, "ARM module nesting too deep", nil)
	}
	var rt rawTemplate
	if err := json.Unmarshal(data, &rt); err != nil {
		return nil, toolErr(KindAdapter, "malformed ARM JSON", err)
	}
	t := &Template{LanguageVersion: rt.LanguageVersion}

	names := make([]string, 0, len(rt.Parameters))
	for n := range rt.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		var p struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(rt.Parameters[n], &p)
		lt := strings.ToLower(p.Type)
		t.Parameters = append(t.Parameters, Parameter{Name: n, Type: p.Type, Secure: lt == "securestring" || lt == "secureobject"})
	}
	names = names[:0]
	for n := range rt.Outputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		o := rt.Outputs[n]
		t.Outputs = append(t.Outputs, Output{Name: n, Type: o.Type, Value: rawString(o.Value)})
	}

	rs := bytes.TrimSpace(rt.Resources)
	switch {
	case len(rs) == 0 || string(rs) == "null":
	case rs[0] == '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(rs, &arr); err != nil {
			return nil, toolErr(KindAdapter, "malformed resources array", err)
		}
		for i, raw := range arr {
			r, err := parseResource(raw, fmt.Sprintf("%s/resources/%d", base, i), "", depth)
			if err != nil {
				return nil, err
			}
			t.Resources = append(t.Resources, r)
		}
	case rs[0] == '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(rs, &obj); err != nil {
			return nil, toolErr(KindAdapter, "malformed resources object", err)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			r, err := parseResource(obj[k], base+"/resources/"+escapePointer(k), k, depth)
			if err != nil {
				return nil, err
			}
			t.Resources = append(t.Resources, r)
		}
	default:
		return nil, toolErr(KindAdapter, "resources is neither array nor object", nil)
	}
	return t, nil
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func parseResource(raw json.RawMessage, ptr, symbolic string, depth int) (Resource, error) {
	var rr rawResource
	if err := json.Unmarshal(raw, &rr); err != nil {
		return Resource{}, toolErr(KindAdapter, "malformed resource", err)
	}
	r := Resource{
		Pointer: ptr, SymbolicName: symbolic, Type: rr.Type, APIVersion: rr.APIVersion,
		Name: rawString(rr.Name), DependsOn: append([]string(nil), rr.DependsOn...),
		Properties: rr.Properties,
	}
	sort.Strings(r.DependsOn)
	if c := bytes.TrimSpace(rr.Condition); len(c) > 0 && string(c) != "null" {
		r.Conditional = true
		r.Condition = rawString(c)
	}
	if rr.Copy != nil {
		r.Copy = &Copy{Name: rr.Copy.Name, Count: rawString(rr.Copy.Count)}
		if r.SymbolicName == "" {
			r.SymbolicName = rr.Copy.Name
		}
	}
	if strings.EqualFold(rr.Type, "Microsoft.Resources/deployments") && len(rr.Properties) > 0 {
		var p struct {
			Template json.RawMessage `json:"template"`
		}
		if json.Unmarshal(rr.Properties, &p) == nil && len(bytes.TrimSpace(p.Template)) > 0 && string(bytes.TrimSpace(p.Template)) != "null" {
			nt, err := parseTemplate(p.Template, ptr+"/properties/template", depth+1)
			if err != nil {
				return Resource{}, err
			}
			r.Nested = nt
		}
	}
	return r, nil
}

// Walk visits every resource depth-first (module deployment first, then its
// children) in deterministic order.
func (t *Template) Walk(fn func(r Resource, depth int)) {
	var walk func(t *Template, d int)
	walk = func(t *Template, d int) {
		for _, r := range t.Resources {
			fn(r, d)
			if r.Nested != nil {
				walk(r.Nested, d+1)
			}
		}
	}
	walk(t, 0)
}

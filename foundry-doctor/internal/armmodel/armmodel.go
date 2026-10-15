// Package armmodel maps a compiled ARM template (as normalised by
// internal/bicep) to the sdk.ARMModel the rules consume.
//
// It reads ARM JSON only; it never parses Bicep source. Values that ARM leaves
// as expressions ("[...]") are passed through unresolved and rules must treat
// them as unknown, never as a pass or a fail.
package armmodel

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// MaxResources bounds the model size so hostile templates cannot exhaust memory.
const MaxResources = 20000

// Options describe where the template came from.
type Options struct {
	// Entry is the repo-relative, slash-separated entry Bicep file. It is used
	// for resource locations; no line numbers are fabricated.
	Entry string
	// Modules optionally maps module deployment names to their source files.
	Modules bicep.ModuleFiles
}

// Model implements sdk.ARMModel.
type Model struct {
	resources []sdk.ARMResource
	outputs   []sdk.ARMOutput
}

var (
	_ sdk.ARMModel   = (*Model)(nil)
	_ sdk.ARMOutputs = (*Model)(nil)
)

// Outputs returns the top-level template outputs in template order.
func (m *Model) Outputs() []sdk.ARMOutput {
	if m == nil {
		return nil
	}
	return append([]sdk.ARMOutput(nil), m.outputs...)
}

// Resources returns the resources in deterministic template order.
func (m *Model) Resources() []sdk.ARMResource {
	if m == nil {
		return nil
	}
	out := make([]sdk.ARMResource, len(m.resources))
	copy(out, m.resources)
	return out
}

// FromARM parses ARM JSON (array or symbolic-name form) into a model.
func FromARM(raw []byte, opt Options) (*Model, error) {
	t, err := bicep.ParseARM(raw)
	if err != nil {
		return nil, err
	}
	return FromTemplate(t, raw, opt)
}

// FromTemplate maps t, which must have been produced from raw by
// bicep.ParseARM. raw supplies the top-level fields (location, kind, sku,
// identity, scope) that bicep.Template does not carry; they are located by the
// resource's JSON pointer.
func FromTemplate(t *bicep.Template, raw []byte, opt Options) (*Model, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	m := &Model{}
	for _, o := range t.Outputs {
		m.outputs = append(m.outputs, sdk.ARMOutput{Name: o.Name, Type: o.Type})
	}
	mapper := bicep.Mapper{Entry: opt.Entry, Modules: opt.Modules}
	var walk func(t *bicep.Template, ancestors []bicep.Resource)
	walk = func(t *bicep.Template, ancestors []bicep.Resource) {
		for _, r := range t.Resources {
			if len(m.resources) >= MaxResources {
				return
			}
			if r.Nested != nil {
				// A module deployment is structure, not a deployed resource;
				// its children are what rules inspect.
				walk(r.Nested, append(append([]bicep.Resource(nil), ancestors...), r))
				continue
			}
			m.resources = append(m.resources, toResource(r, resolve(doc, r.Pointer), mapper.Locate(r, ancestors)))
		}
	}
	walk(t, nil)
	return m, nil
}

func toResource(r bicep.Resource, node map[string]any, bl bicep.Location) sdk.ARMResource {
	res := sdk.ARMResource{
		Type: r.Type, Name: normalizeLiteralRef(r.Name), APIVersion: r.APIVersion,
		Location: sdk.Location{File: bl.File},
	}
	if p := bytes.TrimSpace(r.Properties); len(p) > 0 && string(p) != "null" {
		var props map[string]any
		if json.Unmarshal(p, &props) == nil {
			res.Properties = normalizeValue(props).(map[string]any)
		}
	}
	if tags, ok := node["tags"].(map[string]any); ok {
		res.Tags = tags
	}
	res.Region = str(node["location"])
	if zones, ok := node["zones"].([]any); ok {
		for _, z := range zones {
			if s, ok := z.(string); ok {
				res.Zones = append(res.Zones, s)
			}
		}
	}
	res.Kind = str(node["kind"])
	if sku, ok := node["sku"].(map[string]any); ok {
		res.SKU = sku
		if n := str(sku["name"]); n != "" && !strings.HasPrefix(n, "[") {
			res.SKUName = n
		}
	}
	if id, ok := node["identity"].(map[string]any); ok {
		res.Identity = id
	}
	res.Scope = normalizeLiteralRef(str(node["scope"]))
	return res
}

func normalizeLiteralRef(s string) string {
	if out, ok := normalizeFormatExpr(s); ok {
		return out
	}
	if out, ok := normalizeResourceIDExpr(s); ok {
		return out
	}
	return s
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case string:
		return normalizeLiteralRef(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = normalizeValue(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = normalizeValue(v)
		}
		return out
	default:
		return v
	}
}

func normalizeFormatExpr(s string) (string, bool) {
	body, ok := trimExprCall(s, "format")
	if !ok {
		return "", false
	}
	args, ok := splitLiteralArgs(body)
	if !ok || len(args) < 1 {
		return "", false
	}
	format := args[0]
	for _, ch := range format {
		if !(ch == '/' || (ch >= '0' && ch <= '9') || ch == '{' || ch == '}') {
			return "", false
		}
	}
	out := format
	for i, arg := range args[1:] {
		out = strings.ReplaceAll(out, "{"+strconv.Itoa(i)+"}", arg)
	}
	if strings.Contains(out, "{") || strings.Contains(out, "}") {
		return "", false
	}
	return out, true
}

func normalizeResourceIDExpr(s string) (string, bool) {
	body, ok := trimExprCall(s, "resourceId")
	if !ok {
		return "", false
	}
	args, ok := splitLiteralArgs(body)
	if !ok || len(args) < 2 {
		return "", false
	}
	return strings.Join(args[1:], "/"), true
}

func trimExprCall(s, fn string) (string, bool) {
	if !strings.HasPrefix(s, "["+fn+"(") || !strings.HasSuffix(s, ")]") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(s, "["+fn+"("), ")]"), true
}

func splitLiteralArgs(s string) ([]string, bool) {
	var out []string
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] != '\'' {
			return nil, false
		}
		i++
		start := i
		for i < len(s) && s[i] != '\'' {
			i++
		}
		if i >= len(s) {
			return nil, false
		}
		out = append(out, s[start:i])
		i++
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i < len(s) && s[i] != ',' {
			return nil, false
		}
	}
	return out, len(out) > 0
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// resolve follows an RFC 6901 pointer; it returns nil when the pointer does not
// lead to an object.
func resolve(doc any, ptr string) map[string]any {
	cur := doc
	if ptr != "" {
		for _, seg := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
			seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
			switch c := cur.(type) {
			case map[string]any:
				cur = c[seg]
			case []any:
				i, err := strconv.Atoi(seg)
				if err != nil || i < 0 || i >= len(c) {
					return nil
				}
				cur = c[i]
			default:
				return nil
			}
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

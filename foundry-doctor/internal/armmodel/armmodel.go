// Package armmodel maps a compiled ARM template (as normalised by
// internal/bicep) to the sdk.ARMModel the rules consume.
//
// It reads ARM JSON only; it never parses Bicep source. Values that ARM leaves
// as expressions ("[...]") are passed through unresolved and rules must treat
// them as unknown, never as a pass or a fail.
//
// A Bicep module compiles to a nested template. The model flattens it, and
// while flattening it (1) propagates the ancestor modules' condition and loop
// to every descendant resource, (2) substitutes the calling deployment's
// "parameters" binding into the child's parameters('x') references, and
// (3) qualifies every module-local reference it cannot substitute with the
// deployment path (armexpr.ScopeSep), so equal expression text always means
// the same value.
package armmodel

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
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

// Placement is the template-structure detail of one model resource. The
// Placements slice is parallel to Resources.
type Placement struct {
	// Pointer is the RFC 6901 pointer of the resource in the compiled document.
	Pointer string
	// Module is the deployment path of the module the resource is declared in,
	// "" for the entry template. Two instances of one module have different
	// paths.
	Module string
	// Symbol is the symbolic name qualified by Module ("" when the template has
	// no symbolic names). It is unique across the whole flattened model.
	Symbol string
	// DependsOn are the resource's dependsOn entries with the caller's
	// parameters substituted; symbolic-name entries are qualified like Symbol.
	DependsOn []string
}

// Model implements sdk.ARMModel.
type Model struct {
	resources  []sdk.ARMResource
	placements []Placement
	outputs    []sdk.ARMOutput
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

// Placements returns, for each resource returned by Resources (same order and
// length), where in the nested-template structure it was declared.
func (m *Model) Placements() []Placement {
	if m == nil {
		return nil
	}
	out := make([]Placement, len(m.placements))
	for i, p := range m.placements {
		p.DependsOn = append([]string(nil), p.DependsOn...)
		out[i] = p
	}
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

// scope is the evaluation context of one (nested) template.
type scope struct {
	path string
	// tx rewrites a string of this template into the entry template's terms.
	tx func(string) string
	// never: an ancestor module has a literal false condition.
	never bool
	// maybe/cond: an ancestor module has an unresolvable condition.
	maybe bool
	cond  string
	// looped: an ancestor module is deployed through a copy loop.
	looped bool
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
	usedPaths := map[string]bool{}
	var walk func(t *bicep.Template, sc scope, ancestors []bicep.Resource)
	walk = func(t *bicep.Template, sc scope, ancestors []bicep.Resource) {
		symbolic := map[string]bool{}
		for _, r := range t.Resources {
			if r.SymbolicName != "" {
				symbolic[r.SymbolicName] = true
			}
		}
		for idx, r := range t.Resources {
			if len(m.resources) >= MaxResources {
				return
			}
			if r.Nested != nil {
				// A module deployment is structure, not a deployed resource;
				// its children are what rules inspect.
				walk(r.Nested, childScope(sc, r, idx, usedPaths), append(append([]bicep.Resource(nil), ancestors...), r))
				continue
			}
			res := toResource(r, resolve(doc, r.Pointer), mapper.Locate(r, ancestors), sc)
			m.resources = append(m.resources, res)
			m.placements = append(m.placements, placement(r, sc, symbolic))
		}
	}
	walk(t, scope{tx: func(s string) string { return s }}, nil)
	return m, nil
}

func placement(r bicep.Resource, sc scope, symbolic map[string]bool) Placement {
	p := Placement{Pointer: r.Pointer, Module: sc.path}
	if r.SymbolicName != "" {
		p.Symbol = qualifySymbol(r.SymbolicName, sc.path)
	}
	for _, d := range r.DependsOn {
		if symbolic[d] {
			p.DependsOn = append(p.DependsOn, qualifySymbol(d, sc.path))
			continue
		}
		p.DependsOn = append(p.DependsOn, normalizeLiteralRef(sc.tx(d)))
	}
	return p
}

func qualifySymbol(sym, path string) string {
	if path == "" {
		return sym
	}
	return path + "::" + sym
}

// childScope builds the scope of the template nested in module deployment r,
// which is the idx-th resource of its parent template, declared in sc.
func childScope(sc scope, r bicep.Resource, idx int, used map[string]bool) scope {
	pr := armexpr.PresenceOf(r.Conditional, sc.tx(r.Condition), r.Copy != nil)
	ch := scope{never: sc.never || pr == armexpr.NotDeployed, looped: sc.looped || r.Copy != nil, maybe: sc.maybe, cond: sc.cond}
	if !ch.maybe && r.Conditional && pr == armexpr.MaybeDeployed && r.Copy == nil {
		ch.maybe, ch.cond = true, sc.tx(r.Condition)
	}

	props := moduleProps(r.Properties)
	if props.outer {
		// Legacy outer-scope evaluation: the child's expressions are the
		// caller's, so the caller's scope applies unchanged.
		ch.path, ch.tx = sc.path, sc.tx
		return ch
	}
	ch.path = uniquePath(sc.path, r, idx, used)

	secure := map[string]bool{}
	for _, p := range r.Nested.Parameters {
		if p.Secure {
			secure[strings.ToLower(p.Name)] = true
		}
	}
	bound := map[string]string{}
	for name, v := range props.params {
		if secure[strings.ToLower(name)] {
			continue
		}
		if repl, ok := bindingText(v, sc.tx); ok {
			bound[strings.ToLower(name)] = repl
		}
	}
	path := ch.path
	local := func(s string) string {
		return armexpr.RewriteRefs(s, func(kind, name string) (string, bool) {
			if kind == armexpr.RefParameter {
				if repl, ok := bound[strings.ToLower(name)]; ok {
					return repl, true
				}
			}
			return kind + "(" + armexpr.QuoteLiteral(armexpr.ScopedName(name, path)) + ")", true
		})
	}
	ch.tx = local
	return ch
}

// uniquePath returns the deployment path of module r under parent path.
func uniquePath(parent string, r bicep.Resource, idx int, used map[string]bool) string {
	label := r.SymbolicName
	if label == "" {
		label = "m" + strconv.Itoa(idx) + "-" + sanitize(r.Name)
	}
	p := label
	if parent != "" {
		p = parent + "/" + label
	}
	base := p
	for n := 2; used[p]; n++ {
		p = base + "#" + strconv.Itoa(n)
	}
	used[p] = true
	return p
}

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		}
		if b.Len() >= 24 {
			break
		}
	}
	return b.String()
}

type moduleProperties struct {
	params map[string]any
	outer  bool
}

// moduleProps reads the "parameters" binding and the expression evaluation
// scope of a Microsoft.Resources/deployments resource.
func moduleProps(raw json.RawMessage) moduleProperties {
	var out moduleProperties
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var p struct {
		Parameters map[string]struct {
			Value     any             `json:"value"`
			Reference json.RawMessage `json:"reference"`
		} `json:"parameters"`
		Options struct {
			Scope string `json:"scope"`
		} `json:"expressionEvaluationOptions"`
	}
	if dec.Decode(&p) != nil {
		return out
	}
	out.outer = strings.EqualFold(p.Options.Scope, "outer")
	out.params = map[string]any{}
	for k, v := range p.Parameters {
		if len(v.Reference) > 0 || v.Value == nil {
			continue
		}
		out.params[k] = v.Value
	}
	return out
}

// bindingText renders a deployment parameter value as expression text in the
// entry template's terms. ok is false for values that cannot be inlined.
func bindingText(v any, callerTx func(string) string) (string, bool) {
	switch x := v.(type) {
	case string:
		return armexpr.ExprText(callerTx(x)), true
	case json.Number:
		return x.String(), true
	case bool:
		if x {
			return "true()", true
		}
		return "false()", true
	}
	return "", false
}

func toResource(r bicep.Resource, node map[string]any, bl bicep.Location, sc scope) sdk.ARMResource {
	tx := func(s string) string { return normalizeLiteralRef(sc.tx(s)) }
	res := sdk.ARMResource{
		Type: r.Type, Name: tx(r.Name), APIVersion: r.APIVersion,
		Location: sdk.Location{File: bl.File},
	}
	if p := bytes.TrimSpace(r.Properties); len(p) > 0 && string(p) != "null" {
		var props map[string]any
		if json.Unmarshal(p, &props) == nil {
			res.Properties = normalizeValue(props, tx).(map[string]any)
		}
	}
	if tags, ok := node["tags"].(map[string]any); ok {
		res.Tags = normalizeValue(tags, tx).(map[string]any)
	}
	res.Region = tx(str(node["location"]))
	if zones, ok := node["zones"].([]any); ok {
		for _, z := range zones {
			if s, ok := z.(string); ok {
				res.Zones = append(res.Zones, s)
			}
		}
	}
	res.Kind = str(node["kind"])
	res.Conditional, res.Condition, res.Looped = r.Conditional, sc.tx(r.Condition), r.Copy != nil
	inheritPresence(&res, sc)
	if sku, ok := node["sku"].(map[string]any); ok {
		res.SKU = normalizeValue(sku, tx).(map[string]any)
		if n := str(sku["name"]); n != "" && !strings.HasPrefix(n, "[") {
			res.SKUName = n
		}
	}
	if id, ok := node["identity"].(map[string]any); ok {
		res.Identity = normalizeValue(id, tx).(map[string]any)
	}
	res.Scope = tx(str(node["scope"]))
	return res
}

// inheritPresence folds the ancestor modules' condition and loop into the
// resource: MaybeDeployed if any ancestor is conditional or looped, and a
// literal false condition when an ancestor is never deployed.
func inheritPresence(res *sdk.ARMResource, sc scope) {
	if sc.looped {
		res.Looped = true
	}
	if armexpr.PresenceOf(res.Conditional, res.Condition, false) == armexpr.NotDeployed {
		return
	}
	switch {
	case sc.never:
		res.Conditional, res.Condition = true, "false"
	case sc.maybe && armexpr.PresenceOf(res.Conditional, res.Condition, false) == armexpr.Deployed:
		res.Conditional, res.Condition = true, sc.cond
	}
}

func normalizeLiteralRef(s string) string {
	if out, ok := armexpr.Fold(s); ok {
		return out
	}
	return s
}

func normalizeValue(v any, tx func(string) string) any {
	switch x := v.(type) {
	case string:
		return tx(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = normalizeValue(v, tx)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = normalizeValue(v, tx)
		}
		return out
	default:
		return v
	}
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

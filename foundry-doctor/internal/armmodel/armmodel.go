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
	"sort"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Size bounds. They keep hostile templates from exhausting memory or time:
// a deployment parameter that is substituted into a child template is
// expression text, and a module that passes a parameter into itself many times
// grows the text exponentially with the nesting depth.
const (
	// MaxResources bounds the number of model resources.
	MaxResources = 20000
	// MaxBindingBytes bounds the text a single deployment parameter binding
	// (or resolved module output) may carry into a child template. A larger
	// value is not substituted: the reference keeps its scope-qualified name and
	// compares as unknown.
	MaxBindingBytes = 4 << 10
	// MaxExprBytes bounds the substituted text added to one expression.
	MaxExprBytes = 64 << 10
	// MaxSubstitutedBytes bounds all substituted text in one model, which
	// bounds the substitution work.
	MaxSubstitutedBytes = 8 << 20
	// MaxTextBytes bounds the expression text retained in the model. When it is
	// exceeded the remaining resources are dropped and Truncated reports true.
	MaxTextBytes = 32 << 20
)

// Options describe where the template came from.
type Options struct {
	// Entry is the repo-relative, slash-separated entry Bicep file. It is used
	// for resource locations; no line numbers are fabricated.
	Entry string
	// Modules optionally maps module deployment names to their source files.
	Modules bicep.ModuleFiles
	// ModuleHashes optionally maps the templateHash of a compiled module
	// (metadata._generator.templateHash, see bicep.TemplateHash) to its source
	// file. The compiled ARM carries no source path, but the nested template of
	// a module has the same hash as the module file compiled on its own. It is
	// consulted when Modules has no entry for the deployment name.
	ModuleHashes map[string]string
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
	// ModuleDeps are the deployment paths (Placement.Module values) of module
	// deployments this resource depends on, directly or through an ancestor
	// module's dependsOn. Every resource declared in such a module (or below
	// it) is a dependency.
	ModuleDeps []string
}

// Model implements sdk.ARMModel.
type Model struct {
	resources  []sdk.ARMResource
	placements []Placement
	outputs    []sdk.ARMOutput
	truncated  bool
}

var (
	_ sdk.ARMModel   = (*Model)(nil)
	_ sdk.ARMOutputs = (*Model)(nil)
)

// Truncated reports that a size bound (MaxResources or MaxTextBytes) cut the
// model short; rules must then treat absent resources as unknown.
func (m *Model) Truncated() bool { return m != nil && m.truncated }

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
		p.ModuleDeps = append([]string(nil), p.ModuleDeps...)
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

// budget tracks the work and retained text of one model build.
type budget struct {
	subst, text int
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
	// deps and modDeps are the ancestor modules' dependsOn entries, in the
	// entry template's terms; they apply to every descendant resource.
	deps, modDeps []string
	// file is the source file of the nearest enclosing module, "" if unknown.
	file string
	bud  *budget
}

// modInfo is one module deployment of a template.
type modInfo struct {
	r     bicep.Resource
	props moduleProperties
	path  string
	built bool
	busy  bool
	ch    scope
}

// modIndex finds the module deployments of one template by literal name or
// symbolic name.
type modIndex struct {
	byName, bySym map[string]*modInfo
}

func (x *modIndex) lookup(name string) *modInfo {
	k := strings.ToLower(name)
	if mi := x.byName[k]; mi != nil {
		return mi
	}
	return x.bySym[k]
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
	bud := &budget{}
	var walk func(t *bicep.Template, sc scope, ancestors []bicep.Resource)
	walk = func(t *bicep.Template, sc scope, ancestors []bicep.Resource) {
		symbolic := map[string]bool{}
		mods := &modIndex{byName: map[string]*modInfo{}, bySym: map[string]*modInfo{}}
		order := make([]*modInfo, len(t.Resources))
		for idx, r := range t.Resources {
			if r.SymbolicName != "" {
				symbolic[r.SymbolicName] = true
			}
			if r.Nested == nil {
				continue
			}
			mi := &modInfo{r: r, props: moduleProps(r.Properties)}
			if mi.props.outer {
				mi.path = sc.path
			} else {
				mi.path = uniquePath(sc.path, r, usedPaths)
				if r.SymbolicName != "" {
					mods.bySym[strings.ToLower(r.SymbolicName)] = mi
				}
				if !armexpr.IsExpression(r.Name) && mods.byName[strings.ToLower(r.Name)] == nil {
					mods.byName[strings.ToLower(r.Name)] = mi
				}
			}
			order[idx] = mi
		}
		base := sc.tx
		var child func(mi *modInfo) (scope, bool)
		sc.tx = func(s string) string {
			return armexpr.RewriteDeploymentOutputs(base(s), func(dep, out string) (string, bool) {
				mi := mods.lookup(dep)
				if mi == nil || mi.props.outer || mi.r.Conditional || mi.r.Copy != nil {
					return "", false
				}
				return moduleOutput(mi, out, child, bud)
			})
		}
		// child builds the scope of a module on first use: a module's
		// parameters may read an earlier or later sibling's outputs.
		child = func(mi *modInfo) (scope, bool) {
			if mi.busy {
				return scope{}, false
			}
			if !mi.built {
				mi.busy = true
				mi.ch = childScope(sc, mi.r, mi.path, mi.props, bud)
				mi.busy, mi.built = false, true
			}
			return mi.ch, true
		}
		for idx, r := range t.Resources {
			if len(m.resources) >= MaxResources || bud.text > MaxTextBytes {
				m.truncated = true
				return
			}
			if r.Nested != nil {
				// A module deployment is structure, not a deployed resource;
				// its children are what rules inspect.
				ch, _ := child(order[idx])
				d, md := dependsOf(r, sc, symbolic, mods)
				ch.deps = append(append([]string(nil), sc.deps...), d...)
				ch.modDeps = append(append([]string(nil), sc.modDeps...), md...)
				ch.file = moduleFile(mapper, opt, doc, r)
				walk(r.Nested, ch, append(append([]bicep.Resource(nil), ancestors...), r))
				continue
			}
			loc := mapper.Locate(r, ancestors)
			if sc.file != "" {
				loc.File = sc.file
			}
			res := toResource(r, resolve(doc, r.Pointer), loc, sc)
			bud.text += resourceBytes(res)
			m.resources = append(m.resources, res)
			m.placements = append(m.placements, placement(r, sc, symbolic, mods))
		}
	}
	walk(t, scope{tx: func(s string) string { return s }, bud: bud}, nil)
	return m, nil
}

// resourceBytes approximates the expression text a model resource retains.
func resourceBytes(r sdk.ARMResource) int {
	n := len(r.Name) + len(r.Region) + len(r.Condition) + len(r.Scope)
	var add func(v any)
	add = func(v any) {
		switch x := v.(type) {
		case string:
			n += len(x)
		case map[string]any:
			for k, e := range x {
				n += len(k)
				add(e)
			}
		case []any:
			for _, e := range x {
				add(e)
			}
		}
	}
	add(r.Properties)
	add(r.Tags)
	add(r.SKU)
	add(r.Identity)
	return n
}

// moduleFile returns the source file of module r: from the deployment name
// map, then from the template hash. It returns "" when neither knows.
func moduleFile(mapper bicep.Mapper, opt Options, doc any, r bicep.Resource) string {
	if f, ok := mapper.LookupModule(r.Name); ok {
		return f
	}
	if len(opt.ModuleHashes) > 0 {
		if h := bicep.TemplateHash(resolve(doc, r.Pointer)); h != "" {
			if f, ok := opt.ModuleHashes[h]; ok {
				return f
			}
		}
	}
	return ""
}

// moduleOutput returns the text of output out of module mi in the entry
// template's terms. It resolves only expression outputs of a plain, unlooped,
// unconditional module (the caller checks), and never secure ones.
func moduleOutput(mi *modInfo, out string, child func(*modInfo) (scope, bool), bud *budget) (string, bool) {
	for _, o := range mi.r.Nested.Outputs {
		if !strings.EqualFold(o.Name, out) {
			continue
		}
		if t := strings.ToLower(o.Type); t == "securestring" || t == "secureobject" || !armexpr.IsExpression(o.Value) {
			return "", false
		}
		ch, ok := child(mi)
		if !ok {
			return "", false
		}
		text := armexpr.ExprText(ch.tx(o.Value))
		if len(text) > MaxBindingBytes || bud.subst+len(text) > MaxSubstitutedBytes {
			return "", false
		}
		bud.subst += len(text)
		return text, true
	}
	return "", false
}

// dependsOf splits the dependsOn entries of module r (declared in sc) into
// resource entries and module deployment paths.
func dependsOf(r bicep.Resource, sc scope, symbolic map[string]bool, mods *modIndex) (deps, modDeps []string) {
	for _, d := range r.DependsOn {
		if symbolic[d] {
			if mi := mods.bySym[strings.ToLower(d)]; mi != nil {
				modDeps = append(modDeps, mi.path)
				continue
			}
			deps = append(deps, qualifySymbol(d, sc.path))
			continue
		}
		txt := sc.tx(d)
		if ref := armexpr.ParseRef(txt); !ref.Opaque && strings.EqualFold(ref.Type, armexpr.DeploymentsType) && len(ref.Segments) == 1 && ref.Segments[0].Literal {
			if mi := mods.byName[ref.Segments[0].Text]; mi != nil {
				modDeps = append(modDeps, mi.path)
				continue
			}
		}
		deps = append(deps, normalizeLiteralRef(txt))
	}
	return deps, modDeps
}

func placement(r bicep.Resource, sc scope, symbolic map[string]bool, mods *modIndex) Placement {
	p := Placement{Pointer: r.Pointer, Module: sc.path}
	if r.SymbolicName != "" {
		p.Symbol = qualifySymbol(r.SymbolicName, sc.path)
	}
	deps, modDeps := dependsOf(r, sc, symbolic, mods)
	p.DependsOn = append(deps, sc.deps...)
	p.ModuleDeps = append(modDeps, sc.modDeps...)
	return p
}

func qualifySymbol(sym, path string) string {
	if path == "" {
		return sym
	}
	return path + "::" + sym
}

// childScope builds the scope of the template nested in module deployment r
// (deployment path path), declared in sc.
func childScope(sc scope, r bicep.Resource, path string, props moduleProperties, bud *budget) scope {
	pr := armexpr.PresenceOf(r.Conditional, sc.tx(r.Condition), r.Copy != nil)
	ch := scope{never: sc.never || pr == armexpr.NotDeployed, looped: sc.looped || r.Copy != nil, maybe: sc.maybe, cond: sc.cond, bud: bud}
	if !ch.maybe && r.Conditional && pr == armexpr.MaybeDeployed && r.Copy == nil {
		ch.maybe, ch.cond = true, sc.tx(r.Condition)
	}

	if props.outer {
		// Legacy outer-scope evaluation: the child's expressions are the
		// caller's, so the caller's scope applies unchanged.
		ch.path, ch.tx = sc.path, sc.tx
		return ch
	}
	ch.path = path

	secure := map[string]bool{}
	for _, p := range r.Nested.Parameters {
		if p.Secure {
			secure[strings.ToLower(p.Name)] = true
		}
	}
	bound := map[string]string{}
	names := make([]string, 0, len(props.params))
	for name := range props.params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if secure[strings.ToLower(name)] {
			continue
		}
		if repl, ok := bindingText(props.params[name], sc.tx); ok && len(repl) <= MaxBindingBytes {
			bound[strings.ToLower(name)] = repl
		}
	}
	local := func(s string) string {
		used := 0
		return armexpr.RewriteRefs(s, func(kind, name string) (string, bool) {
			if kind == armexpr.RefParameter {
				if repl, ok := bound[strings.ToLower(name)]; ok &&
					used+len(repl) <= MaxExprBytes && bud.subst+len(repl) <= MaxSubstitutedBytes {
					used += len(repl)
					bud.subst += len(repl)
					return repl, true
				}
			}
			return kind + "(" + armexpr.QuoteLiteral(armexpr.ScopedName(name, path)) + ")", true
		})
	}
	ch.tx = local
	return ch
}

// uniquePath returns the deployment path of module r under parent path. It is
// built from the module's deployment name (its symbolic name when it has one,
// else its name), never from its position, so inserting an unrelated module
// does not change it. Equal labels get a "#n" suffix in template order.
func uniquePath(parent string, r bicep.Resource, used map[string]bool) string {
	label := sanitize(r.SymbolicName)
	if label == "" {
		label = sanitize(r.Name)
	}
	if label == "" {
		label = "module"
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
		if b.Len() >= 48 {
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
	// The scope stays unfolded: resourceId('type', 'name') folded to the bare
	// name would lose the type IDN-004 needs to classify the scope.
	res.Scope = sc.tx(str(node["scope"]))
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

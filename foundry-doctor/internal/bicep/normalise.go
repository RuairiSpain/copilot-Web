package bicep

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Default limits that bound the work done on untrusted template JSON.
const (
	defaultMaxDepth     = 16
	defaultMaxResources = 20000
)

// ErrNotTemplate is returned when the input is not a JSON object.
var ErrNotTemplate = errors.New("bicep: not an ARM template (JSON object expected)")

// Options configures Normalise.
type Options struct {
	// File is the entry Bicep file, relative to the project root. It becomes ARMTemplate.File and
	// Resource.File: the nearest source the compiler lets us attribute a resource to.
	File string
	// ParameterValues are literal parameter values of the root template, normally from
	// ParamsResult.Values. They take precedence over defaults.
	ParameterValues map[string]any
	MaxDepth        int // module nesting limit; zero means 16
	MaxResources    int // zero means 20000
}

// ModuleRef is one step of the module chain from the root template down to a resource.
type ModuleRef struct {
	Label   string // symbolic name when the template has one, else the deployment name
	Pointer string // JSON pointer of the deployments resource
	// Looped and Conditional are the module resource's own flags: everything inside a looped
	// or conditional module is looped or conditional too.
	Looped, Conditional bool
}

// MayNotDeploy reports whether the resource or any enclosing module is conditional.
func (r ResourceInfo) MayNotDeploy() bool {
	if r.Conditional {
		return true
	}
	for _, m := range r.ModuleChain {
		if m.Conditional {
			return true
		}
	}
	return false
}

// MultiInstance reports whether the resource or any enclosing module is looped.
func (r ResourceInfo) MultiInstance() bool {
	if r.Looped {
		return true
	}
	for _, m := range r.ModuleChain {
		if m.Looped {
			return true
		}
	}
	return false
}

// Parameter reference sources.
const (
	SourceSupplied   = "supplied"   // value given by a parameters file or by the calling module
	SourceDefault    = "default"    // the parameter's literal defaultValue
	SourceUnresolved = "unresolved" // not substituted; Reason says why
)

// ParamRef records one place where a resource property is exactly `[parameters('x')]`.
// Properties that mix a parameter into a larger expression are not recorded: they are plain
// ARM expressions and stay Unresolved for Resource.Get.
type ParamRef struct {
	Path      string // dotted path in the resource body, as for Resource.Get
	Parameter string
	Source    string // SourceSupplied, SourceDefault or SourceUnresolved
	Reason    string // for SourceUnresolved: no-default, secure-parameter, default-is-expression, arm-expression, key-vault-reference
}

// ResourceInfo carries what model.Resource cannot: identity inside the document and the flags
// rules need to decide how far to trust a finding.
type ResourceInfo struct {
	Pointer     string      // JSON pointer into the compiled document, for example /resources/acct
	ModuleChain []ModuleRef // empty for resources declared in the entry file
	Parent      string      // symbolic name of the parent for `parent::child` keys (language version 2.0)
	CopyName    string      // `copy.name`; in array form this is the only symbolic name
	Looped      bool        // has a top-level `copy`: the number of instances is not known
	Conditional bool        // has a `condition`: the resource may not deploy
	// ConditionValue is Literal (a bool) only when the condition is a boolean or a parameter
	// reference whose value is known; otherwise Unresolved. Absent when not Conditional.
	ConditionValue model.Value
	Condition      string // the condition expression as written, empty when not Conditional or not a string
	Module         bool   // a Microsoft.Resources/deployments resource; its inline template is expanded
	// Confidence is likely when the resource is declared in the entry file (file known, line not)
	// and uncertain inside a module (the module file cannot be proven).
	Confidence sdk.Confidence
	ParamRefs  []ParamRef
}

// ParameterInfo describes one root template parameter.
type ParameterInfo struct {
	Name   string
	Type   string
	Secure bool
	Value  model.Value // Literal only for a supplied value or a literal default of a non-secure parameter
	Source string      // SourceSupplied, SourceDefault or SourceUnresolved
}

// Normalised is the result of Normalise. Info is parallel to Template.Resources.
type Normalised struct {
	Template   *model.ARMTemplate
	Info       []ResourceInfo
	Parameters []ParameterInfo
	// Existing lists the JSON pointers of `existing` resource references; they are not deployed
	// and are not part of Template.Resources.
	Existing  []string
	Warnings  []string
	Truncated bool
}

// Find returns the index of the resource with the given JSON pointer.
func (n *Normalised) Find(pointer string) (int, bool) {
	if n == nil {
		return 0, false
	}
	for i, in := range n.Info {
		if in.Pointer == pointer {
			return i, true
		}
	}
	return 0, false
}

// Normalise reads compiled ARM JSON in either form (resources as an array, or
// languageVersion 2.0 with resources keyed by symbolic name), expands inline module templates,
// and returns the flattened model. It never evaluates ARM expressions: the only substitution is
// a property that is exactly `[parameters('x')]` where x has a supplied value or a literal
// default. It returns an error only when the input is not a JSON object.
func Normalise(raw []byte, opts Options) (*Normalised, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("normalise ARM: %w", err)
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("normalise ARM: %w", ErrNotTemplate)
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if opts.MaxResources <= 0 {
		opts.MaxResources = defaultMaxResources
	}
	w := &walker{opts: opts, out: &Normalised{Template: &model.ARMTemplate{File: opts.File}}}

	supplied := make(map[string]model.Value, len(opts.ParameterValues))
	for k, v := range opts.ParameterValues {
		supplied[strings.ToLower(k)] = literalOrExpr(v)
	}
	sc := buildScope(root, supplied)
	w.out.Parameters = sc.infos()
	for _, p := range w.out.Parameters {
		w.out.Template.Parameters = append(w.out.Template.Parameters, p.Name)
	}
	w.out.Template.Outputs = outputs(root)
	w.template(root, sc, nil, "", 0)
	return w.out, nil
}

// ---- scope ----

type paramState struct {
	name   string
	typ    string
	secure bool
	val    model.Value
	source string
}

type scope struct{ params map[string]*paramState }

var secureTypes = map[string]bool{"securestring": true, "secureobject": true}

func buildScope(tmpl map[string]any, supplied map[string]model.Value) *scope {
	sc := &scope{params: map[string]*paramState{}}
	params, _ := tmpl["parameters"].(map[string]any)
	for name, v := range params {
		def, _ := v.(map[string]any)
		st := &paramState{name: name}
		st.typ, st.secure = paramType(tmpl, def)
		if sv, ok := supplied[strings.ToLower(name)]; ok {
			st.source = SourceSupplied
			st.val = sv
		} else if dv, ok := def["defaultValue"]; ok {
			st.source = SourceDefault
			st.val = literalOrExpr(dv)
			if st.val.State == model.Unresolved {
				st.val.Reason = "default-is-expression"
			}
		} else {
			st.source = SourceUnresolved
			st.val = model.Value{State: model.Unresolved, Reason: "no-default"}
		}
		if st.secure && st.val.State == model.Literal {
			st.source = SourceUnresolved
			st.val = model.Value{State: model.Unresolved, Reason: "secure-parameter"}
		}
		sc.params[strings.ToLower(name)] = st
	}
	return sc
}

func (s *scope) infos() []ParameterInfo {
	out := make([]ParameterInfo, 0, len(s.params))
	for _, p := range s.params {
		out = append(out, ParameterInfo{Name: p.name, Type: p.typ, Secure: p.secure, Value: p.val, Source: p.source})
	}
	slices.SortFunc(out, func(a, b ParameterInfo) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// paramType returns the declared type (or "$ref:<target>") and whether it is a secure type,
// following up to a few `$ref` hops through `definitions`.
func paramType(tmpl, def map[string]any) (string, bool) {
	cur := def
	for range 5 {
		if t, ok := cur["type"].(string); ok {
			return t, secureTypes[strings.ToLower(t)]
		}
		ref, ok := cur["$ref"].(string)
		if !ok {
			return "", false
		}
		name, found := strings.CutPrefix(ref, "#/definitions/")
		if !found {
			return "$ref:" + ref, false
		}
		defs, _ := tmpl["definitions"].(map[string]any)
		next, ok := defs[name].(map[string]any)
		if !ok {
			return "$ref:" + ref, false
		}
		cur = next
	}
	return "", false
}

// literalOrExpr classifies a JSON value without evaluating anything.
func literalOrExpr(v any) model.Value {
	if s, ok := v.(string); ok && model.IsARMExpression(s) {
		return model.Value{State: model.Unresolved, Reason: "arm-expression"}
	}
	return model.Value{State: model.Literal, Lit: v}
}

var paramRefRe = regexp.MustCompile(`(?i)^\[\s*parameters\(\s*'((?:[^']|'')+)'\s*\)\s*\]$`)

// paramName returns the parameter name when s is exactly `[parameters('name')]`.
func paramName(s string) (string, bool) {
	m := paramRefRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return strings.ReplaceAll(m[1], "''", "'"), true
}

// childScope builds the scope of an inline module template from the deployment's
// `properties.parameters` evaluated in the parent scope.
func childScope(dep map[string]any, child map[string]any, parent *scope) *scope {
	supplied := map[string]model.Value{}
	props, _ := dep["properties"].(map[string]any)
	given, _ := props["parameters"].(map[string]any)
	for name, v := range given {
		pm, _ := v.(map[string]any)
		key := strings.ToLower(name)
		if _, isRef := pm["reference"]; isRef {
			supplied[key] = model.Value{State: model.Unresolved, Reason: "key-vault-reference"}
			continue
		}
		val, has := pm["value"]
		if !has {
			continue
		}
		if s, ok := val.(string); ok {
			if pn, ok := paramName(s); ok {
				if ps, ok := parent.params[strings.ToLower(pn)]; ok {
					supplied[key] = ps.val
				} else {
					supplied[key] = model.Value{State: model.Unresolved, Reason: "arm-expression"}
				}
				continue
			}
		}
		supplied[key] = literalOrExpr(val)
	}
	return buildScope(child, supplied)
}

// ---- walking ----

type walker struct {
	opts Options
	out  *Normalised
}

func (w *walker) warn(format string, args ...any) {
	if len(w.out.Warnings) < 50 {
		w.out.Warnings = append(w.out.Warnings, fmt.Sprintf(format, args...))
	}
}

func (w *walker) template(t map[string]any, sc *scope, chain []ModuleRef, ptr string, depth int) {
	switch rs := t["resources"].(type) {
	case []any:
		for i, item := range rs {
			if m, ok := item.(map[string]any); ok {
				w.resource(m, "", ptr+"/resources/"+strconv.Itoa(i), sc, chain, depth)
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(rs))
		for k := range rs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if m, ok := rs[k].(map[string]any); ok {
				w.resource(m, k, ptr+"/resources/"+escapePointer(k), sc, chain, depth)
			}
		}
	}
}

// skippedBody are top-level keys whose parameter references are not substituted.
var skippedBody = map[string]bool{"condition": true, "copy": true, "dependsOn": true}

func (w *walker) resource(m map[string]any, symbolic, ptr string, sc *scope, chain []ModuleRef, depth int) {
	if ex, _ := m["existing"].(bool); ex {
		w.out.Existing = append(w.out.Existing, ptr)
		return
	}
	if len(w.out.Template.Resources) >= w.opts.MaxResources {
		if !w.out.Truncated {
			w.out.Truncated = true
			w.warn("resource limit of %d reached; the remaining resources were not read", w.opts.MaxResources)
		}
		return
	}
	typ, _ := m["type"].(string)
	name, _ := m["name"].(string)
	apiVersion, _ := m["apiVersion"].(string)
	isModule := strings.EqualFold(typ, "Microsoft.Resources/deployments")

	info := ResourceInfo{Pointer: ptr, ModuleChain: slices.Clone(chain), Module: isModule, Confidence: sdk.ConfidenceLikely}
	if len(chain) > 0 {
		info.Confidence = sdk.ConfidenceUncertain
	}
	if i := strings.LastIndex(symbolic, "::"); i > 0 {
		info.Parent = symbolic[:i]
	}
	if cp, ok := m["copy"].(map[string]any); ok {
		info.Looped = true
		info.CopyName, _ = cp["name"].(string)
	}
	if cond, ok := m["condition"]; ok {
		info.Conditional = true
		info.ConditionValue = w.conditionValue(cond, sc)
		info.Condition, _ = cond.(string)
	}

	body := make(map[string]any, len(m))
	for k, v := range m {
		switch {
		case k == "resources":
			// legacy nested children are expanded below, not kept in the body
		case skippedBody[k]:
			body[k] = cloneJSON(v)
		case isModule && k == "properties":
			body[k] = w.moduleProperties(v, sc, &info)
		default:
			body[k] = w.substitute(v, k, sc, &info)
		}
	}

	slices.SortFunc(info.ParamRefs, func(a, b ParamRef) int { // map iteration order is random
		return strings.Compare(a.Path+"\x00"+a.Parameter, b.Path+"\x00"+b.Parameter)
	})
	var deps []string
	if arr, ok := m["dependsOn"].([]any); ok {
		for _, d := range arr {
			if s, ok := d.(string); ok {
				deps = append(deps, s)
			}
		}
	}
	if bn, ok := body["name"].(string); ok {
		name = bn // a name that is exactly a parameter with a known value is read through
	}
	w.out.Template.Resources = append(w.out.Template.Resources, model.Resource{
		Type: typ, Name: name, APIVersion: apiVersion, Symbolic: symbolic,
		Body: body, DependsOn: deps, File: w.opts.File,
	})
	w.out.Info = append(w.out.Info, info)

	if isModule {
		w.module(m, symbolic, name, ptr, sc, chain, depth, info)
	}
	if kids, ok := m["resources"].([]any); ok { // legacy child nesting; the compiler does not emit it
		for i, item := range kids {
			if km, ok := item.(map[string]any); ok {
				w.resource(km, "", ptr+"/resources/"+strconv.Itoa(i), sc, chain, depth)
			}
		}
	}
}

func (w *walker) module(dep map[string]any, symbolic, name, ptr string, sc *scope, chain []ModuleRef, depth int, self ResourceInfo) {
	props, _ := dep["properties"].(map[string]any)
	tmpl, ok := props["template"].(map[string]any)
	if !ok {
		if _, linked := props["templateLink"]; linked {
			w.warn("module at %s uses templateLink; its content was not analysed", ptr)
		}
		return
	}
	if depth+1 > w.opts.MaxDepth {
		w.warn("module at %s exceeds the nesting limit of %d and was not expanded", ptr, w.opts.MaxDepth)
		return
	}
	label := symbolic
	if label == "" {
		label = name
	}
	if label == "" {
		label = ptr
	}
	if len(label) > 80 {
		label = label[:80]
	}
	inner := childScope(dep, tmpl, sc)
	if opts, _ := props["expressionEvaluationOptions"].(map[string]any); !strings.EqualFold(str(opts["scope"]), "inner") {
		inner = sc // outer (and the ARM default) evaluates the nested template in the caller's scope
	}
	next := append(slices.Clone(chain), ModuleRef{Label: label, Pointer: ptr, Looped: self.Looped, Conditional: self.Conditional})
	w.template(tmpl, inner, next, ptr+"/properties/template", depth+1)
}

// moduleProperties copies a deployments resource's properties without the inline template (the
// template is expanded into separate resources) and with parameter references substituted.
func (w *walker) moduleProperties(v any, sc *scope, info *ResourceInfo) any {
	pm, ok := v.(map[string]any)
	if !ok {
		return cloneJSON(v)
	}
	out := make(map[string]any, len(pm))
	for k, val := range pm {
		if k == "template" {
			continue
		}
		out[k] = w.substitute(val, "properties."+k, sc, info)
	}
	return out
}

func (w *walker) conditionValue(cond any, sc *scope) model.Value {
	switch c := cond.(type) {
	case bool:
		return model.Value{State: model.Literal, Lit: c}
	case string:
		if pn, ok := paramName(c); ok {
			if ps, ok := sc.params[strings.ToLower(pn)]; ok {
				if _, isBool := ps.val.Lit.(bool); isBool && ps.val.State == model.Literal {
					return ps.val
				}
				if ps.val.State == model.Unresolved {
					return ps.val
				}
			}
		}
	}
	return model.Value{State: model.Unresolved, Reason: "arm-expression"}
}

// substitute returns a deep copy of v in which every string that is exactly
// `[parameters('x')]` and whose parameter has a known literal value is replaced by that value.
// Every exact reference is recorded in info.ParamRefs.
func (w *walker) substitute(v any, path string, sc *scope, info *ResourceInfo) any {
	switch x := v.(type) {
	case string:
		pn, ok := paramName(x)
		if !ok {
			return x
		}
		ps, ok := sc.params[strings.ToLower(pn)]
		if !ok {
			info.ParamRefs = append(info.ParamRefs, ParamRef{Path: path, Parameter: pn, Source: SourceUnresolved, Reason: "unknown-parameter"})
			return x
		}
		if ps.val.State == model.Literal {
			info.ParamRefs = append(info.ParamRefs, ParamRef{Path: path, Parameter: ps.name, Source: ps.source})
			return cloneJSON(ps.val.Lit)
		}
		info.ParamRefs = append(info.ParamRefs, ParamRef{Path: path, Parameter: ps.name, Source: SourceUnresolved, Reason: ps.val.Reason})
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = w.substitute(val, path+"."+k, sc, info)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = w.substitute(val, path+"["+strconv.Itoa(i)+"]", sc, info)
		}
		return out
	}
	return v
}

func cloneJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneJSON(val)
		}
		return out
	}
	return v
}

func outputs(root map[string]any) []model.Output {
	om, _ := root["outputs"].(map[string]any)
	out := make([]model.Output, 0, len(om))
	for name, v := range om {
		m, _ := v.(map[string]any)
		t := str(m["type"])
		out = append(out, model.Output{Name: name, Type: t, Secure: secureTypes[strings.ToLower(t)]})
	}
	slices.SortFunc(out, func(a, b model.Output) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

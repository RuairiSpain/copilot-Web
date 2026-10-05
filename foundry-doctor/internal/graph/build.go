package graph

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Node and edge kinds local to this package. The model constants are not extended.
const (
	NodeParameter    = "arm-parameter"
	NodeAzureYAML    = "azure-yaml"    // project-level azure.yaml values
	NodeYAMLResource = "yaml-resource" // a top-level azure.yaml resources: entry
	EdgeReferences   = "references"    // resourceId, reference or parameters in an expression
	EdgeScope        = "scope"         // top-level scope of an ARM resource
	EdgePrivateLink  = "private-link"  // private endpoint to the service it connects to
	EdgeSubnet       = "subnet"        // resource to the subnet it is placed in
)

// scopeKey is the top-level ARM resource key that names the scope of an extension resource. It is not in
// the rule catalogue; it is kept here, in one place, until a catalogue entry verifies it.
const scopeKey = "scope"

// Property paths (arrays written as []) that carry a typed reference.
const (
	pathPrivateLink       = "properties.privateLinkServiceConnections[].properties.privateLinkServiceId"
	pathManualPrivateLink = "properties.manualPrivateLinkServiceConnections[].properties.privateLinkServiceId"
	pathSubnet            = "properties.subnet.id"
	pathScope             = scopeKey
	pathDependsOn         = "dependsOn[]"
)

// roleAssignmentType is the ARM type of a role assignment (rules/catalog).
const roleAssignmentType = "Microsoft.Authorization/roleAssignments"

// ProducesCaveat is attached to produces edges from ARM outputs to env keys. azd is only likely to write
// outputs to the environment by name (FND-CFG-006, confidence likely): the value is absent until provision runs.
const ProducesCaveat = "likely: azd writes deployment outputs to the environment by output name; absent until provision has run (FND-CFG-006)"

// Unresolved is a reference the graph could not resolve statically.
type Unresolved struct {
	Owner  string // node ID that holds the reference
	Kind   string // edge kind, "expression" or "interpolation"
	Name   string // target name or key; never a value
	Reason string
}

// Graph is the model graph plus indices and the facts rules query. Embed it into model.Input.Graph through
// the Graph field.
type Graph struct {
	model.Graph
	envRefs    []EnvRef
	unresolved []Unresolved
	forms      []Form
	envKeys    map[string]struct{}
	hasEnv     bool
	resType    map[string]string
	nodeIdx    map[string]int
	adj        map[adjKey][]string
	radj       map[adjKey][]string
}

type adjKey struct{ id, kind string }

type builder struct {
	nodes    map[string]model.GraphNode
	edges    map[model.GraphEdge]struct{}
	unres    map[Unresolved]struct{}
	refs     []EnvRef
	forms    map[Form]struct{}
	resType  map[string]string
	byKey    map[string]string // lower(key) -> node ID
	bySym    map[string]string
	byName   map[string][]string
	params   map[string]string
	services map[string]string
	yamlRes  map[string]string
}

// Build constructs the graph from the parts of in that exist. Nil parts are skipped.
func Build(in model.Input) *Graph {
	b := &builder{
		nodes: map[string]model.GraphNode{}, edges: map[model.GraphEdge]struct{}{},
		unres: map[Unresolved]struct{}{}, forms: map[Form]struct{}{}, resType: map[string]string{},
		byKey: map[string]string{}, bySym: map[string]string{}, byName: map[string][]string{},
		params: map[string]string{}, services: map[string]string{}, yamlRes: map[string]string{},
	}
	b.addAzureYAML(in.AzureYAML)
	b.addARM(in.ARM)
	g := b.finish()
	if in.Environment != nil {
		g.hasEnv = true
		for _, k := range in.Environment.Keys() {
			g.envKeys[k] = struct{}{}
			b.node(model.NodeEnvKey, k, model.Pos{})
		}
		// env-key nodes may have been added: rebuild the sorted views.
		g = b.finishWith(g)
	}
	return g
}

func (b *builder) node(kind, name string, pos model.Pos) string {
	id := model.NodeID(kind, name)
	if _, ok := b.nodes[id]; !ok {
		b.nodes[id] = model.GraphNode{ID: id, Kind: kind, Name: name, Pos: pos}
	}
	return id
}

func (b *builder) edge(from, to, kind string) {
	b.edges[model.GraphEdge{From: from, To: to, Kind: kind}] = struct{}{}
}

func (b *builder) unresolved(owner, kind, name, reason string) {
	b.unres[Unresolved{Owner: owner, Kind: kind, Name: name, Reason: reason}] = struct{}{}
}

// ---- azure.yaml ----

func (b *builder) addAzureYAML(a *model.AzureYAML) {
	if a == nil {
		return
	}
	for _, s := range a.Services {
		b.services[s.Name] = b.node(model.NodeService, s.Name, s.Pos)
	}
	if a.Root != nil {
		if res := a.Root.Lookup("resources"); res != nil && res.Kind == model.NodeMapping {
			for _, c := range res.Children {
				b.yamlRes[c.Key] = b.node(NodeYAMLResource, c.Key, c.KeyPos)
			}
		}
	}
	for _, s := range a.Services {
		from := b.services[s.Name]
		for _, u := range s.Uses {
			if to, ok := b.services[u.Name]; ok {
				b.edge(from, to, model.EdgeUses)
			} else if to, ok := b.yamlRes[u.Name]; ok {
				b.edge(from, to, model.EdgeUses)
			} else {
				b.unresolved(from, model.EdgeUses, u.Name, ReasonUnknownService)
			}
		}
	}
	switch {
	case a.Root != nil:
		b.scanRoot(a.Root)
	default:
		for _, s := range a.Services {
			if s.Node != nil {
				b.scanNode(s.Node, b.services[s.Name])
			}
		}
	}
}

func (b *builder) scanRoot(root *model.Node) {
	svcs := root.Lookup("services")
	for _, c := range root.Children {
		if c == svcs && svcs != nil && svcs.Kind == model.NodeMapping {
			for _, sc := range svcs.Children {
				owner, ok := b.services[sc.Key]
				if !ok {
					owner = b.projectNode()
				}
				b.scanNode(sc, owner)
			}
			continue
		}
		b.scanNode(c, b.projectNode())
	}
}

func (b *builder) projectNode() string { return b.node(NodeAzureYAML, "azure.yaml", model.Pos{}) }

func (b *builder) scanNode(n *model.Node, owner string) {
	if n == nil {
		return
	}
	if n.Kind == model.NodeScalar {
		for _, it := range ScanInterpolations(n.Value) {
			b.interpolation(owner, it, n.Pos)
		}
		return
	}
	for _, c := range n.Children {
		b.scanNode(c, owner)
	}
}

func (b *builder) interpolation(owner string, it Interpolation, pos model.Pos) {
	if it.Form == FormMalformed {
		b.unresolved(owner, "interpolation", "", ReasonMalformedInterp)
		return
	}
	b.forms[it.Form] = struct{}{}
	b.refs = append(b.refs, EnvRef{Owner: owner, Name: it.Name, Form: it.Form, HasDefault: it.HasDefault, Expression: it.Expression, Pos: pos})
	if it.Expression {
		return
	}
	key := b.node(model.NodeEnvKey, it.Name, model.Pos{})
	b.edge(owner, key, model.EdgeConsumes)
}

// ---- ARM ----

func (b *builder) addARM(t *model.ARMTemplate) {
	if t == nil {
		return
	}
	for _, p := range t.Parameters {
		b.params[p] = b.node(NodeParameter, p, model.Pos{})
	}
	for _, o := range t.Outputs {
		id := b.node(model.NodeOutput, o.Name, model.Pos{})
		b.edge(id, b.node(model.NodeEnvKey, o.Name, model.Pos{}), model.EdgeProduces)
	}
	ids := make([]string, len(t.Resources))
	for i, r := range t.Resources {
		name := resourceName(r.Name, i)
		key := strings.ToLower(r.Type) + "/" + name
		id := b.node(model.NodeResource, key, r.Location)
		ids[i] = id
		b.resType[id] = r.Type
		b.byKey[strings.ToLower(key)] = id
		if r.Symbolic != "" {
			b.bySym[r.Symbolic] = id
		}
		b.byName[r.Name] = appendUnique(b.byName[r.Name], id)
	}
	for i, r := range t.Resources {
		b.resourceEdges(ids[i], r)
	}
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// resourceName returns the literal name, or the canonical text of a name expression.
func resourceName(name string, idx int) string {
	if !model.IsARMExpression(name) {
		return name
	}
	e, err := parseExpression(name)
	if err != nil {
		return "unparsed-" + strconv.Itoa(idx)
	}
	return text(e)
}

func (b *builder) resourceEdges(id string, r model.Resource) {
	if model.IsARMExpression(r.Name) {
		b.apply(id, EdgeReferences, analyzeExpr(r.Name))
	}
	for _, d := range r.DependsOn {
		b.stringRef(id, model.EdgeDependsOn, d)
	}
	b.walkValue(id, "", r.Body)
}

func (b *builder) walkValue(id, path string, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			b.walkValue(id, p, x[k])
		}
	case []any:
		for _, e := range x {
			b.walkValue(id, path+"[]", e)
		}
	case string:
		b.stringRef(id, kindForPath(path), x)
	}
}

func kindForPath(path string) string {
	switch path {
	case pathScope:
		return EdgeScope
	case pathPrivateLink, pathManualPrivateLink:
		return EdgePrivateLink
	case pathSubnet:
		return EdgeSubnet
	case pathDependsOn:
		return model.EdgeDependsOn
	}
	return EdgeReferences
}

// stringRef handles one string that may be an expression, and for typed kinds a literal resource ID.
func (b *builder) stringRef(id, kind, s string) {
	if model.IsARMExpression(s) {
		b.apply(id, kind, analyzeExpr(s))
		return
	}
	if kind == EdgeReferences {
		return
	}
	if to, ok := b.lookupName(s); ok {
		b.edge(id, to, kind)
		return
	}
	b.unresolved(id, kind, s, ReasonTargetNotInTemplate)
}

func (b *builder) apply(owner, kind string, fs []finding) {
	for _, f := range fs {
		switch f.kind {
		case findResource:
			if to, ok := b.byKey[strings.ToLower(f.key)]; ok {
				b.edge(owner, to, kind)
			} else {
				b.unresolved(owner, kind, f.key, ReasonTargetNotInTemplate)
			}
		case findNamed:
			if to, ok := b.lookupName(f.key); ok {
				b.edge(owner, to, kind)
			} else {
				b.unresolved(owner, kind, f.key, ReasonTargetNotInTemplate)
			}
		case findParameter:
			if to, ok := b.params[f.key]; ok {
				b.edge(owner, to, EdgeReferences)
			} else {
				b.unresolved(owner, EdgeReferences, f.key, ReasonUnknownParameter)
			}
		default:
			b.unresolved(owner, "expression", "", f.reason)
		}
	}
}

// lookupName resolves a symbolic name, a unique plain name, or a literal resource ID.
func (b *builder) lookupName(s string) (string, bool) {
	if id, ok := b.bySym[s]; ok {
		return id, true
	}
	if ids := b.byName[s]; len(ids) == 1 {
		return ids[0], true
	}
	if key, ok := literalIDKey(s); ok {
		id, ok := b.byKey[strings.ToLower(key)]
		return id, ok
	}
	return "", false
}

// literalIDKey converts ".../providers/<ns>/<type>/<name>[/<type>/<name>]" into lower(type)/name.
func literalIDKey(s string) (string, bool) {
	const marker = "/providers/"
	i := strings.LastIndex(strings.ToLower(s), marker)
	if i < 0 {
		return "", false
	}
	parts := strings.Split(s[i+len(marker):], "/")
	if len(parts) < 3 || len(parts)%2 == 0 {
		return "", false
	}
	types := []string{parts[0]}
	var names []string
	for j := 1; j+1 < len(parts); j += 2 {
		types = append(types, parts[j])
		names = append(names, parts[j+1])
	}
	return strings.ToLower(strings.Join(types, "/")) + "/" + strings.Join(names, "/"), true
}

// ---- finish ----

func (b *builder) finish() *Graph {
	g := &Graph{envKeys: map[string]struct{}{}}
	return b.finishWith(g)
}

func (b *builder) finishWith(g *Graph) *Graph {
	g.Graph = model.Graph{}
	for _, n := range b.nodes {
		g.Nodes = append(g.Nodes, n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	for e := range b.edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		x, y := g.Edges[i], g.Edges[j]
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		if x.From != y.From {
			return x.From < y.From
		}
		return x.To < y.To
	})
	g.unresolved = g.unresolved[:0]
	for u := range b.unres {
		g.unresolved = append(g.unresolved, u)
	}
	sort.Slice(g.unresolved, func(i, j int) bool {
		x, y := g.unresolved[i], g.unresolved[j]
		if x.Owner != y.Owner {
			return x.Owner < y.Owner
		}
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Reason < y.Reason
	})
	g.envRefs = append([]EnvRef(nil), b.refs...)
	sortEnvRefs(g.envRefs)
	g.forms = g.forms[:0]
	for f := range b.forms {
		g.forms = append(g.forms, f)
	}
	sort.Slice(g.forms, func(i, j int) bool { return g.forms[i] < g.forms[j] })
	g.resType = b.resType
	g.nodeIdx = make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		g.nodeIdx[n.ID] = i
	}
	g.adj = make(map[adjKey][]string)
	g.radj = make(map[adjKey][]string)
	for _, e := range g.Edges {
		g.adj[adjKey{e.From, e.Kind}] = append(g.adj[adjKey{e.From, e.Kind}], e.To)
		g.radj[adjKey{e.To, e.Kind}] = append(g.radj[adjKey{e.To, e.Kind}], e.From)
	}
	for _, s := range g.radj {
		sort.Strings(s)
	}
	return g
}

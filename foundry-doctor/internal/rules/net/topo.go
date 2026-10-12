package net

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Resource types read by the rules in this package.
const (
	typeAccount    = "Microsoft.CognitiveServices/accounts"
	typeProject    = "Microsoft.CognitiveServices/accounts/projects"
	typeCapHost    = "Microsoft.CognitiveServices/accounts/capabilityHosts"
	typeProjCapH   = "Microsoft.CognitiveServices/accounts/projects/capabilityHosts"
	typeConn       = "Microsoft.CognitiveServices/accounts/connections"
	typeProjConn   = "Microsoft.CognitiveServices/accounts/projects/connections"
	typePE         = "Microsoft.Network/privateEndpoints"
	typeZoneGroup  = "Microsoft.Network/privateEndpoints/privateDnsZoneGroups"
	typeZone       = "Microsoft.Network/privateDnsZones"
	typeZoneLink   = "Microsoft.Network/privateDnsZones/virtualNetworkLinks"
	typeVNet       = "Microsoft.Network/virtualNetworks"
	typeSubnet     = "Microsoft.Network/virtualNetworks/subnets"
	typeSearch     = "Microsoft.Search/searchServices"
	typeStorage    = "Microsoft.Storage/storageAccounts"
	typeCosmos     = "Microsoft.DocumentDB/databaseAccounts"
	kindAIServices = "AIServices"
)

// topo indexes the compiled template and the dependency graph for the linkage queries the rules share.
// All lookups are by resource index into ARM.Resources. A resource whose graph node cannot be told apart
// from another resource (same type and canonical name) has no node, so every query on it is "unknown".
type topo struct {
	res    []model.Resource
	g      *graph.Graph
	nodes  []string       // node ID per resource, "" when unknown or ambiguous
	byNode map[string]int // node ID to resource index
	pes    map[int]peInfo
}

func newTopo(in *model.Input) *topo {
	t := &topo{g: graph.Build(*in), byNode: map[string]int{}, pes: map[int]peInfo{}}
	if in.ARM == nil {
		return t
	}
	t.res = in.ARM.Resources
	t.nodes = make([]string, len(t.res))
	seen := map[string]int{}
	for i, r := range t.res {
		id := model.NodeID(model.NodeResource, strings.ToLower(r.Type)+"/"+nameText(r.Name))
		seen[id]++
		if _, ok := t.g.Node(id); ok {
			t.nodes[i] = id
		}
	}
	for i, id := range t.nodes {
		if id == "" {
			continue
		}
		if seen[id] > 1 {
			t.nodes[i] = ""
			continue
		}
		t.byNode[id] = i
	}
	for i, r := range t.res {
		if strings.EqualFold(r.Type, typePE) {
			t.pes[i] = t.buildPE(i)
		}
	}
	return t
}

// ofType returns the indexes of resources of the type, in template order.
func (t *topo) ofType(typ string) []int {
	var out []int
	for i, r := range t.res {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, i)
		}
	}
	return out
}

// outRefs returns the resources of wantType that resource i references through a property expression.
func (t *topo) outRefs(i int, wantType string) []int {
	if t.nodes[i] == "" {
		return nil
	}
	var out []int
	for _, to := range t.g.Out(t.nodes[i], graph.EdgeReferences) {
		if j, ok := t.byNode[to]; ok && strings.EqualFold(t.res[j].Type, wantType) {
			out = append(out, j)
		}
	}
	return out
}

type resolution int

const (
	resolved   resolution = iota // the target is a resource of the template
	external                     // a literal ID of a resource that is not in the template
	unresolved                   // a parameter, an expression or an ID that cannot be read
)

// resolveID resolves an ID-valued property of resource owner. raw is the property as written in the
// template (see rawAt). wantType narrows expression references to one resource type.
func (t *topo) resolveID(owner int, raw any, wantType string) (int, resolution) {
	s, ok := raw.(string)
	if !ok {
		return -1, unresolved
	}
	if model.IsARMExpression(s) {
		refs := t.outRefs(owner, wantType)
		if len(refs) == 1 {
			return refs[0], resolved
		}
		return -1, unresolved
	}
	key, ok := literalKey(s)
	if !ok {
		return -1, unresolved
	}
	for i, r := range t.res {
		if strings.EqualFold(r.Type, wantType) && strings.EqualFold(strings.ToLower(r.Type)+"/"+nameText(r.Name), key) {
			return i, resolved
		}
	}
	return -1, external
}

// literalKey converts ".../providers/<ns>/<type>/<name>[/<type>/<name>]" into lower(type)/names.
func literalKey(s string) (string, bool) {
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

// parentOf returns the index of the parent of child (a resource of a child type), or -1 when unknown.
// It reads the symbolic "parent::child" form, the leading name segment, then the dependsOn list.
func (t *topo) parentOf(child int, parentType string) int {
	r := t.res[child]
	if i := strings.LastIndex(r.Symbolic, "::"); i > 0 {
		for j, p := range t.res {
			if p.Symbolic == r.Symbolic[:i] && strings.EqualFold(p.Type, parentType) {
				return j
			}
		}
	}
	if first, ok := firstNameSegment(r.Name); ok {
		for j, p := range t.res {
			if strings.EqualFold(p.Type, parentType) && p.Name == first {
				return j
			}
		}
	}
	found := -1
	for _, d := range r.DependsOn {
		for j, p := range t.res {
			if strings.EqualFold(p.Type, parentType) && (p.Symbolic == d || (p.Name == d && !model.IsARMExpression(d))) {
				if found >= 0 && found != j {
					return -1
				}
				found = j
			}
		}
	}
	return found
}

var quoted = regexp.MustCompile(`'((?:[^']|'')*)'`)

// firstNameSegment returns the first segment of a child resource name: "vnet" for "vnet/subnet" and
// for "[format('{0}/{1}', 'vnet', 'subnet')]". It is false when the segment is not a literal.
func firstNameSegment(name string) (string, bool) {
	if !model.IsARMExpression(name) {
		if i := strings.Index(name, "/"); i > 0 {
			return name[:i], true
		}
		return "", false
	}
	if m := formatArgs(name); len(m) >= 2 {
		return m[0], true
	}
	return "", false
}

// lastNameSegment returns the last segment of a resource name when it is literal.
func lastNameSegment(name string) (string, bool) {
	if !model.IsARMExpression(name) {
		return name[strings.LastIndex(name, "/")+1:], true
	}
	if m := formatArgs(name); len(m) >= 2 {
		return m[len(m)-1], true
	}
	return "", false
}

// formatArgs returns the literal string arguments of "[format('{0}/{1}', 'a', 'b')]" after the template
// string, or nil when the expression is not a format call whose arguments are all string literals.
func formatArgs(expr string) []string {
	body := strings.TrimSpace(expr[1 : len(expr)-1])
	if !strings.HasPrefix(strings.ToLower(body), "format(") || !strings.HasSuffix(body, ")") {
		return nil
	}
	inner := body[len("format(") : len(body)-1]
	rest := quoted.ReplaceAllString(inner, "")
	if strings.Trim(rest, ", \t") != "" {
		return nil
	}
	ms := quoted.FindAllStringSubmatch(inner, -1)
	if len(ms) < 2 {
		return nil
	}
	out := make([]string, 0, len(ms)-1)
	for _, m := range ms[1:] {
		out = append(out, strings.ReplaceAll(m[1], "''", "'"))
	}
	return out
}

// nameText mirrors the graph's resource key for a name: the literal text, or the canonical text of
// an expression (no whitespace, lower-case function names, string literals kept).
func nameText(name string) string {
	if !model.IsARMExpression(name) {
		return name
	}
	body := name[1 : len(name)-1]
	if s, ok := foldConcat(body); ok {
		return s
	}
	var b strings.Builder
	inStr := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case inStr:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(body) && body[i+1] == '\'' {
					b.WriteByte('\'')
					i++
				} else {
					inStr = false
				}
			}
		case c == '\'':
			inStr = true
			b.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		case isIdentByte(c):
			j := i
			for j < len(body) && isIdentByte(body[j]) {
				j++
			}
			word := body[i:j]
			if i > 0 && body[i-1] == '.' {
				b.WriteString(word)
			} else {
				b.WriteString(strings.ToLower(word))
			}
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// foldConcat folds concat('a', 'b') of string literals.
func foldConcat(body string) (string, bool) {
	b := strings.TrimSpace(body)
	if !strings.HasPrefix(strings.ToLower(b), "concat(") || !strings.HasSuffix(b, ")") {
		return "", false
	}
	inner := b[len("concat(") : len(b)-1]
	if strings.Trim(quoted.ReplaceAllString(inner, ""), ", \t") != "" {
		return "", false
	}
	ms := quoted.FindAllStringSubmatch(inner, -1)
	if len(ms) == 0 {
		return "", false
	}
	var out strings.Builder
	for _, m := range ms {
		out.WriteString(strings.ReplaceAll(m[1], "''", "'"))
	}
	return out.String(), true
}

// rawAt reads a value from a decoded JSON body by object keys (string) and array indexes (int).
// It returns nil when a step is missing. Unlike Resource.Get it returns expressions as written.
func rawAt(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			if v, ok = m[k]; !ok {
				return nil
			}
		case int:
			a, ok := v.([]any)
			if !ok || k < 0 || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// ---- private endpoints ----

// conn is one private link service connection of a private endpoint.
type conn struct {
	manual      bool
	raw         any
	groups      []string
	groupsKnown bool
}

// peInfo is what the rules need to know about a private endpoint.
type peInfo struct {
	conns      []conn
	targets    []int // resources the endpoint connects to, from the graph
	subnets    []int
	unresolved bool // some connection target is not a resource of the template
}

func (t *topo) buildPE(i int) peInfo {
	r := t.res[i]
	var info peInfo
	for _, kind := range []string{"privateLinkServiceConnections", "manualPrivateLinkServiceConnections"} {
		arr := rawAt(r.Body, "properties", kind)
		if arr == nil {
			continue
		}
		list, ok := arr.([]any)
		if !ok { // the whole array is an expression
			info.conns = append(info.conns, conn{manual: kind[0] == 'm', raw: arr})
			info.unresolved = true
			continue
		}
		for j := range list {
			c := conn{manual: kind[0] == 'm', raw: rawAt(list[j], "properties", "privateLinkServiceId")}
			gv := r.Get(fmt.Sprintf("properties.%s[%d].properties.groupIds", kind, j))
			if gl, ok := gv.Lit.([]any); ok && gv.IsLiteral() {
				c.groupsKnown = true
				for k := range gl {
					s, isStr := gl[k].(string)
					if !isStr || model.IsARMExpression(s) {
						c.groupsKnown = false
						continue
					}
					c.groups = append(c.groups, s)
				}
			}
			info.conns = append(info.conns, c)
		}
	}
	if t.nodes[i] != "" {
		for _, to := range t.g.Out(t.nodes[i], graph.EdgePrivateLink) {
			if j, ok := t.byNode[to]; ok {
				info.targets = append(info.targets, j)
			}
		}
		for _, to := range t.g.SubnetsOf(t.nodes[i]) {
			if j, ok := t.byNode[to]; ok {
				info.subnets = append(info.subnets, j)
			}
		}
	}
	if len(info.targets) < len(info.conns) {
		info.unresolved = true
	}
	return info
}

// endpointsFor returns the private endpoints whose connection targets resource i.
func (t *topo) endpointsFor(i int) []int {
	if t.nodes[i] == "" {
		return nil
	}
	var out []int
	for _, id := range t.g.PrivateEndpointsFor(t.nodes[i]) {
		if j, ok := t.byNode[id]; ok {
			out = append(out, j)
		}
	}
	return out
}

// anyUnresolvedEndpoint reports whether some private endpoint of the template has a connection target
// that cannot be matched to a resource: it might target any resource.
func (t *topo) anyUnresolvedEndpoint() bool {
	for _, p := range t.pes {
		if p.unresolved {
			return true
		}
	}
	return false
}

// hasGroup reports whether the endpoint lists the group ID. known is false when a group list was not literal.
func (p peInfo) hasGroup(group string) (has, known bool) {
	known = true
	for _, c := range p.conns {
		if !c.groupsKnown {
			known = false
		}
		for _, g := range c.groups {
			if strings.EqualFold(g, group) {
				has = true
			}
		}
	}
	return has, known
}

// manualOnly reports whether every connection of the endpoint needs manual approval.
func (p peInfo) manualOnly() bool {
	if len(p.conns) == 0 {
		return false
	}
	for _, c := range p.conns {
		if !c.manual {
			return false
		}
	}
	return true
}

// normaliseRegion lower-cases a region and removes spaces, so "West Europe" equals "westeurope".
func normaliseRegion(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
}

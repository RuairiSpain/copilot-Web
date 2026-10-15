package view

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type Mode string

const (
	ModeSource   Mode = "source"
	ModeDeployed Mode = "deployed"
	ModeCombined Mode = "combined"
)

type Origin string

const (
	OriginLocal     Origin = "local"
	OriginAzure     Origin = "azure"
	OriginBoth      Origin = "both"
	OriginExternal  Origin = "external"
	OriginMissing   Origin = "missing"
	OriginCollapsed Origin = "collapsed"
)

type Node struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Kind      string       `json:"kind"`
	Origin    Origin       `json:"origin"`
	Type      string       `json:"type,omitempty"`
	Name      string       `json:"name,omitempty"`
	Resource  string       `json:"resource,omitempty"`
	Source    sdk.Location `json:"source,omitempty"`
	External  bool         `json:"external,omitempty"`
	Missing   bool         `json:"missing,omitempty"`
	Collapsed bool         `json:"collapsed,omitempty"`
	Findings  int          `json:"findings,omitempty"`
	Baselined int          `json:"baselined,omitempty"`
	Severity  sdk.Severity `json:"severity,omitempty"`
	Unhealthy bool         `json:"unhealthy,omitempty"`
	Health    string       `json:"health,omitempty"`
}

type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`
	External  bool   `json:"external,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
}

type Summary struct {
	Mode              Mode `json:"mode"`
	NodeCount         int  `json:"nodeCount"`
	EdgeCount         int  `json:"edgeCount"`
	CollapsedNodes    int  `json:"collapsedNodes,omitempty"`
	CollapsedEdges    int  `json:"collapsedEdges,omitempty"`
	UnmatchedFindings int  `json:"unmatchedFindings,omitempty"`
}

type Graph struct {
	SchemaVersion string  `json:"schemaVersion"`
	Mode          Mode    `json:"mode"`
	Nodes         []Node  `json:"nodes"`
	Edges         []Edge  `json:"edges"`
	Summary       Summary `json:"summary"`
}

type Options struct {
	Mode          Mode
	RedactIDs     bool
	MaxNodes      int
	MaxEdges      int
	CollapseAfter int
}

type SourceInput struct {
	Document  *azureyaml.Document
	Services  []coregraph.Service
	Resources []coregraph.Resource
}

type RuntimeInput struct {
	Account     *azure.Resource
	Project     *azure.FoundryProject
	Connections []azure.FoundryConnection
	Hosts       []azure.CapabilityHost
	Deployments []azure.AccountDeployment
}

type Input struct {
	Source    SourceInput
	Inventory []azure.Resource
	Runtime   RuntimeInput
	Findings  []sdk.Finding
}

type rawEdge struct {
	from, to, kind string
	external       bool
	missing        bool
	collapsed      bool
}

type nodeRec struct {
	key        string
	label      string
	kind       string
	origin     Origin
	typ        string
	name       string
	resourceID string
	source     sdk.Location
	external   bool
	missing    bool
	collapsed  bool
	findings   int
	baselined  int
	severity   sdk.Severity
	unhealthy  bool
	health     string
}

func Build(in Input, opt Options) (Graph, error) {
	if opt.Mode == "" {
		opt.Mode = ModeSource
	}
	nodes := map[string]*nodeRec{}
	edges := map[string]rawEdge{}
	addNode := func(n nodeRec) *nodeRec {
		if cur, ok := nodes[n.key]; ok {
			cur.origin = mergedOrigin(cur.origin, n.origin)
			if cur.label == "" {
				cur.label = n.label
			}
			if cur.kind == "" {
				cur.kind = n.kind
			}
			if cur.typ == "" {
				cur.typ = n.typ
			}
			if cur.name == "" {
				cur.name = n.name
			}
			if cur.resourceID == "" {
				cur.resourceID = n.resourceID
			}
			if cur.source.File == "" {
				cur.source = n.source
			}
			cur.external = cur.external || n.external
			cur.missing = cur.missing || n.missing
			cur.collapsed = cur.collapsed || n.collapsed
			cur.unhealthy = cur.unhealthy || n.unhealthy
			if cur.health == "" {
				cur.health = n.health
			}
			return cur
		}
		cp := n
		nodes[n.key] = &cp
		return &cp
	}
	addEdge := func(from, to, kind string, missing, external, collapsed bool) {
		if from == "" || to == "" || from == to {
			return
		}
		k := from + "\x00" + to + "\x00" + kind
		edges[k] = rawEdge{from: from, to: to, kind: kind, external: external, missing: missing, collapsed: collapsed}
	}

	sourceNodes := map[string]string{}
	sourceInput := coregraph.Input{Services: in.Source.Services, Resources: in.Source.Resources}
	if opt.Mode != ModeDeployed && (len(sourceInput.Services) > 0 || len(sourceInput.Resources) > 0) {
		g, err := coregraph.Build(sourceInput)
		if err != nil {
			return Graph{}, err
		}
		for _, n := range g.Nodes {
			r := nodeRec{origin: OriginLocal}
			switch n.Kind {
			case coregraph.KindService:
				r.key = "svc:" + n.Name
				r.kind = classifyService(in.Source.Document, n.Name)
				r.label = n.Name
				r.typ = n.Type
				r.name = n.Name
				if in.Source.Document != nil {
					if _, loc, ok := in.Source.Document.Lookup("services", n.Name, "host"); ok {
						r.source = loc
					}
				}
			default:
				r.key = "arm:" + n.Type + ":" + n.Name
				r.kind = classifyResource(n.Type)
				r.label = firstNonEmpty(n.Name, n.Type)
				r.typ = n.Type
				r.name = n.Name
				r.resourceID = n.ID
			}
			sourceNodes[n.ID] = r.key
			addNode(r)
		}
		for _, e := range g.Edges {
			addEdge(sourceNodes[e.From], sourceNodes[e.To], string(e.Kind), false, false, false)
		}
		for _, u := range g.Unresolved {
			from := sourceNodes[u.From]
			key := unresolvedKey(u.Ref)
			external := looksExternal(u.Ref)
			if external {
				addNode(nodeRec{key: key, label: externalLabel(u.Ref), kind: "external", origin: OriginExternal, name: u.Ref, external: true})
			} else {
				addNode(nodeRec{key: key, label: findings.Redact(u.Ref), kind: "missing", origin: OriginMissing, name: u.Ref, missing: true})
			}
			addEdge(from, key, string(u.Kind), !external, external, false)
		}
		addDeploymentNodes(in.Source.Document, addNode, addEdge)
	}

	inventoryCounts := map[string]int{}
	for _, r := range in.Inventory {
		inventoryCounts[strings.ToLower(r.Type)+"\x00"+strings.ToLower(r.Name)]++
	}
	for _, r := range in.Inventory {
		key := azureResourceKey(r)
		state := strings.ToLower(strings.TrimSpace(r.Properties["properties.provisioningState"]))
		unhealthy := state != "" && state != "succeeded" && state != "successful" && state != "ready"
		n := addNode(nodeRec{
			key:        key,
			label:      r.Name,
			kind:       classifyResource(r.Type),
			origin:     OriginAzure,
			typ:        r.Type,
			name:       r.Name,
			resourceID: r.ID,
			unhealthy:  unhealthy,
			health:     state,
		})
		if inventoryCounts[strings.ToLower(r.Type)+"\x00"+strings.ToLower(r.Name)] == 1 {
			if matched := matchSource(nodes, r.Type, r.Name); matched != nil {
				n.origin = mergedOrigin(n.origin, matched.origin)
				matched.origin = mergedOrigin(matched.origin, OriginAzure)
				if matched.resourceID == "" {
					matched.resourceID = r.ID
				}
				if unhealthy {
					matched.unhealthy = true
					matched.health = state
				}
				delete(nodes, key)
			}
		}
	}
	addConnectionTargets(in.Source.Document, addNode, addEdge, nodes)

	addRuntimeNodes(in.Runtime, addNode, addEdge, nodes)
	unmatched := applyFindings(nodes, in.Findings)
	filterRaw(nodes, edges, opt.Mode)
	graph := materialise(nodes, edges, opt)
	graph.Summary.UnmatchedFindings = unmatched
	if opt.RedactIDs {
		redactGraph(&graph)
	}
	return graph, nil
}

func addDeploymentNodes(doc *azureyaml.Document, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool)) {
	if doc == nil {
		return
	}
	for _, svc := range doc.ServiceNames() {
		host, _, ok := doc.Lookup("services", svc, "host")
		if !ok || (!strings.EqualFold(host, "azure.ai.project") && !strings.EqualFold(host, "microsoft.foundry") && !strings.EqualFold(host, "azure.ai.agent")) {
			continue
		}
		raw, _, ok := doc.LookupAny("services", svc, "deployments")
		if !ok {
			continue
		}
		items, _ := raw.([]any)
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			label := name
			if model, ok := m["model"].(map[string]any); ok {
				var bits []string
				if v, _ := model["format"].(string); v != "" {
					bits = append(bits, v)
				}
				if v, _ := model["name"].(string); v != "" {
					bits = append(bits, v)
				}
				if v, _ := model["version"].(string); v != "" {
					bits = append(bits, v)
				}
				if len(bits) > 0 {
					label = name + " (" + strings.Join(bits, " / ") + ")"
				}
			}

			key := "model:" + svc + ":" + name
			addNode(nodeRec{key: key, label: label, kind: "model", origin: OriginLocal, name: name})
			addEdge("svc:"+svc, key, "deploys", false, false, false)
		}
	}
}

func addConnectionTargets(doc *azureyaml.Document, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool), nodes map[string]*nodeRec) {
	if doc == nil {
		return
	}
	serviceNames := map[string]bool{}
	for _, name := range doc.ServiceNames() {
		serviceNames[name] = true
	}
	for _, svc := range doc.ServiceNames() {
		host, _, ok := doc.Lookup("services", svc, "host")
		if !ok || !strings.EqualFold(host, "azure.ai.connection") {
			continue
		}
		target, _, ok := doc.Lookup("services", svc, "target")
		if !ok || strings.TrimSpace(target) == "" {
			continue
		}
		if serviceNames[target] {
			addEdge("svc:"+svc, "svc:"+target, "targets", false, false, false)
			continue
		}
		if existing := matchTargetNode(nodes, target); existing != "" {
			addEdge("svc:"+svc, existing, "targets", false, false, false)
			continue
		}
		if typ, name, ok := armResourceTypeName(target); ok {
			if existing := matchNodeByTypeName(nodes, typ, name); existing != "" {
				addEdge("svc:"+svc, existing, "targets", false, false, false)
				continue
			}
		}
		key := unresolvedKey(target)
		if looksExternal(target) {
			addNode(nodeRec{key: key, label: externalLabel(target), kind: "external", origin: OriginExternal, name: target, external: true})
			addEdge("svc:"+svc, key, "targets", false, true, false)
			continue
		}
		addNode(nodeRec{key: key, label: findings.Redact(target), kind: "missing", origin: OriginMissing, name: target, missing: true})
		addEdge("svc:"+svc, key, "targets", true, false, false)
	}
}

func addRuntimeNodes(rt RuntimeInput, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool), nodes map[string]*nodeRec) {
	accountNodeKey := ""
	projectKey := ""
	if rt.Account != nil {
		accountNodeKey = azureResourceKey(*rt.Account)
		if _, ok := nodes[accountNodeKey]; !ok {
			addNode(nodeRec{
				key:        accountNodeKey,
				label:      rt.Account.Name,
				kind:       "foundry-account",
				origin:     OriginAzure,
				typ:        rt.Account.Type,
				name:       rt.Account.Name,
				resourceID: rt.Account.ID,
			})
		}
	}
	if rt.Project != nil {
		key := mergeExistingNode(nodes, "project", rt.Project.ID, "", rt.Project.Name, "project:"+strings.ToLower(rt.Project.Name))
		state := strings.ToLower(rt.Project.ProvisioningState)
		n := addNode(nodeRec{
			key:        key,
			label:      rt.Project.Name,
			kind:       "project",
			origin:     OriginAzure,
			name:       rt.Project.Name,
			resourceID: rt.Project.ID,
			unhealthy:  state != "" && state != "succeeded" && state != "successful" && state != "ready",
			health:     state,
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
		projectKey = key
		if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "contains", false, false, false)
		}
	}
	for _, d := range rt.Deployments {
		key := mergeExistingNode(nodes, "model", "", "Microsoft.CognitiveServices/accounts/deployments", d.Name, "deployment:"+strings.ToLower(d.Name))
		state := strings.ToLower(strings.TrimSpace(d.ProvisioningState))
		deploymentState := strings.ToLower(strings.TrimSpace(d.DeploymentState))
		health := firstNonEmpty(deploymentState, state)
		unhealthy := (state != "" && state != "succeeded" && state != "successful" && state != "ready") ||
			(deploymentState != "" && deploymentState != "running" && deploymentState != "ready")
		n := addNode(nodeRec{
			key:        key,
			label:      d.Name,
			kind:       "model",
			origin:     OriginAzure,
			typ:        d.Type,
			name:       d.Name,
			resourceID: d.ID,
			unhealthy:  unhealthy,
			health:     health,
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
		if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "deploys", false, false, false)
		}
	}
	connByName := map[string]string{}
	for _, c := range rt.Connections {
		key := mergeExistingNode(nodes, "connection", c.ID, "", c.Name, "conn:"+strings.ToLower(c.Name))
		connByName[c.Name] = key
		n := addNode(nodeRec{
			key:        key,
			label:      c.Name,
			kind:       "connection",
			origin:     OriginAzure,
			typ:        c.Type,
			name:       c.Name,
			resourceID: c.ID,
			unhealthy:  strings.TrimSpace(c.Error) != "",
			health:     cleanHealth(c.Error),
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
		if projectKey != "" {
			addEdge(projectKey, key, "uses", false, false, false)
		} else if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "uses", false, false, false)
		}
		if target := matchTargetNode(nodes, c.Target.ResourceID); target != "" {
			addEdge(key, target, "targets", false, false, false)
		} else if looksExternal(c.Target.Endpoint) {
			targetKey := unresolvedKey(c.Target.Endpoint)
			addNode(nodeRec{key: targetKey, label: externalLabel(c.Target.Endpoint), kind: "external", origin: OriginExternal, name: c.Target.Endpoint, external: true})
			addEdge(key, targetKey, "targets", false, true, false)
		} else if looksExternal(c.Target.ResourceID) {
			targetKey := unresolvedKey(c.Target.ResourceID)
			addNode(nodeRec{key: targetKey, label: externalLabel(c.Target.ResourceID), kind: "external", origin: OriginExternal, name: c.Target.ResourceID, external: true})
			addEdge(key, targetKey, "targets", false, true, false)
		}
	}
	for _, h := range rt.Hosts {
		key := mergeExistingNode(nodes, "capability-host", h.ID, "", h.Name, "host:"+strings.ToLower(h.Name))
		state := strings.ToLower(h.ProvisioningState)
		addNode(nodeRec{
			key:        key,
			label:      h.Name,
			kind:       "capability-host",
			origin:     OriginAzure,
			typ:        h.Type,
			name:       h.Name,
			resourceID: h.ID,
			unhealthy:  state != "" && state != "succeeded" && state != "successful" && state != "ready",
			health:     state,
		})
		if projectKey != "" {
			addEdge(projectKey, key, "hosts", false, false, false)
		} else if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "hosts", false, false, false)
		}
		for _, n := range append(append([]string(nil), h.AIServiceConnections...), append(h.StorageConnections, append(h.ThreadStorageConnections, h.VectorStoreConnections...)...)...) {
			if to := connByName[n]; to != "" {
				addEdge(key, to, "uses", false, false, false)
			}
		}
	}
}

func applyFindings(nodes map[string]*nodeRec, fs []sdk.Finding) int {
	unmatched := 0
	for _, f := range fs {
		n := matchFinding(nodes, f)
		if n == nil {
			unmatched++
			continue
		}
		if f.Baselined {
			n.baselined++
			continue
		}
		n.findings++
		if f.Severity.Rank() > n.severity.Rank() {
			n.severity = f.Severity
		}
		if f.Severity == sdk.SeverityError {
			n.unhealthy = true
		}
	}
	return unmatched
}

func matchFinding(nodes map[string]*nodeRec, f sdk.Finding) *nodeRec {
	if f.Resource.ID != "" {
		var matches []*nodeRec
		for _, n := range nodes {
			if strings.EqualFold(n.resourceID, f.Resource.ID) {
				matches = append(matches, n)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		return nil
	}
	if f.Resource.Type != "" || f.Resource.Name != "" {
		var matches []*nodeRec
		for _, n := range nodes {
			if strings.EqualFold(n.typ, f.Resource.Type) && strings.EqualFold(n.name, f.Resource.Name) {
				matches = append(matches, n)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
	}
	return nil
}

func materialise(nodes map[string]*nodeRec, edgeMap map[string]rawEdge, opt Options) Graph {
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	edges := make([]rawEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		return cmp.Or(
			strings.Compare(edges[i].from, edges[j].from),
			strings.Compare(edges[i].to, edges[j].to),
			strings.Compare(edges[i].kind, edges[j].kind),
		) < 0
	})

	kept := map[string]bool{}
	maxNodes := len(keys)
	if opt.MaxNodes > 0 && opt.MaxNodes < maxNodes {
		maxNodes = opt.MaxNodes
	}
	if opt.CollapseAfter > 0 && opt.CollapseAfter < maxNodes {
		maxNodes = opt.CollapseAfter
	}
	for i, k := range keys {
		if i < maxNodes {
			kept[k] = true
		}
	}
	if len(kept) != len(keys) {
		buckets := map[string]string{}
		for _, k := range keys[maxNodes:] {
			n := nodes[k]
			b := "collapsed:" + string(n.origin) + ":" + n.kind
			if _, ok := buckets[b]; !ok {
				buckets[b] = b
				nodes[b] = &nodeRec{
					key:       b,
					label:     fmt.Sprintf("Collapsed %s %s nodes", n.origin, n.kind),
					kind:      n.kind,
					origin:    n.origin,
					collapsed: true,
				}
				keys = append(keys, b)
				kept[b] = true
			}
		}
		for i := range edges {
			if !kept[edges[i].from] {
				n := nodes[edges[i].from]
				edges[i].from = "collapsed:" + string(n.origin) + ":" + n.kind
				edges[i].collapsed = true
			}
			if !kept[edges[i].to] {
				n := nodes[edges[i].to]
				edges[i].to = "collapsed:" + string(n.origin) + ":" + n.kind
				edges[i].collapsed = true
			}
		}
	}

	finalKeys := make([]string, 0, len(kept))
	for k := range kept {
		finalKeys = append(finalKeys, k)
	}
	sort.Strings(finalKeys)
	idByKey := map[string]string{}
	outNodes := make([]Node, 0, len(finalKeys))
	for i, k := range finalKeys {
		id := "n" + strconv.Itoa(i+1)
		idByKey[k] = id
		n := nodes[k]
		name := findings.Redact(n.name)
		if n.external || n.missing || n.collapsed {
			name = ""
		}
		outNodes = append(outNodes, Node{
			ID: id, Label: findings.Redact(n.label), Kind: n.kind, Origin: n.origin,
			Type: n.typ, Name: name, Resource: findings.Redact(n.resourceID), Source: n.source,
			External: n.external, Missing: n.missing, Collapsed: n.collapsed, Findings: n.findings,
			Baselined: n.baselined, Severity: n.severity, Unhealthy: n.unhealthy, Health: findings.Redact(n.health),
		})
	}
	dedup := map[string]bool{}
	outEdges := make([]Edge, 0, len(edges))
	for _, e := range edges {
		from, okFrom := idByKey[e.from]
		to, okTo := idByKey[e.to]
		if !okFrom || !okTo || from == to {
			continue
		}
		key := from + "\x00" + to + "\x00" + e.kind
		if dedup[key] {
			continue
		}
		dedup[key] = true
		outEdges = append(outEdges, Edge{From: from, To: to, Kind: e.kind, External: e.external, Missing: e.missing, Collapsed: e.collapsed})
	}
	maxEdges := len(outEdges)
	if opt.MaxEdges > 0 && opt.MaxEdges < maxEdges {
		maxEdges = opt.MaxEdges
	}
	collapsedEdges := len(outEdges) - maxEdges
	outEdges = append([]Edge(nil), outEdges[:maxEdges]...)
	return Graph{
		SchemaVersion: "1",
		Mode:          opt.Mode,
		Nodes:         outNodes,
		Edges:         outEdges,
		Summary: Summary{
			Mode:           opt.Mode,
			NodeCount:      len(outNodes),
			EdgeCount:      len(outEdges),
			CollapsedNodes: len(keys) - len(finalKeys),
			CollapsedEdges: collapsedEdges,
		},
	}
}

func redactGraph(g *Graph) {
	for i := range g.Nodes {
		h := fnv.New64a()
		_, _ = h.Write([]byte(g.Nodes[i].Name + "\x00" + g.Nodes[i].Resource + "\x00" + g.Nodes[i].Label))
		g.Nodes[i].Label = g.Nodes[i].Kind + "-" + strconv.FormatUint(h.Sum64(), 36)
		g.Nodes[i].Name = ""
		g.Nodes[i].Resource = ""
		g.Nodes[i].Health = ""
		g.Nodes[i].Source = sdk.Location{}
	}
}

func classifyService(doc *azureyaml.Document, name string) string {
	if doc == nil {
		return "service"
	}
	host, _, ok := doc.Lookup("services", name, "host")
	if !ok {
		return "service"
	}
	switch strings.ToLower(host) {
	case "azure.ai.project", "microsoft.foundry":
		return "project"
	case "azure.ai.agent":
		return "agent"
	case "azure.ai.connection":
		return "connection"
	case "azure.ai.toolbox":
		return "toolbox"
	case "azure.ai.skill":
		return "skill"
	case "azure.ai.routine":
		return "routine"
	case "azure.ai.eval":
		return "evaluation"
	default:
		return "service"
	}
}

func classifyResource(typ string) string {
	t := strings.ToLower(typ)
	switch {
	case t == "microsoft.cognitiveservices/accounts":
		return "foundry-account"
	case strings.Contains(t, "microsoft.cognitiveservices/accounts/projects/connections"),
		strings.Contains(t, "microsoft.cognitiveservices/accounts/connections"):
		return "connection"
	case strings.Contains(t, "microsoft.cognitiveservices/accounts/projects/capabilityhosts"),
		strings.Contains(t, "microsoft.cognitiveservices/accounts/capabilityhosts"):
		return "capability-host"
	case strings.Contains(t, "microsoft.cognitiveservices/accounts/projects"):
		return "project"
	case strings.Contains(t, "microsoft.cognitiveservices/accounts/deployments"):
		return "model"
	case strings.Contains(t, "managedidentity"):
		return "identity"
	case strings.Contains(t, "roleassignments"):
		return "role"
	case strings.Contains(t, "microsoft.network/"):
		return "network"
	case strings.Contains(t, "microsoft.search/searchservices"):
		return "search"
	case strings.Contains(t, "microsoft.storage/storageaccounts"):
		return "storage"
	case strings.Contains(t, "microsoft.apimanagement/service"):
		return "apim"
	case strings.Contains(t, "microsoft.operationalinsights/workspaces"), strings.Contains(t, "microsoft.insights/"):
		return "monitoring"
	default:
		return "resource"
	}
}

func unresolvedKey(ref string) string {
	if looksExternal(ref) {
		return "external:" + ref
	}
	return "missing:" + ref
}

func looksExternal(ref string) bool {
	u := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(u, "/subscriptions/") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.Contains(u, "resourceid(")
}

func cleanHealth(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "error reported"
}

func azureResourceKey(r azure.Resource) string {
	if strings.TrimSpace(r.ID) != "" {
		return "az:" + strings.ToLower(strings.TrimSpace(r.ID))
	}
	return "az:" + strings.ToLower(r.Type) + ":" + strings.ToLower(r.Name)
}

func matchSource(nodes map[string]*nodeRec, typ, name string) *nodeRec {
	if n := matchNode(nodes, OriginLocal, classifyResource(typ), typ, name); n != nil {
		return n
	}
	return matchNode(nodes, OriginLocal, "", typ, name)
}

func matchNode(nodes map[string]*nodeRec, origin Origin, kind, typ, name string) *nodeRec {
	var exact *nodeRec
	exactCount := 0
	var fallback *nodeRec
	fallbackCount := 0
	for _, n := range nodes {
		if n.origin != origin {
			continue
		}
		if kind != "" && n.kind != kind {
			continue
		}
		if typ != "" && !strings.EqualFold(n.typ, typ) {
			continue
		}
		if strings.EqualFold(n.name, name) {
			exactCount++
			if exact == nil || n.key < exact.key {
				exact = n
			}
			continue
		}
		if strings.EqualFold(lastSegment(n.name), lastSegment(name)) {
			fallbackCount++
			if fallback == nil || n.key < fallback.key {
				fallback = n
			}
		}
	}
	if exactCount == 1 && exact != nil {
		return exact
	}
	if fallbackCount == 1 {
		return fallback
	}
	return nil
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func rankOrigin(o Origin) int {
	switch o {
	case OriginBoth:
		return 5
	case OriginAzure:
		return 4
	case OriginLocal:
		return 3
	case OriginExternal:
		return 2
	case OriginMissing:
		return 1
	default:
		return 0
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func mergedOrigin(current, incoming Origin) Origin {
	switch {
	case current == incoming:
		return current
	case current == "":
		return incoming
	case (current == OriginLocal && incoming == OriginAzure) || (current == OriginAzure && incoming == OriginLocal):
		return OriginBoth
	default:
		if rankOrigin(incoming) > rankOrigin(current) {
			return incoming
		}
		return current
	}
}

func mergeExistingNode(nodes map[string]*nodeRec, kind, resourceID, typ, name, fallback string) string {
	if strings.TrimSpace(resourceID) != "" {
		for _, n := range nodes {
			if !mergeableOrigin(n.origin) {
				continue
			}
			if kind != "" && n.kind != kind {
				continue
			}
			if strings.EqualFold(n.resourceID, resourceID) {
				return n.key
			}
		}
	}
	var exact *nodeRec
	exactCount := 0
	for _, n := range nodes {
		if !mergeableOrigin(n.origin) {
			continue
		}
		if kind != "" && n.kind != kind {
			continue
		}
		if typ != "" && n.typ != "" && !strings.EqualFold(n.typ, typ) {
			continue
		}
		if strings.EqualFold(n.name, name) {
			exactCount++
			if exact == nil || n.key < exact.key {
				exact = n
			}
		}
	}
	if exactCount == 1 && exact != nil {
		return exact.key
	}
	var candidate *nodeRec
	count := 0
	for _, n := range nodes {
		if !mergeableOrigin(n.origin) {
			continue
		}
		if kind != "" && n.kind != kind {
			continue
		}
		if typ != "" && n.typ != "" && !strings.EqualFold(n.typ, typ) {
			continue
		}
		if strings.EqualFold(lastSegment(n.name), lastSegment(name)) {
			count++
			if candidate == nil || n.key < candidate.key {
				candidate = n
			}
		}
	}
	if count == 1 && candidate != nil {
		return candidate.key
	}
	return fallback
}

func mergeableOrigin(origin Origin) bool {
	switch origin {
	case OriginExternal, OriginMissing, OriginCollapsed:
		return false
	default:
		return true
	}
}

func matchTargetNode(nodes map[string]*nodeRec, resourceID string) string {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return ""
	}
	var matches []string
	for _, n := range nodes {
		if strings.EqualFold(strings.TrimSpace(n.resourceID), resourceID) {
			matches = append(matches, n.key)
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func matchNodeByTypeName(nodes map[string]*nodeRec, typ, name string) string {
	var matches []string
	for _, n := range nodes {
		if strings.EqualFold(n.typ, typ) && strings.EqualFold(n.name, name) {
			matches = append(matches, n.key)
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func armResourceTypeName(id string) (string, string, bool) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(strings.ToLower(id), "/subscriptions/") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i < len(parts); i++ {
		if strings.EqualFold(parts[i], "providers") && i+2 < len(parts) {
			provider := parts[i+1]
			rest := parts[i+2:]
			if len(rest) < 2 {
				return "", "", false
			}
			var typeParts []string
			for j := 0; j < len(rest)-1; j += 2 {
				typeParts = append(typeParts, rest[j])
			}
			return provider + "/" + strings.Join(typeParts, "/"), rest[len(rest)-1], true
		}
	}
	return "", "", false
}

func filterRaw(nodes map[string]*nodeRec, edges map[string]rawEdge, mode Mode) {
	if mode == ModeCombined {
		return
	}
	keepNode := func(n *nodeRec) bool {
		switch mode {
		case ModeSource:
			return n.origin == OriginLocal || n.origin == OriginExternal || n.origin == OriginMissing || n.origin == OriginCollapsed
		case ModeDeployed:
			return n.origin == OriginAzure || n.origin == OriginBoth || n.origin == OriginExternal || n.origin == OriginCollapsed
		default:
			return true
		}
	}
	for key, n := range nodes {
		if !keepNode(n) {
			delete(nodes, key)
		}
	}
	for key, e := range edges {
		if _, ok := nodes[e.from]; !ok {
			delete(edges, key)
			continue
		}
		if _, ok := nodes[e.to]; !ok {
			delete(edges, key)
		}
	}
}

func externalLabel(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "external"
	}
	if strings.HasPrefix(strings.ToLower(ref), "http://") || strings.HasPrefix(strings.ToLower(ref), "https://") {
		return "external-url"
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 0 {
		return "external"
	}
	return "external:" + findings.Redact(parts[len(parts)-1])
}

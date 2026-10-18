package view

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
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
	// ModeDiff is the environment-difference graph built by Diff from two
	// previously rendered JSON graphs.
	ModeDiff Mode = "diff"
)

// SchemaVersion is the graph JSON schemaVersion (schemas/graph.schema.json).
const SchemaVersion = "1"

type Origin string

const (
	OriginLocal     Origin = "local"
	OriginAzure     Origin = "azure"
	OriginBoth      Origin = "both"
	OriginExternal  Origin = "external"
	OriginMissing   Origin = "missing"
	OriginCollapsed Origin = "collapsed"
	// OriginUnobserved marks a combined-mode node declared in source whose
	// type the deployed inventory cannot observe (child and extension
	// resources, azure.yaml services), so neither "local" nor "both" is
	// supported by evidence.
	OriginUnobserved Origin = "unobserved"
)

// Diff states of a node or edge in a ModeDiff graph.
const (
	DiffAdded   = "added"
	DiffRemoved = "removed"
	DiffChanged = "changed"
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
	// Conditional marks a template resource deployed only under a condition or
	// loop the template cannot resolve.
	Conditional bool `json:"conditional,omitempty"`
	// Diff is set only in ModeDiff graphs: added, removed or changed.
	Diff string `json:"diff,omitempty"`
}

type Edge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`
	External  bool   `json:"external,omitempty"`
	Missing   bool   `json:"missing,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
	// Diff is set only in ModeDiff graphs: added or removed.
	Diff string `json:"diff,omitempty"`
}

type Summary struct {
	Mode              Mode `json:"mode"`
	NodeCount         int  `json:"nodeCount"`
	EdgeCount         int  `json:"edgeCount"`
	CollapsedNodes    int  `json:"collapsedNodes,omitempty"`
	CollapsedEdges    int  `json:"collapsedEdges,omitempty"`
	UnmatchedFindings int  `json:"unmatchedFindings,omitempty"`
	AddedNodes        int  `json:"addedNodes,omitempty"`
	RemovedNodes      int  `json:"removedNodes,omitempty"`
	ChangedNodes      int  `json:"changedNodes,omitempty"`
	// Notes are secret-free statements about overlays that were skipped, for
	// example because the caller lacked permission to read them.
	Notes []string `json:"notes,omitempty"`
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
	// Scope is the subscription or resource group ID the deployed inventory
	// covers. An ARM ID inside it that matches no node is drawn as missing,
	// not external. Empty treats every ARM ID reference as external.
	Scope string
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
	// ObservedTypes are the ARM resource types the runtime overlay read
	// successfully (for example Microsoft.CognitiveServices/accounts/deployments),
	// so source nodes of those types are not marked unobserved.
	ObservedTypes []string
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
	key         string
	label       string
	kind        string
	origin      Origin
	typ         string
	name        string
	resourceID  string
	source      sdk.Location
	external    bool
	missing     bool
	collapsed   bool
	findings    int
	baselined   int
	severity    sdk.Severity
	unhealthy   bool
	health      string
	conditional bool
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
			cur.conditional = cur.conditional || n.conditional
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

	listed, observed := observedTypes(in)
	sourceNodes := map[string]string{}
	var unresolved []coregraph.Unresolved
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
				// The node key keeps the scope token that tells two module
				// instances apart; labels and names never show it.
				r.label = firstNonEmpty(armexpr.StripScope(n.Name), n.Type)
				r.typ = n.Type
				r.name = armexpr.StripScope(n.Name)
				// The template pointer (n.ID) is not an ARM ID; resourceID is
				// reserved for ARM IDs, set when inventory correlates the node.
				r.conditional = n.Conditional
			}
			sourceNodes[n.ID] = r.key
			addNode(r)
		}
		for _, e := range g.Edges {
			addEdge(sourceNodes[e.From], sourceNodes[e.To], string(e.Kind), false, false, false)
		}
		// Unresolved references are classified after the inventory merge, so an
		// ARM ID of a deployed resource resolves to its node.
		unresolved = g.Unresolved
		addDeploymentNodes(in.Source.Document, addNode, addEdge)
	}

	inventoryCounts := map[string]int{}
	for _, r := range in.Inventory {
		inventoryCounts[strings.ToLower(r.Type)+"\x00"+strings.ToLower(r.Name)]++
	}
	inventoryKeys := map[string]string{}
	for _, r := range in.Inventory {
		key := azureResourceKey(r)
		state := strings.ToLower(strings.TrimSpace(r.Properties["properties.provisioningState"]))
		unhealthy := unhealthyState(state)
		inventoryKeys[strings.ToLower(strings.TrimSpace(r.ID))] = key
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
				// The deployed ARM ID always wins: it is what runtime
				// connection targets and live findings refer to.
				matched.resourceID = r.ID
				if state != "" {
					matched.health = state
				}
				if unhealthy {
					matched.unhealthy = true
				}
				inventoryKeys[strings.ToLower(strings.TrimSpace(r.ID))] = matched.key
				delete(nodes, key)
			}
		}
	}
	for _, u := range unresolved {
		if to := inventoryKeys[strings.ToLower(strings.TrimSpace(u.Ref))]; to != "" && !u.InTemplate {
			addEdge(sourceNodes[u.From], to, string(u.Kind), false, false, false)
			continue
		}
		key, missing, external := addRefNode(addNode, u.Ref, u.InTemplate, opt.Scope, listed)
		addEdge(sourceNodes[u.From], key, string(u.Kind), missing, external, false)
	}
	addIdentityEdges(in.Inventory, inventoryKeys, opt.Scope, listed, addNode, addEdge, nodes)
	addConnectionTargets(in.Source.Document, opt.Scope, listed, addNode, addEdge, nodes)

	addRuntimeNodes(in.Runtime, opt.Scope, listed, addNode, addEdge, nodes)
	if opt.Mode == ModeCombined {
		markUnobserved(nodes, observed)
	}
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

func addConnectionTargets(doc *azureyaml.Document, scope string, observed map[string]bool, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool), nodes map[string]*nodeRec) {
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
		key, missing, external := addRefNode(addNode, target, false, scope, observed)
		addEdge("svc:"+svc, key, "targets", missing, external, false)
	}
}

// addIdentityEdges draws resource -> user-assigned identity edges from the
// identity.userAssignedIdentities keys of the inventory list response. An
// identity that matches no node is drawn by addRefNode.
func addIdentityEdges(inv []azure.Resource, inventoryKeys map[string]string, scope string, observed map[string]bool, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool), nodes map[string]*nodeRec) {
	for _, r := range inv {
		from := inventoryKeys[strings.ToLower(strings.TrimSpace(r.ID))]
		if from == "" {
			continue
		}
		for _, ua := range r.UserAssignedIdentities {
			if to := inventoryKeys[strings.ToLower(strings.TrimSpace(ua))]; to != "" {
				addEdge(from, to, "usesIdentity", false, false, false)
				continue
			}
			if to := matchTargetNode(nodes, ua); to != "" {
				addEdge(from, to, "usesIdentity", false, false, false)
				continue
			}
			key, missing, external := addRefNode(addNode, ua, false, scope, observed)
			addEdge(from, key, "usesIdentity", missing, external, false)
		}
	}
}

func addRuntimeNodes(rt RuntimeInput, scope string, observed map[string]bool, addNode func(nodeRec) *nodeRec, addEdge func(string, string, string, bool, bool, bool), nodes map[string]*nodeRec) {
	accountNodeKey := ""
	projectKey := ""
	if rt.Account != nil {
		// Resolve the account like the project: a source-declared account
		// already correlated with inventory carries the ARM ID, so the
		// runtime edges hang off that one node instead of a duplicate.
		accountNodeKey = mergeExistingNode(nodes, "foundry-account", rt.Account.ID, rt.Account.Type, rt.Account.Name, azureResourceKey(*rt.Account))
		n := addNode(nodeRec{
			key:        accountNodeKey,
			label:      rt.Account.Name,
			kind:       "foundry-account",
			origin:     OriginAzure,
			typ:        rt.Account.Type,
			name:       rt.Account.Name,
			resourceID: rt.Account.ID,
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
	}
	if rt.Project != nil {
		key := mergeExistingNode(nodes, "project", rt.Project.ID, "", rt.Project.Name, "project:"+strings.ToLower(rt.Project.Name))
		state := strings.ToLower(strings.TrimSpace(rt.Project.ProvisioningState))
		n := addNode(nodeRec{
			key:        key,
			label:      rt.Project.Name,
			kind:       "project",
			origin:     OriginAzure,
			name:       rt.Project.Name,
			resourceID: rt.Project.ID,
			unhealthy:  unhealthyState(state),
			health:     state,
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
		projectKey = key
		if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "contains", false, false, false)
		}
	}
	for _, d := range rt.Deployments {
		key := mergeExistingNode(nodes, "model", d.ID, deploymentType, d.Name, "deployment:"+strings.ToLower(d.Name))
		state := strings.ToLower(strings.TrimSpace(d.ProvisioningState))
		deploymentState := strings.ToLower(strings.TrimSpace(d.DeploymentState))
		// deploymentState Paused is an operator choice, not a failure; only a
		// Failed or Canceled provisioning state marks the node unhealthy.
		n := addNode(nodeRec{
			key:        key,
			label:      d.Name,
			kind:       "model",
			origin:     OriginAzure,
			typ:        d.Type,
			name:       d.Name,
			resourceID: d.ID,
			unhealthy:  unhealthyState(state),
			health:     firstNonEmpty(deploymentState, state),
		})
		n.origin = mergedOrigin(n.origin, OriginAzure)
		if accountNodeKey != "" {
			addEdge(accountNodeKey, key, "deploys", false, false, false)
		}
	}
	connByName := map[string]string{}
	connAccountScoped := map[string]bool{}
	for _, c := range rt.Connections {
		accountScoped := c.ID != "" && !strings.Contains(strings.ToLower(c.ID), "/projects/")
		key := mergeExistingNode(nodes, "connection", c.ID, "", c.Name, "conn:"+strings.ToLower(firstNonEmpty(strings.TrimSpace(c.ID), c.Name)))
		// Capability hosts name connections; a project connection wins over
		// an account connection of the same name.
		if cur, ok := connByName[c.Name]; !ok || (!accountScoped && connAccountScoped[cur]) {
			connByName[c.Name] = key
		}
		connAccountScoped[key] = accountScoped
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
		owner := projectKey
		if owner == "" || (accountScoped && accountNodeKey != "") {
			owner = accountNodeKey
		}
		addEdge(owner, key, "uses", false, false, false)
		// The backing ARM ID is the stronger evidence, so it is classified
		// before the endpoint URL.
		target := matchTargetNode(nodes, c.Target.ResourceID)
		switch {
		case target != "":
			addEdge(key, target, "targets", false, false, false)
		case looksExternal(c.Target.ResourceID):
			targetKey, missing, external := addRefNode(addNode, c.Target.ResourceID, false, scope, observed)
			addEdge(key, targetKey, "targets", missing, external, false)
		case looksExternal(c.Target.Endpoint):
			targetKey := unresolvedKeyFor(c.Target.Endpoint, true)
			addNode(nodeRec{key: targetKey, label: externalLabel(c.Target.Endpoint), kind: "external", origin: OriginExternal, name: c.Target.Endpoint, external: true})
			addEdge(key, targetKey, "targets", false, true, false)
		}
	}
	for _, h := range rt.Hosts {
		key := mergeExistingNode(nodes, "capability-host", h.ID, "", h.Name, "host:"+strings.ToLower(h.Name))
		state := strings.ToLower(strings.TrimSpace(h.ProvisioningState))
		addNode(nodeRec{
			key:        key,
			label:      h.Name,
			kind:       "capability-host",
			origin:     OriginAzure,
			typ:        h.Type,
			name:       h.Name,
			resourceID: h.ID,
			unhealthy:  unhealthyState(state),
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

// deploymentType is the ARM type of a Foundry account model deployment.
const deploymentType = "Microsoft.CognitiveServices/accounts/deployments"

// unhealthyState reports whether a provisioning state is a terminal failure.
// Succeeded, Failed and Canceled are the ARM terminal provisioning states;
// transient states (Creating, Updating, Accepted, ...) are shown as health
// text but are not failures.
func unhealthyState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "failed", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

// observedTypes returns the lower-cased ARM types the deployed inventory
// returned (listed) and those plus the types a successful runtime overlay
// call returned (observed). Unresolved references use listed: they are
// classified before the runtime overlay adds its nodes, so a reference to a
// runtime-only type stays unobserved and the runtime node later merges into
// it by ARM ID. Declared source nodes use observed, after the overlay.
func observedTypes(in Input) (listed, observed map[string]bool) {
	listed, observed = map[string]bool{}, map[string]bool{}
	for _, r := range in.Inventory {
		listed[strings.ToLower(r.Type)] = true
		observed[strings.ToLower(r.Type)] = true
	}
	for _, t := range in.Runtime.ObservedTypes {
		observed[strings.ToLower(t)] = true
	}
	return listed, observed
}

// markUnobserved re-labels combined-mode source nodes that are still local
// but whose type the deployed inventory cannot observe. The ARM Resources -
// List API returns top-level resources; extension resources (for example
// Microsoft.Authorization/roleAssignments) and proxy child resources (subnets,
// account deployments, connections, capability hosts) are read through other
// APIs, so their absence from the list is not evidence that they are not
// deployed. A type the inventory or runtime overlay did return is observable.
func markUnobserved(nodes map[string]*nodeRec, observed map[string]bool) {
	for _, n := range nodes {
		if n.origin != OriginLocal || inventoryObservable(n, observed) {
			continue
		}
		n.origin = OriginUnobserved
	}
}

func inventoryObservable(n *nodeRec, observed map[string]bool) bool {
	switch {
	case strings.HasPrefix(n.key, "arm:"):
		return typeObservable(n.typ, observed)
	case strings.HasPrefix(n.key, "model:"):
		return observed[strings.ToLower(deploymentType)]
	default:
		// azure.yaml services (agents, toolboxes, connections, ...) are not
		// ARM resources the inventory lists.
		return false
	}
}

// typeObservable reports whether the absence of an ARM type from the
// inventory is evidence that no such resource exists in the scope.
func typeObservable(typ string, observed map[string]bool) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	if observed[t] {
		return true
	}
	return strings.Count(t, "/") == 1 && !notInResourceList(t)
}

// refOrigin classifies a reference that matched no node: external when it
// is outside the inventory scope (or no scope is known), unobserved when it is
// an in-scope ARM ID of a type the inventory cannot observe (child and
// extension resources such as subnets or role assignments, or an
// unparseable type), and missing otherwise (a name that resolved to nothing,
// an in-template reference, or an in-scope ARM ID of an observable type the
// complete inventory did not return).
func refOrigin(ref string, inTemplate bool, scope string, observed map[string]bool) Origin {
	if inTemplate || !looksExternal(ref) {
		return OriginMissing
	}
	if isExternalRef(ref, scope) {
		return OriginExternal
	}
	typ, _, ok := armResourceTypeName(ref)
	if !ok || !typeObservable(typ, observed) {
		return OriginUnobserved
	}
	return OriginMissing
}

// addRefNode adds the placeholder node of an unresolved reference and returns
// its key and the missing and external flags for the edge to it.
func addRefNode(addNode func(nodeRec) *nodeRec, ref string, inTemplate bool, scope string, observed map[string]bool) (key string, missing, external bool) {
	display := armexpr.StripScope(ref)
	switch refOrigin(ref, inTemplate, scope, observed) {
	case OriginExternal:
		key = unresolvedKeyFor(ref, true)
		addNode(nodeRec{key: key, label: externalLabel(display), kind: "external", origin: OriginExternal, name: display, external: true})
		return key, false, true
	case OriginUnobserved:
		typ, name, _ := armResourceTypeName(ref)
		key = "unobserved:" + ref
		addNode(nodeRec{key: key, label: findings.Redact(firstNonEmpty(name, display)), kind: classifyResource(typ), origin: OriginUnobserved, typ: typ, name: name, resourceID: ref})
		return key, false, false
	default:
		key = unresolvedKeyFor(ref, false)
		addNode(nodeRec{key: key, label: findings.Redact(display), kind: "missing", origin: OriginMissing, name: display, missing: true})
		return key, true, false
	}
}

// notInResourceList lists top-level types Foundry Doctor does not treat as
// observable through Resources - List: extension resources that have their
// own list APIs. Conservative: a type missing here is assumed observable.
func notInResourceList(t string) bool {
	return strings.HasPrefix(t, "microsoft.authorization/") ||
		t == "microsoft.insights/diagnosticsettings" ||
		t == "microsoft.resources/deployments"
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
	typ, name := f.Resource.Type, f.Resource.Name
	if id := strings.TrimSpace(f.Resource.ID); id != "" {
		var matches []*nodeRec
		for _, n := range nodes {
			if strings.EqualFold(n.resourceID, id) {
				matches = append(matches, n)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0]
		case 0:
			// Fall back to type and name: a live finding's ARM ID can name a
			// resource that is drawn from source without a correlated ID.
			if t, nm, ok := armResourceTypeName(id); ok {
				typ = firstNonEmpty(typ, t)
				name = firstNonEmpty(name, nm)
			}
		default:
			return nil
		}
	}
	if typ != "" || name != "" {
		var matches []*nodeRec
		for _, n := range nodes {
			if strings.EqualFold(n.typ, typ) && strings.EqualFold(n.name, name) {
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
	// Foundry nodes are kept first; leaf infrastructure is collapsed first.
	keepOrder := append([]string(nil), keys...)
	sort.SliceStable(keepOrder, func(i, j int) bool {
		return collapsePriority(nodes[keepOrder[i]].kind) < collapsePriority(nodes[keepOrder[j]].kind)
	})
	for i, k := range keepOrder {
		if i < maxNodes {
			kept[k] = true
		}
	}
	if len(kept) != len(keys) {
		buckets := map[string]string{}
		for _, k := range keepOrder[maxNodes:] {
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
			Conditional: n.conditional,
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
		SchemaVersion: SchemaVersion,
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

func unresolvedKeyFor(ref string, external bool) string {
	if external {
		return "external:" + ref
	}
	return "missing:" + ref
}

func looksExternal(ref string) bool {
	u := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(u, "/subscriptions/") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.Contains(u, "resourceid(")
}

// isExternalRef is looksExternal, except that an ARM ID inside the graphed
// inventory scope is not external. refOrigin decides whether such an ID is
// missing (observable type) or unobserved (child or extension type the
// inventory cannot list).
func isExternalRef(ref, scope string) bool {
	if !looksExternal(ref) {
		return false
	}
	scope = strings.TrimRight(strings.ToLower(strings.TrimSpace(scope)), "/")
	if scope == "" {
		return true
	}
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), scope+"/")
}

// collapsePriority orders nodes for --collapse-after and --max-nodes: lower
// values are kept first.
func collapsePriority(kind string) int {
	switch kind {
	case "foundry-account", "project", "agent", "model", "connection", "capability-host", "toolbox":
		return 0
	case "external", "missing":
		return 2
	default:
		return 1
	}
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
	resourceID = strings.TrimSpace(resourceID)
	// A node already bound to a different ARM ID is a different resource, so
	// name matching never merges, for example, a project connection with an
	// account connection of the same name.
	otherResource := func(n *nodeRec) bool {
		return resourceID != "" && n.resourceID != "" && !strings.EqualFold(n.resourceID, resourceID)
	}
	if resourceID != "" {
		for _, n := range nodes {
			// An unobserved reference node carries the ARM ID it names, so
			// the runtime resource it turns out to be merges into it.
			if !mergeableOrigin(n.origin) && n.origin != OriginUnobserved {
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
		if otherResource(n) {
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
		if otherResource(n) {
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
	case OriginExternal, OriginMissing, OriginCollapsed, OriginUnobserved:
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
			// Source is never built in deployed mode, so missing and
			// unobserved nodes here are Azure-side references (a deleted
			// identity or connection target, or a child resource the
			// inventory cannot list) and are kept.
			return n.origin == OriginAzure || n.origin == OriginBoth || n.origin == OriginExternal || n.origin == OriginCollapsed ||
				n.origin == OriginMissing || n.origin == OriginUnobserved
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

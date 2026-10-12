package sec

import (
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Scope answers "is this Search, Cosmos or Storage resource used by the Foundry project?" from the
// compiled template alone (fact-check item 6). A resource is in scope when a Foundry connection's
// properties.metadata.ResourceId, or a capabilitySettings ARM ID (preview 2026-07-15-preview only),
// names it. Capability hosts list connection NAMES; they only add evidence and expose connections that
// are defined outside the scanned files. properties.target is a URL and is never used to tie a resource.

type scopeState int

const (
	scopeOut       scopeState = iota // nothing in the template points at the resource
	scopeIn                          // a connection or capability setting points at it
	scopeUncertain                   // something Foundry-side might point at it, but the link cannot be read
)

type scopeResult struct {
	state scopeState
	refs  []string // sorted human-readable references, no values
	why   string   // for scopeUncertain
	// authTypes are the literal authType values of matching connections, context only.
	authTypes []string
	// related are the resources that carry the link, for deployment doubt.
	related []model.Resource
}

// targetKind says how a resource type is tied to Foundry.
type targetKind struct {
	typ        string
	categories []string // lower-case connection categories that can name this type
	lists      []string // capability host properties holding connection names that can name this type
	stores     []string // capabilitySettings stores that can name this type
}

var (
	searchKind = targetKind{typ: typSearch, categories: []string{"cognitivesearch"},
		lists: []string{"vectorStoreConnections"}, stores: []string{"vectorStore"}}
	cosmosKind = targetKind{typ: typCosmos, categories: []string{"cosmosdb", "azurecosmosdb"},
		lists:  []string{"threadStorageConnections", "vectorStoreConnections", "storageConnections"},
		stores: []string{"documentStore"}}
	storageKind = targetKind{typ: typStorage, categories: []string{"azurestorageaccount"},
		lists: []string{"storageConnections"}, stores: []string{"blobStore"}}
)

type connInfo struct {
	res      model.Resource
	name     string // lower-case connection name, "" when not literal
	category string // lower-case, "" when absent or unresolved
	key      string // identity key of metadata.ResourceId, "" when it cannot be read
	authType string // literal authType, context only
}

type capEntry struct {
	host model.Resource
	prop string
	name string // lower-case connection name, "" when not literal
}

type storeRef struct {
	owner model.Resource
	store string
	key   string // "" when unreadable
}

type foundryIndex struct {
	conns   []connInfo
	entries []capEntry
	// unresolvedLists are capability host lists that are entirely unresolved.
	unresolvedLists []capEntry
	stores          []storeRef
}

var capLists = []string{"threadStorageConnections", "vectorStoreConnections", "storageConnections"}
var capStores = []string{"documentStore", "vectorStore", "blobStore"}

func buildIndex(t *model.ARMTemplate) *foundryIndex {
	ix := &foundryIndex{}
	if t == nil {
		return ix
	}
	for _, r := range t.Resources {
		switch strings.ToLower(r.Type) {
		case strings.ToLower(typConnection), strings.ToLower(typProjConn):
			ix.conns = append(ix.conns, readConn(r))
		case strings.ToLower(typCapHost), strings.ToLower(typProjCapHost):
			for _, p := range capLists {
				v := r.Get("properties." + p)
				switch {
				case v.State == model.Absent:
				case v.State == model.Unresolved:
					ix.unresolvedLists = append(ix.unresolvedLists, capEntry{host: r, prop: p})
				default:
					arr, _ := v.Lit.([]any)
					for _, it := range arr {
						s, ok := it.(string)
						e := capEntry{host: r, prop: p}
						if ok && !model.IsARMExpression(s) {
							e.name = strings.ToLower(s)
						}
						ix.entries = append(ix.entries, e)
					}
				}
			}
		case strings.ToLower(typAccount), strings.ToLower(typProject):
			for _, s := range capStores {
				if r.Get("properties.capabilitySettings."+s).State == model.Absent {
					continue
				}
				ref := storeRef{owner: r, store: s}
				if str, ok := rawString(r, "properties", "capabilitySettings", s); ok {
					ref.key = idKey(str)
				}
				ix.stores = append(ix.stores, ref)
			}
		}
	}
	return ix
}

func readConn(r model.Resource) connInfo {
	c := connInfo{res: r, name: lastName(r.Name)}
	if s, ok := r.Get("properties.category").String(); ok {
		c.category = strings.ToLower(s)
	}
	if s, ok := rawString(r, "properties", "metadata", "ResourceId"); ok {
		c.key = idKey(s)
	}
	if s, ok := r.Get("properties.authType").String(); ok {
		c.authType = s
	}
	return c
}

func (c connInfo) label() string {
	if c.name != "" {
		return c.name
	}
	return c.res.Name
}

// scope decides whether the target is used by Foundry.
func (ix *foundryIndex) scope(t model.Resource, k targetKind) scopeResult {
	var res scopeResult
	key := identityKey(t.Type, t.Name)
	hosts := map[string][]string{} // connection name -> "host.prop"
	for _, e := range ix.entries {
		if e.name != "" {
			hosts[e.name] = append(hosts[e.name], e.host.Name+"."+e.prop)
		}
	}
	add := func(ref string, rel model.Resource) {
		res.refs = append(res.refs, ref)
		res.related = append(res.related, rel)
	}
	for _, c := range ix.conns {
		if key == "" || c.key != key {
			continue
		}
		ref := "connection " + c.label()
		if hs := hosts[c.name]; c.name != "" && len(hs) > 0 {
			sort.Strings(hs)
			ref += " (listed by " + strings.Join(dedupe(hs), ", ") + ")"
		}
		add(ref, c.res)
		if c.authType != "" {
			res.authTypes = append(res.authTypes, c.authType)
		}
	}
	for _, s := range ix.stores {
		if key != "" && s.key == key && contains(k.stores, s.store) {
			add("capabilitySettings."+s.store+" of "+s.owner.Name, s.owner)
		}
	}
	if len(res.refs) > 0 {
		res.state = scopeIn
		sort.Strings(res.refs)
		res.refs = dedupe(res.refs)
		sort.Strings(res.authTypes)
		res.authTypes = dedupe(res.authTypes)
		return res
	}
	if key == "" {
		return scopeResult{state: scopeUncertain, why: "the resource name cannot be read from the template"}
	}
	known := map[string]bool{}
	for _, c := range ix.conns {
		if c.name != "" {
			known[c.name] = true
		}
	}
	for _, c := range ix.conns {
		if c.key == "" && (c.category == "" || contains(k.categories, c.category)) {
			return scopeResult{state: scopeUncertain, why: "connection " + c.label() + " does not state a readable metadata.ResourceId"}
		}
	}
	for _, e := range ix.entries {
		if contains(k.lists, e.prop) && (e.name == "" || !known[e.name]) {
			return scopeResult{state: scopeUncertain, why: "capability host " + e.host.Name + " lists a connection in " + e.prop + " that is defined outside the scanned files"}
		}
	}
	for _, e := range ix.unresolvedLists {
		if contains(k.lists, e.prop) {
			return scopeResult{state: scopeUncertain, why: "capability host " + e.host.Name + " " + e.prop + " is not known at compile time"}
		}
	}
	for _, s := range ix.stores {
		if s.key == "" && contains(k.stores, s.store) {
			return scopeResult{state: scopeUncertain, why: "capabilitySettings." + s.store + " of " + s.owner.Name + " is not a readable resource ID"}
		}
	}
	return scopeResult{state: scopeOut}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func dedupe(s []string) []string {
	out := s[:0:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// rawString reads a string property without evaluating it. Unlike Resource.Get it returns ARM
// expressions as written, because a resourceId() call is the usual way a template names a resource.
func rawString(r model.Resource, path ...string) (string, bool) {
	var cur any = r.Body
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[k]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

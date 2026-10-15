// Package net implements the Phase 1 FND-NET-* rules over the compiled ARM
// model. Rules are pure functions of sdk.Input; they never do I/O.
package net

import (
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Resource types (lower-cased for comparison).
const (
	tAccount   = "microsoft.cognitiveservices/accounts"
	tPE        = "microsoft.network/privateendpoints"
	tZoneGroup = "microsoft.network/privateendpoints/privatednszonegroups"
	tZone      = "microsoft.network/privatednszones"
	tZoneLink  = "microsoft.network/privatednszones/virtualnetworklinks"
	tVNet      = "microsoft.network/virtualnetworks"
	tSubnet    = "microsoft.network/virtualnetworks/subnets"
	tStorage   = "microsoft.storage/storageaccounts"
	tCosmos    = "microsoft.documentdb/databaseaccounts"
	tSearch    = "microsoft.search/searchservices"
)

func lc(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// isExpr and unresolved are the shared ARM expression test: any string that
// starts with "[" and not "[[" depends on template evaluation, so it cannot be
// compared as a literal.
func isExpr(s string) bool { return armexpr.IsExpression(s) }

func unresolved(s string) bool { return armexpr.IsExpression(s) }

func presence(r sdk.ARMResource) armexpr.Presence {
	return armexpr.PresenceOf(r.Conditional, r.Condition, r.Looped)
}

// refOf is the identity (type and name segments) of a resource.
func refOf(r sdk.ARMResource) armexpr.Ref { return armexpr.ParseName(r.Type, r.Name) }

// corr correlates a reference string (a resource ID or resourceId expression)
// with a resource.
func corr(ref string, r sdk.ARMResource) armexpr.Result {
	return armexpr.Compare(armexpr.ParseRef(ref), refOf(r))
}

// literalRegion reports whether s can be compared as a region: non-empty and
// not an ARM expression.
func literalRegion(s string) bool { return s != "" && !armexpr.IsExpression(s) }

func get(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		found := false
		for k, v := range mm {
			if strings.EqualFold(k, p) {
				cur, found = v, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return cur, true
}

func str(m map[string]any, path ...string) (string, bool) {
	v, ok := get(m, path...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func boolean(m map[string]any, path ...string) (val, ok bool) {
	v, found := get(m, path...)
	if !found {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch lc(x) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func arr(m map[string]any, path ...string) []any {
	v, _ := get(m, path...)
	a, _ := v.([]any)
	return a
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// childOf reports whether child (e.g. "pe/default") is nested under parent by
// comparing the child's parent name segments with the parent's name. The
// result is Unknown when an expression prevents the comparison.
func childOf(child, parent sdk.ARMResource) armexpr.Result {
	segs := refOf(child).Segments
	if len(segs) < 2 {
		return armexpr.Unknown
	}
	return armexpr.Compare(armexpr.Ref{Segments: segs[:len(segs)-1]}, armexpr.Ref{Segments: refOf(parent).Segments})
}

// byType returns the resources of a type that are not provably undeployed.
// Callers must still treat armexpr.MaybeDeployed resources as uncertain.
func byType(m sdk.ARMModel, typ string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range m.Resources() {
		if lc(r.Type) == typ && presence(r) != armexpr.NotDeployed {
			out = append(out, r)
		}
	}
	return out
}

func isFoundryAccount(r sdk.ARMResource) bool {
	k := lc(r.Kind)
	return lc(r.Type) == tAccount && (k == "aiservices" || k == "openai")
}

func foundryAccounts(m sdk.ARMModel) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range byType(m, tAccount) {
		if isFoundryAccount(r) {
			out = append(out, r)
		}
	}
	return out
}

func ref(r sdk.ARMResource) sdk.ResourceRef { return sdk.ResourceRef{Type: r.Type, Name: r.Name} }

type peConn struct {
	target string
	groups []string
}

func peConns(pe sdk.ARMResource) []peConn {
	var out []peConn
	for _, key := range []string{"privateLinkServiceConnections", "manualPrivateLinkServiceConnections"} {
		for _, c := range arr(pe.Properties, key) {
			cm := asMap(c)
			id, _ := str(cm, "properties", "privateLinkServiceId")
			var gs []string
			for _, g := range arr(cm, "properties", "groupIds") {
				if s, ok := g.(string); ok {
					gs = append(gs, s)
				}
			}
			out = append(out, peConn{target: id, groups: gs})
		}
	}
	return out
}

func hasGroup(gs []string, g string) bool {
	for _, x := range gs {
		if strings.EqualFold(x, g) {
			return true
		}
	}
	return false
}

type tri int

const (
	triNo tri = iota
	triYes
	triUnknown
)

// groupState reports whether a private endpoint connection carries a group ID.
// An empty want matches any group; an expression or empty list is unknown.
func groupState(gs []string, raw []any, want string) tri {
	if want == "" {
		return triYes
	}
	if hasGroup(gs, want) {
		return triYes
	}
	for _, g := range raw {
		if s, ok := g.(string); !ok || armexpr.IsExpression(s) {
			return triUnknown
		}
	}
	if len(gs) == 0 {
		return triUnknown
	}
	return triNo
}

// pesFor returns the deployed private endpoints that provably target res with
// the group ID (empty = any). uncertain is true when another endpoint might
// target res: its target cannot be correlated, it is conditional or looped, or
// its group ID is an expression. A conditional endpoint is never proof.
func pesFor(m sdk.ARMModel, res sdk.ARMResource, group string) (found []sdk.ARMResource, uncertain bool) {
nextPE:
	for _, pe := range byType(m, tPE) {
		deployed := presence(pe) == armexpr.Deployed
		for _, key := range []string{"privateLinkServiceConnections", "manualPrivateLinkServiceConnections"} {
			for _, c := range arr(pe.Properties, key) {
				cm := asMap(c)
				id, _ := str(cm, "properties", "privateLinkServiceId")
				rawGroups := arr(cm, "properties", "groupIds")
				var gs []string
				for _, g := range rawGroups {
					if s, ok := g.(string); ok {
						gs = append(gs, s)
					}
				}
				cmp := corr(id, res)
				if cmp == armexpr.Different {
					continue
				}
				gst := groupState(gs, rawGroups, group)
				if gst == triNo {
					continue
				}
				if cmp == armexpr.Same && gst == triYes && deployed {
					found = append(found, pe)
					continue nextPE
				}
				uncertain = true
			}
		}
	}
	return found, uncertain
}

// publicAccess returns the lower-cased publicNetworkAccess; absent means
// enabled (ARM default).
func publicAccess(r sdk.ARMResource) (string, bool) {
	s, ok := str(r.Properties, "publicNetworkAccess")
	if !ok {
		return "enabled", true
	}
	if unresolved(s) {
		return "", false
	}
	return lc(s), true
}

func defaultAction(r sdk.ARMResource) (string, bool) {
	s, ok := str(r.Properties, "networkAcls", "defaultAction")
	if !ok {
		return "allow", true
	}
	if unresolved(s) {
		return "", false
	}
	return lc(s), true
}

type injection struct {
	subnet  string
	managed bool
	agent   bool
}

func injections(r sdk.ARMResource) []injection {
	var out []injection
	for _, v := range arr(r.Properties, "networkInjections") {
		m := asMap(v)
		sc, _ := str(m, "scenario")
		sn, _ := str(m, "subnetArmId")
		mg, _ := boolean(m, "useMicrosoftManagedNetwork")
		out = append(out, injection{subnet: sn, managed: mg, agent: lc(sc) == "agent"})
	}
	return out
}

func isManagedNetwork(r sdk.ARMResource) bool {
	for _, i := range injections(r) {
		if i.managed {
			return true
		}
	}
	return false
}

// isPrivate reports whether the account is the private-network setup:
// public access explicitly Disabled.
func isPrivate(r sdk.ARMResource) bool {
	pa, ok := publicAccess(r)
	return ok && pa == "disabled"
}

// capabilityRef returns the ARM ID of a capabilitySettings entry, which may be
// a plain string or an object holding resourceId/id.
func capabilityRef(r sdk.ARMResource, key string) string {
	v, ok := get(r.Properties, "capabilitySettings", key)
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if s, ok := str(x, "resourceId"); ok {
			return s
		}
		s, _ := str(x, "id")
		return s
	}
	return ""
}

type subnetInfo struct {
	name  string
	props map[string]any
	vnet  sdk.ARMResource
	loc   sdk.Location
}

var subnetIdxRe = regexp.MustCompile(`(?i)^\.subnets\[(\d+)\]\.id$`)

// subnetRef is a subnet reference reduced to its virtual network and either a
// subnet name or the index into the virtual network's inline subnets (the
// compiled form of vnet.properties.subnets[0].id).
type subnetRef struct {
	vnet  armexpr.Ref
	name  armexpr.Segment
	named bool
	index int
}

func parseSubnetRef(id string) (subnetRef, bool) {
	id = strings.TrimSpace(id)
	if armexpr.IsExpression(id) {
		if c, ok := armexpr.ParseCall(id); ok && c.Fn == "reference" && len(c.Args) > 0 {
			if mm := subnetIdxRe.FindStringSubmatch(c.Suffix); mm != nil {
				v := armexpr.ParseRef("[" + c.Args[0] + "]")
				if v.Opaque || (v.Type != "" && v.Type != tVNet) {
					return subnetRef{}, false
				}
				idx := 0
				for _, ch := range mm[1] {
					idx = idx*10 + int(ch-'0')
				}
				v.Type = tVNet
				return subnetRef{vnet: v, index: idx}, true
			}
			return subnetRef{}, false
		}
	}
	r := armexpr.ParseRef(id)
	if r.Opaque || len(r.Segments) < 2 || (r.Type != "" && r.Type != tSubnet) {
		return subnetRef{}, false
	}
	n := len(r.Segments)
	return subnetRef{
		vnet:  armexpr.Ref{Type: tVNet, Segments: r.Segments[:n-1]},
		name:  r.Segments[n-1],
		named: true,
		index: -1,
	}, true
}

// resolveSubnet finds the subnet a reference points at, as a standalone
// subnets resource or nested in a virtual network's properties.subnets. It
// returns false when the reference cannot be correlated with exactly one
// deployed virtual network of the template.
func resolveSubnet(m sdk.ARMModel, id string) (subnetInfo, bool) {
	if id == "" {
		return subnetInfo{}, false
	}
	sr, ok := parseSubnetRef(id)
	if !ok {
		return subnetInfo{}, false
	}
	var vnet *sdk.ARMResource
	vnets := byType(m, tVNet)
	for i := range vnets {
		if armexpr.Compare(sr.vnet, refOf(vnets[i])) == armexpr.Same {
			if vnet != nil {
				return subnetInfo{}, false
			}
			vnet = &vnets[i]
		}
	}
	if vnet == nil || presence(*vnet) == armexpr.MaybeDeployed {
		return subnetInfo{}, false
	}
	if !sr.named {
		subs := arr(vnet.Properties, "subnets")
		if sr.index >= len(subs) {
			return subnetInfo{}, false
		}
		em := asMap(subs[sr.index])
		sn, _ := str(em, "name")
		if sn == "" || unresolved(sn) {
			return subnetInfo{}, false
		}
		return subnetInfo{name: sn, props: asMap(em["properties"]), vnet: *vnet, loc: vnet.Location}, true
	}
	for _, s := range byType(m, tSubnet) {
		segs := refOf(s).Segments
		if len(segs) < 2 || presence(s) == armexpr.MaybeDeployed {
			continue
		}
		parent := armexpr.Ref{Type: tVNet, Segments: segs[:len(segs)-1]}
		if armexpr.Compare(parent, refOf(*vnet)) != armexpr.Same {
			continue
		}
		last := segs[len(segs)-1]
		if armexpr.Compare(armexpr.Ref{Segments: []armexpr.Segment{last}}, armexpr.Ref{Segments: []armexpr.Segment{sr.name}}) == armexpr.Same {
			return subnetInfo{name: last.Text, props: s.Properties, vnet: *vnet, loc: s.Location}, true
		}
	}
	for _, e := range arr(vnet.Properties, "subnets") {
		em := asMap(e)
		sn, _ := str(em, "name")
		if sn == "" {
			continue
		}
		cand := armexpr.ParseName("", sn)
		if armexpr.Compare(armexpr.Ref{Segments: cand.Segments}, armexpr.Ref{Segments: []armexpr.Segment{sr.name}}) == armexpr.Same {
			return subnetInfo{name: sn, props: asMap(em["properties"]), vnet: *vnet, loc: vnet.Location}, true
		}
	}
	return subnetInfo{}, false
}

var regionStrip = regexp.MustCompile(`[\s_\-]+`)

// normRegion canonicalises "Sweden Central" and "swedencentral" alike.
func normRegion(s string) string { return regionStrip.ReplaceAllString(lc(s), "") }

func prefixLen(cidr string) (int, bool) {
	p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return 0, false
	}
	return p.Bits(), true
}

func isIPv4OrCIDR(s string) bool {
	s = strings.TrimSpace(s)
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Addr().Is4()
	}
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// outcome accumulates findings and skip reasons for one rule evaluation.
type outcome struct {
	id        string
	findings  []sdk.Finding
	skips     []string
	evaluated int
}

func (o *outcome) add(r sdk.ARMResource, evidence string) {
	o.findings = append(o.findings, sdk.Finding{Resource: ref(r), Location: r.Location, Evidence: evidence})
}

// addKey adds a finding with a stable Key separating several findings of one
// rule on one resource.
func (o *outcome) addKey(r sdk.ARMResource, key, evidence string) {
	o.add(r, evidence)
	o.findings[len(o.findings)-1].Key = key
}

func (o *outcome) skip(reason string) { o.skips = append(o.skips, reason) }

func (o *outcome) result() sdk.Result {
	if len(o.findings) == 0 && o.evaluated == 0 {
		reason := sdk.SkipNotApplicable + ": no applicable resources in the compiled template"
		if len(o.skips) > 0 {
			sort.Strings(o.skips)
			reason = strings.Join(o.skips, "; ")
		}
		return sdk.Result{Skipped: &sdk.Skip{RuleID: o.id, Reason: reason}}
	}
	sort.SliceStable(o.findings, func(i, j int) bool {
		a, b := o.findings[i], o.findings[j]
		if a.Resource.Name != b.Resource.Name {
			return a.Resource.Name < b.Resource.Name
		}
		return a.Evidence < b.Evidence
	})
	return sdk.Result{Findings: o.findings}
}

func noARM(id string, in *sdk.Input) (sdk.Result, bool) {
	if in == nil || in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: id, Reason: sdk.SkipInputUnavailable}}, true
	}
	return sdk.Result{}, false
}

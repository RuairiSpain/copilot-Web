// Package net implements the Phase 1 FND-NET-* rules over the compiled ARM
// model. Rules are pure functions of sdk.Input; they never do I/O.
package net

import (
	"net/netip"
	"regexp"
	"sort"
	"strings"

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

func isExpr(s string) bool { return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") }

// unresolved reports whether s depends on a parameter/variable/reference and
// so cannot be evaluated from the template alone.
func unresolved(s string) bool {
	if !isExpr(s) {
		return false
	}
	l := strings.ToLower(s)
	return strings.Contains(l, "parameters(") || strings.Contains(l, "variables(") || strings.Contains(l, "reference(")
}

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

// refMatches reports whether an ARM ID or expression points at the named
// resource. Matching is by trailing name segment.
func refMatches(ref, name string) bool {
	if ref == "" || name == "" {
		return false
	}
	r, n := lc(ref), lc(name)
	if r == n || strings.HasSuffix(r, "/"+n) {
		return true
	}
	return strings.Contains(r, "'"+n+"')") || strings.Contains(r, "'"+n+"'")
}

// childOf reports whether child (e.g. "pe/default") is nested under parent.
func childOf(child, parent string) bool {
	c, p := lc(child), lc(parent)
	if strings.HasPrefix(c, p+"/") {
		return true
	}
	return isExpr(child) && p != "" && strings.Contains(c, "'"+p+"'")
}

func byType(m sdk.ARMModel, typ string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range m.Resources() {
		if lc(r.Type) == typ {
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

// pesFor returns endpoints targeting name with the group ID (empty = any).
// unresolvedPE is true when some endpoint has a parameter-valued target and so
// might target the resource.
func pesFor(m sdk.ARMModel, name, group string) (found []sdk.ARMResource, unresolvedPE bool) {
	for _, pe := range byType(m, tPE) {
		for _, c := range peConns(pe) {
			if unresolved(c.target) {
				unresolvedPE = true
				continue
			}
			if !refMatches(c.target, name) {
				continue
			}
			if group == "" || hasGroup(c.groups, group) {
				found = append(found, pe)
				break
			}
		}
	}
	return found, unresolvedPE
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

// resolveSubnet finds the subnet an ARM ID points at, as a standalone subnets
// resource or nested in a virtual network's properties.subnets.
func resolveSubnet(m sdk.ARMModel, id string) (subnetInfo, bool) {
	if id == "" || unresolved(id) {
		return subnetInfo{}, false
	}
	vnets := byType(m, tVNet)
	for _, s := range byType(m, tSubnet) {
		parts := strings.Split(s.Name, "/")
		if len(parts) < 2 {
			continue
		}
		vn, sn := parts[len(parts)-2], parts[len(parts)-1]
		if !refMatches(id, sn) || !strings.Contains(lc(id), lc(vn)) {
			continue
		}
		for _, v := range vnets {
			if lc(v.Name) == lc(vn) {
				return subnetInfo{name: sn, props: s.Properties, vnet: v, loc: s.Location}, true
			}
		}
	}
	for _, v := range vnets {
		for _, e := range arr(v.Properties, "subnets") {
			em := asMap(e)
			sn, _ := str(em, "name")
			if sn == "" || !refMatches(id, sn) || !strings.Contains(lc(id), lc(v.Name)) {
				continue
			}
			return subnetInfo{name: sn, props: asMap(em["properties"]), vnet: v, loc: v.Location}, true
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

func (o *outcome) skip(reason string) { o.skips = append(o.skips, reason) }

func (o *outcome) result() sdk.Result {
	if len(o.findings) == 0 && o.evaluated == 0 {
		reason := "no applicable resources in the compiled template"
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

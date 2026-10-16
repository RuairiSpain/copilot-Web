package net

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Register returns the Phase 1 NET rules.
func Register() []sdk.Rule {
	return []sdk.Rule{net001{}, net002{}, net003{}, net004{}, net005{}, net006{}, net007{}, net008{}, net009{}, net010{}, net011{}}
}

type net001 struct{}
type net002 struct{}
type net003 struct{}
type net004 struct{}
type net005 struct{}
type net006 struct{}
type net007 struct{}
type net008 struct{}
type net009 struct{}
type net010 struct{}
type net011 struct{}

func (net001) ID() string { return "FND-NET-001" }
func (net002) ID() string { return "FND-NET-002" }
func (net003) ID() string { return "FND-NET-003" }
func (net004) ID() string { return "FND-NET-004" }
func (net005) ID() string { return "FND-NET-005" }
func (net006) ID() string { return "FND-NET-006" }
func (net007) ID() string { return "FND-NET-007" }
func (net008) ID() string { return "FND-NET-008" }
func (net009) ID() string { return "FND-NET-009" }
func (net010) ID() string { return "FND-NET-010" }
func (net011) ID() string { return "FND-NET-011" }

// publicAccessPolicy is the ADR-007 key policy.network.publicAccess.
const publicAccessPolicy = "network.publicAccess"

// FND-NET-001: Foundry account public access / private endpoint posture.
//
// A private endpoint counts as proof only when it is deployed and its target
// provably names the account. A conditional or looped endpoint, or one whose
// target cannot be correlated, makes the account uncertain, not failed.
func (r net001) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	declared := policyString(in, publicAccessPolicy)
	for _, a := range foundryAccounts(in.ARM) {
		if !o.usable(a) {
			continue
		}
		pa, ok := publicAccess(a)
		if !ok {
			o.skip(a.Name + ": publicNetworkAccess is not resolvable from the template")
			continue
		}
		da, dok := defaultAction(a)
		switch pa {
		case "enabled":
			if !dok {
				o.skip(a.Name + ": networkAcls.defaultAction is not resolvable from the template")
				continue
			}
			o.evaluated++
			if da == "allow" {
				ev := "publicNetworkAccess is Enabled (or unset) and networkAcls.defaultAction is Allow (or unset)"
				if declared == "entra-only" || declared == "allowed" {
					ev += "; policy.network.publicAccess is " + declared + ", so public access is declared deliberate"
				}
				o.add(a, ev)
				// ADR-007: a declared deliberate posture lowers the severity
				// (entra-only: warning, allowed: info). The engine only ever
				// lowers, so the dev profile (info) is unchanged.
				switch declared {
				case "entra-only":
					o.findings[len(o.findings)-1].Severity = sdk.SeverityWarning
				case "allowed":
					o.findings[len(o.findings)-1].Severity = sdk.SeverityInfo
				}
			}
		case "disabled":
			pes, unc := pesFor(in.ARM, a, "account")
			if len(pes) > 0 {
				o.evaluated++
				continue
			}
			if unc {
				o.skip(a.Name + ": a private endpoint that is conditional, looped or has a target the template cannot correlate may cover this account")
				continue
			}
			o.evaluated++
			o.add(a, "publicNetworkAccess is Disabled but no deployed private endpoint with group ID 'account' targets the account")
		default:
			o.skip(a.Name + ": unrecognised publicNetworkAccess value")
		}
	}
	return o.result(), nil
}

type dep struct {
	key, typ, group, label string
}

var deps = []dep{
	{"blobStore", tStorage, "blob", "Storage"},
	{"documentStore", tCosmos, "Sql", "Cosmos DB"},
	{"vectorStore", tSearch, "searchService", "AI Search"},
}

// findByRef finds the deployed resource of a type that id provably names.
// uncertain is true when some resource of the type could not be ruled out.
func findByRef(m sdk.ARMModel, typ, id string) (res sdk.ARMResource, found, uncertain bool) {
	for _, r := range byType(m, typ) {
		switch corr(id, r) {
		case armexpr.Same:
			if !found {
				res, found = r, true
			}
		case armexpr.Unknown:
			uncertain = true
		}
	}
	return res, found, uncertain
}

// FND-NET-002: dependent store posture for the private standard-agent setup.
func (r net002) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
		if !o.usable(a) {
			continue
		}
		if !isPrivate(a) {
			o.skip(a.Name + ": account is not private; rule applies to the private standard-agent setup only")
			continue
		}
		if injectionsUncertain(a) {
			o.skip(uncertainInjectionReason(a))
			continue
		}
		if isManagedNetwork(a) {
			o.skip(a.Name + ": managed-network mode is out of scope")
			continue
		}
		for _, d := range deps {
			id := capabilityRef(a, d.key)
			if id == "" {
				continue
			}
			target, ok, unc := findByRef(in.ARM, d.typ, id)
			if !ok {
				if unc || unresolved(id) {
					o.skip(fmt.Sprintf("%s: capabilitySettings.%s cannot be correlated with a resource in the template", a.Name, d.key))
				} else {
					o.skip(fmt.Sprintf("%s: capabilitySettings.%s resource is not in the template (external or live inventory required)", a.Name, d.key))
				}
				continue
			}
			if !o.usable(target) {
				continue
			}
			pa, pok := publicAccess(target)
			if !pok {
				o.skip(target.Name + ": publicNetworkAccess is not resolvable")
				continue
			}
			pes, unres := pesFor(in.ARM, target, d.group)
			o.evaluated++
			if pa != "disabled" {
				o.addKey(target, "publicNetworkAccess", fmt.Sprintf("%s (%s) publicNetworkAccess is %q, expected Disabled", d.label, d.key, pa))
			}
			if len(pes) == 0 && !unres {
				o.addKey(target, "privateEndpoint", fmt.Sprintf("%s (%s) has no private endpoint with group ID %q in the template", d.label, d.key, d.group))
			}
		}
	}
	return o.result(), nil
}

// Cloud-specific private DNS zones (Private Link DNS zone values doc: Commercial,
// Government and China tables). Groups absent for a cloud have no catalogued zone
// and are skipped rather than guessed.
var expectedZones = map[string]map[string][]string{
	"public": {
		"account":       {"privatelink.cognitiveservices.azure.com", "privatelink.openai.azure.com", "privatelink.services.ai.azure.com"},
		"searchservice": {"privatelink.search.windows.net"},
		"sql":           {"privatelink.documents.azure.com"},
		"blob":          {"privatelink.blob.core.windows.net"},
		"vault":         {"privatelink.vaultcore.azure.net"},
		"registry":      {"privatelink.azurecr.io"},
	},
	"usgov": {
		"account":       {"privatelink.cognitiveservices.azure.us"},
		"searchservice": {"privatelink.search.azure.us"},
		"sql":           {"privatelink.documents.azure.us"},
		"blob":          {"privatelink.blob.core.usgovcloudapi.net"},
		"vault":         {"privatelink.vaultcore.usgovcloudapi.net"},
		"registry":      {"privatelink.azurecr.us"},
	},
	"china": {
		"sql":   {"privatelink.documents.azure.cn"},
		"blob":  {"privatelink.blob.core.chinacloudapi.cn"},
		"vault": {"privatelink.vaultcore.azure.cn"},
	},
}

// Policy keys read by FND-NET-003 (effective-policy keys, relative to policy:):
//   - managedByAzurePolicy (list, ADR-007): containing private-dns-zone-group
//     means Azure Policy creates the zone groups; the rule skips.
//   - network.cloud (string: public|usgov|china): selects the zone table. When
//     unset the cloud is inferred from literal resource regions (usgov*/usdod*
//     => usgov, china* => china, other regions => public); mixed or
//     unresolved regions make the cloud undeterminable and the rule skips.
//   - network.dnsManagedByPolicy (bool): legacy alias of the managedByAzurePolicy
//     entry.
//   - network.centralDns (bool): DNS is centralised (hub zones, private
//     resolver or custom DNS servers); the rule skips instead of failing.
func detectCloud(in *sdk.Input) string {
	if v, ok := policyValue(in, "network.cloud"); ok {
		if s, isS := v.(string); isS {
			switch c := lc(s); c {
			case "public", "usgov", "china":
				return c
			}
		}
		return ""
	}
	found := map[string]bool{}
	for _, r := range in.ARM.Resources() {
		if !literalRegion(r.Region) {
			continue
		}
		reg := normRegion(r.Region)
		switch {
		case strings.HasPrefix(reg, "usgov"), strings.HasPrefix(reg, "usdod"):
			found["usgov"] = true
		case strings.HasPrefix(reg, "china"):
			found["china"] = true
		default:
			found["public"] = true
		}
	}
	if len(found) != 1 {
		return ""
	}
	for c := range found {
		return c
	}
	return ""
}

// zoneNameOf returns the private DNS zone name a zone ID refers to, when the ID
// is a literal or a resourceId() call with literal arguments.
func zoneNameOf(id string) (string, bool) {
	ref := armexpr.ParseRef(id)
	if !ref.Literal() {
		return "", false
	}
	return ref.Segments[len(ref.Segments)-1].Text, true
}

// foundryScope decides whether a private endpoint targets the Foundry account
// or a store linked to it.
type foundryScope struct {
	accounts []sdk.ARMResource
	linked   foundrylink.Set
}

func newFoundryScope(m sdk.ARMModel) foundryScope {
	return foundryScope{accounts: foundryAccounts(m), linked: foundrylink.Collect(m)}
}

// classify reports whether target names a Foundry account or linked store.
func (s foundryScope) classify(target string) foundrylink.State {
	ref := armexpr.ParseRef(target)
	state := foundrylink.NotLinked
	for _, a := range s.accounts {
		switch armexpr.Compare(ref, refOf(a)) {
		case armexpr.Same:
			return foundrylink.Linked
		case armexpr.Unknown:
			state = foundrylink.Uncertain
		}
	}
	switch s.linked.ClassifyRef(ref) {
	case foundrylink.Linked:
		return foundrylink.Linked
	case foundrylink.Uncertain:
		state = foundrylink.Uncertain
	}
	return state
}

// FND-NET-003: private DNS zone groups and VNet links for private endpoints of
// the Foundry account and its linked stores.
func (r net003) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	if policyListHas(in, "managedByAzurePolicy", "private-dns-zone-group") || policyBool(in, "network.dnsManagedByPolicy") {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "policy-managed"}}, nil
	}
	if policyBool(in, "network.centralDns") {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "central-dns: DNS declared as centrally managed in policy"}}, nil
	}
	scope := newFoundryScope(in.ARM)
	// The cloud is needed only when a private endpoint is in scope. With none,
	// the rule does not apply (not-applicable); with an in-scope endpoint and
	// no determinable cloud, the missing policy key is the real cause.
	inScope, maybeInScope := false, false
	for _, pe := range byType(in.ARM, tPE) {
		for _, c := range peConns(pe) {
			switch scope.classify(c.target) {
			case foundrylink.Linked:
				inScope = true
			case foundrylink.Uncertain:
				maybeInScope = true
			}
		}
	}
	if !inScope && !maybeInScope {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: sdk.SkipNotApplicable + ": no private endpoint targets the Foundry account or a linked store"}}, nil
	}
	cloud := detectCloud(in)
	if cloud == "" {
		if !inScope {
			return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: foundrylink.SkipScopeUncertain + ": a private endpoint may target a Foundry resource but its target cannot be correlated"}}, nil
		}
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "profile-key-missing:network.cloud: private endpoints are in scope but the cloud cannot be determined from literal regions; set policy.network.cloud to public, usgov or china", MissingCapability: "policy network.cloud"}}, nil
	}
	zonesFor := expectedZones[cloud]
	o := &outcome{id: r.ID()}
	zoneGroups := byType(in.ARM, tZoneGroup)
	zones := byType(in.ARM, tZone)
	links := byType(in.ARM, tZoneLink)
	for _, pe := range byType(in.ARM, tPE) {
		// Scope: only endpoints that target the Foundry account or a linked
		// store are evaluated; others are not applicable.
		applies, uncertain := false, false
		for _, c := range peConns(pe) {
			switch scope.classify(c.target) {
			case foundrylink.Linked:
				applies = true
			case foundrylink.Uncertain:
				uncertain = true
			}
		}
		if !applies {
			if uncertain {
				o.skip(foundrylink.SkipScopeUncertain + ": " + pe.Name + " may target a Foundry resource but its target cannot be correlated")
			}
			continue
		}
		if !o.usable(pe) {
			continue
		}
		var groups []string
		for _, c := range peConns(pe) {
			groups = append(groups, c.groups...)
		}
		if len(groups) == 0 {
			o.skip(pe.Name + ": group IDs unknown")
			continue
		}
		var want []string
		for _, g := range groups {
			want = append(want, zonesFor[lc(g)]...)
		}
		if len(want) == 0 {
			o.skip(pe.Name + ": group ID has no catalogued DNS zone")
			continue
		}
		var zg *sdk.ARMResource
		zgUncertain := false
		for i := range zoneGroups {
			switch childOf(zoneGroups[i], pe) {
			case armexpr.Same:
				if presence(zoneGroups[i]) == armexpr.Deployed {
					if zg == nil {
						zg = &zoneGroups[i]
					}
				} else {
					zgUncertain = true
				}
			case armexpr.Unknown:
				zgUncertain = true
			}
		}
		if zg == nil {
			if zgUncertain {
				o.skip(pe.Name + ": a privateDnsZoneGroups child may belong to this endpoint but is conditional or cannot be correlated")
				continue
			}
			o.evaluated++
			o.add(pe, "private endpoint has no privateDnsZoneGroups child in the template")
			continue
		}
		o.evaluated++
		have := map[string]bool{}
		idsResolved := true
		for _, c := range arr(zg.Properties, "privateDnsZoneConfigs") {
			id, ok := str(asMap(c), "properties", "privateDnsZoneId")
			if !ok {
				continue
			}
			if name, ok := zoneNameOf(id); ok {
				have[name] = true
			} else {
				idsResolved = false
			}
		}
		for _, w := range want {
			if !have[w] {
				if !idsResolved {
					o.skip(pe.Name + ": a privateDnsZoneId is not resolvable, so zone " + w + " cannot be confirmed")
					continue
				}
				o.addKey(pe, "zone:"+w, "zone group lacks expected private DNS zone "+w)
				continue
			}
			var zone *sdk.ARMResource
			zoneUncertain := false
			for i := range zones {
				switch armexpr.Compare(armexpr.ParseName("", w), refOf(zones[i])) {
				case armexpr.Same:
					zone = &zones[i]
				case armexpr.Unknown:
					zoneUncertain = true
				}
			}
			if zone == nil || presence(*zone) != armexpr.Deployed {
				if zoneUncertain || zone != nil {
					o.skip(w + ": zone is conditional or cannot be correlated; virtual network link not verifiable")
				} else {
					o.skip(w + ": zone not in the template; virtual network link not verifiable")
				}
				continue
			}
			subnetID, _ := str(pe.Properties, "subnet", "id")
			sr, ok := parseSubnetRef(subnetID)
			if !ok {
				o.skip(pe.Name + ": endpoint subnet unresolved; link not verifiable")
				continue
			}
			linked, linkUncertain := false, false
			for _, l := range links {
				if childOf(l, *zone) != armexpr.Same {
					if childOf(l, *zone) == armexpr.Unknown {
						linkUncertain = true
					}
					continue
				}
				lv, _ := str(l.Properties, "virtualNetwork", "id")
				switch armexpr.Compare(armexpr.ParseRef(lv), sr.vnet) {
				case armexpr.Same:
					if presence(l) == armexpr.Deployed {
						linked = true
					} else {
						linkUncertain = true
					}
				case armexpr.Unknown:
					linkUncertain = true
				}
			}
			switch {
			case linked:
			case linkUncertain:
				o.skip(w + ": a virtual network link may connect the endpoint network but is conditional or cannot be correlated")
			default:
				o.addKey(*zone, "vnetLink:"+nameKey(refOf(pe).Segments), "private DNS zone has no virtualNetworkLinks entry for the endpoint virtual network of "+pe.Name)
			}
		}
	}
	return o.result(), nil
}

// sameSubnet reports whether two subnet references provably name one subnet.
func sameSubnet(m sdk.ARMModel, a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	sa, oka := resolveSubnet(m, a)
	sb, okb := resolveSubnet(m, b)
	if oka && okb {
		return armexpr.Compare(refOf(sa.vnet), refOf(sb.vnet)) == armexpr.Same && lc(sa.name) == lc(sb.name)
	}
	return armexpr.Normalize(a) == armexpr.Normalize(b)
}

// FND-NET-004: agent subnet delegation, size, name and sharing.
func (r net004) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	accounts := foundryAccounts(in.ARM)
	for ai, a := range accounts {
		if !o.usable(a) {
			continue
		}
		if injectionsUncertain(a) {
			o.skip(uncertainInjectionReason(a))
			continue
		}
		for _, inj := range injections(a) {
			if !inj.agent || inj.subnet == "" {
				continue
			}
			if inj.managed {
				o.skip(a.Name + ": managed network mode")
				continue
			}
			sn, ok := resolveSubnet(in.ARM, inj.subnet)
			if !ok {
				o.skip(a.Name + ": agent subnet is unresolved or not in the template")
				continue
			}
			o.evaluated++
			delegated := false
			for _, d := range arr(sn.props, "delegations") {
				if s, ok := str(asMap(d), "properties", "serviceName"); ok && strings.EqualFold(s, "Microsoft.App/environments") {
					delegated = true
				}
			}
			if !delegated {
				o.addKey(a, "delegation", "agent subnet is not delegated to Microsoft.App/environments")
			}
			var prefixes []string
			if s, ok := str(sn.props, "addressPrefix"); ok {
				prefixes = append(prefixes, s)
			}
			for _, p := range arr(sn.props, "addressPrefixes") {
				if s, ok := p.(string); ok {
					prefixes = append(prefixes, s)
				}
			}
			for _, p := range prefixes {
				if unresolved(p) {
					continue
				}
				if n, ok := prefixLen(p); ok && n > 27 {
					o.addKey(a, "prefix:"+p, fmt.Sprintf("agent subnet prefix %s is smaller than /27", p))
				}
			}
			if len(sn.name) > 63 && !isExpr(sn.name) {
				o.addKey(a, "subnetName", "agent subnet name exceeds 63 bytes")
			}
			for oi, other := range accounts {
				if oi == ai || presence(other) == armexpr.NotDeployed {
					continue
				}
				for _, oinj := range injections(other) {
					if oinj.agent && sameSubnet(in.ARM, oinj.subnet, inj.subnet) {
						o.addKey(a, "shared:"+nameKey(refOf(other).Segments), "agent subnet is shared with account "+other.Name)
					}
				}
			}
		}
	}
	return o.result(), nil
}

// FND-NET-005: region consistency. Regions are compared only when both sides
// are literals; an expression such as resourceGroup().location or a parameter
// is not comparable inside a flattened template, so the check is skipped.
func (r net005) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
		if !o.usable(a) {
			continue
		}
		if injectionsUncertain(a) {
			o.skip(uncertainInjectionReason(a))
			continue
		}
		for _, inj := range injections(a) {
			if !inj.agent || inj.managed || inj.subnet == "" {
				continue
			}
			sn, ok := resolveSubnet(in.ARM, inj.subnet)
			if !ok {
				o.skip(a.Name + ": agent subnet is unresolved or not in the template")
				continue
			}
			switch regionCompare(a.Region, sn.vnet.Region) {
			case armexpr.Same:
				o.evaluated++
			case armexpr.Different:
				o.evaluated++
				o.addKey(a, "agentSubnetRegion:"+armexpr.StripScope(sn.vnet.Name)+"/"+sn.name, fmt.Sprintf("account region %q differs from agent virtual network region %q", armexpr.StripScope(a.Region), armexpr.StripScope(sn.vnet.Region)))
			default:
				o.skip(a.Name + ": account or virtual network region is an expression or unset, so regions are not comparable")
			}
		}
	}
	for _, pe := range byType(in.ARM, tPE) {
		if !o.usable(pe) {
			continue
		}
		sid, _ := str(pe.Properties, "subnet", "id")
		sn, ok := resolveSubnet(in.ARM, sid)
		if !ok {
			o.skip(pe.Name + ": endpoint subnet is unresolved or not in the template")
			continue
		}
		switch regionCompare(pe.Region, sn.vnet.Region) {
		case armexpr.Same:
			o.evaluated++
		case armexpr.Different:
			o.evaluated++
			o.addKey(pe, "region", fmt.Sprintf("private endpoint region %q differs from its virtual network region %q", armexpr.StripScope(pe.Region), armexpr.StripScope(sn.vnet.Region)))
		default:
			o.skip(pe.Name + ": endpoint or virtual network region is an expression or unset, so regions are not comparable")
		}
	}
	return o.result(), nil
}

// FND-NET-007: restricted mode needs an allowed range.
func (r net007) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
		pa, ok := publicAccess(a)
		da, dok := defaultAction(a)
		if !ok || !dok || pa != "enabled" || da != "deny" {
			continue
		}
		if !o.usable(a) {
			continue
		}
		if pes, unc := pesFor(in.ARM, a, ""); len(pes) > 0 || unc {
			o.skip(a.Name + ": a private endpoint is present or may target this account")
			continue
		}
		rawIP, _ := get(a.Properties, "networkAcls", "ipRules")
		if s, isS := rawIP.(string); isS && unresolved(s) {
			o.skip(a.Name + ": ipRules come from a parameter")
			continue
		}
		o.evaluated++
		ips := arr(a.Properties, "networkAcls", "ipRules")
		vns := arr(a.Properties, "networkAcls", "virtualNetworkRules")
		if len(ips) == 0 && len(vns) == 0 {
			o.addKey(a, "ipRules", "restricted mode (defaultAction Deny) has no ipRules or virtualNetworkRules")
			continue
		}
		for _, e := range ips {
			v, _ := str(asMap(e), "value")
			if v == "" || unresolved(v) || v == "0.0.0.0/0" {
				continue
			}
			if !isIPv4OrCIDR(v) {
				o.addKey(a, "ipRule:"+v, fmt.Sprintf("ipRules value %q is not an IPv4 address or CIDR", v))
			}
		}
	}
	return o.result(), nil
}

// FND-NET-008: Free Search SKU cannot be private.
func (r net008) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	vector := map[string]bool{}
	for _, a := range foundryAccounts(in.ARM) {
		if !isPrivate(a) {
			continue
		}
		if id := capabilityRef(a, "vectorStore"); id != "" {
			for _, s := range byType(in.ARM, tSearch) {
				if corr(id, s) == armexpr.Same {
					vector[nameKey(refOf(s).Segments)] = true
				}
			}
		}
	}
	for _, s := range byType(in.ARM, tSearch) {
		if !o.usable(s) {
			continue
		}
		if unresolved(s.SKUName) {
			o.skip(s.Name + ": sku is a parameter")
			continue
		}
		if s.SKUName == "" {
			continue
		}
		o.evaluated++
		if lc(s.SKUName) != "free" {
			continue
		}
		pes, _ := pesFor(in.ARM, s, "searchService")
		pa, _ := publicAccess(s)
		anyPE, _ := pesFor(in.ARM, s, "")
		switch {
		case len(pes) > 0:
			o.add(s, "Free SKU search service has a private endpoint, which Free does not support")
		case pa == "disabled" && len(anyPE) > 0:
			o.add(s, "Free SKU search service has public access disabled with a private endpoint")
		case vector[nameKey(refOf(s).Segments)]:
			o.add(s, "Free SKU search service is the vectorStore of a private Foundry account")
		}
	}
	return o.result(), nil
}

// regionCompare compares two regions: literals by normalised name, expressions
// only when armexpr proves them equal within one deployment scope (Same).
func regionCompare(a, b string) armexpr.Result {
	if literalRegion(a) && literalRegion(b) {
		if normRegion(a) == normRegion(b) {
			return armexpr.Same
		}
		return armexpr.Different
	}
	return armexpr.CompareValues(a, b)
}

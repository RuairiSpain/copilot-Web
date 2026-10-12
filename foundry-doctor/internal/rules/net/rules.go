package net

import (
	"context"
	"fmt"
	"strings"

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

// FND-NET-001: Foundry account public access / private endpoint posture.
func (r net001) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
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
				o.add(a, "publicNetworkAccess is Enabled (or unset) and networkAcls.defaultAction is Allow (or unset)")
			}
		case "disabled":
			pes, unres := pesFor(in.ARM, a.Name, "account")
			if len(pes) > 0 {
				o.evaluated++
				continue
			}
			if unres {
				o.skip(a.Name + ": a private endpoint with a parameter-valued target may cover this account")
				continue
			}
			o.evaluated++
			o.add(a, "publicNetworkAccess is Disabled but no private endpoint with group ID 'account' targets the account")
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

func findByRef(m sdk.ARMModel, typ, id string) (sdk.ARMResource, bool) {
	for _, r := range byType(m, typ) {
		if refMatches(id, r.Name) {
			return r, true
		}
	}
	return sdk.ARMResource{}, false
}

// FND-NET-002: dependent store posture for the private standard-agent setup.
func (r net002) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
		if !isPrivate(a) {
			o.skip(a.Name + ": account is not private; rule applies to the private standard-agent setup only")
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
			if unresolved(id) {
				o.skip(fmt.Sprintf("%s: capabilitySettings.%s is unresolved", a.Name, d.key))
				continue
			}
			target, ok := findByRef(in.ARM, d.typ, id)
			if !ok {
				o.skip(fmt.Sprintf("%s: capabilitySettings.%s resource is not in the template (external or live inventory required)", a.Name, d.key))
				continue
			}
			pa, pok := publicAccess(target)
			if !pok {
				o.skip(target.Name + ": publicNetworkAccess is not resolvable")
				continue
			}
			pes, unres := pesFor(in.ARM, target.Name, d.group)
			o.evaluated++
			if pa != "disabled" {
				o.add(target, fmt.Sprintf("%s (%s) publicNetworkAccess is %q, expected Disabled", d.label, d.key, pa))
			}
			if len(pes) == 0 && !unres {
				o.add(target, fmt.Sprintf("%s (%s) has no private endpoint with group ID %q in the template", d.label, d.key, d.group))
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

// Policy keys read by FND-NET-003:
//   - policy.network.cloud (string: public|usgov|china): selects the zone table.
//     When unset the cloud is inferred from resource regions (usgov*/usdod* =>
//     usgov, china* => china, other regions => public); mixed or unresolved
//     regions make the cloud undeterminable and the rule skips.
//   - policy.network.dnsManagedByPolicy (bool): DINE policy manages zone groups.
//   - policy.network.centralDns (bool): DNS is centralised (hub zones, private
//     resolver or custom DNS servers); the rule skips instead of failing.
func policyBool(in *sdk.Input, key string) bool {
	if in.Policy == nil {
		return false
	}
	v, ok := in.Policy.Get(key)
	b, isB := v.(bool)
	return ok && isB && b
}

func detectCloud(in *sdk.Input) string {
	if in.Policy != nil {
		if v, ok := in.Policy.Get("policy.network.cloud"); ok {
			if s, isS := v.(string); isS {
				switch c := lc(s); c {
				case "public", "usgov", "china":
					return c
				}
			}
			return ""
		}
	}
	found := map[string]bool{}
	for _, r := range in.ARM.Resources() {
		if r.Region == "" || unresolved(r.Region) {
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
func zoneNameOf(id string) string {
	if name, ok := literalResourceIDName(id); ok {
		return lc(name)
	}
	i := strings.LastIndex(id, "/")
	if i >= 0 {
		return lc(strings.Trim(id[i+1:], "')]"))
	}
	return lc(id)
}

func literalResourceIDName(id string) (string, bool) {
	if !strings.HasPrefix(id, "[resourceId(") || !strings.HasSuffix(id, ")]") {
		return "", false
	}
	var parts []string
	for i := 0; i < len(id); {
		if id[i] != '\'' {
			i++
			continue
		}
		i++
		start := i
		for i < len(id) && id[i] != '\'' {
			i++
		}
		if i >= len(id) {
			return "", false
		}
		parts = append(parts, id[start:i])
		i++
	}
	if len(parts) < 2 {
		return "", false
	}
	return parts[len(parts)-1], true
}

// FND-NET-003: private DNS zone groups and VNet links.
func (r net003) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	if policyBool(in, "policy.network.dnsManagedByPolicy") {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "policy-managed"}}, nil
	}
	if policyBool(in, "policy.network.centralDns") {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "central-dns: DNS declared as centrally managed in policy"}}, nil
	}
	cloud := detectCloud(in)
	if cloud == "" {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: "cloud undeterminable: set policy.network.cloud to public, usgov or china"}}, nil
	}
	zonesFor := expectedZones[cloud]
	o := &outcome{id: r.ID()}
	zoneGroups := byType(in.ARM, tZoneGroup)
	zones := byType(in.ARM, tZone)
	links := byType(in.ARM, tZoneLink)
	for _, pe := range byType(in.ARM, tPE) {
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
		for i := range zoneGroups {
			if childOf(zoneGroups[i].Name, pe.Name) {
				zg = &zoneGroups[i]
				break
			}
		}
		o.evaluated++
		if zg == nil {
			o.add(pe, "private endpoint has no privateDnsZoneGroups child in the template")
			continue
		}
		have := map[string]string{}
		for _, c := range arr(zg.Properties, "privateDnsZoneConfigs") {
			if id, ok := str(asMap(c), "properties", "privateDnsZoneId"); ok {
				have[zoneNameOf(id)] = id
			}
		}
		for _, w := range want {
			id, ok := have[w]
			if !ok {
				o.add(pe, "zone group lacks expected private DNS zone "+w)
				continue
			}
			var zone *sdk.ARMResource
			for i := range zones {
				if lc(zones[i].Name) == w {
					zone = &zones[i]
				}
			}
			if zone == nil {
				_ = id
				o.skip(w + ": zone not in the template; virtual network link not verifiable")
				continue
			}
			vnetID, _ := str(pe.Properties, "subnet", "id")
			if vnetID == "" || unresolved(vnetID) {
				o.skip(pe.Name + ": endpoint subnet unresolved; link not verifiable")
				continue
			}
			linked := false
			for _, l := range links {
				if !childOf(l.Name, zone.Name) {
					continue
				}
				lv, _ := str(l.Properties, "virtualNetwork", "id")
				if unresolved(lv) {
					linked = true // cannot disprove
					continue
				}
				i := strings.Index(lc(vnetID), "/subnets/")
				base := lc(vnetID)
				if i >= 0 {
					base = base[:i]
				}
				if lv != "" && (strings.HasPrefix(base, lc(lv)) || strings.HasPrefix(lc(lv), base) || vnetNameMatch(lv, vnetID)) {
					linked = true
				}
			}
			if !linked {
				o.add(*zone, "private DNS zone has no virtualNetworkLinks entry for the endpoint virtual network of "+pe.Name)
			}
		}
	}
	return o.result(), nil
}

func vnetNameMatch(vnetID, subnetID string) bool {
	parts := strings.Split(vnetID, "/")
	if len(parts) == 0 {
		return false
	}
	n := strings.Trim(parts[len(parts)-1], "')]")
	n = strings.Trim(n, "'")
	return n != "" && strings.Contains(lc(subnetID), "'"+lc(n)+"'") ||
		strings.Contains(lc(subnetID), "/virtualnetworks/"+lc(n)+"/")
}

// FND-NET-004: agent subnet delegation, size, name and sharing.
func (r net004) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	accounts := foundryAccounts(in.ARM)
	for _, a := range accounts {
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
				o.add(a, "agent subnet is not delegated to Microsoft.App/environments")
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
					o.add(a, fmt.Sprintf("agent subnet prefix %s is smaller than /27", p))
				}
			}
			if len(sn.name) > 63 && !isExpr(sn.name) {
				o.add(a, "agent subnet name exceeds 63 bytes")
			}
			for _, other := range accounts {
				if lc(other.Name) == lc(a.Name) {
					continue
				}
				for _, oi := range injections(other) {
					if oi.agent && lc(oi.subnet) == lc(inj.subnet) {
						o.add(a, "agent subnet is shared with account "+other.Name)
					}
				}
			}
		}
	}
	return o.result(), nil
}

// FND-NET-005: region and subscription consistency.
func (r net005) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, a := range foundryAccounts(in.ARM) {
		for _, inj := range injections(a) {
			if !inj.agent || inj.managed || inj.subnet == "" {
				continue
			}
			sn, ok := resolveSubnet(in.ARM, inj.subnet)
			if !ok || sn.vnet.Region == "" || a.Region == "" || unresolved(sn.vnet.Region) || unresolved(a.Region) {
				o.skip(a.Name + ": account or virtual network region unresolved")
				continue
			}
			o.evaluated++
			if normRegion(a.Region) != normRegion(sn.vnet.Region) {
				o.add(a, fmt.Sprintf("account region %q differs from agent virtual network region %q", a.Region, sn.vnet.Region))
			}
		}
	}
	for _, pe := range byType(in.ARM, tPE) {
		sid, _ := str(pe.Properties, "subnet", "id")
		sn, ok := resolveSubnet(in.ARM, sid)
		if !ok || pe.Region == "" || sn.vnet.Region == "" || unresolved(pe.Region) || unresolved(sn.vnet.Region) {
			o.skip(pe.Name + ": endpoint or virtual network region unresolved")
			continue
		}
		o.evaluated++
		if normRegion(pe.Region) != normRegion(sn.vnet.Region) {
			o.add(pe, fmt.Sprintf("private endpoint region %q differs from its virtual network region %q", pe.Region, sn.vnet.Region))
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
		if pes, _ := pesFor(in.ARM, a.Name, ""); len(pes) > 0 {
			o.skip(a.Name + ": private endpoint present")
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
			o.add(a, "restricted mode (defaultAction Deny) has no ipRules or virtualNetworkRules")
			continue
		}
		for _, e := range ips {
			v, _ := str(asMap(e), "value")
			if v == "" || unresolved(v) || v == "0.0.0.0/0" {
				continue
			}
			if !isIPv4OrCIDR(v) {
				o.add(a, fmt.Sprintf("ipRules value %q is not an IPv4 address or CIDR", v))
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
				if refMatches(id, s.Name) {
					vector[lc(s.Name)] = true
				}
			}
		}
	}
	for _, s := range byType(in.ARM, tSearch) {
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
		pes, _ := pesFor(in.ARM, s.Name, "searchService")
		pa, _ := publicAccess(s)
		anyPE, _ := pesFor(in.ARM, s.Name, "")
		switch {
		case len(pes) > 0:
			o.add(s, "Free SKU search service has a private endpoint, which Free does not support")
		case pa == "disabled" && len(anyPE) > 0:
			o.add(s, "Free SKU search service has public access disabled with a private endpoint")
		case vector[lc(s.Name)]:
			o.add(s, "Free SKU search service is the vectorStore of a private Foundry account")
		}
	}
	return o.result(), nil
}

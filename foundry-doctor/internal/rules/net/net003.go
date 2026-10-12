package net

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net003 implements FND-NET-003: private endpoint DNS zone groups and VNet links.
type net003 struct{ meta }

func newNet003() rules.Rule { return net003{meta{"FND-NET-003", 1}} }

const net003Fix = `resource zoneGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-05-01' = {
  parent: pe
  name: 'default'
  properties: { privateDnsZoneConfigs: [ { name: 'cognitiveservices', properties: { privateDnsZoneId: zone.id } } ] }
}
resource link 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  parent: zone
  name: 'vnet-link'
  location: 'global'
  properties: { virtualNetwork: { id: vnetId }, registrationEnabled: false }
}`

// Public-cloud zone names per private endpoint group ID (Private Link DNS zone table, fact-check item 7).
// Storage groups other than blob map to privatelink.<service>.core.windows.net.
func expectedZones(group string) []string {
	switch strings.ToLower(group) {
	case "account":
		return []string{"privatelink.cognitiveservices.azure.com", "privatelink.openai.azure.com", "privatelink.services.ai.azure.com"}
	case "searchservice":
		return []string{"privatelink.search.windows.net"}
	case "sql":
		return []string{"privatelink.documents.azure.com"}
	case "blob", "table", "queue", "file", "web", "dfs":
		return []string{"privatelink." + strings.ToLower(group) + ".core.windows.net"}
	case "vault":
		return []string{"privatelink.vaultcore.azure.net"}
	case "registry":
		return []string{"privatelink.azurecr.io"}
	}
	return nil
}

// Zone names with these suffixes belong to a sovereign cloud, whose table is not encoded: skip, not fail.
var sovereignSuffixes = []string{".azure.us", ".usgovcloudapi.net", ".azure.cn", ".chinacloudapi.cn"}

const maxZonesPerGroup = 5 // Private Link documentation limit

var zoneNameExpr = regexp.MustCompile(`(?i)privateDnsZones'\s*,\s*'((?:[^']|'')*)'`)

// zoneName reads the Private DNS zone name from a privateDnsZoneId value.
func zoneName(raw any) (string, bool) {
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	if model.IsARMExpression(s) {
		if m := zoneNameExpr.FindStringSubmatch(s); m != nil {
			return strings.ToLower(strings.ReplaceAll(m[1], "''", "'")), true
		}
		return "", false
	}
	const marker = "/privatednszones/"
	i := strings.LastIndex(strings.ToLower(s), marker)
	if i < 0 {
		return "", false
	}
	name := s[i+len(marker):]
	if j := strings.Index(name, "/"); j >= 0 {
		name = name[:j]
	}
	return strings.ToLower(name), name != ""
}

// Evaluate checks every private endpoint for a zone group with the documented zones, and every
// referenced zone declared in the template for a link to the endpoint's VNet.
func (r net003) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	if slices.Contains(in.Policy.ManagedByAzurePolicy, model.ManagedPrivateDNSZoneGroup) {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"policy-managed: policy.managedByAzurePolicy lists private-dns-zone-group, so zone groups are created after deployment by Azure Policy",
			"", sdk.ResourceRef{})}, nil
	}
	var out []rules.Result
	for _, p := range t.ofType(typePE) {
		out = append(out, r.endpoint(t, p)...)
	}
	return out, nil
}

func (r net003) endpoint(t *topo, p int) []rules.Result {
	pe := t.res[p]
	info := t.pes[p]
	var groups []string
	for _, c := range info.conns {
		if !c.groupsKnown {
			return []rules.Result{skipped(r, pe, rules.ReasonUnresolvedValue("group-ids"), "the group IDs of the endpoint are not literal", "resolved template value")}
		}
		groups = append(groups, c.groups...)
	}
	if len(groups) == 0 {
		return []rules.Result{skipped(r, pe, rules.ReasonUnresolvedValue("group-ids"), "the endpoint declares no readable group ID", "resolved template value")}
	}
	var zgs []int
	for _, z := range t.ofType(typeZoneGroup) {
		switch t.parentOf(z, typePE) {
		case p:
			zgs = append(zgs, z)
		case -1:
			return []rules.Result{skipped(r, pe, rules.ReasonUnresolvedValue("zone-group-parent"),
				"a zone group of the template cannot be attributed to a private endpoint", "resolved template value")}
		}
	}
	gid := strings.Join(groups, ",")
	if len(zgs) == 0 {
		ev := fmt.Sprintf("Private endpoint %s (group ID %s) has no privateDnsZoneGroup, so its name will not resolve to the private IP.", pe.Name, gid)
		return []rules.Result{failure(r, pe, "", "no-zone-group", text{ev,
			"Add a privateDnsZoneGroup with the documented privatelink zones for the group ID.", net003Fix}, sdk.ConfidenceLikely)}
	}
	if len(zgs) > 1 {
		ev := fmt.Sprintf("Private endpoint %s has %d zone groups; only one zone group per endpoint is allowed.", pe.Name, len(zgs))
		return []rules.Result{failure(r, t.res[zgs[1]], "", "zone-group-limit", text{ev,
			"Keep one privateDnsZoneGroup per endpoint.", net003Fix}, sdk.ConfidenceLikely, pe)}
	}
	return r.zoneGroup(t, p, zgs[0], groups)
}

func (r net003) zoneGroup(t *topo, p, z int, groups []string) []rules.Result {
	pe, zg := t.res[p], t.res[z]
	cfgs, ok := rawAt(zg.Body, "properties", "privateDnsZoneConfigs").([]any)
	if !ok {
		return []rules.Result{skipped(r, zg, rules.ReasonUnresolvedValue("privateDnsZoneConfigs"), "the zone configs are an expression or absent", "resolved template value")}
	}
	var names []string
	unknown := false
	for _, c := range cfgs {
		n, known := zoneName(rawAt(c, "properties", "privateDnsZoneId"))
		if !known {
			unknown = true
			continue
		}
		names = append(names, n)
	}
	for _, n := range names {
		for _, s := range sovereignSuffixes {
			if strings.HasSuffix(n, s) {
				return []rules.Result{skipped(r, zg, rules.ReasonNotApplicable,
					"zone names of a sovereign cloud are not checked: the expected set is chosen per cloud", "")}
			}
		}
	}
	var out []rules.Result
	dup := len(slices.Compact(slices.Sorted(slices.Values(names)))) != len(names)
	if len(cfgs) > maxZonesPerGroup || dup {
		ev := fmt.Sprintf("Zone group %s has %d zone configs (limit %d, one zone per name).", zg.Name, len(cfgs), maxZonesPerGroup)
		out = append(out, failure(r, zg, "/properties/privateDnsZoneConfigs", "zone-group-limit", text{ev,
			"Use at most five distinct zones per zone group.", net003Fix}, sdk.ConfidenceLikely, pe))
	}
	var missing []string
	for _, g := range groups {
		for _, want := range expectedZones(g) {
			if !slices.Contains(names, want) && !slices.Contains(missing, want) {
				missing = append(missing, want)
			}
		}
	}
	switch {
	case len(missing) > 0 && unknown:
		out = append(out, skipped(r, zg, rules.ReasonUnresolvedValue("zone-id"), "a zone ID is a parameter or expression, so the expected zone set cannot be compared", "resolved template value"))
	case len(missing) > 0:
		ev := fmt.Sprintf("Zone group %s of endpoint %s lacks the documented zone(s): %s.", zg.Name, pe.Name, strings.Join(missing, ", "))
		out = append(out, failure(r, zg, "/properties/privateDnsZoneConfigs", "zone-group-missing-zone", text{ev,
			"Add the missing privatelink zones to the zone group.", net003Fix}, sdk.ConfidenceLikely, pe))
	}
	out = append(out, r.links(t, p, names)...)
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}

// links checks that each referenced zone declared in the template is linked to the VNet of the endpoint.
func (r net003) links(t *topo, p int, names []string) []rules.Result {
	pe := t.res[p]
	var out []rules.Result
	vnet := -1
	if s := t.pes[p].subnets; len(s) == 1 {
		vnet = t.parentOf(s[0], typeVNet)
	}
	var elsewhere, noVNet []string
	for _, n := range names {
		z := -1
		for _, i := range t.ofType(typeZone) {
			if strings.EqualFold(nameText(t.res[i].Name), n) {
				z = i
			}
		}
		switch {
		case z < 0:
			elsewhere = append(elsewhere, n)
			continue
		case vnet < 0:
			noVNet = append(noVNet, n)
			continue
		}
		linked, unknown := false, false
		for _, l := range t.ofType(typeZoneLink) {
			if t.parentOf(l, typeZone) != z {
				continue
			}
			v, st := t.resolveID(l, rawAt(t.res[l].Body, "properties", "virtualNetwork", "id"), typeVNet)
			switch {
			case st == resolved && v == vnet:
				linked = true
			case st != resolved:
				unknown = true
			}
		}
		switch {
		case linked:
		case unknown:
			out = append(out, skipped(r, t.res[z], rules.ReasonUnresolvedValue("link-vnet"), "a link of zone "+n+" targets a VNet that cannot be resolved", "resolved template value"))
		default:
			ev := fmt.Sprintf("Private DNS zone %s has no virtualNetworkLink to the VNet %s that hosts private endpoint %s.", n, t.res[vnet].Name, pe.Name)
			out = append(out, failure(r, t.res[z], "", "zone-without-vnet-link", text{ev,
				"Link the zone to the VNet that resolves the names.", net003Fix}, sdk.ConfidenceLikely, pe))
		}
	}
	if len(elsewhere) > 0 {
		out = append(out, skipped(r, pe, rules.ReasonControlPlaneUnavailable,
			"zone(s) "+strings.Join(elsewhere, ", ")+" are not declared in the template (hub or other subscription); their VNet links need live ARM access", "live Azure access"))
	}
	if len(noVNet) > 0 {
		out = append(out, skipped(r, pe, rules.ReasonUnresolvedValue("endpoint-vnet"),
			"the VNet of the endpoint cannot be identified, so the links of zone(s) "+strings.Join(noVNet, ", ")+" cannot be compared", "resolved VNet"))
	}
	return out
}

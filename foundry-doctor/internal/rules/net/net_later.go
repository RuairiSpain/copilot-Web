package net

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	tVNetPeering       = "microsoft.network/virtualnetworks/virtualnetworkpeerings"
	tAPIMService       = "microsoft.apimanagement/service"
	tAMPLS             = "microsoft.insights/privatelinkscopes"
	tAMPLSScoped       = "microsoft.insights/privatelinkscopes/scopedresources"
	tAppInsights       = "microsoft.insights/components"
	tWorkspace         = "microsoft.operationalinsights/workspaces"
	tPublicIP          = "microsoft.network/publicipaddresses"
	uncertainPrefixNET = "uncertain: "
)

var (
	rfc1918  = mustPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
	reserved = mustPrefixes(
		"169.254.0.0/16", "172.30.0.0/16", "172.31.0.0/16", "192.0.2.0/24",
		"0.0.0.0/8", "127.0.0.0/8", "100.100.0.0/17", "100.100.192.0/19", "100.100.224.0/19",
	)
	cgnat = mustPrefix("100.64.0.0/10")
)

func (r net006) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	spaces := vnetAddressSpaces(in.ARM)
	peers := peeringPairs(in.ARM)
	for _, a := range foundryAccounts(in.ARM) {
		for _, inj := range injections(a) {
			if inj.managed || !inj.agent || inj.subnet == "" {
				continue
			}
			sn, ok := resolveSubnet(in.ARM, inj.subnet)
			if !ok {
				o.skip(a.Name + ": agent subnet is unresolved or not in the template")
				continue
			}
			prefixes := subnetPrefixes(sn.props)
			if len(prefixes) == 0 {
				o.skip(a.Name + ": agent subnet prefix is unresolved")
				continue
			}
			for _, cidr := range prefixes {
				p, err := netip.ParsePrefix(cidr)
				if err != nil {
					o.skip(a.Name + ": agent subnet prefix is not a literal CIDR")
					continue
				}
				o.evaluated++
				switch {
				case cgnat.Contains(p.Addr()) && !intersectsAny(p, reserved):
					o.skip(a.Name + ": " + cidr + " is in 100.64.0.0/10 and the documentation conflicts")
				case !withinAny(p, rfc1918):
					o.add(a, "agent subnet prefix "+cidr+" is not in RFC 1918 private address space")
				case intersectsAny(p, reserved):
					o.add(a, "agent subnet prefix "+cidr+" intersects a reserved range")
				}
			}
			for _, p := range spaces[strings.ToLower(sn.vnet.Name)] {
				if intersectsAny(p, reserved) {
					o.evaluated++
					o.add(sn.vnet, "virtual network address space "+p.String()+" intersects a reserved range")
				}
			}
		}
	}
	for _, pair := range peers {
		left := spaces[pair[0]]
		right := spaces[pair[1]]
		for _, a := range left {
			for _, b := range right {
				if prefixesOverlap(a, b) {
					o.evaluated++
					o.add(sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks", Name: pair[0]}, fmt.Sprintf("peered virtual networks %s and %s have overlapping address spaces", pair[0], pair[1]))
				}
			}
		}
	}
	return o.result(), nil
}

func (r net009) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	for _, svc := range byType(in.ARM, tAPIMService) {
		sku := lc(svc.SKUName)
		if sku == "" || unresolved(svc.SKUName) {
			o.skip(svc.Name + ": APIM sku is unresolved")
			continue
		}
		vt, _ := str(svc.Properties, "virtualNetworkType")
		subnetID, _ := str(svc.Properties, "virtualNetworkConfiguration", "subnetResourceId")
		if strings.EqualFold(vt, "External") || strings.EqualFold(vt, "Internal") {
			o.evaluated++
			if sku != "developer" && sku != "premium" {
				o.add(svc, "classic APIM virtual network injection requires Developer or Premium")
			}
			continue
		}
		if subnetID != "" {
			if sn, ok := resolveSubnet(in.ARM, subnetID); ok {
				deleg := subnetDelegation(sn.props)
				o.evaluated++
				switch deleg {
				case "microsoft.web/serverfarms":
					if sku != "standardv2" && sku != "premiumv2" {
						o.add(svc, "outbound VNet integration requires StandardV2 or PremiumV2")
					}
				case "microsoft.web/hostingenvironments":
					if sku != "premiumv2" {
						o.add(svc, "PremiumV2 is required for the delegated injection subnet")
					}
				}
			}
		}
		pes, _ := pesFor(in.ARM, svc.Name, "gateway")
		if len(pes) > 0 {
			o.evaluated++
			switch sku {
			case "developer", "basic", "standard", "standardv2", "premium", "premiumv2":
			case "basicv2":
				o.skip(svc.Name + ": inbound private endpoint support for BasicV2 is not verified")
			default:
				o.add(svc, "APIM SKU does not support the configured inbound private endpoint model")
			}
		}
	}
	return o.result(), nil
}

func (r net010) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	if !hasPrivateFoundry(in.ARM) {
		return o.result(), nil
	}
	publicTelemetry := false
	if in != nil && in.Policy != nil {
		if v, ok := in.Policy.Get("monitoring.publicTelemetry"); ok {
			if b, ok := v.(bool); ok {
				publicTelemetry = b
			}
		}
	}
	components := byType(in.ARM, tAppInsights)
	if len(components) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.ID(), Reason: sdk.SkipInputUnavailable + ": no Application Insights component is in the scanned template"}}, nil
	}
	scoped := amplsScopedResources(in.ARM)
	amplsPEs := amplsPrivateEndpoints(in.ARM)
	for _, c := range components {
		o.evaluated++
		if publicTelemetry {
			if v, ok := str(c.Properties, "publicNetworkAccessForIngestion"); ok && strings.TrimSpace(v) != "" {
				continue
			}
			o.add(c, "public telemetry is declared but Application Insights does not set publicNetworkAccessForIngestion explicitly")
			continue
		}
		links := scoped[strings.ToLower(c.Name)]
		if len(links) == 0 {
			o.add(c, "private Foundry design has Application Insights without an Azure Monitor Private Link Scope or a public telemetry declaration")
			continue
		}
		for _, scope := range links {
			if !amplsPEs[strings.ToLower(scope)] {
				o.add(c, "Azure Monitor Private Link Scope "+scope+" has no private endpoint with groupId azuremonitor")
			}
			if !amplsModesExplicit(in.ARM, scope) {
				o.add(c, "Azure Monitor Private Link Scope "+scope+" does not set explicit ingestionAccessMode and queryAccessMode")
			}
		}
	}
	return o.result(), nil
}

func (r net011) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	vnetProtected := map[string]bool{}
	for _, v := range byType(in.ARM, tVNet) {
		enabled, _ := boolean(v.Properties, "enableDdosProtection")
		plan, _ := str(v.Properties, "ddosProtectionPlan", "id")
		vnetProtected[lc(v.Name)] = enabled && plan != ""
	}
	agentSubnets := map[string]bool{}
	for _, a := range foundryAccounts(in.ARM) {
		for _, inj := range injections(a) {
			if inj.agent {
				agentSubnets[lc(inj.subnet)] = true
				agentSubnets[lc(lastName(inj.subnet))] = true
			}
		}
	}
	for _, v := range byType(in.ARM, tVNet) {
		for _, raw := range arr(v.Properties, "subnets") {
			sm := asMap(raw)
			name, _ := str(sm, "name")
			props := asMap(sm["properties"])
			if exemptSubnet(name, props) {
				continue
			}
			nsg, _ := str(props, "networkSecurityGroup", "id")
			if nsg == "" {
				o.evaluated++
				if agentSubnets[lc(name)] {
					o.skip(name + ": uncertain: NSG support for the agent subnet is not verified")
				} else {
					o.add(sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks/subnets", Name: v.Name + "/" + name}, "subnet has no networkSecurityGroup association")
				}
			}
		}
	}
	for _, pip := range byType(in.ARM, tPublicIP) {
		o.evaluated++
		mode, _ := str(pip.Properties, "ddosSettings", "protectionMode")
		if strings.EqualFold(mode, "Enabled") {
			continue
		}
		protected := false
		for name, ok := range vnetProtected {
			if ok && strings.Contains(lc(pip.Name), name) {
				protected = true
				break
			}
		}
		if !protected {
			o.add(pip, "public IP address is present without explicit DDoS protection")
		}
	}
	return o.result(), nil
}

func mustPrefix(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic(err)
	}
	return p
}

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		out = append(out, mustPrefix(v))
	}
	return out
}

func subnetPrefixes(props map[string]any) []string {
	var out []string
	if s, ok := str(props, "addressPrefix"); ok && s != "" {
		out = append(out, s)
	}
	for _, p := range arr(props, "addressPrefixes") {
		if s, ok := p.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func vnetAddressSpaces(m sdk.ARMModel) map[string][]netip.Prefix {
	out := map[string][]netip.Prefix{}
	for _, v := range byType(m, tVNet) {
		for _, raw := range arr(v.Properties, "addressSpace", "addressPrefixes") {
			s, ok := raw.(string)
			if !ok {
				continue
			}
			if p, err := netip.ParsePrefix(s); err == nil {
				out[lc(v.Name)] = append(out[lc(v.Name)], p)
			}
		}
	}
	return out
}

func peeringPairs(m sdk.ARMModel) [][2]string {
	var out [][2]string
	for _, p := range byType(m, tVNetPeering) {
		parent := strings.ToLower(strings.TrimSpace(strings.Split(p.Name, "/")[0]))
		remote, _ := str(p.Properties, "remoteVirtualNetwork", "id")
		if remote == "" {
			continue
		}
		out = append(out, [2]string{parent, lc(lastName(remote))})
	}
	return out
}

func lastName(id string) string {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func withinAny(p netip.Prefix, allowed []netip.Prefix) bool {
	for _, a := range allowed {
		if a.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

func intersectsAny(p netip.Prefix, others []netip.Prefix) bool {
	for _, o := range others {
		if prefixesOverlap(p, o) {
			return true
		}
	}
	return false
}

func prefixesOverlap(a, b netip.Prefix) bool {
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func subnetDelegation(props map[string]any) string {
	for _, d := range arr(props, "delegations") {
		if s, ok := str(asMap(d), "properties", "serviceName"); ok {
			return lc(s)
		}
	}
	return ""
}

func hasPrivateFoundry(m sdk.ARMModel) bool {
	for _, a := range foundryAccounts(m) {
		if isPrivate(a) {
			return true
		}
	}
	return false
}

func amplsScopedResources(m sdk.ARMModel) map[string][]string {
	out := map[string][]string{}
	for _, r := range byType(m, tAMPLSScoped) {
		scope := strings.Split(r.Name, "/")[0]
		id, _ := str(r.Properties, "linkedResourceId")
		if id != "" {
			out[lc(lastName(id))] = append(out[lc(lastName(id))], scope)
		}
	}
	return out
}

func amplsPrivateEndpoints(m sdk.ARMModel) map[string]bool {
	out := map[string]bool{}
	for _, pe := range byType(m, tPE) {
		for _, c := range peConns(pe) {
			if hasGroup(c.groups, "azuremonitor") {
				out[lc(lastName(c.target))] = true
			}
		}
	}
	return out
}

func amplsModesExplicit(m sdk.ARMModel, scope string) bool {
	for _, r := range byType(m, tAMPLS) {
		if lc(r.Name) != lc(scope) {
			continue
		}
		_, inok := str(r.Properties, "accessModeSettings", "ingestionAccessMode")
		_, qok := str(r.Properties, "accessModeSettings", "queryAccessMode")
		return inok && qok
	}
	return false
}

func exemptSubnet(name string, props map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "gatewaysubnet", "azurefirewallsubnet", "azurefirewallmanagementsubnet", "routeserversubnet":
		return true
	}
	return strings.EqualFold(subnetDelegation(props), "Microsoft.HardwareSecurityModules/dedicatedHSMs")
}

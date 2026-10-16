package net

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
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
	// cgnat is RFC 6598 shared address space. The Foundry virtual network
	// limitations page lists it as supported for the agent subnet except for the
	// three 100.100.* ranges in reserved.
	cgnat = mustPrefix("100.64.0.0/10")
	// supported is every range the agent subnet may use before the reserved
	// ranges are removed.
	supported = append(append([]netip.Prefix(nil), rfc1918...), cgnat)
)

func (r net006) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	if res, skip := noARM(r.ID(), in); skip {
		return res, nil
	}
	o := &outcome{id: r.ID()}
	spaces := vnetAddressSpaces(in.ARM)
	peers := peeringPairs(in.ARM)
	for _, a := range foundryAccounts(in.ARM) {
		if !o.usable(a) {
			continue
		}
		if injectionsUncertain(a) {
			o.skip(uncertainInjectionReason(a))
			continue
		}
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
				case !withinAny(p, supported):
					o.addKey(a, "prefix:"+cidr, "agent subnet prefix "+cidr+" is not in RFC 1918 or RFC 6598 (100.64.0.0/10) address space")
				case intersectsAny(p, reserved):
					o.addKey(a, "prefix:"+cidr, "agent subnet prefix "+cidr+" intersects a reserved range")
				}
			}
			for _, p := range spaces[nameKey(refOf(sn.vnet).Segments)] {
				if intersectsAny(p, reserved) {
					o.evaluated++
					o.addKey(sn.vnet, "addressSpace:"+p.String(), "virtual network address space "+p.String()+" intersects a reserved range")
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
					o.addKey(sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks", Name: pair[0]}, "peer:"+pair[1]+":"+a.String()+":"+b.String(), fmt.Sprintf("peered virtual networks %s and %s have overlapping address spaces", pair[0], pair[1]))
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
		if !o.usable(svc) {
			continue
		}
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
				o.addKey(svc, "classicVnetSku", "classic APIM virtual network injection requires Developer or Premium")
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
						o.addKey(svc, "outboundSku", "outbound VNet integration requires StandardV2 or PremiumV2")
					}
				case "microsoft.web/hostingenvironments":
					if sku != "premiumv2" {
						o.addKey(svc, "injectionSubnetSku", "PremiumV2 is required for the delegated injection subnet")
					}
				}
			}
		}
		pes, _ := pesFor(in.ARM, svc, "gateway")
		if len(pes) > 0 {
			o.evaluated++
			switch sku {
			case "developer", "basic", "standard", "standardv2", "premium", "premiumv2":
			case "basicv2":
				o.skip(svc.Name + ": inbound private endpoint support for BasicV2 is not verified")
			default:
				o.addKey(svc, "privateEndpointSku", "APIM SKU does not support the configured inbound private endpoint model")
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
	scoped, scopedUncertain := amplsScopedResources(in.ARM)
	amplsPEs := amplsPrivateEndpoints(in.ARM)
	for _, c := range components {
		if !o.usable(c) {
			continue
		}
		if publicTelemetry {
			o.evaluated++
			if v, ok := str(c.Properties, "publicNetworkAccessForIngestion"); ok && strings.TrimSpace(v) != "" {
				continue
			}
			o.addKey(c, "publicTelemetry", "public telemetry is declared but Application Insights does not set publicNetworkAccessForIngestion explicitly")
			continue
		}
		links := scoped[nameKey(refOf(c).Segments)]
		if len(links) == 0 {
			if scopedUncertain {
				o.skip(c.Name + ": a scopedResources entry with a link target the template cannot correlate may cover this component")
				continue
			}
			o.evaluated++
			o.addKey(c, "noAmpls", "private Foundry design has Application Insights without an Azure Monitor Private Link Scope or a public telemetry declaration")
			continue
		}
		o.evaluated++
		for _, scope := range links {
			if !amplsPEs[scope] {
				o.addKey(c, "ampls:"+scope+":privateEndpoint", "Azure Monitor Private Link Scope "+scope+" has no private endpoint with groupId azuremonitor")
			}
			if !amplsModesExplicit(in.ARM, scope) {
				o.addKey(c, "ampls:"+scope+":accessModes", "Azure Monitor Private Link Scope "+scope+" does not set explicit ingestionAccessMode and queryAccessMode")
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
		vnetProtected[nameKey(refOf(v).Segments)] = enabled && plan != ""
	}
	// Agent subnets are resolved to (virtual network, subnet) first so that a
	// reference such as reference(vnet).subnets[0].id exempts the right subnet.
	agentSubnets := map[string]bool{}
	agentUnresolved := false
	for _, a := range foundryAccounts(in.ARM) {
		if injectionsUncertain(a) {
			agentUnresolved = true
			o.skip(uncertainInjectionReason(a))
		}
		for _, inj := range injections(a) {
			if !inj.agent || inj.managed {
				continue
			}
			sn, ok := resolveSubnet(in.ARM, inj.subnet)
			if !ok {
				agentUnresolved = true
				continue
			}
			agentSubnets[nameKey(refOf(sn.vnet).Segments)+"/"+lc(sn.name)] = true
		}
	}
	checkSubnet := func(vkey, vnetName, name string, props map[string]any) {
		if exemptSubnet(name, props) {
			return
		}
		nsg, _ := str(props, "networkSecurityGroup", "id")
		if nsg != "" {
			o.evaluated++
			return
		}
		switch {
		case agentSubnets[vkey+"/"+lc(name)]:
			o.skip(name + ": uncertain: NSG support for the agent subnet is not verified")
		case agentUnresolved:
			o.skip(name + ": uncertain: an agent subnet injection could not be resolved, so this subnet may be the agent subnet")
		default:
			o.evaluated++
			o.add(sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks/subnets", Name: vnetName + "/" + name}, "subnet has no networkSecurityGroup association")
		}
	}
	for _, v := range byType(in.ARM, tVNet) {
		if !o.usable(v) {
			continue
		}
		vkey := nameKey(refOf(v).Segments)
		for _, raw := range arr(v.Properties, "subnets") {
			sm := asMap(raw)
			name, _ := str(sm, "name")
			checkSubnet(vkey, v.Name, name, asMap(sm["properties"]))
		}
	}
	// Subnets declared as Microsoft.Network/virtualNetworks/subnets resources.
	for _, sres := range byType(in.ARM, tSubnet) {
		if !o.usable(sres) {
			continue
		}
		segs := refOf(sres).Segments
		if len(segs) < 2 {
			o.skip(sres.Name + ": uncertain: subnet resource name has no virtual network parent segment")
			continue
		}
		vkey := nameKey(segs[:len(segs)-1])
		last := segs[len(segs)-1].Text
		vnetName := strings.TrimSuffix(sres.Name, "/"+last)
		checkSubnet(vkey, vnetName, last, sres.Properties)
	}
	for _, pip := range byType(in.ARM, tPublicIP) {
		if !o.usable(pip) {
			continue
		}
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
				k := nameKey(refOf(v).Segments)
				out[k] = append(out[k], p)
			}
		}
	}
	return out
}

func peeringPairs(m sdk.ARMModel) [][2]string {
	var out [][2]string
	for _, p := range byType(m, tVNetPeering) {
		segs := refOf(p).Segments
		if len(segs) < 2 {
			continue
		}
		remote, _ := str(p.Properties, "remoteVirtualNetwork", "id")
		rr := armexpr.ParseRef(remote)
		if rr.Opaque || len(rr.Segments) == 0 || (rr.Type != "" && rr.Type != tVNet) {
			continue
		}
		out = append(out, [2]string{nameKey(segs[:len(segs)-1]), nameKey(rr.Segments)})
	}
	return out
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

// amplsScopedResources maps a linked resource key to the Private Link Scope
// keys that scope it. uncertain is true when a scoped resource's link target
// cannot be reduced to a name.
func amplsScopedResources(m sdk.ARMModel) (out map[string][]string, uncertain bool) {
	out = map[string][]string{}
	for _, r := range byType(m, tAMPLSScoped) {
		segs := refOf(r).Segments
		if len(segs) < 2 {
			uncertain = true
			continue
		}
		scope := nameKey(segs[:len(segs)-1])
		id, _ := str(r.Properties, "linkedResourceId")
		ref := armexpr.ParseRef(id)
		if ref.Opaque || len(ref.Segments) == 0 {
			uncertain = true
			continue
		}
		k := nameKey(ref.Segments)
		out[k] = append(out[k], scope)
	}
	return out, uncertain
}

func amplsPrivateEndpoints(m sdk.ARMModel) map[string]bool {
	out := map[string]bool{}
	for _, pe := range byType(m, tPE) {
		for _, c := range peConns(pe) {
			if hasGroup(c.groups, "azuremonitor") {
				if ref := armexpr.ParseRef(c.target); !ref.Opaque && len(ref.Segments) > 0 {
					out[nameKey(ref.Segments)] = true
				}
			}
		}
	}
	return out
}

func amplsModesExplicit(m sdk.ARMModel, scope string) bool {
	for _, r := range byType(m, tAMPLS) {
		if nameKey(refOf(r).Segments) != scope {
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

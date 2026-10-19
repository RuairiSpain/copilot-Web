package rel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FND-REL-007 is an answerable question. Without a declared disaster-recovery
// objective the rule skips with the head no-resilience-objective-declared (the
// assessment shows it as QUESTION) and carries a summary of the regions found in
// the template. With a multi-region objective declared it reports every
// stateful component that is provably single-region.
//
// Policy keys (not yet in the configuration schema; read through the policy
// interface like the reliability.apim* keys of FND-REL-009):
//
//	reliability.disasterRecovery          or reliability.disasterRecovery.strategy
//	  required | multi-region | hot-hot | hot-warm | hot-cold  -> multi-region objective
//	  single-region                                            -> declared decision, nothing to check
//
// Component evidence uses properties already read by other rel rules: Storage
// sku.name, Cosmos properties.locations[].locationName, APIM
// properties.additionalLocations, and the resource location of Foundry accounts
// and Search services. Search has no replication feature in the sources read, so
// a second Search service in another region is the only multi-region shape.
const (
	drKey         = "reliability.disasterRecovery"
	drStrategyKey = "reliability.disasterRecovery.strategy"
)

type drObjective int

const (
	drNone drObjective = iota
	drSingleRegion
	drMultiRegion
)

func readObjective(in *sdk.Input) drObjective {
	if in == nil || in.Policy == nil {
		return drNone
	}
	for _, k := range []string{drKey, drStrategyKey} {
		v, ok := in.Policy.Get(k)
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "required", "multi-region", "hot-hot", "hot-warm", "hot-cold":
			return drMultiRegion
		case "single-region":
			return drSingleRegion
		}
	}
	return drNone
}

var geoStorage = map[string]bool{
	"standard_grs": true, "standard_ragrs": true, "standard_gzrs": true, "standard_ragzrs": true,
	"standardv2_grs": true, "standardv2_gzrs": true, "premiumv2_grs": true, "premiumv2_gzrs": true,
}

var localStorage = map[string]bool{
	"standard_lrs": true, "standard_zrs": true, "premium_lrs": true, "premium_zrs": true,
	"standardv2_lrs": true, "standardv2_zrs": true, "premiumv2_lrs": true, "premiumv2_zrs": true,
}

// regionSet returns the distinct literal regions of rs and whether any region
// is empty or an unresolved expression.
func regionSet(rs []sdk.ARMResource) (regions []string, unresolvedRegion bool) {
	for _, r := range rs {
		reg := strings.ToLower(strings.TrimSpace(r.Region))
		if reg == "" || isExpr(reg) {
			unresolvedRegion = true
			continue
		}
		if !slices.Contains(regions, reg) {
			regions = append(regions, reg)
		}
	}
	slices.Sort(regions)
	return regions, unresolvedRegion
}

// cosmosRegions returns the distinct literal locationName values.
func cosmosRegions(r sdk.ARMResource) (regions []string, known bool) {
	locs, ok := r.Properties["locations"].([]any)
	if !ok || len(locs) == 0 {
		return nil, false
	}
	known = true
	for _, raw := range locs {
		m, _ := raw.(map[string]any)
		name, _ := m["locationName"].(string)
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || isExpr(name) {
			known = false
			continue
		}
		if !slices.Contains(regions, name) {
			regions = append(regions, name)
		}
	}
	slices.Sort(regions)
	return regions, known
}

func foundryAccountsOnly(rs []sdk.ARMResource) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range rs {
		if strings.EqualFold(r.Kind, "AIServices") || strings.EqualFold(r.Kind, "OpenAI") {
			out = append(out, r)
		}
	}
	return out
}

func firstByName(rs []sdk.ARMResource) sdk.ARMResource {
	best := rs[0]
	for _, r := range rs[1:] {
		if strings.Compare(r.Name, best.Name) < 0 {
			best = r
		}
	}
	return best
}

func eval007(in *sdk.Input) sdk.Result {
	objective := readObjective(in)
	o := newOutcome(in)
	accounts := foundryAccountsOnly(o.of(typeFoundry))
	searches := o.of(typeSearch)
	storages := o.of(typeStorage)
	cosmoses := o.of(typeCosmos)
	apims := o.of(typeAPIM)
	deployments := o.of(typeDeployment)

	if objective == drNone {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-resilience-objective-declared: template evidence: " + drSummary(accounts, searches, storages, cosmoses, apims, deployments)}}
	}
	if objective == drSingleRegion {
		return sdk.Result{} // a declared single-region decision is a valid answer
	}

	for _, g := range []struct {
		rs   []sdk.ARMResource
		key  string
		text string
	}{
		{accounts, "single-region-foundry-account", "all Foundry accounts in the template are in one region, so a regional outage stops model access"},
		{searches, "single-region-search", "all Search services in the template are in one region; Search has no cross-region replica, so a second service in another region is needed"},
	} {
		if len(g.rs) == 0 {
			continue
		}
		regions, unres := regionSet(g.rs)
		switch {
		case len(regions) >= 2:
		case unres:
			o.notes = append(o.notes, g.rs[0].Type+": a region is unresolved, so multi-region cannot be shown")
		default:
			o.add(firstByName(g.rs), g.key, g.text)
		}
	}
	for _, r := range storages {
		sku := strings.ToLower(strings.TrimSpace(r.SKUName))
		switch {
		case geoStorage[sku]:
		case localStorage[sku]:
			o.add(r, "single-region-storage", "storage account replicates within one region only (LRS or ZRS)")
		default:
			o.notes = append(o.notes, r.Name+": storage sku.name is unresolved or not in the verified redundancy mapping")
		}
	}
	for _, r := range cosmoses {
		regions, known := cosmosRegions(r)
		switch {
		case len(regions) >= 2:
		case !known:
			o.notes = append(o.notes, r.Name+": Cosmos locations are unavailable or unresolved")
		default:
			o.add(r, "single-region-cosmos", "Cosmos account has a single location")
		}
	}
	for _, r := range apims {
		additional, ok := r.Properties["additionalLocations"]
		list, isList := additional.([]any)
		switch {
		case ok && !isList:
			o.notes = append(o.notes, r.Name+": APIM additionalLocations is unresolved")
		case strings.TrimSpace(r.SKUName) == "" || isExpr(r.SKUName):
			o.notes = append(o.notes, r.Name+": APIM sku.name is unresolved")
		case !strings.EqualFold(r.SKUName, "Premium") || len(list) == 0:
			o.add(r, "single-region-apim", "API Management has no additionalLocations on the documented Premium multi-region shape")
		}
	}
	return o.result()
}

func drSummary(accounts, searches, storages, cosmoses, apims, deployments []sdk.ARMResource) string {
	var parts []string
	count := func(label string, rs []sdk.ARMResource) {
		if len(rs) == 0 {
			return
		}
		regions, unres := regionSet(rs)
		s := fmt.Sprintf("%s=%d", label, len(regions))
		if unres {
			s += "+unresolved"
		}
		parts = append(parts, s)
	}
	count("foundry-regions", accounts)
	count("search-regions", searches)
	if len(storages) > 0 {
		geo, local, other := 0, 0, 0
		for _, r := range storages {
			sku := strings.ToLower(strings.TrimSpace(r.SKUName))
			switch {
			case geoStorage[sku]:
				geo++
			case localStorage[sku]:
				local++
			default:
				other++
			}
		}
		switch {
		case other > 0:
			parts = append(parts, "storage=unresolved")
		case geo > 0 && local > 0:
			parts = append(parts, "storage=mixed")
		case geo > 0:
			parts = append(parts, "storage=geo-redundant")
		default:
			parts = append(parts, "storage=single-region")
		}
	}
	if len(cosmoses) > 0 {
		most := 0
		for _, r := range cosmoses {
			if regions, _ := cosmosRegions(r); len(regions) > most {
				most = len(regions)
			}
		}
		parts = append(parts, fmt.Sprintf("cosmos-regions=%d", most))
	}
	if len(apims) > 0 {
		multi := 0
		for _, r := range apims {
			if list, _ := r.Properties["additionalLocations"].([]any); len(list) > 0 {
				multi++
			}
		}
		parts = append(parts, fmt.Sprintf("apim-multi-region=%d/%d", multi, len(apims)))
	}
	// Model deployments of one model across several accounts (redundancy).
	byModel := map[string]map[string]bool{}
	for _, r := range deployments {
		m, _ := r.Properties["model"].(map[string]any)
		name := strings.ToLower(fmt.Sprint(m["format"]) + "/" + fmt.Sprint(m["name"]) + "/" + fmt.Sprint(m["version"]))
		if byModel[name] == nil {
			byModel[name] = map[string]bool{}
		}
		byModel[name][strings.ToLower(parentName(r.Name))] = true
	}
	if len(byModel) > 0 {
		redundant := 0
		for _, accts := range byModel {
			if len(accts) >= 2 {
				redundant++
			}
		}
		parts = append(parts, fmt.Sprintf("models-deployed-in-multiple-accounts=%d", redundant))
	}
	if len(parts) == 0 {
		return "no stateful resources in the template"
	}
	return strings.Join(parts, "; ")
}

package rel

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func inRegion(r sdk.ARMResource, region string) sdk.ARMResource {
	r.Region = region
	return r
}

func cosmosLocs(names ...string) sdk.ARMResource {
	var locs []any
	for i, n := range names {
		locs = append(locs, map[string]any{"locationName": n, "failoverPriority": float64(i)})
	}
	return sdk.ARMResource{Type: typeCosmos, Name: "cosmos", Region: names[0], Properties: map[string]any{"locations": locs}}
}

func TestREL007Registered(t *testing.T) {
	if findRule("FND-REL-007") == nil {
		t.Fatal("FND-REL-007 must be registered")
	}
}

func TestREL007NoObjectiveSkipsWithSummary(t *testing.T) {
	arm := model{
		inRegion(sdk.ARMResource{Type: typeFoundry, Name: "acct", Kind: "AIServices"}, "eastus"),
		inRegion(res(typeStorage, "stor", "Standard_GRS", nil), "eastus"),
		cosmosLocs("eastus", "westus"),
	}
	for name, p := range map[string]sdk.Policy{
		"no policy":           nil,
		"empty policy":        pol{},
		"unrecognised value":  pol{"reliability.disasterRecovery": "maybe"},
		"non string value":    pol{"reliability.disasterRecovery": 3},
		"declared key is not": pol{"disasterRecovery.declared": true},
	} {
		t.Run(name, func(t *testing.T) {
			got := eval(t, "FND-REL-007", &sdk.Input{ARM: arm, Policy: p})
			if got.Skipped == nil || len(got.Findings) != 0 {
				t.Fatalf("want skip, got %+v", got)
			}
			r := got.Skipped.Reason
			if !strings.HasPrefix(r, "no-resilience-objective-declared") {
				t.Fatalf("reason head: %q", r)
			}
			for _, want := range []string{"foundry-regions=1", "storage=geo-redundant", "cosmos-regions=2"} {
				if !strings.Contains(r, want) {
					t.Fatalf("summary %q missing %q", r, want)
				}
			}
		})
	}
	if got := eval(t, "FND-REL-007", &sdk.Input{}); got.Skipped == nil {
		t.Fatalf("missing ARM must skip: %+v", got)
	}
}

func TestREL007DeclaredObjective(t *testing.T) {
	acct := func(name, region string) sdk.ARMResource {
		return inRegion(sdk.ARMResource{Type: typeFoundry, Name: name, Kind: "AIServices"}, region)
	}
	srch := func(name, region string) sdk.ARMResource {
		return inRegion(res(typeSearch, name, "standard", nil), region)
	}
	storage := func(sku, region string) sdk.ARMResource { return inRegion(res(typeStorage, "stor", sku, nil), region) }
	required := pol{"reliability.disasterRecovery": "required"}
	multi := model{acct("a1", "eastus"), acct("a2", "westus"), srch("s1", "eastus"), srch("s2", "westus"), storage("Standard_GZRS", "eastus"), cosmosLocs("eastus", "westus")}
	cases := []struct {
		name   string
		arm    model
		policy pol
		keys   []string
		uncert bool
	}{
		{"fully multi-region passes", multi, required, nil, false},
		{"single-region everything fails for each stateful type", model{acct("a1", "eastus"), srch("s1", "eastus"), storage("Standard_LRS", "eastus"), cosmosLocs("eastus")}, required,
			[]string{typeCosmos + "=single-region-cosmos", typeFoundry + "=single-region-foundry-account", typeSearch + "=single-region-search", typeStorage + "=single-region-storage"}, false},
		{"hot-hot strategy value also requires multi-region", model{storage("Standard_ZRS", "eastus")}, pol{"reliability.disasterRecovery": "hot-hot"}, []string{typeStorage + "=single-region-storage"}, false},
		{"hot-warm strategy value also requires multi-region", model{storage("Standard_ZRS", "eastus")}, pol{"reliability.disasterRecovery": "hot-warm"}, []string{typeStorage + "=single-region-storage"}, false},
		{"hot-cold strategy value also requires multi-region", model{storage("Standard_ZRS", "eastus")}, pol{"reliability.disasterRecovery": "hot-cold"}, []string{typeStorage + "=single-region-storage"}, false},
		{"strategy key form is accepted", model{storage("Standard_ZRS", "eastus")}, pol{"reliability.disasterRecovery.strategy": "required"}, []string{typeStorage + "=single-region-storage"}, false},
		{"declared single-region decision is a valid answer", model{acct("a1", "eastus"), storage("Standard_LRS", "eastus")}, pol{"reliability.disasterRecovery": "single-region"}, nil, false},
		{"RA-GRS storage is multi-region", model{storage("Standard_RAGRS", "eastus")}, required, nil, false},
		{"GRS storage is multi-region", model{storage("Standard_GRS", "eastus")}, required, nil, false},
		{"ZRS storage is single-region", model{storage("Standard_ZRS", "eastus")}, required, []string{typeStorage + "=single-region-storage"}, false},
		{"unknown storage sku is uncertain", model{storage("Weird_SKU", "eastus")}, required, nil, true},
		{"empty storage sku is uncertain", model{storage("", "eastus")}, required, nil, true},
		{"cosmos with two locations passes", model{cosmosLocs("eastus", "westus")}, required, nil, false},
		{"cosmos with two identical locations is one region", model{cosmosLocs("eastus", "eastus")}, required, []string{typeCosmos + "=single-region-cosmos"}, false},
		{"cosmos locations missing is uncertain", model{{Type: typeCosmos, Name: "cosmos"}}, required, nil, true},
		{"cosmos location expression is uncertain", model{cosmosLocs("eastus", "[parameters('second')]")}, required, nil, true},
		{"two accounts in one region are single-region", model{acct("a1", "eastus"), acct("a2", "eastus")}, required, []string{typeFoundry + "=single-region-foundry-account"}, false},
		{"account region expression is uncertain", model{acct("a1", "[parameters('location')]")}, required, nil, true},
		{"one literal and one expression region is uncertain", model{acct("a1", "eastus"), acct("a2", "[parameters('location')]")}, required, nil, true},
		{"two search services in two regions pass", model{srch("s1", "eastus"), srch("s2", "westus")}, required, nil, false},
		{"APIM with additional locations passes", model{{Type: typeAPIM, Name: "apim", SKUName: "Premium", Region: "eastus", Properties: map[string]any{"additionalLocations": []any{map[string]any{"location": "westus"}}}}}, required, nil, false},
		{"APIM without additional locations fails", model{{Type: typeAPIM, Name: "apim", SKUName: "Premium", Region: "eastus", Properties: map[string]any{}}}, required, []string{typeAPIM + "=single-region-apim"}, false},
		{"no stateful resources in template", model{}, required, nil, false},
		{"conditional resource is uncertain", model{func() sdk.ARMResource {
			r := storage("Standard_LRS", "eastus")
			r.Conditional, r.Condition = true, "[parameters('deploy')]"
			return r
		}()}, required, nil, true},
		{"literal false resource is ignored", model{func() sdk.ARMResource {
			r := storage("Standard_LRS", "eastus")
			r.Conditional, r.Condition = true, "[false()]"
			return r
		}()}, required, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-REL-007", &sdk.Input{ARM: tc.arm, Policy: tc.policy})
			if tc.uncert {
				if got.Skipped == nil || len(got.Findings) != 0 || !strings.Contains(got.Skipped.Reason, "uncertain") {
					t.Fatalf("want uncertain skip, got %+v", got)
				}
				return
			}
			if g := findingKeys(got); !sameStrings(g, tc.keys) {
				t.Fatalf("keys %v want %v (%+v)", g, tc.keys, got)
			}
			if len(tc.keys) == 0 && got.Skipped != nil {
				t.Fatalf("want pass, got skip %+v", got.Skipped)
			}
			for _, f := range got.Findings {
				if f.Evidence == "" || f.Key == "" {
					t.Fatalf("finding needs evidence and key: %+v", f)
				}
			}
		})
	}
}

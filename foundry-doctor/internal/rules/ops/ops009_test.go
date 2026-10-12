package ops

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func resInput(t *testing.T, scope model.DataResidencyScope, names ...string) *model.Input {
	t.Helper()
	in := input(arm(t, "residency.arm.json", names...))
	in.Policy.DataResidency.Scope = &scope
	in.Policy.DataResidency.Regions = []string{"swedencentral", "westeurope"}
	return in
}

func scenarios009() scenarios {
	r := ops009{}
	return scenarios{
		"positive/global-standard-when-data-zone-required": func(t *testing.T) {
			got := eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-gs"))
			wantKeys(t, got, sdk.StatusFail, "sku-scope")
			if !strings.Contains(fails(got)[0].Evidence, "global processing scope") {
				t.Fatal(fails(got)[0].Evidence)
			}
		},
		"positive/global-batch-when-geography-required": func(t *testing.T) {
			wantKeys(t, eval(t, r, resInput(t, model.ResidencyGeography, "acct-eu/dep-batch")), sdk.StatusFail, "sku-scope")
			wantKeys(t, eval(t, r, resInput(t, model.ResidencyGeography, "acct-eu/dep-dz")), sdk.StatusFail, "sku-scope")
		},
		"positive/developer-tier-in-environment-with-residency-requirement": func(t *testing.T) {
			wantKeys(t, eval(t, r, resInput(t, model.ResidencyGlobal, "acct-eu/dep-dev")), sdk.StatusFail, "developer-tier")
		},
		"positive/account-location-outside-declared-regions": func(t *testing.T) {
			got := eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-us"))
			wantKeys(t, got, sdk.StatusFail, "location")
			if fails(got)[0].Confidence != sdk.ConfidenceCertain {
				t.Fatal("confidence")
			}
		},
		"positive/search-service-in-region-not-allowed": func(t *testing.T) {
			wantKeys(t, eval(t, r, resInput(t, model.ResidencyDatazoneEU, "search-far")), sdk.StatusFail, "location")
		},
		"negative/data-zone-standard-with-data-zone-required": func(t *testing.T) {
			wantPass(t, eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-dz", "acct-eu")))
		},
		"negative/standard-in-allowed-geography-region": func(t *testing.T) {
			wantPass(t, eval(t, r, resInput(t, model.ResidencyGeography, "acct-eu/dep-std", "acct-eu")))
			// Global scope tolerates every SKU except DeveloperTier.
			wantPass(t, eval(t, r, resInput(t, model.ResidencyGlobal, "acct-eu/dep-gs", "acct-eu/dep-batch")))
		},
		"negative/global-standard-when-no-restriction-declared-is-not-evaluated": func(t *testing.T) {
			in := resInput(t, model.ResidencyGlobal, "acct-eu/dep-gs")
			in.Policy.DataResidency = model.PolicyDataResidency{}
			got := eval(t, r, in)
			wantNoFindings(t, got)
			if hasStatus(got, sdk.StatusPass) {
				t.Fatal("an unevaluated rule must not pass")
			}
		},
		"skipped/no-residency-declared-in-profile": func(t *testing.T) {
			in := resInput(t, model.ResidencyGlobal, "acct-us")
			in.Policy.DataResidency.Scope = nil
			got := eval(t, r, in)
			wantSkip(t, got, "profile-key-missing:policy.dataResidency.scope")
			if len(got) != 1 {
				t.Fatalf("results = %d", len(got))
			}
		},
		"skipped/sku-name-not-in-table": func(t *testing.T) {
			// The evidence definition reports an unknown SKU as uncertain, never as a violation.
			got := eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-weird"))
			wantKeys(t, got, sdk.StatusUncertain, "sku-unknown")
			if hasStatus(got, sdk.StatusFail) {
				t.Fatal("unknown SKU must not be a violation")
			}
		},
		"skipped/location-is-unresolved-parameter": func(t *testing.T) {
			got := eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-param"))
			wantSkip(t, got, "unresolved-value:location-arm-expression")
			wantNoFindings(t, got)
		},
		"skipped/control-plane-unavailable": func(t *testing.T) {
			wantSkip(t, eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-eu")), "control-plane-unavailable")
		},
	}
}

func TestOPS009Extras(t *testing.T) {
	r := ops009{}
	t.Run("regions missing names the exact key", func(t *testing.T) {
		in := resInput(t, model.ResidencyDatazoneUS, "acct-us")
		in.Policy.DataResidency.Regions = nil
		got := eval(t, r, in)
		wantSkip(t, got, "profile-key-missing:policy.dataResidency.regions")
		if len(got) != 1 {
			t.Fatal("must be a single skip")
		}
		in.Policy.DataResidency.Regions = []string{}
		wantSkip(t, eval(t, r, in), "profile-key-missing:policy.dataResidency.regions")
	})
	t.Run("deployment SKU list is authoritative", func(t *testing.T) {
		in := resInput(t, model.ResidencyGeography, "acct-eu/dep-gs", "acct-eu/dep-std")
		in.Policy.DataResidency.DeploymentSkus = []string{"Standard"}
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "sku-not-allowed")
	})
	t.Run("unresolved and absent SKU are skipped", func(t *testing.T) {
		got := eval(t, r, resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-param", "acct-eu/dep-nosku"))
		wantNoFindings(t, got)
		wantSkip(t, got, "unresolved-value:sku-arm-expression")
		wantSkip(t, got, "unresolved-value:sku-absent")
	})
	t.Run("azure.yaml deployments without ARM", func(t *testing.T) {
		in := yamlInput(t, `name: demo
services:
  ai:
    host: azure.ai.project
    deployments:
      - name: chat
        model: { format: OpenAI, name: gpt-x, version: "1" }
        sku: { name: GlobalStandard, capacity: 10 }
      - name: env
        model: { format: OpenAI, name: gpt-x, version: "1" }
        sku: { name: "${SKU}" }
`)
		scope := model.ResidencyDatazoneUS
		in.Policy.DataResidency = model.PolicyDataResidency{Scope: &scope, Regions: []string{"eastus"}}
		got := eval(t, r, in)
		wantKeys(t, got, sdk.StatusFail, "sku-scope")
		f := fails(got)[0]
		if f.Resource.Kind != "service" || f.Location.File != "azure.yaml" || f.Location.Line == 0 {
			t.Fatalf("finding = %+v", f)
		}
		wantSkip(t, got, "synthetic-infrastructure")
		wantSkip(t, got, "unresolved-value:sku-env-reference")
	})
	t.Run("AZURE_LOCATION", func(t *testing.T) {
		in := resInput(t, model.ResidencyDatazoneEU, "acct-eu")
		bad := model.NewEnvironment("prod", map[string]string{"AZURE_LOCATION": "eastus"})
		in.Environment = &bad
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "env-location")
		good := model.NewEnvironment("prod", map[string]string{"AZURE_LOCATION": "westeurope"})
		in.Environment = &good
		wantPass(t, eval(t, r, in))
		none := model.NewEnvironment("prod", map[string]string{})
		in.Environment = &none
		wantPass(t, eval(t, r, in))
	})
	t.Run("regions compare exactly", func(t *testing.T) {
		in := resInput(t, model.ResidencyDatazoneEU, "acct-eu")
		in.Policy.DataResidency.Regions = []string{"SwedenCentral"}
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "location")
	})
	t.Run("conditional deployment lowers confidence", func(t *testing.T) {
		in := resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-gs")
		in.ARM.Resources[0].Meta.MultiInstance = true
		if f := fails(eval(t, r, in))[0]; f.Confidence != sdk.ConfidenceLikely {
			t.Fatalf("confidence = %s", f.Confidence)
		}
	})
	t.Run("non-string SKU ignored", func(t *testing.T) {
		in := resInput(t, model.ResidencyDatazoneEU, "acct-eu/dep-gs")
		in.ARM.Resources[0].Body["sku"] = map[string]any{"name": float64(1)}
		wantNoFindings(t, eval(t, r, in))
	})
}

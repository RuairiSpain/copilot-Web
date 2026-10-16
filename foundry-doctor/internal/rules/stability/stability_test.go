// Package stability holds cross-package tests of what the ARM-based rules show
// to users: no internal scope token (name@path) in any finding or skip reason,
// and finding fingerprints that do not move when an unrelated module is added.
package stability

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cost"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/idn"
	netrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/net"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/ops"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var scopeToken = regexp.MustCompile(`(?i)(parameters|variables)\(\s*'[^'@]*@[^']*'`)

func allRules() []sdk.Rule {
	var out []sdk.Rule
	for _, rs := range [][]sdk.Rule{sec.Register(), ops.Register(), rel.Register(), idn.Register(), cost.Register(), netrules.Register(), cfg.Register()} {
		out = append(out, rs...)
	}
	return out
}

// secureModules is a template whose module parameters are secure, so they are
// never substituted and every module-local name keeps its scope token.
const secureModules = `{"resources":[
 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"srch-a","properties":{"mode":"Incremental",
  "expressionEvaluationOptions":{"scope":"inner"},
  "parameters":{"name":{"value":"x"},"loc":{"value":"westeurope"}},
  "template":{"parameters":{"name":{"type":"securestring"},"loc":{"type":"securestring"}},"resources":[
   {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('name')]","location":"[parameters('loc')]","sku":{"name":"standard"},"properties":{"disableLocalAuth":false}},
   {"type":"Microsoft.CognitiveServices/accounts","apiVersion":"2025-06-01","name":"[parameters('name')]","location":"[parameters('loc')]","kind":"AIServices","sku":{"name":"S0"},"identity":{"type":"SystemAssigned"},"properties":{"disableLocalAuth":false,"publicNetworkAccess":"Enabled"}}]}}},
 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"srch-b","properties":{"mode":"Incremental",
  "expressionEvaluationOptions":{"scope":"inner"},
  "parameters":{"name":{"value":"y"},"loc":{"value":"northeurope"}},
  "template":{"parameters":{"name":{"type":"securestring"},"loc":{"type":"securestring"}},"resources":[
   {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('name')]","location":"[parameters('loc')]","sku":{"name":"standard"},"properties":{"disableLocalAuth":false}}]}}}
]}`

type input struct {
	name string
	raw  []byte
}

func inputs(t *testing.T) []input {
	t.Helper()
	var out []input
	for _, s := range []string{"twice", "condmod", "loopmod", "nested", "pemod", "avm"} {
		out = append(out, input{"scenario/" + s, rulestest.ScenarioJSON(t, s)})
	}
	out = append(out, input{"main", rulestest.JSON()}, input{"secure", []byte(secureModules)})
	return out
}

type observed struct {
	fingerprint, evidence string
}

func evaluate(t *testing.T, raw []byte) (map[string]observed, []string) {
	t.Helper()
	m, err := armmodel.FromARM(raw, armmodel.Options{Entry: "main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	in := &sdk.Input{ARM: m}
	got := map[string]observed{}
	var skips []string
	for _, r := range allRules() {
		res, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatalf("%s: %v", r.ID(), err)
		}
		if res.Skipped != nil {
			skips = append(skips, r.ID()+": "+res.Skipped.Reason)
		}
		for _, f := range res.Findings {
			f.RuleID = r.ID()
			key := r.ID() + "|" + f.Resource.Type + "|" + f.Resource.Name + "|" + f.Key + "|" + f.Evidence
			got[key] = observed{findings.Fingerprint(f), f.Evidence}
			if scopeToken.MatchString(f.Resource.Name + "\n" + f.Evidence + "\n" + f.Key) {
				t.Errorf("%s leaks a scope token: name %q evidence %q key %q", r.ID(), f.Resource.Name, f.Evidence, f.Key)
			}
		}
	}
	for _, s := range skips {
		if scopeToken.MatchString(s) {
			t.Errorf("skip reason leaks a scope token: %q", s)
		}
	}
	sort.Strings(skips)
	return got, skips
}

// R2: no internal scope token reaches a finding, evidence or skip reason.
func TestNoScopeTokensInOutput(t *testing.T) {
	for _, in := range inputs(t) {
		t.Run(in.name, func(t *testing.T) { evaluate(t, in.raw) })
	}
}

// insertModule puts an unrelated module deployment at the front of the
// template's resources array. It returns nil for symbolic-name templates.
func insertModule(t *testing.T, raw []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rs, ok := doc["resources"].([]any)
	if !ok {
		return nil
	}
	extra := map[string]any{
		"type": "Microsoft.Resources/deployments", "apiVersion": "2025-04-01", "name": "unrelated-storage",
		"properties": map[string]any{
			"mode": "Incremental", "expressionEvaluationOptions": map[string]any{"scope": "inner"},
			"template": map[string]any{"resources": []any{map[string]any{
				"type": "Microsoft.Storage/storageAccounts", "apiVersion": "2023-05-01", "name": "unrelatedstore",
				"location": "westeurope", "kind": "StorageV2", "sku": map[string]any{"name": "Standard_LRS"},
				"properties": map[string]any{},
			}}},
		},
	}
	doc["resources"] = append([]any{extra}, rs...)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// R2: inserting an unrelated module earlier in the template does not change
// the fingerprint of any existing finding.
func TestFingerprintsStableWhenModuleInserted(t *testing.T) {
	compared := 0
	for _, in := range inputs(t) {
		t.Run(in.name, func(t *testing.T) {
			changed := insertModule(t, in.raw)
			if changed == nil {
				t.Skip("symbolic-name template")
			}
			before, _ := evaluate(t, in.raw)
			after, _ := evaluate(t, changed)
			for k, b := range before {
				a, ok := after[k]
				if !ok {
					t.Errorf("finding disappeared or changed text after inserting a module: %s", k)
					continue
				}
				if a.fingerprint != b.fingerprint {
					t.Errorf("fingerprint changed for %s", k)
				}
				compared++
			}
		})
	}
	if compared == 0 {
		t.Fatal("no finding was compared; the test proves nothing")
	}
}

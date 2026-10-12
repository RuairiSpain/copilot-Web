package cost_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cost"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	catalogfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range cost.Rules() {
		if r.ID() == "" || r.Version() < 1 {
			t.Fatalf("bad rule %T", r)
		}
	}
}

func rule(t *testing.T) rules.Rule {
	t.Helper()
	rs := cost.Rules()
	if len(rs) != 1 || rs[0].ID() != "FND-COST-002" || rs[0].Version() != 1 {
		t.Fatalf("unexpected rules %v", rs)
	}
	return rs[0]
}

func arm(t *testing.T, name string) *model.ARMTemplate {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "rules", "cost", name+".arm.json"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := bicep.Normalise(raw, bicep.Options{File: "main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	return n.Template
}

func ptr[T any](v T) *T { return &v }

// input builds a dev input for environment envName.
func input(envName string, tmpl *model.ARMTemplate, mut ...func(*model.Input)) *model.Input {
	e := model.NewEnvironment(envName, map[string]string{"AZURE_ENV_NAME": envName})
	in := &model.Input{
		Project:     model.Project{Name: "p", IaC: model.IaCBicep, CurrentEnvironment: envName},
		Environment: &e,
		ARM:         tmpl,
		Policy:      model.Policy{Environments: model.PolicyEnvironments{Development: []string{"dev"}, Production: []string{"prod"}}},
	}
	if tmpl == nil {
		in.Project.IaC = model.IaCSynthetic
	}
	for _, m := range mut {
		m(in)
	}
	return in
}

func withLimit(n int) func(*model.Input) {
	return func(in *model.Input) { in.Policy.Cost.DevMaxCosmosThroughput = ptr(n) }
}

func eval(t *testing.T, in *model.Input) []rules.Result {
	t.Helper()
	res, err := rule(t).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func wantFail(t *testing.T, res []rules.Result, key string) {
	t.Helper()
	for _, r := range res {
		if r.Status == sdk.StatusFail && r.Finding.Key == key {
			f := r.Finding
			if f.RuleID != "FND-COST-002" || f.RuleVersion != 1 || f.Evidence == "" || f.Recommendation == "" || f.Fix == "" || f.Resource.Name == "" || f.Confidence == "" {
				t.Fatalf("incomplete finding %+v", f)
			}
			return
		}
	}
	t.Fatalf("no failing finding with key %q in %+v", key, summarise(res))
}

func summarise(res []rules.Result) []string {
	var s []string
	for _, r := range res {
		x := string(r.Status)
		if r.Finding != nil {
			x += ":" + r.Finding.Key
		}
		if r.Skip != nil {
			x += ":" + r.Skip.Reason
		}
		s = append(s, x)
	}
	return s
}

func wantPass(t *testing.T, res []rules.Result) {
	t.Helper()
	if len(res) != 1 || res[0].Status != sdk.StatusPass {
		t.Fatalf("want pass, got %v", summarise(res))
	}
}

func wantSkip(t *testing.T, res []rules.Result, reason string) {
	t.Helper()
	for _, r := range res {
		if r.Status == sdk.StatusSkipped && r.Skip.Reason == reason {
			return
		}
	}
	t.Fatalf("want skip %q, got %v", reason, summarise(res))
}

func noFail(t *testing.T, res []rules.Result) {
	t.Helper()
	for _, r := range res {
		if r.Status == sdk.StatusFail || r.Status == sdk.StatusUncertain {
			t.Fatalf("unexpected %v", summarise(res))
		}
	}
}

var scenarios = map[string]func(t *testing.T){
	// positive
	"dev-environment-with-global-provisioned-managed-deployment": func(t *testing.T) {
		wantFail(t, eval(t, input("dev", arm(t, "global-provisioned-managed"))), "ptu-deployment")
	},
	"dev-search-standard3": func(t *testing.T) {
		wantFail(t, eval(t, input("dev", arm(t, "search-standard3"))), "search-sku")
	},
	"dev-search-two-replicas": func(t *testing.T) {
		wantFail(t, eval(t, input("dev", arm(t, "search-two-replicas"))), "replica-count")
	},
	"dev-apim-premium": func(t *testing.T) {
		wantFail(t, eval(t, input("dev", arm(t, "apim-premium"))), "apim-sku")
	},
	"dev-cosmos-throughput-above-profile-limit": func(t *testing.T) {
		wantFail(t, eval(t, input("dev", arm(t, "cosmos-5000"), withLimit(4000))), "cosmos-throughput")
	},
	// negative
	"dev-global-standard-deployment": func(t *testing.T) {
		wantPass(t, eval(t, input("dev", arm(t, "global-standard"))))
	},
	"dev-search-basic-single-unit": func(t *testing.T) {
		wantPass(t, eval(t, input("dev", arm(t, "search-basic-single"))))
	},
	"prod-environment-with-provisioned-managed": func(t *testing.T) {
		res := eval(t, input("prod", arm(t, "global-provisioned-managed")))
		noFail(t, res)
		wantSkip(t, res, rules.ReasonNotApplicable)
	},
	"dev-cosmos-at-foundry-minimum-3000-ru": func(t *testing.T) {
		wantPass(t, eval(t, input("dev", arm(t, "cosmos-3000"), withLimit(1000))))
	},
	// skipped
	"environment-not-classified-as-dev": func(t *testing.T) {
		res := eval(t, input("sandbox", arm(t, "global-provisioned-managed")))
		noFail(t, res)
		wantSkip(t, res, rules.ReasonNotApplicable)
		res = eval(t, input("dev", arm(t, "global-provisioned-managed"), func(in *model.Input) { in.Policy.Environments.Development = nil }))
		wantSkip(t, res, rules.ReasonProfileKeyMissing(model.KeyEnvironmentsDevelopment))
	},
	"sku-value-is-unresolved-parameter": func(t *testing.T) {
		res := eval(t, input("dev", arm(t, "unresolved-sku")))
		noFail(t, res)
		wantSkip(t, res, rules.ReasonUnresolvedValue("arm-expression"))
	},
	"no-cosmos-limit-in-profile": func(t *testing.T) {
		res := eval(t, input("dev", arm(t, "cosmos-5000")))
		noFail(t, res)
		wantSkip(t, res, rules.ReasonProfileKeyMissing(model.KeyCostDevMaxCosmosThroughput))
	},
}

func TestScenarios(t *testing.T) {
	names := make([]string, 0, len(scenarios))
	for n := range scenarios {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		t.Run(n, scenarios[n])
	}
}

func TestScenarioCoverage(t *testing.T) {
	all, err := catalog.Load(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range all {
		if r.ID != "FND-COST-002" {
			continue
		}
		found = true
		if r.Version != cost.Rules()[0].Version() {
			t.Errorf("catalogue version %d != code %d", r.Version, cost.Rules()[0].Version())
		}
		for _, group := range [][]string{r.Tests.Positive, r.Tests.Negative, r.Tests.Skipped, r.Tests.Uncertain} {
			for _, s := range group {
				if scenarios[s] == nil {
					t.Errorf("catalogue scenario %q has no subtest", s)
				}
			}
		}
	}
	if !found {
		t.Fatal("FND-COST-002 not in catalogue")
	}
}

func TestSyntheticSkipsARMPartsOnly(t *testing.T) {
	src := "name: p\nservices:\n  agent:\n    host: azure.ai.agent\n    deployments:\n      - name: chat\n        sku: { name: GlobalProvisionedManaged, capacity: 1 }\n      - name: small\n        sku: { name: GlobalStandard, capacity: 1 }\n"
	res, err := azureyaml.Parse([]byte(src), azureyaml.Options{File: "azure.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	in := input("dev", nil, func(in *model.Input) { in.AzureYAML = res.YAML })
	out := eval(t, in)
	wantSkip(t, out, rules.ReasonSyntheticInfrastructure)
	wantFail(t, out, "ptu-deployment:agent:0")
	if len(out) != 2 {
		t.Fatalf("got %v", summarise(out))
	}
}

func TestExemptionAndMultiInstanceAndNoEnvironment(t *testing.T) {
	tm := arm(t, "search-standard3")
	wantPass(t, eval(t, input("dev", tm, func(in *model.Input) { in.Policy.Cost.ProductionSizedSkuExemptions = []string{"srch"} })))

	cond := *tm
	cond.Resources = slices.Clone(tm.Resources)
	cond.Resources[0].Meta.MayNotDeploy = true
	res := eval(t, input("dev", &cond))
	if res[0].Status != sdk.StatusUncertain || res[0].Finding.Confidence != sdk.ConfidenceUncertain {
		t.Fatalf("want uncertain, got %v", summarise(res))
	}

	res = eval(t, input("", tm, func(in *model.Input) { in.Environment = nil; in.Project.CurrentEnvironment = "" }))
	wantSkip(t, res, rules.ReasonNotApplicable)

	// the project's current environment is used when no environment is selected
	res = eval(t, input("dev", tm, func(in *model.Input) { in.Environment = nil }))
	wantFail(t, res, "search-sku")
}

func TestOtherSKUsAndAutoscale(t *testing.T) {
	// storage_optimized and Isolated, autoscale maximum, apim capacity
	mk := func(typ, name string, body map[string]any) model.Resource {
		return model.Resource{Type: typ, Name: name, Body: body}
	}
	tm := &model.ARMTemplate{Resources: []model.Resource{
		mk("Microsoft.Search/searchServices", "s1", map[string]any{"sku": map[string]any{"name": "storage_optimized_l1"}}),
		mk("Microsoft.Search/searchServices", "s2", map[string]any{"sku": map[string]any{"name": "basic"}, "properties": map[string]any{"partitionCount": float64(3)}}),
		mk("Microsoft.ApiManagement/service", "a1", map[string]any{"sku": map[string]any{"name": "Isolated", "capacity": float64(2)}}),
		mk("Microsoft.ApiManagement/service", "a2", map[string]any{"sku": map[string]any{"name": "Developer", "capacity": float64(2)}}),
		mk("Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers/throughputSettings", "c", map[string]any{"properties": map[string]any{"resource": map[string]any{"autoscaleSettings": map[string]any{"maxThroughput": float64(40000)}}}}),
		mk("Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", "d", map[string]any{"properties": map[string]any{"resource": map[string]any{"throughput": "[parameters('ru')]"}}}),
	}}
	res := eval(t, input("dev", tm, withLimit(4000)))
	for _, k := range []string{"search-sku", "partition-count", "apim-sku", "apim-capacity", "cosmos-autoscale-max"} {
		wantFail(t, res, k)
	}
	wantSkip(t, res, rules.ReasonUnresolvedValue("arm-expression"))
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rule(t).Evaluate(ctx, input("dev", arm(t, "global-standard"))); err == nil {
		t.Fatal("want error")
	}
}

func TestDeterministicAndNoStampedFields(t *testing.T) {
	tm := arm(t, "search-two-replicas")
	a := summarise(eval(t, input("dev", tm)))
	b := summarise(eval(t, input("dev", tm)))
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatal("not deterministic")
	}
	for _, r := range eval(t, input("dev", tm)) {
		if f := r.Finding; f != nil && (f.Severity != "" || f.Profile != "" || f.Fingerprint != "") {
			t.Fatalf("rule set engine-owned fields: %+v", f)
		}
	}
}

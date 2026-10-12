package env_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/suppress"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	catalogfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func TestRulesCompile(t *testing.T) {
	want := []string{"FND-ENV-001", "FND-ENV-002", "FND-ENV-003", "FND-ENV-004"}
	var got []string
	for _, r := range env.Rules() {
		if r.ID() == "" || r.Version() != 1 {
			t.Fatalf("bad rule %T", r)
		}
		got = append(got, r.ID())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rules %v", got)
	}
}

// ---- helpers ----

// canary values are assembled at runtime so no secret-shaped literal sits in the source.
func canary(tag string) string { return "canary" + "-" + tag + "-" + strings.Repeat("z", 6) }

func pe(t *testing.T, name, text string) model.Environment {
	t.Helper()
	res, err := project.ParseEnv(context.Background(), name, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return res.Env
}

func yamlOf(t *testing.T, src string) *model.AzureYAML {
	t.Helper()
	res, err := azureyaml.Parse([]byte(src), azureyaml.Options{File: "azure.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return res.YAML
}

func rule(t *testing.T, id string, envs ...model.Environment) rules.Rule {
	t.Helper()
	for _, r := range env.RulesWith(func(*model.Input) []model.Environment { return envs }) {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("no rule %s", id)
	return nil
}

func tiers(in *model.Input) {
	in.Policy.Environments = model.PolicyEnvironments{Production: []string{"prod"}, NonProduction: []string{"dev"}}
}

func input(mut ...func(*model.Input)) *model.Input {
	in := &model.Input{Project: model.Project{Name: "p", IaC: model.IaCSynthetic, Environments: []string{"dev", "prod"}, CurrentEnvironment: "dev"}}
	for _, m := range mut {
		m(in)
	}
	return in
}

func withYAML(y *model.AzureYAML) func(*model.Input) {
	return func(in *model.Input) { in.AzureYAML = y }
}

func eval(t *testing.T, r rules.Rule, in *model.Input) []rules.Result {
	t.Helper()
	res, err := r.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range res {
		if err := x.Validate(); err != nil {
			t.Fatal(err)
		}
		if f := x.Finding; f != nil {
			if f.RuleID != r.ID() || f.RuleVersion != r.Version() || f.Key == "" || f.Evidence == "" || f.Recommendation == "" || f.Fix == "" || f.Confidence == "" || f.Resource.Kind == "" {
				t.Fatalf("incomplete finding %+v", f)
			}
		}
	}
	return res
}

func summarise(res []rules.Result) string {
	var s []string
	for _, r := range res {
		x := string(r.Status)
		if r.Finding != nil {
			x += ":" + r.Finding.Key
		}
		if r.Skip != nil {
			x += ":" + r.Skip.Reason + ":" + r.Skip.Detail
		}
		s = append(s, x)
	}
	return strings.Join(s, " | ")
}

func wantKey(t *testing.T, res []rules.Result, prefix string) {
	t.Helper()
	for _, r := range res {
		if r.Finding != nil && r.Status == sdk.StatusFail && strings.HasPrefix(r.Finding.Key, prefix) {
			return
		}
	}
	t.Fatalf("no finding with key prefix %q: %s", prefix, summarise(res))
}

func wantPass(t *testing.T, res []rules.Result) {
	t.Helper()
	for _, r := range res {
		if r.Status != sdk.StatusPass {
			t.Fatalf("want pass: %s", summarise(res))
		}
	}
	if len(res) == 0 {
		t.Fatal("no results")
	}
}

func wantSkip(t *testing.T, res []rules.Result, reason, detail string) {
	t.Helper()
	if len(res) != 1 || res[0].Status != sdk.StatusSkipped || res[0].Skip.Reason != reason || !strings.Contains(res[0].Skip.Detail, detail) {
		t.Fatalf("want skip %s/%s: %s", reason, detail, summarise(res))
	}
}

const sub = "AZURE_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000001\n"

func two(t *testing.T, devText, prodText string) (model.Environment, model.Environment) {
	return pe(t, "dev", devText), pe(t, "prod", prodText)
}

func applySuppression(t *testing.T, res []rules.Result, ruleID, keyName string) []sdk.Finding {
	t.Helper()
	var fs []sdk.Finding
	for _, r := range res {
		if r.Finding != nil {
			fs = append(fs, *r.Finding)
		}
	}
	f := suppress.File{Version: 1, Suppressions: []suppress.Suppression{{
		RuleID: ruleID, Resource: suppress.Selector{Kind: "env-key", Name: keyName},
		Reason: "intentionally shared", Owner: "platform", Expires: "2099-01-01",
	}}}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	return suppress.Apply(fs, f, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)).Findings
}

func allSuppressed(t *testing.T, fs []sdk.Finding) {
	t.Helper()
	if len(fs) == 0 {
		t.Fatal("expected findings to suppress")
	}
	for _, f := range fs {
		if f.Suppressed == nil || f.Suppressed.Reason == "" {
			t.Fatalf("finding %s not suppressed", f.Key)
		}
	}
}

// ---- scenarios, keyed by the catalogue test names ----

var scenarios = map[string]func(t *testing.T){
	// FND-ENV-001 positive
	"dev-and-prod-same-resource-group": func(t *testing.T) {
		d, p := two(t, sub+"AZURE_RESOURCE_GROUP=rg-app\nAZURE_ENV_NAME=dev\n", sub+"AZURE_RESOURCE_GROUP=RG-APP\nAZURE_ENV_NAME=prod\n")
		res := eval(t, rule(t, "FND-ENV-001", d, p), input())
		wantKey(t, res, "shared-resourcegroup:dev+prod:AZURE_RESOURCE_GROUP")
	},
	"literal-resourcegroup-with-two-environments": func(t *testing.T) {
		d, p := two(t, sub+"AZURE_RESOURCE_GROUP=rg-dev\n", sub+"AZURE_RESOURCE_GROUP=rg-prod\n")
		y := yamlOf(t, "name: p\nresourceGroup: rg-shared\nservices:\n  api:\n    host: containerapp\n    project: ./src\n    resourceGroup: rg-api\n")
		res := eval(t, rule(t, "FND-ENV-001", d, p), input(withYAML(y)))
		wantKey(t, res, "literal-resourcegroup:project")
		wantKey(t, res, "literal-resourcegroup:service:api")
		wantKey(t, res, "shared-resourcegroup:dev+prod:project")
		for _, r := range res {
			if r.Finding != nil && r.Finding.Location.Line == 0 && strings.HasPrefix(r.Finding.Key, "literal") {
				t.Fatal("literal finding should locate the azure.yaml line")
			}
		}
	},
	// negative
	"env-name-in-resourcegroup": func(t *testing.T) {
		d, p := two(t, sub+"AZURE_RESOURCE_GROUP=rg-dev\nAZURE_ENV_NAME=dev\n", sub+"AZURE_RESOURCE_GROUP=rg-prod\nAZURE_ENV_NAME=prod\n")
		y := yamlOf(t, "name: p\nresourceGroup: rg-${AZURE_ENV_NAME}\n")
		wantPass(t, eval(t, rule(t, "FND-ENV-001", d, p), input(withYAML(y))))
	},
	"distinct-subscriptions-same-group-name": func(t *testing.T) {
		d, p := two(t, "AZURE_SUBSCRIPTION_ID=sub-a\nAZURE_RESOURCE_GROUP=rg-app\n", "AZURE_SUBSCRIPTION_ID=sub-b\nAZURE_RESOURCE_GROUP=rg-app\n")
		wantPass(t, eval(t, rule(t, "FND-ENV-001", d, p), input()))
	},
	// skipped (ENV-001, 002, 004 share these names)
	"single-environment-visible": func(t *testing.T) {
		d := pe(t, "dev", sub+"AZURE_RESOURCE_GROUP=rg-app\n")
		for _, id := range []string{"FND-ENV-001", "FND-ENV-002", "FND-ENV-004"} {
			res := eval(t, rule(t, id, d), input(tiers, func(in *model.Input) { in.Project.Environments = []string{"dev"} }))
			wantSkip(t, res, rules.ReasonNotApplicable, "single-environment-visible")
		}
	},
	"env-files-not-in-workspace": func(t *testing.T) {
		d := pe(t, "dev", sub+"AZURE_RESOURCE_GROUP=rg-app\n")
		res := eval(t, rule(t, "FND-ENV-001", d), input()) // azd lists dev and prod; only dev is readable
		wantSkip(t, res, rules.ReasonNotApplicable, "env-files-not-in-workspace")
		if res[0].Skip.MissingCapability == "" {
			t.Fatal("missing capability not named")
		}
	},

	// FND-ENV-002
	"identical-project-endpoint-in-dev-and-prod": func(t *testing.T) {
		ep := "AZURE_AI_PROJECT_ENDPOINT=https://acct.services.ai.azure.com/api/projects/p\n"
		d, p := two(t, ep+"AZURE_ENV_NAME=dev\n", ep+"AZURE_ENV_NAME=prod\n")
		wantKey(t, eval(t, rule(t, "FND-ENV-002", d, p), input(tiers)), "copied-value:AZURE_AI_PROJECT_ENDPOINT:prod+dev")
	},
	"identical-key-vault-name-across-environments": func(t *testing.T) {
		d, p := two(t, "AZURE_KEY_VAULT_NAME=kv-shared\nAZURE_ENV_NAME=dev\n", "AZURE_KEY_VAULT_NAME='kv-shared'\nAZURE_ENV_NAME=prod\n")
		wantKey(t, eval(t, rule(t, "FND-ENV-002", d, p), input(tiers)), "copied-value:AZURE_KEY_VAULT_NAME:prod+dev")
	},
	"environment-specific-values": func(t *testing.T) {
		d, p := two(t, "AZURE_KEY_VAULT_NAME=kv-dev\nAZURE_ENV_NAME=dev\nAZURE_LOCATION=westeurope\nEMPTY_NAME=\n", "AZURE_KEY_VAULT_NAME=kv-prod\nAZURE_ENV_NAME=prod\nAZURE_LOCATION=westeurope\nEMPTY_NAME=\n")
		wantPass(t, eval(t, rule(t, "FND-ENV-002", d, p), input(tiers)))
	},
	"shared-value-suppressed-with-reason": func(t *testing.T) {
		d, p := two(t, "AZURE_KEY_VAULT_NAME=kv-shared\nAZURE_ENV_NAME=dev\n", "AZURE_KEY_VAULT_NAME=kv-shared\nAZURE_ENV_NAME=prod\n")
		res := eval(t, rule(t, "FND-ENV-002", d, p), input(tiers))
		allSuppressed(t, applySuppression(t, res, "FND-ENV-002", "AZURE_KEY_VAULT_NAME"))
	},

	// FND-ENV-003
	"prod-project-endpoint-is-dev-project": func(t *testing.T) {
		d, p := two(t, "AZURE_AI_PROJECT_ENDPOINT=https://dev-acct.services.ai.azure.com/api/projects/dev\n", "AZURE_AI_PROJECT_ENDPOINT=https://DEV-acct.services.ai.azure.com/api/projects/other\n")
		wantKey(t, eval(t, rule(t, "FND-ENV-003", d, p), input(tiers)), "prod-ref:AZURE_AI_PROJECT_ENDPOINT:prod->dev")
	},
	"prod-parameter-references-dev-resource-group-id": func(t *testing.T) {
		d, p := two(t, "AZURE_SUBSCRIPTION_ID=sub-1\nAZURE_RESOURCE_GROUP=rg-dev\n", "AZURE_SUBSCRIPTION_ID=sub-1\nAZURE_RESOURCE_GROUP=rg-prod\n")
		y := yamlOf(t, "name: p\nservices:\n  api:\n    host: containerapp\n    project: ./src\n    config:\n      storage: /subscriptions/sub-1/resourceGroups/RG-DEV/providers/Microsoft.Storage/storageAccounts/st\n")
		res := eval(t, rule(t, "FND-ENV-003", d, p), input(tiers, withYAML(y)))
		wantKey(t, res, "prod-ref-yaml:services.api.config.storage:prod->dev")
		for _, r := range res {
			if r.Finding != nil && r.Finding.Resource.Pointer != "/services/api/config/storage" {
				t.Fatalf("pointer %q", r.Finding.Resource.Pointer)
			}
		}
	},
	"prod-references-prod-resources": func(t *testing.T) {
		d, p := two(t, "AZURE_SUBSCRIPTION_ID=sub-1\nAZURE_RESOURCE_GROUP=rg-dev\nEP=https://dev.services.ai.azure.com\n", "AZURE_SUBSCRIPTION_ID=sub-1\nAZURE_RESOURCE_GROUP=rg-prod\nEP=https://prod.services.ai.azure.com\nID=/subscriptions/sub-1/resourceGroups/rg-prod/providers/X/y/z\n")
		y := yamlOf(t, "name: p\nresourceGroup: rg-${AZURE_ENV_NAME}\n")
		wantPass(t, eval(t, rule(t, "FND-ENV-003", d, p), input(tiers, withYAML(y))))
	},
	"prod-references-shared-explicitly-allowed": func(t *testing.T) {
		d, p := two(t, "SEARCH_ENDPOINT=https://shared.search.windows.net\n", "SEARCH_ENDPOINT=https://shared.search.windows.net\n")
		res := eval(t, rule(t, "FND-ENV-003", d, p), input(tiers))
		allSuppressed(t, applySuppression(t, res, "FND-ENV-003", "SEARCH_ENDPOINT"))
	},
	"environment-tiers-not-configured": func(t *testing.T) {
		d, p := two(t, "A=1\n", "A=1\n")
		for _, id := range []string{"FND-ENV-002", "FND-ENV-003"} {
			wantSkip(t, eval(t, rule(t, id, d, p), input()), rules.ReasonProfileKeyMissing(model.KeyEnvironmentsProduction), "never guessed")
			only := func(in *model.Input) { in.Policy.Environments.Production = []string{"prod"} }
			wantSkip(t, eval(t, rule(t, id, d, p), input(only)), rules.ReasonProfileKeyMissing(model.KeyEnvironmentsNonProduction), "never guessed")
		}
	},
	"sibling-environments-not-visible": func(t *testing.T) {
		p := pe(t, "prod", "A=1\n")
		x := pe(t, "other", "A=1\n")
		for _, id := range []string{"FND-ENV-002", "FND-ENV-003"} {
			wantSkip(t, eval(t, rule(t, id, p, x), input(tiers)), rules.ReasonNotApplicable, "sibling-environments-not-visible")
		}
	},

	// FND-ENV-004
	"key-only-in-one-environment": func(t *testing.T) {
		d, p := two(t, "A=1\nONLY_DEV=1\n", "A=1\nONLY_PROD=1\n")
		res := eval(t, rule(t, "FND-ENV-004", d, p), input())
		wantKey(t, res, "diff:dev+prod")
		ev := res[0].Finding.Evidence
		for _, want := range []string{"ONLY_DEV", "ONLY_PROD", "dev (default)"} {
			if !strings.Contains(ev, want) {
				t.Errorf("evidence lacks %q: %s", want, ev)
			}
		}
	},
	"value-differs-between-environments": func(t *testing.T) {
		d, p := two(t, "A=1\nB_NAME="+canary("d")+"\nREGION=x\n", "A=1\nB_NAME="+canary("p")+"\nREGION=y\n")
		res := eval(t, rule(t, "FND-ENV-004", d, p), input())
		wantKey(t, res, "diff:dev+prod")
		ev := res[0].Finding.Evidence
		if !strings.Contains(ev, "2 differ [B_NAME, REGION], of which 1 environment-bound [B_NAME]") {
			t.Errorf("evidence: %s", ev)
		}
	},
	"identical-environments": func(t *testing.T) {
		d, p := two(t, "A=1\nB=2\n", "B=2\nA=1\n")
		wantPass(t, eval(t, rule(t, "FND-ENV-004", d, p), input()))
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
	impl := map[string]int{}
	for _, r := range env.Rules() {
		impl[r.ID()] = r.Version()
	}
	seen := 0
	for _, r := range all {
		if r.Group != "ENV" {
			continue
		}
		seen++
		if impl[r.ID] != r.Version {
			t.Errorf("%s: catalogue version %d, code %d", r.ID, r.Version, impl[r.ID])
		}
		for _, group := range [][]string{r.Tests.Positive, r.Tests.Negative, r.Tests.Skipped, r.Tests.Uncertain} {
			for _, s := range group {
				if scenarios[s] == nil {
					t.Errorf("%s: catalogue scenario %q has no subtest", r.ID, s)
				}
			}
		}
	}
	if seen != len(impl) {
		t.Fatalf("catalogue has %d ENV rules, code %d", seen, len(impl))
	}
}

// ---- cross-cutting ----

func TestEnvValuesNeverPrinted(t *testing.T) {
	cs := map[string]string{}
	mk := func(tag string) string { cs[tag] = canary(tag); return cs[tag] }
	devText := fmt.Sprintf("%sAZURE_RESOURCE_GROUP=%s\nAZURE_KEY_VAULT_NAME=%s\nAZURE_OPENAI_API_KEY=%s\nONLY_DEV=%s\nEP=https://%s.services.ai.azure.com\nAZURE_ENV_NAME=dev\nSHARED_NAME=%s\n",
		sub, mk("rg"), mk("kv"), mk("key1"), mk("od"), mk("host"), mk("sh"))
	prodText := fmt.Sprintf("%sAZURE_RESOURCE_GROUP=%s\nAZURE_KEY_VAULT_NAME=%s\nAZURE_OPENAI_API_KEY=%s\nONLY_PROD=%s\nEP=https://%s.services.ai.azure.com\nAZURE_ENV_NAME=prod\nSHARED_NAME=%s\n",
		sub, cs["rg"], mk("kv2"), mk("key2"), mk("op"), cs["host"], cs["sh"])
	d, p := two(t, devText, prodText)
	y := yamlOf(t, "name: p\nresourceGroup: "+cs["rg"]+"\n")
	in := input(tiers, withYAML(y))
	var buf strings.Builder
	n := 0
	for _, r := range env.RulesWith(func(*model.Input) []model.Environment { return []model.Environment{d, p} }) {
		res, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range res {
			b, _ := json.Marshal(x)
			buf.Write(b)
			buf.WriteString(fmt.Sprintf("%v %+v %#v", x, x, x))
			if x.Finding != nil {
				n++
				buf.WriteString(fmt.Sprintf("%v%+v", *x.Finding, x.Finding))
			}
			if x.Skip != nil {
				buf.WriteString(fmt.Sprintf("%+v", *x.Skip))
			}
		}
	}
	if n < 4 {
		t.Fatalf("test input too weak: %d findings", n)
	}
	buf.WriteString(d.String() + p.String() + fmt.Sprintf("%v %+v %#v", d, p, d))
	out := buf.String()
	for tag, c := range cs {
		if strings.Contains(out, c) {
			t.Errorf("value of canary %q leaked", tag)
		}
	}
	// the secret-bearing key name may appear; its value may not
	if strings.Contains(out, "canary") {
		t.Error("a canary fragment leaked")
	}
}

func TestDeterministic(t *testing.T) {
	d, p := two(t, sub+"AZURE_RESOURCE_GROUP=rg\nA_NAME=x\nONLY=1\n", sub+"AZURE_RESOURCE_GROUP=rg\nA_NAME=x\n")
	var first string
	for i := range 5 {
		var all []rules.Result
		for _, r := range env.RulesWith(func(*model.Input) []model.Environment { return []model.Environment{p, d} }) {
			res, err := r.Evaluate(context.Background(), input(tiers))
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, res...)
		}
		b, _ := json.Marshal(all)
		if i == 0 {
			first = string(b)
		} else if first != string(b) {
			t.Fatal("not deterministic")
		}
	}
}

func TestDefaultSourceSkipsCrossEnvironmentRules(t *testing.T) {
	e := pe(t, "dev", "A=1\n")
	for _, in := range []*model.Input{input(tiers), input(tiers, func(in *model.Input) { in.Environment = &e })} {
		for _, r := range env.Rules() {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != 1 || res[0].Status != sdk.StatusSkipped {
				t.Errorf("%s: %s", r.ID(), summarise(res))
			}
		}
	}
	if len(env.RulesWith(nil)) != 4 {
		t.Fatal("nil source should default")
	}
}

func TestEnvironmentsAreDedupedAndSubstitution(t *testing.T) {
	d := pe(t, "dev", sub+"AZURE_RESOURCE_GROUP=rg-x\n")
	p := pe(t, "prod", sub+"AZURE_RESOURCE_GROUP=rg-y\n")
	// duplicates in the source do not produce a pair of an environment with itself
	r := rule(t, "FND-ENV-001", d, d, p)
	wantPass(t, eval(t, r, input()))

	// ${VAR:-default}, ${VAR-default} and ${VAR} expansion; blank result falls through to no group
	for _, tc := range []struct {
		yaml string
		want bool
	}{
		{"name: p\nresourceGroup: ${MISSING:-rg-fixed}\n", true},
		{"name: p\nresourceGroup: ${MISSING-rg-fixed}\n", true},
		{"name: p\nresourceGroup: ${MISSING}\n", false},
		{"name: p\nresourceGroup: ${AZURE_ENV_NAME}-rg\n", false},
	} {
		res := eval(t, rule(t, "FND-ENV-001", d, p), input(withYAML(yamlOf(t, tc.yaml))))
		got := false
		for _, x := range res {
			if x.Finding != nil && strings.HasPrefix(x.Finding.Key, "shared-resourcegroup:dev+prod:project") {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("%q: shared=%v want %v (%s)", tc.yaml, got, tc.want, summarise(res))
		}
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, p := two(t, "A=1\n", "A=2\n")
	in := input(tiers)
	for _, r := range env.RulesWith(func(*model.Input) []model.Environment { return []model.Environment{d, p} }) {
		if r.ID() == "FND-ENV-002" {
			continue // identical-key scan is bounded and does not block
		}
		if _, err := r.Evaluate(ctx, in); err == nil {
			t.Errorf("%s: want cancellation error", r.ID())
		}
	}
}

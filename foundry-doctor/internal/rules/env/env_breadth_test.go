package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	rgDev  = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/rg-dev"
	kvID   = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/rg-shared/providers/Microsoft.KeyVault/vaults/kv-shared"
	prjEnd = "https://shared.services.ai.azure.com/api/projects/p"
)

func TestTierFromName(t *testing.T) {
	cases := map[string]string{
		"prod": tierProd, "production": tierProd, "prd": tierProd, "PROD": tierProd, "contoso-prod-eu": tierProd, "app_prd": tierProd,
		"dev": tierNonProd, "development": tierNonProd, "test": tierNonProd, "stage": tierNonProd, "staging": tierNonProd, "uat": tierNonProd, "qa": tierNonProd, "myapp-dev": tierNonProd,
		"pre-prod": tierNonProd, "non-prod": tierNonProd, "nonprod-test": tierNonProd,
		"prod-test":   "", // both kinds: not classified
		"preprod":     "", // one token, not in either list
		"production2": "", // token is production2, not production
		"alice":       "", "x": "", "": "",
	}
	for name, want := range cases {
		if got := tierFromName(name); got != want {
			t.Errorf("tierFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestResolveTiersPrecedence(t *testing.T) {
	envs := []Environment{{Name: "prod-eu"}, {Name: "dev"}, {Name: "sandbox"}, {Name: "alice"}, {Name: "qa"}}
	in := &sdk.Input{Policy: pol{
		TierPolicyKey:       map[string]any{"prod-eu": "dev"}, // explicit wins over the name
		policyProduction:    []any{"alice"},                   // lists beat the derivation
		policyNonProduction: []string{"sandbox"},
	}}
	got := resolveTiers(in, envs)
	want := map[string]tierInfo{
		"prod-eu": {tier: "dev"},
		"dev":     {tier: tierNonProd, derived: true},
		"sandbox": {tier: tierNonProd},
		"alice":   {tier: tierProd},
		"qa":      {tier: tierNonProd, derived: true},
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("tier of %s = %+v, want %+v", n, got[n], w)
		}
	}
	if len(resolveTiers(&sdk.Input{}, []Environment{{Name: "zzz"}})) != 0 {
		t.Error("an unclassifiable name must have no tier")
	}
}

func TestENV003WorksWithoutTierPolicyByDerivingFromNames(t *testing.T) {
	prod := Environment{Name: "contoso-prod", ResourceGroup: "rg-shared"}
	dev := Environment{Name: "contoso-dev", ResourceGroup: "RG-Shared"}
	// no policy at all
	res := eval(t, "FND-ENV-003", prov{dev, prod}, &sdk.Input{})
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "prod environment contoso-prod shares resource group") || !strings.Contains(res.Findings[0].Evidence, "derived from the environment name") {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Skips) != 1 || !strings.HasPrefix(res.Skips[0].Reason, report.UncertainPrefix) || !strings.Contains(res.Skips[0].Reason, "contoso-dev=nonprod") || !strings.Contains(res.Skips[0].Reason, "contoso-prod=prod") || !strings.Contains(res.Skips[0].Reason, TierPolicyKey) {
		t.Fatalf("the derivation must be exposed as a note: %+v", res.Skips)
	}
	// distinct targets: a clean pass, no skip
	prod.ResourceGroup = "rg-prod"
	if res := eval(t, "FND-ENV-003", prov{dev, prod}, &sdk.Input{}); len(res.Findings) != 0 || res.Skipped != nil {
		t.Fatalf("distinct targets: %+v", res)
	}
	// the finding key is the same whether the tier came from a name or the policy
	withName := eval(t, "FND-ENV-003", prov{dev, {Name: "contoso-prod", ResourceGroup: "rg-shared"}}, &sdk.Input{})
	withPol := eval(t, "FND-ENV-003", prov{dev, {Name: "contoso-prod", ResourceGroup: "rg-shared"}}, &sdk.Input{Policy: pol{TierPolicyKey: map[string]any{"contoso-prod": "prod", "contoso-dev": "dev"}}})
	if len(withName.Findings) != 1 || len(withPol.Findings) != 1 || withName.Findings[0].Key != withPol.Findings[0].Key {
		t.Fatalf("keys differ: %+v vs %+v", withName.Findings, withPol.Findings)
	}
	if len(withPol.Skips) != 0 {
		t.Errorf("an explicit policy classification needs no derivation note: %+v", withPol.Skips)
	}
}

func TestENV003ExplicitPolicyWinsOverTheName(t *testing.T) {
	a := Environment{Name: "prod-like", ResourceGroup: "rg-x"}
	b := Environment{Name: "real", ResourceGroup: "rg-x"}
	// By name alone, "prod-like" would be prod and "real" would be unclassified.
	policy := pol{TierPolicyKey: map[string]any{"prod-like": "dev", "real": "prod"}}
	res := eval(t, "FND-ENV-003", prov{a, b}, &sdk.Input{Policy: policy})
	if len(res.Findings) != 1 || res.Findings[0].Resource.Name != "real" {
		t.Fatalf("explicit tiers must win: %+v", res)
	}
	// An environment listed only in environments.production is prod whatever its name.
	res = eval(t, "FND-ENV-003", prov{{Name: "alpha", ResourceGroup: "rg-x"}, {Name: "beta-dev", ResourceGroup: "rg-x"}}, &sdk.Input{Policy: pol{policyProduction: []string{"alpha"}}})
	if len(res.Findings) != 1 || res.Findings[0].Resource.Name != "alpha" {
		t.Fatalf("lists: %+v", res)
	}
}

func TestENV003AmbiguousNamesAreUncertain(t *testing.T) {
	res := eval(t, "FND-ENV-003", prov{{Name: "prod-test", ResourceGroup: "a"}, {Name: "dev", ResourceGroup: "a"}}, &sdk.Input{})
	if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, report.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "environment prod-test: no tier") {
		t.Fatalf("res = %+v", res)
	}
}

func TestENV004OneSummaryPerPairWithTopKeys(t *testing.T) {
	a := map[string]string{}
	b := map[string]string{}
	for i := 0; i < 25; i++ {
		a[fmt.Sprintf("K%02d", i)] = "x"
		b[fmt.Sprintf("K%02d", i)] = "y" // 25 differing values
	}
	a["ONLY_A"] = "1"
	b["ONLY_B"] = "1"
	envs := prov{{Name: "dev", Values: a}, {Name: "prod", Values: b}}
	res := eval(t, "FND-ENV-004", envs, &sdk.Input{})
	if len(res.Findings) != 1 {
		t.Fatalf("want one summary finding for the pair, got %d: %+v", len(res.Findings), res.Findings)
	}
	f := res.Findings[0]
	if f.Key != "env-diff-summary:dev,prod" || f.Resource.Name != "dev,prod" {
		t.Errorf("key/resource = %q / %q", f.Key, f.Resource.Name)
	}
	for _, want := range []string{"differ in 27 key(s)", "1 only in dev", "1 only in prod", "25 with different values", "first 10:", "and 17 more", "K00 (differs between dev and prod)", "K09 ("} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("evidence %q lacks %q", f.Evidence, want)
		}
	}
	if strings.Contains(f.Evidence, "K10") {
		t.Errorf("more than %d keys listed: %s", MaxDiffKeys, f.Evidence)
	}
	// Rotating every value keeps the key (the fingerprint input) unchanged.
	for k := range b {
		b[k] = "rotated"
	}
	res2 := eval(t, "FND-ENV-004", prov{{Name: "dev", Values: a}, {Name: "prod", Values: b}}, &sdk.Input{})
	if len(res2.Findings) != 1 || res2.Findings[0].Key != f.Key {
		t.Fatalf("key changed: %+v", res2.Findings)
	}
}

func TestENV004ThreeEnvironmentsGiveOneFindingPerPair(t *testing.T) {
	envs := prov{
		{Name: "a", Values: vals("X", "1")},
		{Name: "b", Values: vals("X", "2")},
		{Name: "c", Values: vals("X", "3")},
	}
	res := eval(t, "FND-ENV-004", envs, &sdk.Input{})
	if len(res.Findings) != 3 {
		t.Fatalf("pairs: %+v", res.Findings)
	}
	seen := map[string]bool{}
	for _, f := range res.Findings {
		seen[f.Key] = true
	}
	for _, k := range []string{"env-diff-summary:a,b", "env-diff-summary:a,c", "env-diff-summary:b,c"} {
		if !seen[k] {
			t.Errorf("missing %s", k)
		}
	}
	// Identical pair: no finding; secrets never appear.
	res = eval(t, "FND-ENV-004", prov{{Name: "a", Values: vals("API_KEY", "s1")}, {Name: "b", Values: vals("API_KEY", "s2")}}, &sdk.Input{})
	if len(res.Findings) != 1 || strings.Contains(res.Findings[0].Evidence, "s1") || strings.Contains(res.Findings[0].Evidence, "s2") {
		t.Fatalf("secret evidence: %+v", res.Findings)
	}
}

func TestENV001And002CompareAnyTwoOfSeveralEnvironments(t *testing.T) {
	res := eval(t, "FND-ENV-001", prov{
		{Name: "a", Subscription: "s", ResourceGroup: "rg1"},
		{Name: "b", Subscription: "s", ResourceGroup: "rg2"},
		{Name: "c", Subscription: "s", ResourceGroup: "RG2"},
	}, &sdk.Input{})
	if len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "b, c") {
		t.Fatalf("001: %+v", res)
	}
	res = eval(t, "FND-ENV-002", prov{
		{Name: "a", Values: vals("AZURE_ENV_NAME", "a", "KV_ID", kvID)},
		{Name: "b", Values: vals("AZURE_ENV_NAME", "b", "KV_ID", "/subscriptions/1/resourceGroups/other")},
		{Name: "c", Values: vals("AZURE_ENV_NAME", "c", "KV_ID", kvID)},
	}, &sdk.Input{})
	if len(res.Findings) != 1 || res.Findings[0].Key != "identical-value:KV_ID:a,c" {
		t.Fatalf("002: %+v", res)
	}
}

func TestNotApplicableNamesDeclaredEnvironments(t *testing.T) {
	in := &sdk.Input{Policy: pol{policyProduction: []string{"prod"}, policyDevelopment: []any{"dev"}, TierPolicyKey: map[string]any{"qa": "test"}}}
	res := eval(t, "FND-ENV-003", prov{{Name: "dev"}}, in)
	if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipNotApplicable) || !strings.Contains(res.Skipped.Reason, "prod, qa") || strings.Contains(res.Skipped.Reason, "dev,") {
		t.Fatalf("reason = %+v", res.Skipped)
	}
	// Nothing declared: no hint, the reason is still not-applicable.
	res = eval(t, "FND-ENV-003", prov{{Name: "dev"}}, &sdk.Input{})
	if res.Skipped == nil || strings.Contains(res.Skipped.Reason, "declares") {
		t.Fatalf("reason = %+v", res.Skipped)
	}
	// Many declared names are bounded.
	var many []string
	for i := 0; i < 12; i++ {
		many = append(many, fmt.Sprintf("e%02d", i))
	}
	res = eval(t, "FND-ENV-003", prov{{Name: "dev"}}, &sdk.Input{Policy: pol{policyProduction: many}})
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "and 4 more") {
		t.Fatalf("reason = %+v", res.Skipped)
	}
}

// ---- parameter-file environments -------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func paramFile(kv ...string) string {
	var b strings.Builder
	b.WriteString(`{"$schema":"x","contentVersion":"1.0.0.0","parameters":{`)
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:{\"value\":%s}", kv[i], kv[i+1])
	}
	b.WriteString("}}")
	return b.String()
}

func discover(t *testing.T, dir string) []Environment {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	return ParameterFileEnvironments(root, "infra")
}

func TestParameterFileEnvironmentsDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "infra/main.parameters.json", paramFile("kv", `"`+kvID+`"`))                         // the azd file: not an environment
	writeFile(t, dir, "infra/main.parameters.dev.json", paramFile("kv", `"`+kvID+`"`, "plain", `"hello"`)) // pattern 1
	writeFile(t, dir, "infra/main.prod.parameters.json", paramFile("kv", `"`+kvID+`"`, "n", `7`))          // pattern 2
	writeFile(t, dir, "infra/main.qa.bicepparam", "using './main.bicep'\nparam x = 'y'\n")                 // never parsed
	writeFile(t, dir, "infra/other.parameters.dev.json", paramFile("kv", `"`+kvID+`"`))                    // not the entry template
	writeFile(t, dir, "infra/main.bad name.parameters.json", "{}")                                         // invalid environment name
	envs := discover(t, dir)
	byName := map[string]Environment{}
	for _, e := range envs {
		byName[e.Name] = e
	}
	if len(envs) != 3 || byName["dev"].Status != StatusOK || byName["prod"].Status != StatusOK || byName["qa"].Status != StatusNotRead {
		t.Fatalf("envs = %+v", envs)
	}
	for _, e := range envs {
		if e.Origin != OriginParameterFile || !e.soft() {
			t.Errorf("%s origin = %q", e.Name, e.Origin)
		}
	}
	if byName["dev"].Values["kv"] != kvID || byName["dev"].Values["AZURE_ENV_NAME"] != "dev" {
		t.Errorf("dev values = %v", byName["dev"].Values)
	}
	if _, kept := byName["dev"].Values["plain"]; kept {
		t.Error("a non-identifier parameter value must not be retained")
	}
}

func TestParameterFileEnvironmentsAreSafe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "infra/main.parameters.a.json", "{not json")
	writeFile(t, dir, "infra/main.parameters.big.json", strings.Repeat(" ", maxParamFileBytes+10))
	writeFile(t, dir, "infra/main.parameters.refs.json", paramFile(
		"ph", `"${AZURE_KV}"`,
		"expr", `"[parameters('x')]"`,
		"secretRef", `{"reference":{"keyVault":{"id":"`+kvID+`"},"secretName":"s"}}`,
		"conn", `"Endpoint=https://x.azure.com;Key=abc"`,
		"sas", `"https://x.blob.core.windows.net/c?sig=abcdef"`,
		"ok", `"`+prjEnd+`"`))
	// A symlink named like a parameter file is never read.
	writeFile(t, dir, "elsewhere.json", paramFile("kv", `"`+kvID+`"`))
	if err := os.Symlink(filepath.Join(dir, "elsewhere.json"), filepath.Join(dir, "infra", "main.parameters.link.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	byName := map[string]Environment{}
	for _, e := range discover(t, dir) {
		byName[e.Name] = e
	}
	if byName["a"].Status != StatusUnparseable || byName["big"].Status != StatusOversize {
		t.Errorf("statuses: %+v %+v", byName["a"], byName["big"])
	}
	if _, ok := byName["link"]; ok {
		t.Error("a symlinked parameter file was read")
	}
	refs := byName["refs"]
	if len(refs.Values) != 2 || refs.Values["ok"] != prjEnd {
		t.Errorf("only the endpoint and the name may remain: %v", refs.Values)
	}
	for k, v := range refs.Values {
		if strings.Contains(v, "sig=") || strings.Contains(v, "Key=") {
			t.Errorf("secret-shaped value kept under %s", k)
		}
	}
	// Missing infra directory or nil root: nothing, no panic.
	if got := discover(t, t.TempDir()); len(got) != 0 {
		t.Errorf("empty project: %+v", got)
	}
	if ParameterFileEnvironments(nil, "infra") != nil {
		t.Error("nil root")
	}
}

type fixedEnvs []Environment

func (f fixedEnvs) Environments() []Environment { return append([]Environment(nil), f...) }

func TestWithParameterFilesMergesWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "infra/main.parameters.dev.json", paramFile("kv", `"`+kvID+`"`))
	writeFile(t, dir, "infra/main.parameters.prod.json", paramFile("kv", `"`+kvID+`"`))
	base := fixedEnvs{{Name: "dev", Values: vals("AZURE_ENV_NAME", "dev")}} // dev exists as an azd env
	p := WithParameterFiles(base, dir, "infra")
	got := p.Environments()
	if len(got) != 2 || got[0].Name != "dev" || got[0].Origin != OriginAzdEnv || got[1].Name != "prod" || got[1].Origin != OriginParameterFile {
		t.Fatalf("envs = %+v", got)
	}
	if WithParameterFiles(base, "", "infra").Environments()[0].Name != "dev" {
		t.Error("an empty dir must return the base provider")
	}
	if WithParameterFiles(nil, dir, "infra") != nil {
		t.Error("a nil base stays nil")
	}
}

func TestParameterFileEnvironmentsFeedTheRulesAsNotesNeverFindings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "infra/main.parameters.dev.json", paramFile("kv", `"`+kvID+`"`, "rg", `"`+rgDev+`"`))
	writeFile(t, dir, "infra/main.parameters.prod.json", paramFile("kv", `"`+kvID+`"`, "rg", `"`+rgDev+`"`))
	p := WithParameterFiles(fixedEnvs{}, dir, "infra")

	// ENV-003: prod and dev (from the names) share two identifiers: a note, no finding.
	res := eval(t, "FND-ENV-003", p, &sdk.Input{})
	if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, report.UncertainPrefix) ||
		!strings.Contains(res.Skipped.Reason, "UNVERIFIED") || !strings.Contains(res.Skipped.Reason, "kv, rg") || strings.Contains(res.Skipped.Reason, "00000000-0000") {
		t.Fatalf("003: %+v", res)
	}
	// ENV-002: identical identifiers in two parameter files: a note naming keys, no finding, no value.
	res = eval(t, "FND-ENV-002", p, &sdk.Input{})
	if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "UNVERIFIED") || strings.Contains(res.Skipped.Reason, "00000000-0000") {
		t.Fatalf("002: %+v", res)
	}
	// ENV-001 and ENV-004 do not compare parameter files: uncertain, never a pass or a finding.
	for _, id := range []string{"FND-ENV-001", "FND-ENV-004"} {
		res = eval(t, id, p, &sdk.Input{})
		if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, report.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "fewer than two") {
			t.Fatalf("%s: %+v", id, res)
		}
	}
	// A not-read bicepparam environment is named, not hidden.
	writeFile(t, dir, "infra/main.uat.bicepparam", "using './main.bicep'\n")
	res = eval(t, "FND-ENV-002", WithParameterFiles(fixedEnvs{}, dir, "infra"), &sdk.Input{})
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "environment uat: parameter file not read") {
		t.Fatalf("bicepparam: %+v", res)
	}
}

func TestMixedAzdAndParameterFileEnvironments(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "infra/main.parameters.prod.json", paramFile("searchId", `"`+kvID+`"`))
	base := fixedEnvs{{Name: "dev", Values: vals("AZURE_ENV_NAME", "dev", "AZURE_KEYVAULT_ID", kvID)}}
	res := eval(t, "FND-ENV-003", WithParameterFiles(base, dir, "infra"), &sdk.Input{})
	if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "searchId") || !strings.Contains(res.Skipped.Reason, "UNVERIFIED") {
		t.Fatalf("a value shared across sources must be matched by value and reported as a note: %+v", res)
	}
}

package env

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

type prov []Environment

func (p prov) Environments() []Environment { return p }

type pol map[string]any

func (p pol) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func eval(t *testing.T, id string, p Provider, in *sdk.Input) sdk.Result {
	t.Helper()
	for _, r := range RegisterWith(p) {
		if r.ID() == id {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
	}
	t.Fatalf("no rule %s", id)
	return sdk.Result{}
}

func vals(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestRules(t *testing.T) {
	tiersPol := pol{TierPolicyKey: map[string]any{"p": "prod", "d": "dev"}}
	const endpoint = "https://a.services.ai.azure.com"
	tests := []struct {
		name       string
		id         string
		p          Provider
		policy     sdk.Policy
		wantSkip   bool
		wantReason string // substring of the skip reason
		want       int
		contains   string
	}{
		{"001 shared rg", "FND-ENV-001", prov{{Name: "d", Subscription: "S", ResourceGroup: "RG"}, {Name: "p", Subscription: "s", ResourceGroup: "rg"}}, nil, false, "", 1, "share"},
		{"001 distinct", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a"}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, false, "", 0, ""},
		{"001 distinct subscriptions same group", "FND-ENV-001", prov{{Name: "d", Subscription: "s1", ResourceGroup: "a"}, {Name: "p", Subscription: "s2", ResourceGroup: "a"}}, nil, false, "", 0, ""},
		{"001 literal top-level rg", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a", LiteralRGAt: []string{"resourceGroup"}}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, false, "", 1, "(resourceGroup)"},
		{"001 literal service rg is uncertain", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a", LiteralRGAt: []string{"services.web.resourceGroup"}}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, true, "services.web.resourceGroup; the azd schema does not say", 0, ""},
		{"001 literal top-level and service rg", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a", LiteralRGAt: []string{"resourceGroup", "services.web.resourceGroup"}}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, false, "", 1, "(resourceGroup)"},
		{"001 single env is not applicable", "FND-ENV-001", prov{{Name: "d"}}, nil, true, sdk.SkipNotApplicable, 0, ""},
		{"001 no provider skips", "FND-ENV-001", nil, nil, true, sdk.SkipInputUnavailable, 0, ""},
		{"001 no resource group is uncertain", "FND-ENV-001", prov{{Name: "d", Subscription: "s"}, {Name: "p", Subscription: "s", ResourceGroup: "b"}}, nil, true, "environment d: no AZURE_SUBSCRIPTION_ID or AZURE_RESOURCE_GROUP", 0, ""},
		{"001 no subscription is uncertain", "FND-ENV-001", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p", ResourceGroup: "a"}}, nil, true, "no AZURE_SUBSCRIPTION_ID", 0, ""},
		{"001 missing env file is uncertain", "FND-ENV-001", prov{{Name: "d", Subscription: "s", ResourceGroup: "a"}, {Name: "p", Status: StatusMissing}}, nil, true, "environment p: .env missing", 0, ""},
		{"001 one usable of three still reports finding", "FND-ENV-001", prov{{Name: "a", Subscription: "s", ResourceGroup: "r"}, {Name: "b", Subscription: "s", ResourceGroup: "r"}, {Name: "c", Status: StatusOversize}}, nil, false, "", 1, "share"},

		{"002 shared endpoint", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "X_ENDPOINT", endpoint)}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "X_ENDPOINT", endpoint)}}, nil, false, "", 1, "fingerprint"},
		{"002 shared arm id value", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "FOO", "/subscriptions/1/resourceGroups/r")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "FOO", "/subscriptions/1/resourceGroups/r")}}, nil, false, "", 1, "FOO"},
		{"002 different", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "X_ID", "1")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "X_ID", "2")}}, nil, false, "", 0, ""},
		{"002 not env bound", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "FOO", "1")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "FOO", "1")}}, nil, false, "", 0, ""},
		{"002 tenant id is not reported", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "AZURE_TENANT_ID", "t")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "AZURE_TENANT_ID", "t")}}, nil, false, "", 0, ""},
		{"002 principal id is not reported", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "AZURE_PRINCIPAL_ID", "x", "APP_PRINCIPAL_ID", "y")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "AZURE_PRINCIPAL_ID", "x", "APP_PRINCIPAL_ID", "y")}}, nil, false, "", 0, ""},
		{"002 model deployment name is not reported", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "MODEL_DEPLOYMENT_NAME", "gpt-4o", "AZURE_AI_MODEL_NAME", "gpt-4o")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "MODEL_DEPLOYMENT_NAME", "gpt-4o", "AZURE_AI_MODEL_NAME", "gpt-4o")}}, nil, false, "", 0, ""},
		{"002 weak name key is uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "SEARCH_SERVICE_NAME", "srch")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "SEARCH_SERVICE_NAME", "srch")}}, nil, true, "key SEARCH_SERVICE_NAME", 0, ""},
		{"002 weak id key is uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "CLIENT_ID", "c")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "CLIENT_ID", "c")}}, nil, true, "not known to be environment-bound", 0, ""},
		{"002 equal env names are uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "same", "X_ENDPOINT", endpoint)}, {Name: "p", Values: vals("AZURE_ENV_NAME", "same", "X_ENDPOINT", endpoint)}}, nil, true, "AZURE_ENV_NAME is empty or equal", 0, ""},
		{"002 empty env names are uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("X_ENDPOINT", endpoint)}, {Name: "p", Values: vals("X_ENDPOINT", endpoint)}}, nil, true, "environments d and p", 0, ""},
		{"002 one empty env name is uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d")}, {Name: "p", Values: vals("X", "1")}}, nil, true, "AZURE_ENV_NAME", 0, ""},
		{"002 truncated env is uncertain", "FND-ENV-002", prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d")}, {Name: "p", Status: StatusTruncated}}, nil, true, "environment p: .env truncated", 0, ""},

		{"003 no policy skips", "FND-ENV-003", prov{{Name: "d"}, {Name: "p"}}, nil, true, sdk.SkipMissingPolicyKey(TierPolicyKey), 0, ""},
		{"003 shared rg", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "RG"}, {Name: "p", ResourceGroup: "rg"}}, tiersPol, false, "", 1, "prod environment p"},
		{"003 distinct", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p", ResourceGroup: "b"}}, tiersPol, false, "", 0, ""},
		{"003 prod without targets is uncertain", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p"}}, tiersPol, true, "environment p: no resource group", 0, ""},
		{"003 untiered environment is uncertain", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p", ResourceGroup: "b"}, {Name: "x", ResourceGroup: "c"}}, tiersPol, true, "environment x: no tier", 0, ""},
		{"003 no prod visible is uncertain", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "q", ResourceGroup: "b"}}, pol{TierPolicyKey: map[string]any{"d": "dev", "q": "test"}}, true, "no visible environment is configured as prod", 0, ""},
		{"003 unreadable prod is uncertain", "FND-ENV-003", prov{{Name: "d", ResourceGroup: "a"}, {Name: "p", Status: StatusUnparseable}}, tiersPol, true, "environment p: .env unparseable", 0, ""},

		{"002 single env is not applicable", "FND-ENV-002", prov{{Name: "d"}}, nil, true, sdk.SkipNotApplicable, 0, ""},
		{"003 single env is not applicable", "FND-ENV-003", prov{{Name: "d"}}, nil, true, sdk.SkipNotApplicable, 0, ""},
		{"004 single env is not applicable", "FND-ENV-004", prov{{Name: "d"}}, nil, true, sdk.SkipNotApplicable, 0, ""},
		{"004 no environments is not applicable", "FND-ENV-004", prov{}, nil, true, sdk.SkipNotApplicable, 0, ""},

		// Digested values keep the shape classified before digesting (R3).
		{"003 digested resource id shared under KEYVAULT key", "FND-ENV-003", prov{
			{Name: "d", Values: vals("AZURE_KEYVAULT_ID", "sha256:abc"), Shapes: map[string]Shape{"AZURE_KEYVAULT_ID": ShapeResourceID}},
			{Name: "p", Values: vals("AZURE_KEYVAULT_ID", "sha256:abc"), Shapes: map[string]Shape{"AZURE_KEYVAULT_ID": ShapeResourceID}}}, tiersPol, false, "", 1, "prod environment p shares resource identifier"},
		{"003 digested plain secret is not a target", "FND-ENV-003", prov{
			{Name: "d", ResourceGroup: "a", Values: vals("API_KEY", "sha256:abc")}, {Name: "p", ResourceGroup: "b", Values: vals("API_KEY", "sha256:abc")}}, tiersPol, false, "", 0, ""},
		{"002 digested resource id under KEY name", "FND-ENV-002", prov{
			{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "AZURE_KEYVAULT_ID", "sha256:abc"), Shapes: map[string]Shape{"AZURE_KEYVAULT_ID": ShapeResourceID}},
			{Name: "p", Values: vals("AZURE_ENV_NAME", "p", "AZURE_KEYVAULT_ID", "sha256:abc"), Shapes: map[string]Shape{"AZURE_KEYVAULT_ID": ShapeResourceID}}}, nil, false, "", 1, "AZURE_KEYVAULT_ID"},
		{"002 digested plain secret is not reported", "FND-ENV-002", prov{
			{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "API_KEY", "sha256:abc")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "API_KEY", "sha256:abc")}}, nil, false, "", 0, ""},

		{"004 only in", "FND-ENV-004", prov{{Name: "d", Values: vals("A", "1")}, {Name: "p", Values: map[string]string{}}}, nil, false, "", 1, "only in d"},
		{"004 secret by name", "FND-ENV-004", prov{{Name: "d", Values: vals("API_KEY", "s1")}, {Name: "p", Values: vals("API_KEY", "s2")}}, nil, false, "", 1, "differs between"},
		{"004 same", "FND-ENV-004", prov{{Name: "d", Values: vals("A", "1")}, {Name: "p", Values: vals("A", "1")}}, nil, false, "", 0, ""},
		{"004 unreadable env does not read as identical", "FND-ENV-004", prov{{Name: "d", Values: vals("A", "1")}, {Name: "p", Status: StatusUnreadable}}, nil, true, "environment p: .env unreadable", 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := eval(t, tc.id, tc.p, &sdk.Input{Policy: tc.policy})
			if (res.Skipped != nil) != tc.wantSkip {
				t.Fatalf("skipped=%v want %v (findings %+v)", res.Skipped, tc.wantSkip, res.Findings)
			}
			if res.Skipped != nil {
				if res.Skipped.RuleID != tc.id {
					t.Fatalf("skip rule id %q", res.Skipped.RuleID)
				}
				if !strings.Contains(res.Skipped.Reason, tc.wantReason) {
					t.Fatalf("reason %q lacks %q", res.Skipped.Reason, tc.wantReason)
				}
				if tc.wantReason != sdk.SkipInputUnavailable && tc.wantReason != sdk.SkipNotApplicable && !strings.HasPrefix(res.Skipped.Reason, report.UncertainPrefix) && !strings.HasPrefix(res.Skipped.Reason, sdk.SkipProfileKeyPrefix) {
					t.Fatalf("reason %q is neither uncertain nor a named input skip", res.Skipped.Reason)
				}
			}
			if len(res.Findings) != tc.want {
				t.Fatalf("findings=%d want %d: %+v", len(res.Findings), tc.want, res.Findings)
			}
			if tc.contains != "" && !strings.Contains(res.Findings[0].Evidence, tc.contains) {
				t.Fatalf("evidence %q lacks %q", res.Findings[0].Evidence, tc.contains)
			}
		})
	}
}

func TestNilInputSkips(t *testing.T) {
	for _, r := range RegisterWith(prov{{Name: "a"}, {Name: "b"}}) {
		res, err := r.Evaluate(context.Background(), nil)
		if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable+": ") {
			t.Errorf("%s: %+v %v", r.ID(), res, err)
		}
	}
}

func TestUncertainReasonIsBoundedAndHasNoValues(t *testing.T) {
	var envs prov
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		envs = append(envs, Environment{Name: n, Status: StatusMissing, Values: vals("API_KEY", "topsecret1")})
	}
	res := eval(t, "FND-ENV-004", envs, &sdk.Input{})
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "and 4 more") || strings.Contains(res.Skipped.Reason, "topsecret") {
		t.Fatalf("%+v", res.Skipped)
	}
}

func TestNoRawValuesInEvidence(t *testing.T) {
	p := prov{{Name: "d", Values: vals("API_KEY", "topsecret1", "N", "plainval1")}, {Name: "p", Values: vals("API_KEY", "topsecret2", "N", "plainval2")}}
	res := eval(t, "FND-ENV-004", p, &sdk.Input{})
	for _, f := range res.Findings {
		for _, v := range []string{"topsecret", "plainval"} {
			if strings.Contains(f.Evidence, v) {
				t.Fatalf("leaked %s", v)
			}
		}
	}
	q := prov{{Name: "d", Values: vals("AZURE_ENV_NAME", "d", "SEARCH_SERVICE_NAME", "plainval1")}, {Name: "p", Values: vals("AZURE_ENV_NAME", "p", "SEARCH_SERVICE_NAME", "plainval1")}}
	r2 := eval(t, "FND-ENV-002", q, &sdk.Input{})
	if r2.Skipped == nil || strings.Contains(r2.Skipped.Reason, "plainval") {
		t.Fatalf("uncertain reason leaks value: %+v", r2.Skipped)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		k, v string
		want int
	}{
		{"AZURE_ENV_NAME", "x", classNone},
		{"AZURE_SUBSCRIPTION_ID", "x", classStrong},
		{"AZURE_RESOURCE_GROUP", "x", classStrong},
		{"AZURE_AI_PROJECT_ENDPOINT", "x", classStrong},
		{"ANY", "/subscriptions/1/x", classStrong},
		{"AZURE_TENANT_ID", "x", classNone},
		{"AZURE_PRINCIPAL_ID", "x", classNone},
		{"AZURE_OPENAI_DEPLOYMENT_NAME", "x", classNone},
		{"MODEL_DEPLOYMENT_NAME", "x", classNone},
		{"AZURE_AI_MODEL_NAME", "x", classNone},
		{"KEYVAULT_NAME", "x", classWeak},
		{"CLIENT_ID", "x", classWeak},
		{"FOO", "x", classNone},
	}
	for _, tc := range tests {
		if got := classify(tc.k, Environment{}.shapeOf(tc.k, tc.v)); got != tc.want {
			t.Errorf("classify(%s)=%d want %d", tc.k, got, tc.want)
		}
	}
}

func TestRegisteredIDsInCatalogue(t *testing.T) {
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range rules {
		for _, ph := range r.Phases {
			if ph == "1" {
				ids[r.ID] = true
			}
		}
	}
	for _, r := range Register() {
		if !ids[r.ID()] {
			t.Errorf("%s not phase 1 in catalogue", r.ID())
		}
	}
}

func TestClassifyShape(t *testing.T) {
	tests := []struct {
		v    string
		want Shape
	}{
		{"/subscriptions/1/resourceGroups/r/providers/Microsoft.KeyVault/vaults/kv", ShapeResourceID},
		{"https://a.services.ai.azure.com/api/projects/p", ShapeFoundryEndpoint},
		{"https://kv.vault.azure.net/", ShapeEndpoint},
		{"https://kv.vault.azure.net/?sig=abc", ShapePlain},
		{"https://user:pw@kv.vault.azure.net/", ShapePlain},
		{"Endpoint=https://x.azure.com;Key=abc", ShapePlain},
		{"http://x.azure.com", ShapePlain},
		{"https://example.com", ShapePlain},
		{"sk-abcdef", ShapePlain},
		{"", ShapePlain},
	}
	for _, tc := range tests {
		if got := ClassifyShape(tc.v); got != tc.want {
			t.Errorf("ClassifyShape(%q)=%q want %q", tc.v, got, tc.want)
		}
	}
}

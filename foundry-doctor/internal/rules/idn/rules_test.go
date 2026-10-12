package idn_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/idn"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	rulesdata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

const (
	live       = "skipped:control-plane-unavailable"
	unresolved = "skipped:unresolved-value:"
)

// scenario is one fixture and the exact sorted outcome signatures a rule must return for it. A catalogue
// scenario uses the catalogue name; the others are extra cases.
type scenario struct {
	name string
	want []string
}

func ruleByID(t *testing.T, id string) rules.Rule {
	t.Helper()
	for _, r := range idn.Rules() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("rule %s is not registered", id)
	return nil
}

func fixtureDir(id string) string {
	return filepath.Join("..", "..", "..", "test", "fixtures", "rules", "idn", "idn"+strings.TrimPrefix(id, "FND-IDN-"))
}

// loadRaw reads a committed ARM JSON fixture. If mutate is non-nil it edits the decoded document first.
func loadRaw(t *testing.T, id, name string, mutate func(doc map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir(id), name+".arm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mutate == nil {
		return raw
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func inputFrom(t *testing.T, raw []byte) *model.Input {
	t.Helper()
	n, err := bicep.Normalise(raw, bicep.Options{File: "main.bicep"})
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	return &model.Input{ARM: n.Template, Profile: rules.ProfileProd}
}

func load(t *testing.T, id, name string) *model.Input {
	t.Helper()
	return inputFrom(t, loadRaw(t, id, name, nil))
}

func evaluate(t *testing.T, r rules.Rule, in *model.Input) []rules.Result {
	t.Helper()
	res, err := r.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for i, x := range res {
		if err := x.Validate(); err != nil {
			t.Fatalf("result %d invalid: %v", i, err)
		}
	}
	return res
}

func sig(x rules.Result) string {
	switch x.Status {
	case sdk.StatusFail, sdk.StatusUncertain:
		return string(x.Status) + ":" + x.Finding.Key
	case sdk.StatusSkipped:
		return "skipped:" + x.Skip.Reason
	}
	return string(x.Status)
}

func sigs(res []rules.Result) []string {
	var out []string
	for _, x := range res {
		out = append(out, sig(x))
	}
	slices.Sort(out)
	return out
}

func sorted(s ...string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// tables holds the expected outcome of every fixture, by rule.
var tables = map[string][]scenario{
	"FND-IDN-001": {
		{"account-without-identity", sorted("fail:identity-missing", "pass")},
		{"project-identity-none", sorted("fail:identity-none", "pass")},
		{"container-app-calling-foundry-without-identity", sorted("fail:identity-missing", "pass", "pass")},
		{"user-assigned-type-with-empty-identity-map", sorted("fail:user-assigned-identities-empty", "pass")},
		{"account-and-project-system-assigned", sorted("pass", "pass")},
		{"workload-user-assigned-with-identity-map", sorted("pass", "pass", "pass")},
		{"identity-type-is-parameter", sorted(unresolved+"parameter", "pass")},
		{"project-defined-outside-template", sorted("pass", "skipped:not-applicable")},
		{"workload-identity-set-by-module-output", sorted("uncertain:identity-from-module-output", "pass", "pass")},
		{"workload-not-calling-foundry", sorted("pass", "pass")},
		{"both-identities", sorted("pass", "pass")},
		{"conditional-account-without-identity", sorted("fail:identity-missing", "pass")},
	},
	"FND-IDN-002": {
		{"search-missing-service-contributor", sorted("fail:search-service-contributor@srch", "pass", live)},
		{"blobstore-without-blob-data-owner-on-agent-container", sorted("fail:storage-blob-data-owner@stor", "pass", live)},
		{"cosmos-operator-without-sql-data-contributor", sorted("fail:cosmos-sql-data-contributor@cosm", "pass", live)},
		{"hosted-agent-acr-without-pull-role", sorted("fail:registry-pull@reg", live)},
		{"all-documented-roles-assigned", sorted("pass", "pass", "pass", "pass", "pass", "pass", "pass", live)},
		{"store-not-referenced-by-capability-settings", sorted("pass", live)},
		{"roles-granted-at-account-scope-containing-store", sorted("pass", "pass", live)},
		{"principal-is-module-output", sorted(unresolved+"principal", "pass", live)},
		{"group-membership-unreadable", sorted(unresolved+"principal", "pass", live)},
		{"live-role-assignments-not-readable", nil}, // uses the all-documented-roles-assigned fixture; see TestLiveHalfSkipped
		{"container-scope-uses-workspace-id-expression", sorted("pass", "uncertain:storage-blob-data-owner@stor", live)},
		{"project-without-identity", sorted("skipped:not-applicable", live)},
		{"role-for-other-principal", sorted("fail:search-index-data-contributor@srch", "fail:search-service-contributor@srch", live)},
		{"user-assigned-project-identity", sorted("pass", "pass", live)},
		{"sql-role-wrong-id", sorted("fail:cosmos-sql-data-contributor@cosm", "pass", live)},
	},
	"FND-IDN-003": {
		{"managed-identity-assignment-without-principal-type", sorted("uncertain:principal-type-missing-same-template")},
		{"principal-id-reads-client-id", sorted("fail:principal-id-is-client-id")},
		{"role-assignment-api-version-2020-04-01-preview", sorted("fail:api-version-too-old")},
		{"principal-type-user-for-same-template-managed-identity", sorted("fail:principal-type-not-service-principal")},
		{"service-principal-type-with-identity-principal-id", sorted("pass")},
		{"user-assignment-with-guid-and-principal-type-user", sorted("pass")},
		{"principal-id-is-parameter", sorted(unresolved + "parameter")},
		{"module-output-principal", sorted(unresolved + "module-output")},
		{"agent-identity-principal-type-choice", sorted("uncertain:agent-principal-type")},
		{"principal-id-not-a-guid", sorted("fail:principal-id-not-guid")},
		{"other-principal-without-principal-type", sorted("uncertain:principal-type-missing")},
		{"principal-type-parameter", sorted(unresolved + "arm-expression")},
		{"api-version-without-principal-type", sorted("uncertain:principal-type-missing")},
	},
	"FND-IDN-004": {
		{"project-identity-contributor-on-resource-group", sorted("fail:contributor-resource-group", live)},
		{"workload-identity-owner-on-subscription", sorted("fail:owner-subscription", live)},
		{"project-identity-user-access-administrator-no-condition", sorted("fail:user-access-administrator-resource", live)},
		{"project-identity-foundry-user-on-account", sorted(live)},
		{"pipeline-identity-contributor-on-project", sorted("pass", live)},
		{"rbac-administrator-with-condition", sorted("pass", live)},
		{"principal-is-parameter", sorted(unresolved+"principal", live)},
		{"live-role-assignments-not-readable", nil}, // see TestLiveHalfSkipped
		{"identity-is-both-application-and-deployment", sorted("uncertain:application-and-deployment-identity", live)},
		{"owner-on-account-resource", sorted("fail:owner-resource", live)},
		{"contributor-on-account-resource", sorted("pass", live)},
		{"contributor-on-subscription", sorted("fail:contributor-subscription", live)},
		{"contributor-on-management-group", sorted("fail:contributor-management-group", live)},
		{"contributor-at-deployment-scope", sorted("fail:contributor-deployment", live)},
		{"rbac-administrator-no-condition", sorted("fail:rbac-administrator-resource", live)},
		{"contributor-scope-unknown", sorted("uncertain:contributor-scope-unknown", live)},
		{"owner-for-account-identity", sorted("fail:owner-resource-group", live)},
		{"role-unresolved-for-app-identity", sorted(unresolved+"role-definition", live)},
		{"literal-principal-owner", sorted(unresolved+"principal", live)},
	},
	"FND-IDN-006": {
		{"http-issuer", sorted("fail:issuer-not-https")},
		{"subject-with-wildcard", sorted("fail:subject-wildcard")},
		{"github-subject-without-entity", sorted("fail:github-subject-invalid")},
		{"aks-subject-with-wildcard-service-account", sorted("fail:subject-wildcard")},
		{"github-environment-subject-https-issuer", sorted("pass")},
		{"github-immutable-subject", sorted("pass")},
		{"aks-exact-namespace-and-service-account", sorted("pass")},
		{"issuer-is-parameter", sorted(unresolved + "parameter")},
		{"app-registration-credential-graph-only", sorted("skipped:not-applicable")},
		{"custom-issuer-subject-format-unknown", sorted("uncertain:subject-format-unknown")},
		{"empty-subject", sorted("fail:subject-empty")},
		{"issuer-without-host", sorted("fail:issuer-no-host")},
		{"issuer-not-a-url", sorted("fail:issuer-not-url")},
		{"gitlab-subject", sorted("pass")},
		{"pull-request-subject", sorted("pass")},
		{"aks-incomplete-subject", sorted("fail:aks-subject-invalid")},
		{"subject-is-parameter", sorted(unresolved + "parameter")},
		{"issuer-missing", sorted("fail:issuer-missing")},
	},
}

// aliases maps a catalogue scenario with no fixture of its own to the fixture it reuses.
var aliases = map[string]map[string]string{
	"FND-IDN-002": {"live-role-assignments-not-readable": "all-documented-roles-assigned"},
	"FND-IDN-004": {"live-role-assignments-not-readable": "project-identity-foundry-user-on-account"},
}

func fixtureFor(id, name string) string {
	if a, ok := aliases[id][name]; ok {
		return a
	}
	return name
}

func TestScenarios(t *testing.T) {
	for id, table := range tables {
		r := ruleByID(t, id)
		for _, sc := range table {
			t.Run(sc.name, func(t *testing.T) {
				in := load(t, id, fixtureFor(id, sc.name))
				got := sigs(evaluate(t, r, in))
				want := sc.want
				if want == nil { // an alias scenario: assert the live skip only
					if !slices.Contains(got, live) {
						t.Fatalf("%s: got %v, want it to contain %s", id, got, live)
					}
					return
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: got  %v\nwant %v", id, got, want)
				}
			})
		}
	}
}

func TestLiveHalfSkipped(t *testing.T) {
	for _, id := range []string{"FND-IDN-002", "FND-IDN-004"} {
		res := evaluate(t, ruleByID(t, id), load(t, id, fixtureFor(id, "live-role-assignments-not-readable")))
		found := false
		for _, x := range res {
			if x.Status == sdk.StatusSkipped && x.Skip.Reason == rules.ReasonControlPlaneUnavailable {
				found = true
				if x.Skip.MissingCapability == "" || x.Skip.Detail == "" || x.Skip.RuleID != id || x.Skip.RuleVersion != 1 {
					t.Fatalf("%s: incomplete skip %+v", id, x.Skip)
				}
			}
		}
		if !found {
			t.Fatalf("%s: no control-plane-unavailable skip", id)
		}
	}
}

// TestScenarioCoverage fails when a catalogue scenario has no matching subtest, and checks that each
// scenario kind behaves as its kind promises.
func TestScenarioCoverage(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]catalog.Rule{}
	for _, m := range cat {
		byID[m.ID] = m
	}
	for id, table := range tables {
		m, ok := byID[id]
		if !ok {
			t.Fatalf("%s is not in the catalogue", id)
		}
		have := map[string]scenario{}
		for _, sc := range table {
			have[sc.name] = sc
		}
		kinds := map[string][]string{"positive": m.Tests.Positive, "negative": m.Tests.Negative, "skipped": m.Tests.Skipped, "uncertain": m.Tests.Uncertain}
		for kind, names := range kinds {
			for _, name := range names {
				sc, ok := have[name]
				if !ok {
					t.Errorf("%s: catalogue %s scenario %q has no subtest", id, kind, name)
					continue
				}
				in := load(t, id, fixtureFor(id, name))
				got := sigs(evaluate(t, ruleByID(t, id), in))
				has := func(prefix string) bool {
					for _, g := range got {
						if strings.HasPrefix(g, prefix) {
							return true
						}
					}
					return false
				}
				switch kind {
				case "positive":
					if !has("fail:") && !has("uncertain:") {
						t.Errorf("%s: positive scenario %q produced no finding: %v", id, name, got)
					}
				case "negative":
					if has("fail:") || has("uncertain:") {
						t.Errorf("%s: negative scenario %q produced a finding: %v", id, name, got)
					}
				case "skipped":
					if !has("skipped:") {
						t.Errorf("%s: skipped scenario %q produced no skip: %v", id, name, got)
					}
				case "uncertain":
					if !has("uncertain:") {
						t.Errorf("%s: uncertain scenario %q produced no uncertain finding: %v", id, name, got)
					}
				}
				_ = sc
			}
		}
	}
}

func TestMetadataMatchesCatalogue(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]catalog.Rule{}
	for _, m := range cat {
		byID[m.ID] = m
	}
	got := idn.Rules()
	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID())
		m, ok := byID[r.ID()]
		if !ok {
			t.Fatalf("%s not in catalogue", r.ID())
		}
		if r.Version() != m.Version {
			t.Errorf("%s: version %d, catalogue %d", r.ID(), r.Version(), m.Version)
		}
		if !rules.NeedsImplementation(m, rules.Phase1) {
			t.Errorf("%s is not a native phase 1 rule", r.ID())
		}
		// Every finding of the rule carries the catalogue recommendation and fix.
		for _, sc := range tables[r.ID()] {
			for _, x := range evaluate(t, r, load(t, r.ID(), fixtureFor(r.ID(), sc.name))) {
				if x.Finding == nil {
					continue
				}
				if strings.Join(strings.Fields(x.Finding.Recommendation), " ") != strings.Join(strings.Fields(m.Recommendation), " ") {
					t.Errorf("%s/%s: recommendation differs from the catalogue", r.ID(), sc.name)
				}
				if strings.TrimSpace(x.Finding.Fix) != strings.TrimSpace(m.Fix) {
					t.Errorf("%s/%s: fix differs from the catalogue", r.ID(), sc.name)
				}
			}
		}
	}
	want := []string{"FND-IDN-001", "FND-IDN-002", "FND-IDN-003", "FND-IDN-004", "FND-IDN-006"}
	if !slices.Equal(ids, want) {
		t.Fatalf("Rules() = %v, want %v", ids, want)
	}
}

func TestNoARMSkipsAsSynthetic(t *testing.T) {
	for _, r := range idn.Rules() {
		for _, in := range []*model.Input{nil, {}, {Project: model.Project{IaC: model.IaCSynthetic}}} {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != 1 || res[0].Status != sdk.StatusSkipped || res[0].Skip.Reason != rules.ReasonSyntheticInfrastructure ||
				res[0].Skip.MissingCapability != "compiled ARM template" {
				t.Fatalf("%s: got %+v", r.ID(), res)
			}
		}
	}
}

func TestEmptyTemplateYieldsNoFindings(t *testing.T) {
	for _, r := range idn.Rules() {
		res := evaluate(t, r, &model.Input{ARM: &model.ARMTemplate{}})
		for _, x := range res {
			if x.Status == sdk.StatusFail || x.Status == sdk.StatusUncertain {
				t.Fatalf("%s: finding on empty template: %+v", r.ID(), x)
			}
		}
	}
}

func TestFindingShape(t *testing.T) {
	for id, table := range tables {
		r := ruleByID(t, id)
		for _, sc := range table {
			in := load(t, id, fixtureFor(id, sc.name))
			for _, x := range evaluate(t, r, in) {
				f := x.Finding
				if f == nil {
					continue
				}
				if f.RuleID != id || f.RuleVersion != 1 || f.Key == "" || f.Evidence == "" || f.Recommendation == "" || f.Fix == "" {
					t.Errorf("%s/%s: incomplete finding %+v", id, sc.name, f)
				}
				if f.Resource.Kind != "arm-resource" || f.Resource.Type == "" || f.Resource.Name == "" || !strings.HasPrefix(f.Resource.Pointer, "/") {
					t.Errorf("%s/%s: bad resource ref %+v", id, sc.name, f.Resource)
				}
				if f.Location.File != "main.bicep" || f.Location.Pointer == "" {
					t.Errorf("%s/%s: bad location %+v", id, sc.name, f.Location)
				}
				if f.Severity != "" || f.Profile != "" || f.Fingerprint != "" {
					t.Errorf("%s/%s: engine-owned field set", id, sc.name)
				}
				if x.Status == sdk.StatusUncertain && f.Confidence != sdk.ConfidenceUncertain {
					t.Errorf("%s/%s: uncertain result with confidence %s", id, sc.name, f.Confidence)
				}
			}
			for _, x := range evaluate(t, r, in) {
				if x.Skip != nil && (x.Skip.RuleID != id || x.Skip.RuleVersion != 1 || x.Skip.MissingCapability == "") {
					t.Errorf("%s/%s: incomplete skip %+v", id, sc.name, x.Skip)
				}
			}
		}
	}
}

func TestDeterminismAndFingerprintStability(t *testing.T) {
	for id, table := range tables {
		r := ruleByID(t, id)
		for _, sc := range table {
			name := fixtureFor(id, sc.name)
			a := evaluate(t, r, load(t, id, name))
			b := evaluate(t, r, load(t, id, name))
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s/%s: output differs between runs", id, sc.name)
			}
			for i := range a {
				if a[i].Finding == nil {
					continue
				}
				f1, f2 := *a[i].Finding, *b[i].Finding
				// A line move and a different profile must not change the fingerprint.
				f2.Location.Line += 40
				f2.Location.Column += 3
				f2.Profile = "foundry-dev"
				f2.Severity = sdk.SeverityInfo
				if findings.Fingerprint(f1) != findings.Fingerprint(f2) {
					t.Errorf("%s/%s: fingerprint not stable", id, sc.name)
				}
				f2.Key += "-other"
				if findings.Fingerprint(f1) == findings.Fingerprint(f2) {
					t.Errorf("%s/%s: fingerprint ignores the key", id, sc.name)
				}
			}
		}
	}
}

// TestRedactionCanary adds a fake secret to the tags and to a free property of every resource of every fixture and
// requires that no result text carries it. The secret is assembled at run time.
func TestRedactionCanary(t *testing.T) {
	canary := "sk-" + strings.Repeat("Ab1", 4) + "-" + "CANARY" + strings.Repeat("9", 8)
	for id, table := range tables {
		r := ruleByID(t, id)
		for _, sc := range table {
			raw := loadRaw(t, id, fixtureFor(id, sc.name), func(doc map[string]any) {
				for _, res := range doc["resources"].([]any) {
					m := res.(map[string]any)
					m["tags"] = map[string]any{"secret": canary}
					if p, ok := m["properties"].(map[string]any); ok {
						p["description"] = canary
						p["connectionString"] = "AccountKey=" + canary
					}
				}
			})
			for _, x := range evaluate(t, r, inputFrom(t, raw)) {
				var text string
				if x.Finding != nil {
					text = fmt.Sprintf("%+v", *x.Finding)
				}
				if x.Skip != nil {
					text += fmt.Sprintf("%+v", *x.Skip)
				}
				if strings.Contains(text, canary) || strings.Contains(text, "AccountKey") {
					t.Fatalf("%s/%s: secret leaked: %s", id, sc.name, text)
				}
				if x.Finding != nil && findings.Redact(x.Finding.Evidence) != x.Finding.Evidence {
					t.Errorf("%s/%s: evidence changes under redaction: %q", id, sc.name, x.Finding.Evidence)
				}
			}
		}
	}
	// The committed canary fixture carries a placeholder; replace it at run time too.
	raw := loadRaw(t, "FND-IDN-006", "secret-canary", nil)
	raw = []byte(strings.ReplaceAll(string(raw), "@@CANARY@@", canary))
	for _, x := range evaluate(t, ruleByID(t, "FND-IDN-006"), inputFrom(t, raw)) {
		if x.Finding != nil && strings.Contains(fmt.Sprintf("%+v", *x.Finding), canary) {
			t.Fatal("secret leaked from canary fixture")
		}
	}
}

func TestConfidenceLoweredByMeta(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.ResourceMeta)
		want   sdk.Confidence
	}{
		{"plain", func(*model.ResourceMeta) {}, sdk.ConfidenceCertain},
		{"may-not-deploy", func(m *model.ResourceMeta) { m.MayNotDeploy = true }, sdk.ConfidenceLikely},
		{"multi-instance", func(m *model.ResourceMeta) { m.MultiInstance = true }, sdk.ConfidenceLikely},
		{"module-not-proven", func(m *model.ResourceMeta) { m.Confidence = string(sdk.ConfidenceUncertain) }, sdk.ConfidenceLikely},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := load(t, "FND-IDN-001", "account-without-identity")
			for i := range in.ARM.Resources {
				tc.mutate(&in.ARM.Resources[i].Meta)
			}
			for _, x := range evaluate(t, ruleByID(t, "FND-IDN-001"), in) {
				if x.Status == sdk.StatusFail && x.Finding.Confidence != tc.want {
					t.Fatalf("confidence %s, want %s", x.Finding.Confidence, tc.want)
				}
			}
		})
	}
}

func TestHandBuiltResourcesHaveNoMeta(t *testing.T) {
	in := &model.Input{ARM: &model.ARMTemplate{Resources: []model.Resource{{
		Type: "Microsoft.CognitiveServices/accounts", Name: "a", Body: map[string]any{},
	}}}}
	res := evaluate(t, ruleByID(t, "FND-IDN-001"), in)
	if len(res) != 2 || res[0].Status != sdk.StatusFail {
		t.Fatalf("got %v", sigs(res))
	}
	if res[0].Finding.Resource.Pointer != "/identity" {
		t.Fatalf("pointer %q", res[0].Finding.Resource.Pointer)
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for id, table := range tables {
		in := load(t, id, fixtureFor(id, table[0].name))
		_, err := ruleByID(t, id).Evaluate(ctx, in)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", id, err)
		}
	}
}

func TestEngineExecutesRulesUnderProfiles(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := rules.NewRegistry(cat, idn.Rules(), rules.Phase1)
	if err != nil {
		t.Fatal(err)
	}
	entries := reg.Executable()
	if len(entries) != 5 {
		t.Fatalf("executable entries = %d, want 5", len(entries))
	}
	in := load(t, "FND-IDN-001", "account-without-identity")
	for profile, want := range map[string]sdk.Severity{rules.ProfileDev: sdk.SeverityWarning, rules.ProfileTest: sdk.SeverityWarning, rules.ProfileProd: sdk.SeverityError} {
		out, err := rules.Execute(context.Background(), in, entries, rules.ExecConfig{Profile: profile, Stamper: stamper{}})
		if err != nil || len(out.Errors) != 0 {
			t.Fatalf("execute: %v %v", err, out.Errors)
		}
		var found bool
		for _, f := range out.Findings {
			if f.RuleID == "FND-IDN-001" {
				found = true
				if f.Severity != want || f.Fingerprint == "" {
					t.Errorf("%s: severity %s fingerprint %q", profile, f.Severity, f.Fingerprint)
				}
			}
		}
		if !found {
			t.Fatalf("%s: no IDN-001 finding", profile)
		}
	}
}

type stamper struct{}

func (stamper) Fingerprint(f sdk.Finding) string { return findings.Fingerprint(f) }
func (stamper) Redact(s string) string           { return findings.Redact(s) }

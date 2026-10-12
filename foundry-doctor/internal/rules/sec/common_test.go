package sec_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	rulesfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func loadCatalog(t testing.TB) map[string]catalog.Rule {
	t.Helper()
	all, err := catalog.Load(context.Background(), rulesfs.FS, rulesfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]catalog.Rule{}
	for _, r := range all {
		m[r.ID] = r
	}
	return m
}

// stamper is a minimal engine stamper: the fingerprint hashes rule, resource and key.
type stamper struct{}

func (stamper) Fingerprint(f sdk.Finding) string {
	return f.RuleID + "|" + f.Resource.Type + "|" + f.Resource.Name + "|" + f.Key
}
func (stamper) Redact(s string) string { return strings.ReplaceAll(s, canary(), "[redacted]") }

func execute(t testing.TB, id string, in *model.Input, profile string) rules.Outcome {
	t.Helper()
	meta := loadCatalog(t)[id]
	out, err := rules.Execute(context.Background(), in,
		[]rules.Entry{{Meta: meta, Impl: ruleByID(t, id)}}, rules.ExecConfig{Profile: profile, Stamper: stamper{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) > 0 {
		t.Fatalf("rule errors: %v", out.Errors)
	}
	return out
}

// TestCatalogueMatch: ID and Version equal the catalogue; FND-SEC-014 is not registered.
func TestCatalogueMatch(t *testing.T) {
	cat := loadCatalog(t)
	var ids []string
	for _, r := range sec.Rules() {
		m, ok := cat[r.ID()]
		if !ok {
			t.Fatalf("%s is not in the catalogue", r.ID())
		}
		if m.Version != r.Version() {
			t.Errorf("%s: version %d, catalogue %d", r.ID(), r.Version(), m.Version)
		}
		if m.Implementation.Owner != "native" {
			t.Errorf("%s: owner %s", r.ID(), m.Implementation.Owner)
		}
		ids = append(ids, r.ID())
	}
	want := "FND-SEC-001,FND-SEC-002,FND-SEC-003,FND-SEC-004,FND-SEC-006"
	if strings.Join(ids, ",") != want {
		t.Fatalf("registered %v, want %s", ids, want)
	}
}

// TestScenarioCoverage fails when a catalogue test scenario has no matching case in this package.
func TestScenarioCoverage(t *testing.T) {
	cat := loadCatalog(t)
	all := map[string][]tcase{
		"FND-SEC-001": cases001(), "FND-SEC-002": cases002(), "FND-SEC-003": cases003(),
		"FND-SEC-004": cases004(), "FND-SEC-006": cases006(),
	}
	for id, cases := range all {
		have := map[string]bool{}
		for _, c := range cases {
			have[c.name] = true
		}
		m := cat[id]
		want := map[string]bool{}
		for _, group := range [][]string{m.Tests.Positive, m.Tests.Negative, m.Tests.Skipped} {
			for _, s := range group {
				want[s] = true
				if !have[s] {
					t.Errorf("%s: catalogue scenario %q has no test case", id, s)
				}
			}
		}
		for n := range have {
			if !want[n] {
				t.Errorf("%s: test case %q is not a catalogue scenario", id, n)
			}
		}
	}
}

// TestRedactionCanary plants a fake secret in every property that is not evidence and checks that no
// rule copies it into a finding, skip, recommendation or fix.
func TestRedactionCanary(t *testing.T) {
	c := canary()
	conn := connection("acct", "conn1", "CognitiveSearch", rid("Microsoft.Search/searchServices", "srch"))
	conn["properties"].(obj)["target"] = "https://" + c + ".invalid"
	conn["properties"].(obj)["metadata"].(obj)["apiKey"] = c
	conn["properties"].(obj)["authType"] = "Key" + c
	in := inputOf(t, []obj{
		account("acct", obj{"disableLocalAuth": false, "customSubDomainName": c, "networkAcls": obj{"ipRules": []obj{{"value": "10.0.0.0/8"}}}}, obj{"tags": obj{"k": c}}),
		project("acct", "proj"), conn,
		search("srch", obj{"disableLocalAuth": false, "authOptions": obj{"aadOrApiKey": obj{"aadAuthFailureMode": c}}}),
		cosmos("cdb", obj{"disableLocalAuth": c, "ipRules": []obj{{"ipAddressOrRange": c}}}),
		storage("st", obj{"minimumTlsVersion": c, "allowSharedKeyAccess": c, "networkAcls": obj{"ipRules": []obj{{"value": c}}}}),
		res("Microsoft.KeyVault/vaults", c, obj{"networkAcls": obj{"ipRules": []obj{{"value": "0.0.0.0/0"}}}}),
	})
	total := 0
	for _, r := range sec.Rules() {
		for _, res := range eval(t, r.ID(), in) {
			switch {
			case res.Finding != nil:
				total++
				f := res.Finding
				for _, s := range []string{f.Evidence, f.Recommendation, f.Fix, f.Key, f.Resource.Name, f.Location.Pointer} {
					if strings.Contains(s, c) {
						t.Errorf("%s: canary leaked into %+v", r.ID(), f)
					}
				}
			case res.Skip != nil:
				if strings.Contains(res.Skip.Detail+res.Skip.Reason+res.Skip.MissingCapability, c) {
					t.Errorf("%s: canary leaked into skip %+v", r.ID(), res.Skip)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("the canary input produced no findings; the test proves nothing")
	}
}

// TestDeterminismAndFingerprintStability: repeated runs are identical, and the fingerprint does not
// change when an unrelated resource is added or the resources are reordered.
func TestDeterminismAndFingerprintStability(t *testing.T) {
	base := []obj{
		account("acct", obj{"disableLocalAuth": false}), project("acct", "proj"),
		connection("acct", "c1", "CognitiveSearch", rid("Microsoft.Search/searchServices", "srch")),
		search("srch", obj{"authOptions": obj{"apiKeyOnly": obj{}}}),
		storage("st", obj{"minimumTlsVersion": "TLS1_0", "allowSharedKeyAccess": true}),
		connection("acct", "c2", "AzureStorageAccount", rid("Microsoft.Storage/storageAccounts", "st")),
	}
	fingerprints := func(resources []obj) map[string]bool {
		got := map[string]bool{}
		for _, r := range sec.Rules() {
			for _, f := range execute(t, r.ID(), inputOf(t, resources), rules.ProfileProd).Findings {
				got[f.Fingerprint] = true
			}
		}
		return got
	}
	a, b := fingerprints(base), fingerprints(base)
	if len(a) < 4 {
		t.Fatalf("only %d fingerprints", len(a))
	}
	extra := append([]obj{res("Microsoft.Network/virtualNetworks", "vnet", obj{})}, base...)
	for name, other := range map[string]map[string]bool{"repeat": b, "extra resource": fingerprints(extra)} {
		for fp := range a {
			if !other[fp] {
				t.Errorf("%s: fingerprint %q changed", name, fp)
			}
		}
	}
	// Raw results are identical in order and content between runs.
	in := inputOf(t, base)
	for _, r := range sec.Rules() {
		first, second := eval(t, r.ID(), in), eval(t, r.ID(), in)
		if len(first) != len(second) {
			t.Fatalf("%s: result count differs", r.ID())
		}
		for i := range first {
			if first[i].Status != second[i].Status {
				t.Fatalf("%s: result %d differs", r.ID(), i)
			}
			if first[i].Finding != nil && !reflect.DeepEqual(*first[i].Finding, *second[i].Finding) {
				t.Fatalf("%s: finding %d differs", r.ID(), i)
			}
		}
	}
}

func TestRulesAreReadOnly(t *testing.T) {
	in := inputOf(t, []obj{account("acct", nil), project("acct", "p"), storage("st", nil)})
	before := len(in.ARM.Resources)
	for _, r := range sec.Rules() {
		eval(t, r.ID(), in)
	}
	if len(in.ARM.Resources) != before || in.ARM.Resources[0].Body["properties"] != nil {
		t.Fatal("a rule modified the input")
	}
}

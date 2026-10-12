package net_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/net"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	catalogfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

const fixtureDir = "../../../test/fixtures/rules/net"

// bundle loads the scenario templates of one rule: scenario name to committed ARM JSON.
func bundle(t *testing.T, id string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// input compiles a scenario through the real normaliser.
func input(t *testing.T, raw []byte, mut ...func(*model.Input)) *model.Input {
	t.Helper()
	n, err := bicep.Normalise(raw, bicep.Options{File: "infra/main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	in := &model.Input{Profile: rules.ProfileProd, ARM: n.Template, Project: model.Project{IaC: model.IaCBicep}}
	for _, m := range mut {
		m(in)
	}
	return in
}

func ruleByID(t *testing.T, id string) rules.Rule {
	t.Helper()
	for _, r := range net.Rules() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("rule %s not registered", id)
	return nil
}

// summarise renders results as "pass", "fail:<key>", "uncertain:<key>" or "skip:<reason>", sorted.
func summarise(t *testing.T, res []rules.Result) []string {
	t.Helper()
	var out []string
	for _, r := range res {
		if err := r.Validate(); err != nil {
			t.Fatalf("invalid result: %v", err)
		}
		switch {
		case r.Skip != nil:
			out = append(out, "skip:"+r.Skip.Reason)
		case r.Finding != nil:
			out = append(out, string(r.Status)+":"+r.Finding.Key)
		default:
			out = append(out, "pass")
		}
	}
	sort.Strings(out)
	return out
}

type scenario struct {
	name   string
	want   []string
	skip   string // non-empty: the scenario cannot be exercised; the reason is recorded
	policy func(*model.Policy)
}

func (s scenario) wantSorted() []string {
	w := slices.Clone(s.want)
	sort.Strings(w)
	return w
}

func runScenarios(t *testing.T, id string, cases []scenario) {
	t.Helper()
	b := bundle(t, id)
	r := ruleByID(t, id)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.skip != "" {
				t.Skip(c.skip)
			}
			raw, ok := b[c.name]
			if !ok {
				t.Fatalf("no fixture for scenario %q", c.name)
			}
			in := input(t, raw, func(in *model.Input) {
				if c.policy != nil {
					c.policy(&in.Policy)
				}
			})
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			got := summarise(t, res)
			if !reflect.DeepEqual(got, c.wantSorted()) && !(len(got) == 0 && len(c.want) == 0) {
				t.Fatalf("got %v, want %v", got, c.wantSorted())
			}
			for _, rs := range res {
				if rs.Finding != nil {
					checkFinding(t, r, *rs.Finding)
				}
			}
		})
	}
	for name := range b { // every fixture is used by a case
		if !slices.ContainsFunc(cases, func(c scenario) bool { return c.name == name }) {
			t.Errorf("fixture scenario %q has no case", name)
		}
	}
}

func checkFinding(t *testing.T, r rules.Rule, f sdk.Finding) {
	t.Helper()
	if f.Severity != "" || f.Profile != "" || f.Fingerprint != "" {
		t.Errorf("rule set an engine-owned field")
	}
	if f.Key == "" || f.Evidence == "" || f.Recommendation == "" || f.Fix == "" || f.Confidence == "" {
		t.Errorf("finding incomplete: %+v", f)
	}
	if f.Resource.Type == "" || f.Resource.Name == "" || f.Resource.Pointer == "" || f.Resource.Kind != "arm-resource" {
		t.Errorf("resource ref incomplete: %+v", f.Resource)
	}
	if f.Location.File == "" || !strings.HasPrefix(f.Location.Pointer, f.Resource.Pointer) {
		t.Errorf("location incomplete: %+v", f.Location)
	}
	if f.RuleID != r.ID() || f.RuleVersion != r.Version() {
		t.Errorf("finding names the wrong rule")
	}
}

func policyPublic(p model.PublicAccess) func(*model.Policy) {
	return func(pol *model.Policy) { pol.Network.PublicAccess = &p }
}

func TestNet001(t *testing.T) {
	runScenarios(t, "FND-NET-001", []scenario{
		{name: "account-public-enabled-default-allow-in-prod", want: []string{"fail:public-open"}},
		{name: "account-public-absent-defaults", want: []string{"fail:public-open"}},
		{name: "account-public-disabled-no-private-endpoint", want: []string{"fail:no-private-endpoint"}},
		{name: "account-public-disabled-endpoint-pending-live", want: []string{"uncertain:private-endpoint-pending-approval", "skip:control-plane-unavailable"}},
		{name: "account-public-disabled-with-account-groupid-endpoint", want: []string{"pass", "skip:control-plane-unavailable"}},
		{name: "account-public-disabled-wrong-groupid-endpoint", want: []string{"fail:no-private-endpoint"}},
		{name: "account-public-disabled-conditional-endpoint", want: []string{"uncertain:private-endpoint-conditional", "skip:control-plane-unavailable"}},
		{name: "account-public-disabled-looped-endpoint", want: []string{"pass", "skip:control-plane-unavailable"}},
		{name: "account-restricted-mode-with-ip-rules", want: []string{"pass"}},
		{name: "dev-profile-public-account-reports-info", want: []string{"fail:public-open"}},
		{name: "private-endpoint-defined-in-another-template", want: []string{"skip:unresolved-value:private-endpoint-target"}},
		{name: "account-resource-id-is-parameter-and-no-arm-access", want: []string{"skip:unresolved-value:private-endpoint-target"}},
		{name: "account-public-access-is-parameter", want: []string{"skip:unresolved-value:properties.publicNetworkAccess"}},
		{name: "account-kind-is-parameter", want: []string{"skip:unresolved-value:kind"}},
		{name: "not-an-ai-services-account"},
		{name: "project-under-account", want: []string{"fail:public-open"}},
	})
}

func TestNet001PolicyVariations(t *testing.T) {
	raw := bundle(t, "FND-NET-001")["account-public-enabled-default-allow-in-prod"]
	r := ruleByID(t, "FND-NET-001")
	for _, p := range []struct {
		name   string
		policy func(*model.Policy)
		text   string
	}{
		{"unset-baseline-forbidden", nil, "forbidden"},
		{"forbidden", policyPublic(model.PublicForbidden), "forbidden"},
		{"entra-only", policyPublic(model.PublicEntraOnly), "entra-only"},
		{"allowed", policyPublic(model.PublicAllowed), "allowed"},
	} {
		t.Run(p.name, func(t *testing.T) {
			in := input(t, raw, func(in *model.Input) {
				if p.policy != nil {
					p.policy(&in.Policy)
				}
			})
			res, err := r.Evaluate(context.Background(), in)
			if err != nil || len(res) != 1 || res[0].Finding == nil {
				t.Fatalf("want exactly one finding, got %v, %v", res, err)
			}
			// The rule never skips on policy: public access is reported under every value.
			if !strings.Contains(res[0].Finding.Evidence, "publicAccess is "+p.text) {
				t.Fatalf("evidence does not name the policy: %s", res[0].Finding.Evidence)
			}
		})
	}
}

func TestNet001ProjectsNamed(t *testing.T) {
	in := input(t, bundle(t, "FND-NET-001")["project-under-account"])
	res, _ := ruleByID(t, "FND-NET-001").Evaluate(context.Background(), in)
	if !strings.Contains(res[0].Finding.Evidence, "1 project(s)") {
		t.Fatalf("evidence: %s", res[0].Finding.Evidence)
	}
}

func TestNet002(t *testing.T) {
	runScenarios(t, "FND-NET-002", []scenario{
		{name: "private-account-with-public-storage", want: []string{"fail:storage:public-access", "pass", "pass"}},
		{name: "private-account-with-search-public-and-no-endpoint", want: []string{"fail:search:public-access", "fail:search:no-private-endpoint"}},
		{name: "cosmos-endpoint-uses-wrong-group-id", want: []string{"fail:cosmos:private-endpoint-group-id"}},
		{name: "all-three-dependents-disabled-with-endpoints", want: []string{"pass", "pass", "pass"}},
		{name: "storage-default-deny-with-blob-endpoint", want: []string{"pass"}},
		{name: "cosmos-vnet-filter-with-rules-and-endpoint", want: []string{"pass"}},
		{name: "foundry-account-is-public", want: []string{"skip:not-applicable"}},
		{name: "managed-virtual-network-mode", want: []string{"skip:not-applicable"}},
		{name: "dependent-resource-id-unresolvable-parameter", want: []string{"skip:unresolved-value:dependent-resource-id"}},
		{name: "dependent-defined-in-another-template", want: []string{"skip:unresolved-value:dependent-not-in-template"}},
		{name: "private-account-without-dependents", want: []string{"skip:not-applicable"}},
		{name: "endpoint-target-unresolved", want: []string{"skip:unresolved-value:private-endpoint-target"}},
		{name: "dependent-publicaccess-is-parameter", want: []string{"skip:unresolved-value:public access settings"}},
		{name: "legacy-capability-host-connections", want: []string{"fail:storage:public-access", "fail:storage:no-private-endpoint"}},
	})
}

func TestNet003(t *testing.T) {
	runScenarios(t, "FND-NET-003", []scenario{
		{name: "foundry-endpoint-without-zone-group", want: []string{"fail:no-zone-group"}},
		{name: "foundry-endpoint-zone-group-with-only-cognitiveservices-zone", want: []string{"fail:zone-group-missing-zone"}},
		{name: "zone-without-vnet-link", want: []string{"fail:zone-without-vnet-link"}},
		{name: "search-endpoint-wrong-zone-name", want: []string{"fail:zone-group-missing-zone", "skip:control-plane-unavailable"}},
		{name: "foundry-endpoint-three-zones-linked", want: []string{"pass"}},
		{name: "storage-blob-endpoint-with-linked-zone", want: []string{"pass"}},
		{name: "azure-government-zone-names", want: []string{"skip:not-applicable"}},
		{name: "central-hub-dns-zone-in-other-subscription", want: []string{"skip:control-plane-unavailable"}},
		{name: "dine-policy-manages-dns-zone-group", want: []string{"skip:not-applicable"},
			policy: func(p *model.Policy) { p.ManagedByAzurePolicy = []model.ManagedBy{model.ManagedPrivateDNSZoneGroup} }},
		{name: "zone-group-with-six-zones", want: []string{"fail:zone-group-limit", "skip:control-plane-unavailable"}},
		{name: "zone-id-is-parameter", want: []string{"skip:unresolved-value:zone-id"}},
		{name: "link-to-other-vnet", want: []string{"fail:zone-without-vnet-link"}},
		{name: "link-vnet-is-parameter", want: []string{"skip:unresolved-value:link-vnet"}},
		{name: "endpoint-subnet-is-parameter", want: []string{"skip:unresolved-value:endpoint-vnet"}},
		{name: "endpoint-group-ids-are-parameter", want: []string{"skip:unresolved-value:group-ids"}},
		{name: "two-zone-groups", want: []string{"fail:zone-group-limit"}},
		{name: "unknown-group-id-with-zone-group", want: []string{"skip:control-plane-unavailable"}},
		{name: "custom-dns-server-declared-in-config",
			skip: "ADR-007 defines no policy key for a custom DNS server or private resolver; the scenario cannot be exercised until one is added"},
	})
}

func TestNet003ManagedByPolicyVariants(t *testing.T) {
	raw := bundle(t, "FND-NET-003")["foundry-endpoint-without-zone-group"]
	r := ruleByID(t, "FND-NET-003")
	for name, list := range map[string][]model.ManagedBy{
		"nil-default":              nil,
		"empty-list":               {},
		"diagnostic-settings-only": {model.ManagedDiagnosticSettings},
		"both-entries":             {model.ManagedDiagnosticSettings, model.ManagedPrivateDNSZoneGroup},
		"private-dns-zone-group":   {model.ManagedPrivateDNSZoneGroup},
	} {
		t.Run(name, func(t *testing.T) {
			in := input(t, raw, func(in *model.Input) { in.Policy.ManagedByAzurePolicy = list })
			res, _ := r.Evaluate(context.Background(), in)
			got := summarise(t, res)
			want := []string{"fail:no-zone-group"}
			if slices.Contains(list, model.ManagedPrivateDNSZoneGroup) {
				want = []string{"skip:not-applicable"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			if want[0] == "skip:not-applicable" && !strings.HasPrefix(res[0].Skip.Detail, "policy-managed") {
				t.Fatalf("detail: %s", res[0].Skip.Detail)
			}
		})
	}
}

func TestNet004(t *testing.T) {
	runScenarios(t, "FND-NET-004", []scenario{
		{name: "subnet-without-delegation", want: []string{"fail:delegation-missing"}},
		{name: "subnet-delegated-to-wrong-service", want: []string{"fail:delegation-service"}},
		{name: "subnet-prefix-slash-28", want: []string{"fail:size"}},
		{name: "subnet-name-over-63-bytes", want: []string{"fail:name-length"}},
		{name: "same-subnet-used-by-two-accounts", want: []string{"fail:shared-subnet"}},
		{name: "slash-24-delegated-to-microsoft-app-environments", want: []string{"pass"}},
		{name: "slash-27-minimum-accepted", want: []string{"pass"}},
		{name: "use-microsoft-managed-network", want: []string{"skip:not-applicable"}},
		{name: "subnet-id-is-parameter", want: []string{"skip:unresolved-value:subnet-id"}},
		{name: "subnet-not-in-template", want: []string{"skip:unresolved-value:subnet-id"}},
		{name: "address-prefixes-all-small", want: []string{"fail:size"}},
		{name: "address-prefixes-one-large", want: []string{"pass"}},
		{name: "prefix-is-parameter", want: []string{"skip:unresolved-value:subnet address prefix"}},
		{name: "delegations-are-parameter", want: []string{"skip:unresolved-value:properties.delegations"}},
		{name: "no-network-injection"},
		{name: "injection-managed-flag-is-parameter", want: []string{"skip:unresolved-value:networkInjections.useMicrosoftManagedNetwork"}},
		{name: "injections-are-parameter", want: []string{"skip:unresolved-value:properties.networkInjections"}},
		{name: "injection-for-other-scenario"},
	})
}

func TestNet005(t *testing.T) {
	runScenarios(t, "FND-NET-005", []scenario{
		{name: "account-and-agent-vnet-in-different-regions", want: []string{"fail:account-vnet-region"}},
		{name: "private-endpoint-region-differs-from-its-vnet", want: []string{"fail:private-endpoint-region", "pass"}},
		{name: "all-resources-same-region", want: []string{"pass", "pass"}},
		{name: "foundry-endpoint-in-peered-hub-vnet-same-region", want: []string{"pass", "pass"}},
		{name: "cosmos-in-other-region-reports-info-only", want: []string{"pass"}},
		{name: "region-display-name-equals-canonical", want: []string{"pass"}},
		{name: "location-is-unresolved-parameter", want: []string{"skip:unresolved-value:location"}},
		{name: "same-location-expression-on-both", want: []string{"pass"}},
		{name: "managed-virtual-network-mode", want: []string{"skip:not-applicable"}},
		{name: "endpoint-subnet-is-parameter", want: []string{"skip:unresolved-value:subnet-id"}},
		{name: "endpoint-subnet-not-in-template", want: []string{"skip:unresolved-value:subnet-id"}},
		{name: "subnet-without-vnet-parent", want: []string{"skip:unresolved-value:vnet"}},
		{name: "private-endpoint-subscription-differs-from-vnet",
			skip: "the subscription of an endpoint is not part of the compiled resource (it is deployment scope); not comparable from Resource"},
	})
}

func TestNet007(t *testing.T) {
	runScenarios(t, "FND-NET-007", []scenario{
		{name: "restricted-account-with-empty-ip-and-vnet-rules", want: []string{"fail:no-allowed-networks"}},
		{name: "restricted-account-without-rule-lists", want: []string{"fail:no-allowed-networks"}},
		{name: "ip-rule-value-is-not-ipv4", want: []string{"fail:ip-rule-invalid"}},
		{name: "restricted-account-with-one-ip-rule", want: []string{"pass"}},
		{name: "restricted-account-with-single-ip-address", want: []string{"pass"}},
		{name: "restricted-account-with-vnet-rule-only", want: []string{"pass"}},
		{name: "public-disabled-account-ignores-acls", want: []string{"pass"}},
		{name: "default-allow-is-not-restricted", want: []string{"pass"}},
		{name: "ip-rules-supplied-by-parameter-array", want: []string{"skip:unresolved-value:networkAcls.ipRules or virtualNetworkRules"}},
		{name: "ip-rule-value-is-parameter", want: []string{"skip:unresolved-value:networkAcls.ipRules value"}},
		{name: "default-action-is-parameter", want: []string{"skip:unresolved-value:publicNetworkAccess or networkAcls.defaultAction"}},
		{name: "deny-default-with-approved-private-endpoint-no-ip-rules", want: []string{"skip:not-applicable"}},
		{name: "deny-default-with-unresolved-endpoint", want: []string{"skip:unresolved-value:private-endpoint-target"}},
		{name: "ip-rule-value-looks-like-a-secret", want: []string{"fail:ip-rule-invalid"}},
	})
}

func TestNet008(t *testing.T) {
	runScenarios(t, "FND-NET-008", []scenario{
		{name: "free-search-with-private-endpoint", want: []string{"fail:free-with-private-endpoint"}},
		{name: "free-search-public-disabled", want: []string{"fail:free-public-disabled"}},
		{name: "free-search-as-vectorstore-of-private-account", want: []string{"fail:free-vector-store-of-private-account"}},
		{name: "basic-search-with-private-endpoint", want: []string{"pass"}},
		{name: "free-search-public-and-not-referenced-by-private-foundry", want: []string{"pass"}},
		{name: "search-sku-is-parameter", want: []string{"skip:unresolved-value:sku.name"}},
		{name: "serverless-search-is-not-free", want: []string{"pass"}},
		{name: "free-search-uppercase-sku", want: []string{"fail:free-public-disabled"}},
		{name: "free-search-vectorstore-of-public-account", want: []string{"pass"}},
		{name: "free-search-all-three", want: []string{"fail:free-public-disabled", "fail:free-vector-store-of-private-account", "fail:free-with-private-endpoint"}},
	})
}

// TestScenarioCoverage fails when a catalogue scenario of an implemented rule has no subtest, or has
// neither a fixture nor an explicit skip reason.
func TestScenarioCoverage(t *testing.T) {
	cat, err := catalog.Load(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("rules_test.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	impl := map[string]bool{}
	for _, r := range net.Rules() {
		impl[r.ID()] = true
	}
	checked := 0
	for _, m := range cat {
		if !impl[m.ID] {
			continue
		}
		for _, name := range slices.Concat(m.Tests.Positive, m.Tests.Negative, m.Tests.Skipped, m.Tests.Uncertain) {
			checked++
			entry := regexp.MustCompile(`\{name: "` + regexp.QuoteMeta(name) + `"`)
			if !entry.MatchString(src) {
				t.Errorf("%s: catalogue scenario %q has no subtest", m.ID, name)
				continue
			}
			explicitSkip := regexp.MustCompile(`\{name: "` + regexp.QuoteMeta(name) + `",\s*skip:`).MatchString(src)
			if _, ok := bundle(t, m.ID)[name]; !ok && !explicitSkip {
				t.Errorf("%s: scenario %q has neither a fixture nor an explicit skip reason", m.ID, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no catalogue scenarios checked")
	}
}

func TestIdentityMatchesCatalogue(t *testing.T) {
	cat, err := catalog.Load(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]catalog.Rule{}
	for _, m := range cat {
		byID[m.ID] = m
	}
	want := []string{"FND-NET-001", "FND-NET-002", "FND-NET-003", "FND-NET-004", "FND-NET-005", "FND-NET-007", "FND-NET-008"}
	var got []string
	for _, r := range net.Rules() {
		got = append(got, r.ID())
		m, ok := byID[r.ID()]
		if !ok || m.Version != r.Version() {
			t.Errorf("%s v%d does not match the catalogue", r.ID(), r.Version())
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Rules() = %v, want %v", got, want)
	}
	if _, err := rules.NewRegistry(cat, net.Rules(), rules.Phase1); err != nil {
		t.Fatal(err)
	}
}

func TestSkipsWithoutARM(t *testing.T) {
	for _, r := range net.Rules() {
		t.Run(r.ID(), func(t *testing.T) {
			for _, in := range []*model.Input{{}, nil} {
				res, err := r.Evaluate(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				got := summarise(t, res)
				if !reflect.DeepEqual(got, []string{"skip:" + rules.ReasonSyntheticInfrastructure}) || res[0].Skip.MissingCapability == "" {
					t.Fatalf("got %v", got)
				}
			}
		})
	}
}

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := input(t, bundle(t, "FND-NET-001")["account-public-absent-defaults"])
	for _, r := range net.Rules() {
		if _, err := r.Evaluate(ctx, in); err == nil {
			t.Errorf("%s ignored the cancelled context", r.ID())
		}
	}
}

// allScenarios runs every rule over every fixture of every rule: no panic, valid results, deterministic.
func TestAllRulesAllFixturesDeterministic(t *testing.T) {
	for _, id := range []string{"FND-NET-001", "FND-NET-002", "FND-NET-003", "FND-NET-004", "FND-NET-005", "FND-NET-007", "FND-NET-008"} {
		for name, raw := range bundle(t, id) {
			for _, r := range net.Rules() {
				first, err := r.Evaluate(context.Background(), input(t, raw))
				if err != nil {
					t.Fatalf("%s on %s/%s: %v", r.ID(), id, name, err)
				}
				second, _ := r.Evaluate(context.Background(), input(t, raw))
				if !reflect.DeepEqual(first, second) {
					t.Errorf("%s on %s/%s is not deterministic", r.ID(), id, name)
				}
				summarise(t, first)
			}
		}
	}
}

func TestRealCompiledFixtures(t *testing.T) {
	for _, f := range []string{"bicep/foundry/foundry.arm.json", "bicep/foundry-v2/foundry.arm.json", "bicep/synthetic/main.arm.json"} {
		raw, err := os.ReadFile(filepath.Join("../../../test/fixtures", f))
		if err != nil {
			t.Fatal(err)
		}
		in := input(t, raw)
		for _, r := range net.Rules() {
			res, err := r.Evaluate(context.Background(), in)
			if err != nil {
				t.Fatalf("%s on %s: %v", r.ID(), f, err)
			}
			summarise(t, res)
		}
	}
}

// ---- engine integration: severity, redaction, fingerprints ----

func execute(t *testing.T, profile string, in *model.Input) rules.Outcome {
	t.Helper()
	cat, err := catalog.Load(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := rules.NewRegistry(cat, net.Rules(), rules.Phase1)
	if err != nil {
		t.Fatal(err)
	}
	out, err := rules.Execute(context.Background(), in, reg.Executable(), rules.ExecConfig{Profile: profile, Stamper: findingsStamper{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) > 0 {
		t.Fatalf("internal errors: %v", out.Errors)
	}
	return out
}

func TestSeverityPerProfile(t *testing.T) {
	raw := bundle(t, "FND-NET-001")["dev-profile-public-account-reports-info"]
	for profile, want := range map[string]string{rules.ProfileDev: "info", rules.ProfileTest: "warning", rules.ProfileProd: "error"} {
		out := execute(t, profile, input(t, raw))
		var got []string
		for _, f := range out.Findings {
			if f.RuleID == "FND-NET-001" {
				got = append(got, string(f.Severity))
			}
		}
		if !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("%s: NET-001 severity %v, want %s", profile, got, want)
		}
	}
	// NET-004 has basis platform: an error in every profile.
	sub := bundle(t, "FND-NET-004")["subnet-without-delegation"]
	for _, profile := range []string{rules.ProfileDev, rules.ProfileTest, rules.ProfileProd} {
		out := execute(t, profile, input(t, sub))
		n := 0
		for _, f := range out.Findings {
			if f.RuleID == "FND-NET-004" {
				n++
				if f.Severity != "error" {
					t.Errorf("%s: platform rule severity %s", profile, f.Severity)
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: %d NET-004 findings", profile, n)
		}
	}
}

func TestRedactionCanary(t *testing.T) {
	secret := "sk-" + strings.Repeat("Ab3", 12) // assembled at run time; not a real credential
	raw := strings.ReplaceAll(string(bundle(t, "FND-NET-007")["ip-rule-value-looks-like-a-secret"]), "SECRETCANARY", secret)
	if !strings.Contains(raw, secret) {
		t.Fatal("canary not planted")
	}
	out := execute(t, rules.ProfileProd, input(t, []byte(raw)))
	if len(out.Findings) != 1 {
		t.Fatalf("findings: %+v", out.Findings)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), secret) || strings.Contains(string(b), "Ab3Ab3") {
		t.Fatalf("secret reached the outcome: %s", b)
	}
	// Direct rule output must not carry the value either.
	res, _ := ruleByID(t, "FND-NET-007").Evaluate(context.Background(), input(t, []byte(raw)))
	for _, r := range res {
		if r.Finding != nil && (strings.Contains(r.Finding.Evidence, secret) || strings.Contains(r.Finding.Key, secret)) {
			t.Fatal("rule echoed the ipRules value")
		}
	}
}

func TestFingerprintStability(t *testing.T) {
	raw := bundle(t, "FND-NET-001")["account-public-disabled-no-private-endpoint"]
	a := execute(t, rules.ProfileProd, input(t, raw))
	b := execute(t, rules.ProfileProd, input(t, raw))
	if len(a.Findings) != 1 || a.Findings[0].Fingerprint == "" || a.Findings[0].Fingerprint != b.Findings[0].Fingerprint {
		t.Fatalf("unstable: %+v %+v", a.Findings, b.Findings)
	}
	// A different project profile does not change the fingerprint of the same finding.
	c := execute(t, rules.ProfileDev, input(t, raw))
	if c.Findings[0].Fingerprint != a.Findings[0].Fingerprint {
		t.Fatal("fingerprint depends on the profile")
	}
}

func TestConfidenceLoweredForConditionalAndLooped(t *testing.T) {
	cond := strings.Replace(string(bundle(t, "FND-NET-001")["account-public-absent-defaults"]), `"kind"`, `"condition": "[parameters('c')]", "kind"`, 1)
	cond = strings.Replace(cond, `"parameters": {}`, `"parameters": {"c": {"type": "bool"}}`, 1)
	res, _ := ruleByID(t, "FND-NET-001").Evaluate(context.Background(), input(t, []byte(cond)))
	if len(res) != 1 || res[0].Finding == nil || res[0].Finding.Confidence == "certain" {
		t.Fatalf("conditional account must not be certain: %+v", res)
	}
	looped := strings.Replace(string(bundle(t, "FND-NET-001")["account-public-absent-defaults"]), `"kind"`, `"copy": {"name": "n", "count": 2}, "kind"`, 1)
	res, _ = ruleByID(t, "FND-NET-001").Evaluate(context.Background(), input(t, []byte(looped)))
	if len(res) != 1 || res[0].Finding == nil || res[0].Finding.Confidence == "certain" {
		t.Fatalf("looped account must not be certain: %+v", res)
	}
}

func TestUncertainFindingsAreUncertain(t *testing.T) {
	res, _ := ruleByID(t, "FND-NET-001").Evaluate(context.Background(), input(t, bundle(t, "FND-NET-001")["account-public-disabled-endpoint-pending-live"]))
	for _, r := range res {
		if r.Status == "uncertain" && r.Finding.Confidence != "uncertain" {
			t.Fatalf("confidence %s", r.Finding.Confidence)
		}
	}
}

func TestNet007ConfidenceIsLikely(t *testing.T) {
	res, _ := ruleByID(t, "FND-NET-007").Evaluate(context.Background(), input(t, bundle(t, "FND-NET-007")["restricted-account-with-empty-ip-and-vnet-rules"]))
	if res[0].Finding.Confidence != "likely" {
		t.Fatalf("confidence %s", res[0].Finding.Confidence)
	}
}

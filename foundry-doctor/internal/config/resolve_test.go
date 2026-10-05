package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func profiles(t *testing.T) config.ProfileSet {
	t.Helper()
	s, err := config.DefaultProfiles()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func parse(t *testing.T, doc string) *config.File {
	t.Helper()
	f, err := config.Parse("t.yaml", []byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func entry(t *testing.T, e config.Effective, key string) config.PolicyValue {
	t.Helper()
	for _, p := range e.PolicyEntries {
		if p.Key == key {
			return p
		}
	}
	t.Fatalf("no entry for %s", key)
	return config.PolicyValue{}
}

func TestResolveNoFileUsesDefaults(t *testing.T) {
	e, err := config.Resolve(config.Flags{}, nil, "", profiles(t))
	if err != nil {
		t.Fatal(err)
	}
	if e.Profile.Name != "foundry-dev" || e.ProfileSource != config.SourceDefault {
		t.Errorf("profile = %s from %s", e.Profile.Name, e.ProfileSource)
	}
	if e.Inputs.AzureYAML != "./azure.yaml" || e.Sources["inputs.azureYaml"] != config.SourceDefault {
		t.Errorf("inputs = %+v %v", e.Inputs, e.Sources)
	}
	if e.Validation.Runtime != config.ModeDisabled || e.Outputs.Directory != ".foundry-doctor/out" {
		t.Errorf("defaults wrong: %+v %+v", e.Validation, e.Outputs)
	}
	if !reflect.DeepEqual(e.Policy, model.Policy{}) {
		t.Errorf("policy must stay empty without configuration: %+v", e.Policy)
	}
	if len(e.PolicyEntries) != 20 {
		t.Errorf("%d policy entries, want 20 (ADR-007 keys)", len(e.PolicyEntries))
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	ps := profiles(t)
	cases := []struct {
		name    string
		flag    string
		file    string
		want    string
		wantSrc string
		wantErr string
	}{
		{"default", "", "", "foundry-dev", config.SourceDefault, ""},
		{"file", "", "prod", "foundry-prod", config.SourceRepository, ""},
		{"file long", "", "foundry-test", "foundry-test", config.SourceRepository, ""},
		{"flag beats file", "test", "prod", "foundry-test", config.SourceFlag, ""},
		{"bad flag", "staging", "prod", "", "", "unknown profile"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := "version: 1\n"
			if c.file != "" {
				doc += "profile: " + c.file + "\n"
			}
			e, err := config.Resolve(config.Flags{Profile: c.flag}, parse(t, doc), "", ps)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if e.Profile.Name != c.want || e.ProfileSource != c.wantSrc {
				t.Errorf("got %s from %s", e.Profile.Name, e.ProfileSource)
			}
		})
	}
	// The resolved profile carries the right severities.
	e, _ := config.Resolve(config.Flags{Profile: "prod"}, nil, "", ps)
	if s, ok := e.Profile.Severity("FND-CFG-001"); !ok || !s.Valid() {
		t.Errorf("severity = %q %v", s, ok)
	}
}

func TestResolveMissingProfileInSet(t *testing.T) {
	_, err := config.Resolve(config.Flags{}, nil, "", config.ProfileSet{})
	if err == nil || !strings.Contains(err.Error(), "not in the profile set") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveFlagsBeatFileBeatDefaults(t *testing.T) {
	f := parse(t, `version: 1
inputs: {azureYaml: ./a.yaml, infraPath: ./i}
rules: {include: [must-have], exclude: [FND-CFG-001]}
adoption: {baseline: b.yaml}
outputs: {formats: [json], directory: out}
advanced: {checkov: {enabled: true}}
`)
	e, err := config.Resolve(config.Flags{
		AzureYAML: "/abs/azure.yaml", Rules: []string{"FND-SEC"}, Formats: []config.Format{config.FormatSARIF}, Baseline: "/x/base.yaml",
	}, f, "", profiles(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"inputs.azureYaml": config.SourceFlag, "inputs.infraPath": config.SourceRepository, "rules.include": config.SourceFlag,
		"rules.exclude": config.SourceRepository, "rules.packs": config.SourceDefault, "adoption.baseline": config.SourceFlag,
		"adoption.suppressions": config.SourceDefault, "outputs.formats": config.SourceFlag, "outputs.directory": config.SourceRepository,
		"advanced.checkov.enabled": config.SourceRepository, "advanced.psrule.enabled": config.SourceDefault,
	}
	for k, v := range want {
		if e.Sources[k] != v {
			t.Errorf("source of %s = %q, want %q", k, e.Sources[k], v)
		}
	}
	if e.Inputs.AzureYAML != "/abs/azure.yaml" || e.Inputs.InfraPath != "./i" || e.Rules.Include[0] != "FND-SEC" ||
		e.Outputs.Formats[0] != config.FormatSARIF || e.Outputs.Directory != "out" || e.Advanced.Checkov.Enabled != config.ToggleTrue {
		t.Errorf("effective = %+v", e)
	}
	if e.Rules.Exclude[0] != "FND-CFG-001" || e.Advanced.Bicep.Executable != "auto" {
		t.Errorf("effective = %+v", e)
	}
}

func TestResolveDoesNotAliasInputs(t *testing.T) {
	f := parse(t, "version: 1\nrules:\n  include: [a]\npolicy:\n  models:\n    allow: [x/y]\n")
	e, err := config.Resolve(config.Flags{}, f, "", profiles(t))
	if err != nil {
		t.Fatal(err)
	}
	e.Rules.Include[0] = "changed"
	e.Policy.Models.Allow[0] = "changed"
	if f.Rules.Include[0] != "a" || f.Policy.Models.Allow[0] != "x/y" {
		t.Error("Resolve returned slices aliasing the file")
	}
}

func TestResolveFlagValidation(t *testing.T) {
	cases := []struct {
		name  string
		flags config.Flags
		want  string
	}{
		{"format", config.Flags{Formats: []config.Format{"html"}}, "not one of"},
		{"format dup", config.Flags{Formats: []config.Format{"json", "json"}}, "duplicate"},
		{"selector", config.Flags{Rules: []string{"a b"}}, "does not match"},
		{"control char path", config.Flags{AzureYAML: "a\nb"}, "control"},
		{"baseline ctrl", config.Flags{Baseline: "a\x01"}, "control"},
		{"env shape", config.Flags{Environment: "a b"}, "must match"},
	}
	for _, c := range cases {
		_, err := config.Resolve(c.flags, nil, "", profiles(t))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}

func TestResolveRejectsInvalidFileBuiltInCode(t *testing.T) {
	_, err := config.Resolve(config.Flags{}, &config.File{Version: 7}, "", profiles(t))
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Errorf("err = %v", err)
	}
}

const policyFile = `version: 1
policy:
  resourceScope: same-subscription
  logRetention: {minimumDays: 90}
  models: {allow: [OpenAI/gpt-4o]}
  environments: {production: [prod]}
  cost: {devMaxCosmosThroughput: 1000}
  disasterRecovery: {declared: true, reference: 'https://example.invalid/dr?token=SECRET'}
environments:
  prod:
    policy:
      logRetention: {minimumDays: 365}
      models: {allow: []}
      network: {publicAccess: allowed}
      managedByAzurePolicy: []
      disasterRecovery: {reference: docs/dr.md}
  dev:
    policy:
      cost: {devMaxCosmosThroughput: 0}
`

func TestResolveEnvironmentPolicyOverridesRepository(t *testing.T) {
	f := parse(t, policyFile)
	ps := profiles(t)

	prod, err := config.Resolve(config.Flags{}, f, "prod", ps)
	if err != nil {
		t.Fatal(err)
	}
	if got := *prod.Policy.LogRetention.MinimumDays; got != 365 {
		t.Errorf("minimumDays = %d, want 365", got)
	}
	if prod.Policy.Models.Allow == nil || len(prod.Policy.Models.Allow) != 0 {
		t.Errorf("env empty list must replace the repository list: %#v", prod.Policy.Models.Allow)
	}
	if *prod.Policy.ResourceScope != model.ScopeSameSubscription {
		t.Error("untouched repository key lost")
	}
	wantSrc := map[string]string{
		model.KeyLogRetentionMinimumDays: "environment:prod", model.KeyModelsAllow: "environment:prod",
		model.KeyResourceScope: "repository", model.KeyEnvironmentsProduction: "repository",
		model.KeyNetworkPublicAccess: "environment:prod", model.KeyManagedByAzurePolicy: "environment:prod",
		model.KeyDisasterRecoveryDeclared: "environment:prod",
	}
	for k, v := range wantSrc {
		if got := entry(t, prod, k).Source; got != v {
			t.Errorf("%s source = %q, want %q", k, got, v)
		}
	}

	dev, _ := config.Resolve(config.Flags{}, f, "dev", ps)
	if *dev.Policy.LogRetention.MinimumDays != 90 || *dev.Policy.Cost.DevMaxCosmosThroughput != 0 {
		t.Errorf("dev policy wrong: %+v", dev.Policy)
	}
	if entry(t, dev, model.KeyCostDevMaxCosmosThroughput).Value != "0" {
		t.Error("an explicit zero must be reported as set, not as unset")
	}

	other, _ := config.Resolve(config.Flags{}, f, "staging", ps)
	if *other.Policy.LogRetention.MinimumDays != 90 {
		t.Error("an environment without an override must see the repository policy")
	}
	if other.Environment != "staging" {
		t.Errorf("environment = %q", other.Environment)
	}

	// The repository file was not modified by merging.
	if *f.Policy.LogRetention.MinimumDays != 90 || len(f.Policy.Models.Allow) != 1 {
		t.Error("Resolve mutated the repository file")
	}
}

func TestResolveEnvironmentSelection(t *testing.T) {
	f := parse(t, "version: 1\ninputs: {environment: prod}\nenvironments:\n  prod: {policy: {logRetention: {minimumDays: 7}}}\n  test: {policy: {logRetention: {minimumDays: 8}}}\n")
	ps := profiles(t)
	cases := []struct {
		name     string
		flags    config.Flags
		arg      string
		wantDays int
		wantSrc  string
	}{
		{"file", config.Flags{}, "", 7, config.SourceRepository},
		{"flag", config.Flags{Environment: "test"}, "", 8, config.SourceFlag},
		{"argument beats flag", config.Flags{Environment: "test"}, "prod", 7, config.SourceFlag},
	}
	for _, c := range cases {
		e, err := config.Resolve(c.flags, f, c.arg, ps)
		if err != nil {
			t.Fatal(err)
		}
		if got := *e.Policy.LogRetention.MinimumDays; got != c.wantDays || e.Sources["inputs.environment"] != c.wantSrc {
			t.Errorf("%s: days %d source %q", c.name, got, e.Sources["inputs.environment"])
		}
	}
}

func TestEffectivePolicyHeaderMarksBaselines(t *testing.T) {
	e, err := config.Resolve(config.Flags{}, parse(t, policyFile), "", profiles(t))
	if err != nil {
		t.Fatal(err)
	}
	// Baselines: shown, marked, and not written into Policy (rules must still see "missing" where ADR-007 says skip).
	for key, want := range map[string]string{
		model.KeyNetworkPublicAccess: "forbidden", model.KeyMonitoringPublicTelemetry: "false",
		model.KeyManagedByAzurePolicy: "[]", model.KeyAllowedExternalScopes: "[]", model.KeyCostProductionSizedSkuExempt: "[]",
	} {
		p := entry(t, e, key)
		if !p.Baseline || p.Source != config.SourceBaseline || p.Value != want {
			t.Errorf("%s = %+v, want baseline %q", key, p, want)
		}
	}
	if e.Policy.Network.PublicAccess != nil || e.Policy.AllowedExternalScopes != nil {
		t.Error("baselines leaked into the configured policy")
	}
	// Configured value.
	if p := entry(t, e, model.KeyLogRetentionMinimumDays); p.Baseline || p.Value != "90" || p.Source != config.SourceRepository {
		t.Errorf("minimumDays = %+v", p)
	}
	// Unset key without baseline: skipped, with the note.
	p := entry(t, e, model.KeyTagsRequired)
	if p.Source != config.SourceUnset || p.Baseline || p.Value != "" || !strings.Contains(p.Note, "profile-key-missing") {
		t.Errorf("tags.required = %+v", p)
	}
	// The DR reference may hold a token; it must never reach the header.
	for _, pe := range e.PolicyEntries {
		if strings.Contains(pe.Value, "SECRET") || strings.Contains(pe.Value, "example.invalid") {
			t.Errorf("reference leaked: %+v", pe)
		}
	}
	if dr := entry(t, e, model.KeyDisasterRecoveryDeclared); dr.Value != "true (reference set)" {
		t.Errorf("dr = %+v", dr)
	}
	// Order and determinism.
	e2, _ := config.Resolve(config.Flags{}, parse(t, policyFile), "", profiles(t))
	if !reflect.DeepEqual(e.PolicyEntries, e2.PolicyEntries) {
		t.Error("PolicyEntries not deterministic")
	}
	if e.PolicyEntries[0].Key != model.KeyResourceScope || e.PolicyEntries[19].Key != model.KeyCostProductionSizedSkuExempt {
		t.Error("entries are not in ADR-007 table order")
	}
}

func TestRenderOfAllKinds(t *testing.T) {
	f := parse(t, `version: 1
policy:
  tags:
    required: [{name: owner}, {name: cc, format: '^C'}]
  dataResidency: {scope: datazone-eu, regions: [swedencentral, francecentral]}
  monitoring: {publicTelemetry: true}
  managedByAzurePolicy: [diagnostic-settings]
`)
	e, _ := config.Resolve(config.Flags{}, f, "", profiles(t))
	cases := map[string]string{
		model.KeyTagsRequired:              "[owner, cc =~ ^C]",
		model.KeyDataResidencyScope:        "datazone-eu",
		model.KeyDataResidencyRegions:      "[swedencentral, francecentral]",
		model.KeyMonitoringPublicTelemetry: "true",
		model.KeyManagedByAzurePolicy:      "[diagnostic-settings]",
	}
	for k, want := range cases {
		if got := entry(t, e, k); got.Value != want || got.Baseline {
			t.Errorf("%s = %+v, want %q", k, got, want)
		}
	}
}

// TestPolicyKeysCoverModel fails when model.Policy gains a leaf that the effective-policy table lacks.
func TestPolicyKeysCoverModel(t *testing.T) {
	e, _ := config.Resolve(config.Flags{}, nil, "", profiles(t))
	have := map[string]bool{}
	for _, p := range e.PolicyEntries {
		have[p.Key] = true
	}
	var leaves []string
	var walk func(rt reflect.Type, prefix string)
	walk = func(rt reflect.Type, prefix string) {
		for i := 0; i < rt.NumField(); i++ {
			sf := rt.Field(i)
			name, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
			p := prefix + "." + name
			if sf.Type.Kind() == reflect.Struct && sf.Type != reflect.TypeFor[model.TagRequirement]() {
				walk(sf.Type, p)
				continue
			}
			leaves = append(leaves, p)
		}
	}
	walk(reflect.TypeFor[model.Policy](), "policy")
	for _, l := range leaves {
		if l == "policy.disasterRecovery.reference" { // travels with declared
			continue
		}
		if !have[l] {
			t.Errorf("model.Policy key %s has no effective-policy entry", l)
		}
	}
	if len(leaves)-1 != len(have) {
		t.Errorf("%d model leaves, %d entries", len(leaves), len(have))
	}
}

func TestProfileHelpers(t *testing.T) {
	ps := profiles(t)
	p, ok := ps.Get("foundry-prod")
	if !ok || len(p.RuleIDs()) != 108 {
		t.Fatalf("prod profile: %v %d", ok, len(p.RuleIDs()))
	}
	if _, ok := ps.Get("nope"); ok {
		t.Error("Get(nope) succeeded")
	}
	if s, ok := p.Severity("FND-ZZZ-999"); ok || s != "" {
		t.Errorf("unknown rule: %q %v", s, ok)
	}
	if got := sdk.SeverityError; !got.Valid() {
		t.Error("sdk changed")
	}
}

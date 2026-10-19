package config

import (
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const fullPolicyYAML = `version: 1
policy:
  resourceScope: same-subscription
  allowedExternalScopes: [/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/shared]
  identity:
    deploymentPrincipalIds: [11111111-2222-3333-4444-555555555555]
  environments:
    production: [prod]
    nonProduction: [test]
    development: [dev]
    tiers: {prod: prod, qa: test}
  tags:
    required:
      - name: owner
        format: '^[a-z]+$'
    resourceTypes: [Microsoft.CognitiveServices/accounts]
  logRetention: {minimumDays: 30}
  models:
    allow: [OpenAI/gpt-4o/2024-11-20]
    deny: [OpenAI/gpt-35-turbo]
  dataResidency:
    scope: datazone-eu
    regions: [swedencentral]
    deploymentSkus: [DataZoneStandard]
  disasterRecovery: {declared: true, reference: runbook-1}
  network: {publicAccess: entra-only, dnsManagedByPolicy: true, centralDns: true, cloud: usgov}
  monitoring: {publicTelemetry: true}
  managedByAzurePolicy: [private-dns-zone-group, diagnostic-settings]
  knowledge: {requireDocumentLevelAccess: true}
  cost: {devMaxCosmosThroughput: 1000, productionSizedSkuExemptions: [Standard]}
  preflight: {deploymentHistoryMargin: 70, regionMatrixStalenessDays: 30}
  reliability: {locksManagedOutsideTemplate: true, disasterRecovery: multi-region}
  runtime: {throttling: {maxRate429: 0.1, windowMinutes: 30, minRequests: 50}}
`

func TestEveryPolicyKeyIsLoadedAndFlattened(t *testing.T) {
	cfg, err := Load([]byte(fullPolicyYAML))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0)
	for k := range cfg.Policy.flatten() {
		got = append(got, k)
	}
	slices.Sort(got)
	if want := PolicyKeys(); !slices.Equal(got, want) {
		t.Fatalf("flattened keys differ from PolicyKeys:\n got %v\nwant %v", got, want)
	}
	eff, err := Resolve(cfg, Flags{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eff.Policy.Keys(), PolicyKeys()) {
		t.Fatalf("effective keys = %v", eff.Policy.Keys())
	}
	for k, want := range map[string]any{
		"resourceScope": "same-subscription", "network.cloud": "usgov",
		"logRetention.minimumDays": 30, "preflight.deploymentHistoryMargin": 70,
		"disasterRecovery.declared": true,
	} {
		if v, ok := eff.Policy.Get(k); !ok || v != want {
			t.Errorf("Get(%s) = %v, %v; want %v", k, v, ok, want)
		}
		if eff.Sources["policy."+k] != "repo" {
			t.Errorf("source of %s = %q, want repo", k, eff.Sources["policy."+k])
		}
	}
}

func TestPolicyValidationErrors(t *testing.T) {
	tests := []struct{ name, policy, want string }{
		{"external scope not a resource id", "allowedExternalScopes: [rg-shared]", "allowedExternalScopes"},
		{"principal id not a guid", "identity: {deploymentPrincipalIds: [alice]}", "GUID"},
		{"empty principal id", "identity: {deploymentPrincipalIds: ['']}", "must not be empty"},
		{"tier unknown", "environments: {tiers: {qa: staging}}", "environments.tiers"},
		{"tier env name invalid", "environments: {tiers: {'bad name!': prod}}", "valid environment name"},
		{"env in two groups", "environments: {production: [a], development: [A]}", "listed under both"},
		{"empty production entry", "environments: {production: ['']}", "must not be empty"},
		{"tag without name", "tags: {required: [{format: x}]}", "name is required"},
		{"empty resource type", "tags: {resourceTypes: ['']}", "must not be empty"},
		{"retention zero", "logRetention: {minimumDays: 0}", "at least 1"},
		{"bad model", "models: {allow: [gpt-4o]}", "format/name"},
		{"bad residency scope", "dataResidency: {scope: moon}", "dataResidency.scope"},
		{"empty region", "dataResidency: {regions: ['']}", "must not be empty"},
		{"bad public access", "network: {publicAccess: open}", "network.publicAccess"},
		{"bad cloud", "network: {cloud: mars}", "network.cloud"},
		{"bad managed", "managedByAzurePolicy: [everything]", "managedByAzurePolicy"},
		{"cosmos throughput zero", "cost: {devMaxCosmosThroughput: 0}", "at least 1"},
		{"empty exemption", "cost: {productionSizedSkuExemptions: ['']}", "must not be empty"},
		{"negative margin", "preflight: {deploymentHistoryMargin: -1}", "at least 0"},
		{"staleness zero", "preflight: {regionMatrixStalenessDays: 0}", "at least 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load([]byte("version: 1\npolicy:\n  " + tc.policy + "\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "policy.") {
				t.Errorf("error should name the policy key path: %v", err)
			}
		})
	}
}

func TestEnvironmentPolicyValidationNamesPath(t *testing.T) {
	_, err := Load([]byte("version: 1\nenvironments:\n  prod:\n    policy:\n      network: {cloud: mars}\n"))
	if err == nil || !strings.Contains(err.Error(), "environments.prod.policy.network.cloud") {
		t.Fatalf("err = %v", err)
	}
	_, err = Load([]byte("version: 1\nenvironments:\n  'bad name!':\n    policy: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid environment name") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"bad inputs.environment", "version: 1\ninputs:\n  environment: '-x'\n", "inputs.environment"},
		{"nul in path", "version: 1\ninputs:\n  infraPath: \"a\\0b\"\n", "NUL"},
		{"bad include", "version: 1\nrules:\n  include: [everything]\n", "rules.include"},
		{"bad exclude", "version: 1\nrules:\n  exclude: [SEC-1]\n", "rules.exclude"},
		{"empty pack", "version: 1\nrules:\n  packs: ['']\n", "rules.packs"},
		{"empty adapter", "version: 1\nadvanced:\n  adapters: [' ']\n", "advanced.adapters"},
		{"tristate maybe", "version: 1\nadvanced:\n  psrule:\n    enabled: maybe\n", "auto, true or false"},
		{"tristate map", "version: 1\nadvanced:\n  psrule:\n    enabled: {a: 1}\n", "auto, true or false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
	// Oversized files are rejected before parsing.
	if _, err := Load([]byte(strings.Repeat("#", MaxConfigBytes+1))); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized: %v", err)
	}
	// All problems are reported together.
	_, err := Load([]byte("version: 1\nrules:\n  include: [x]\nvalidation:\n  bicep: maybe\n"))
	if err == nil || !strings.Contains(err.Error(), "rules.include") || !strings.Contains(err.Error(), "validation.bicep") {
		t.Fatalf("want both problems: %v", err)
	}
}

func TestTristateYAMLRoundTrip(t *testing.T) {
	cfg, err := Load([]byte("version: 1\nadvanced:\n  psrule: {enabled: true}\n  checkov: {enabled: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Advanced.PSRule.Enabled != True || cfg.Advanced.Checkov.Enabled != False {
		t.Fatalf("advanced = %+v", cfg.Advanced)
	}
	out, err := yaml.Marshal(cfg.Advanced)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "enabled: true") || !strings.Contains(s, "enabled: false") {
		t.Fatalf("tristate must marshal as booleans:\n%s", s)
	}
	auto, err := yaml.Marshal(ToolConfig{Enabled: Auto})
	if err != nil || !strings.Contains(string(auto), "enabled: auto") {
		t.Fatalf("auto marshal = %q, %v", auto, err)
	}
}

func TestEffectivePolicyGetReturnsCopies(t *testing.T) {
	p := EffectivePolicy{"managedByAzurePolicy": []string{"diagnostic-settings"}, "tags.required": []TagRequirement{{Name: "owner"}}}
	v, _ := p.Get("managedByAzurePolicy")
	v.([]string)[0] = "mutated"
	if again, _ := p.Get("managedByAzurePolicy"); again.([]string)[0] != "diagnostic-settings" {
		t.Fatal("Get must return a copy of lists")
	}
	tv, _ := p.Get("tags.required")
	tv.([]TagRequirement)[0].Name = "mutated"
	if again, _ := p.Get("tags.required"); again.([]TagRequirement)[0].Name != "owner" {
		t.Fatal("Get must return a copy of tag requirements")
	}
	if v, ok := p.Get("absent"); ok || v != nil {
		t.Fatalf("absent key = %v, %v", v, ok)
	}
}

func TestEffectivePolicyDumpIsDeterministic(t *testing.T) {
	p := EffectivePolicy{"b": 1, "a": "x", "c": []string{"z"}}
	first, err := p.Dump()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if again, _ := p.Dump(); again != first {
			t.Fatalf("dump changed:\n%s\n%s", first, again)
		}
	}
	if strings.Index(first, "a:") > strings.Index(first, "b:") {
		t.Fatalf("keys not sorted:\n%s", first)
	}
	if got := p.Keys(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("Keys = %v", got)
	}
}

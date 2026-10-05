package config_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func policyDoc(body string) string { return "version: 1\npolicy:\n" + body }

func TestPolicyValueErrors(t *testing.T) {
	cases := []struct {
		name, doc string
		path      string // expected key path in the error
		line      int
		msg       string
	}{
		{"scope enum", policyDoc("  resourceScope: galaxy\n"), "policy.resourceScope", 3, "not one of"},
		{"scope id shape", policyDoc("  allowedExternalScopes: [rg-x]\n"), "policy.allowedExternalScopes[0]", 3, "not a resource ID"},
		{"scope id space", policyDoc("  allowedExternalScopes: ['/subscriptions/a b']\n"), "allowedExternalScopes[0]", 3, "not a resource ID"},
		{"scope id dup case-insensitive", policyDoc("  allowedExternalScopes: [/subscriptions/A, /subscriptions/a]\n"), "allowedExternalScopes[1]", 3, "duplicate"},
		{"env name shape", policyDoc("  environments:\n    production: ['bad name']\n"), "environments.production[0]", 4, "does not match"},
		{"env dup", policyDoc("  environments:\n    production: [prod, prod]\n"), "environments.production[1]", 4, "duplicate"},
		{"env prod and nonprod", policyDoc("  environments:\n    production: [x]\n    nonProduction: [x]\n"), "environments.nonProduction", 5, "production and nonProduction"},
		{"env prod and dev", policyDoc("  environments:\n    production: [x]\n    development: [x]\n"), "environments.development", 5, "production and development"},
		{"tag name empty", policyDoc("  tags:\n    required:\n      - name: ''\n"), "required[0].name", 5, "required"},
		{"tag name padded", policyDoc("  tags:\n    required:\n      - name: ' a'\n"), "required[0].name", 5, "whitespace"},
		{"tag name dup ci", policyDoc("  tags:\n    required:\n      - name: Owner\n      - name: owner\n"), "required[1].name", 6, "case-insensitively"},
		{"tag regex invalid", policyDoc("  tags:\n    required:\n      - name: a\n        format: '(['\n"), "required[0].format", 6, "regular expression"},
		{"tag regex lookahead (RE2)", policyDoc("  tags:\n    required:\n      - name: a\n        format: '(?=x)'\n"), "required[0].format", 6, "regular expression"},
		{"tag regex too long", policyDoc("  tags:\n    required:\n      - name: a\n        format: '" + strings.Repeat("a", 2000) + "'\n"), "required[0].format", 6, "longer than"},
		{"resource type shape", policyDoc("  tags:\n    resourceTypes: [accounts]\n"), "resourceTypes[0]", 4, "resource type"},
		{"resource type dup ci", policyDoc("  tags:\n    resourceTypes: [Microsoft.A/b, microsoft.a/B]\n"), "resourceTypes[1]", 4, "duplicate"},
		{"negative days", policyDoc("  logRetention:\n    minimumDays: -1\n"), "policy.logRetention.minimumDays", 4, "non-negative"},
		{"model one segment", policyDoc("  models:\n    allow: [gpt-4o]\n"), "models.allow[0]", 4, "format/name"},
		{"model four segments", policyDoc("  models:\n    deny: [a/b/c/d]\n"), "models.deny[0]", 4, "format/name"},
		{"model empty segment", policyDoc("  models:\n    allow: ['a//c']\n"), "models.allow[0]", 4, "format/name"},
		{"model space", policyDoc("  models:\n    allow: ['a/b c']\n"), "models.allow[0]", 4, "format/name"},
		{"model dup", policyDoc("  models:\n    deny: [a/b, a/b]\n"), "models.deny[1]", 4, "duplicate"},
		{"model allow and deny", policyDoc("  models:\n    allow: [a/b]\n    deny: [a/b]\n"), "models.deny", 5, "both allow and deny"},
		{"residency scope", policyDoc("  dataResidency:\n    scope: mars\n"), "dataResidency.scope", 4, "not one of"},
		{"region uppercase", policyDoc("  dataResidency:\n    regions: [WestEurope]\n"), "regions[0]", 4, "does not match"},
		{"region spaced", policyDoc("  dataResidency:\n    regions: ['west europe']\n"), "regions[0]", 4, "does not match"},
		{"region dup", policyDoc("  dataResidency:\n    regions: [westeurope, westeurope]\n"), "regions[1]", 4, "duplicate"},
		{"region empty", policyDoc("  dataResidency:\n    regions: ['']\n"), "regions[0]", 4, "empty entry"},
		{"sku shape", policyDoc("  dataResidency:\n    deploymentSkus: ['Global Standard']\n"), "deploymentSkus[0]", 4, "does not match"},
		{"dr reference blank", policyDoc("  disasterRecovery:\n    reference: '  '\n"), "disasterRecovery.reference", 4, "non-empty"},
		{"public access", policyDoc("  network:\n    publicAccess: yes-please\n"), "network.publicAccess", 4, "not one of"},
		{"managed by", policyDoc("  managedByAzurePolicy: [nsg]\n"), "managedByAzurePolicy[0]", 3, "not one of"},
		{"managed by dup", policyDoc("  managedByAzurePolicy: [diagnostic-settings, diagnostic-settings]\n"), "managedByAzurePolicy[1]", 3, "duplicate"},
		{"cosmos negative", policyDoc("  cost:\n    devMaxCosmosThroughput: -1\n"), "devMaxCosmosThroughput", 4, "non-negative"},
		{"exemption blank", policyDoc("  cost:\n    productionSizedSkuExemptions: [' ']\n"), "productionSizedSkuExemptions[0]", 4, "non-empty"},
		{"exemption dup", policyDoc("  cost:\n    productionSizedSkuExemptions: [a, a]\n"), "productionSizedSkuExemptions[1]", 4, "duplicate"},
		{"env policy error is located under environments", "version: 1\nenvironments:\n  prod:\n    policy:\n      network:\n        publicAccess: nope\n", "environments.prod.policy.network.publicAccess", 6, "not one of"},
		{"env name shape", "version: 1\nenvironments:\n  'a b':\n    policy: {}\n", "environments.a b", 3, "must match"},
		{"profile", "version: 1\nprofile: PROD\n", "profile", 2, "unknown profile"},
		{"validation mode", "version: 1\nvalidation:\n  azure: sometimes\n", "validation.azure", 3, "not one of"},
		{"rules selector", "version: 1\nrules:\n  include: ['bad selector']\n", "rules.include[0]", 3, "does not match"},
		{"rules dup", "version: 1\nrules:\n  exclude: [FND-CFG-001, FND-CFG-001]\n", "rules.exclude[1]", 3, "duplicate"},
		{"pack shape", "version: 1\nrules:\n  packs: ['*']\n", "rules.packs[0]", 3, "does not match"},
		{"absolute input", "version: 1\ninputs:\n  azureYaml: /etc/passwd\n", "inputs.azureYaml", 3, "relative"},
		{"windows drive", "version: 1\ninputs:\n  infraPath: 'C:\\infra'\n", "inputs.infraPath", 3, "relative"},
		{"escaping input", "version: 1\ninputs:\n  infraPath: ../../etc\n", "inputs.infraPath", 3, "inside the project root"},
		{"escaping via clean", "version: 1\nadoption:\n  baseline: a/../../b\n", "adoption.baseline", 3, "inside the project root"},
		{"input env", "version: 1\ninputs:\n  environment: 'a b'\n", "inputs.environment", 3, "must match"},
		{"format enum", "version: 1\noutputs:\n  formats: [html]\n", "outputs.formats[0]", 3, "not one of"},
		{"format dup", "version: 1\noutputs:\n  formats: [json, json]\n", "outputs.formats[1]", 3, "duplicate"},
		{"output dir escape", "version: 1\noutputs:\n  directory: ../out\n", "outputs.directory", 3, "inside"},
		{"toggle", "version: 1\nadvanced:\n  psrule:\n    enabled: yes\n", "advanced.psrule.enabled", 4, "auto, true, false"},
		{"tool config abs", "version: 1\nadvanced:\n  checkov:\n    config: /tmp/c.yaml\n", "advanced.checkov.config", 4, "relative"},
		{"bicep exec path", "version: 1\nadvanced:\n  bicep:\n    executable: ./bin/bicep\n", "advanced.bicep.executable", 4, "bare command"},
		{"bicep config escape", "version: 1\nadvanced:\n  bicep:\n    config: ../x\n", "advanced.bicep.config", 4, "inside"},
		{"adapter shape", "version: 1\nadvanced:\n  adapters: ['a b']\n", "advanced.adapters[0]", 3, "does not match"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.Parse("c.yaml", []byte(c.doc))
			if err == nil {
				t.Fatal("accepted")
			}
			var found *config.Error
			for _, p := range err.(*config.InvalidError).Problems {
				if strings.Contains(p.Path, c.path) && strings.Contains(p.Msg, c.msg) {
					found = p
				}
			}
			if found == nil {
				t.Fatalf("no problem with path %q and message %q in:\n%v", c.path, c.msg, err)
			}
			if found.Line != c.line {
				t.Errorf("line = %d, want %d (%v)", found.Line, c.line, found)
			}
		})
	}
}

func TestPolicyValuesAccepted(t *testing.T) {
	cases := []string{
		policyDoc("  logRetention:\n    minimumDays: 0\n"),
		policyDoc("  cost:\n    devMaxCosmosThroughput: 0\n"),
		policyDoc("  models:\n    allow: [OpenAI/gpt-4o, OpenAI/gpt-4o/2024-11-20]\n    deny: [OpenAI/gpt-35-turbo]\n"),
		policyDoc("  tags:\n    required:\n      - {name: owner}\n      - {name: cost, format: '^[A-Z]{2}-\\d+$'}\n    resourceTypes: [Microsoft.CognitiveServices/accounts/deployments]\n"),
		policyDoc("  dataResidency:\n    scope: geography\n    regions: [swedencentral]\n    deploymentSkus: [GlobalStandard]\n"),
		policyDoc("  disasterRecovery:\n    declared: false\n"),
		policyDoc("  allowedExternalScopes: ['/providers/Microsoft.Management/managementGroups/mg1']\n"),
		policyDoc("  environments:\n    production: [prod]\n    nonProduction: [test, dev]\n    development: [dev]\n"),
	}
	for i, doc := range cases {
		if _, err := config.Parse("c.yaml", []byte(doc)); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestValidatePolicyValuesOnBareStruct(t *testing.T) {
	if got := config.ValidatePolicyValues(model.Policy{}); len(got) != 0 {
		t.Errorf("zero policy has problems: %v", got)
	}
	bad := model.PublicAccess("x")
	neg := -1
	got := config.ValidatePolicyValues(model.Policy{Network: model.PolicyNetwork{PublicAccess: &bad}, LogRetention: model.PolicyLogRetention{MinimumDays: &neg}})
	if len(got) != 2 || got[0].Path != "policy.logRetention.minimumDays" || got[1].Path != "policy.network.publicAccess" {
		t.Errorf("got %v", got)
	}
}

func TestErrorFormat(t *testing.T) {
	cases := []struct {
		e    config.Error
		want string
	}{
		{config.Error{File: "a.yaml", Line: 3, Path: "x.y", Msg: "boom"}, "config a.yaml:3: x.y: boom"},
		{config.Error{File: "a.yaml", Msg: "boom"}, "config a.yaml: boom"},
		{config.Error{Msg: "boom"}, "config: boom"},
	}
	for _, c := range cases {
		if got := c.e.Error(); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

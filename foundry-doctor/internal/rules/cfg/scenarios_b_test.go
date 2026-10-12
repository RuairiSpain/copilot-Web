package cfg_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func cfgRules() []rules.Rule { return cfg.Rules() }

// canary builds a fake secret at run time so no secret-shaped literal is committed.
func canary() string { return "sk-" + strings.Repeat("Zq7x", 8) }

// ---- FND-CFG-004 ----

func scenarios004() scenarioSet {
	const id = "FND-CFG-004"
	prompt := func(model, instr string) string {
		return "name: d\nservices:\n  ai-project:\n    host: azure.ai.project\n    deployments:\n      - name: gpt-a\n  helper:\n    host: azure.ai.agent\n    project: src/helper\n    uses: [ai-project]\n    kind: prompt\n    model: " + model + "\n    instructions: " + instr + "\n"
	}
	hosted := func(body string) string {
		return "name: d\nservices:\n  h:\n    host: azure.ai.agent\n    kind: hosted\n" + body
	}
	return scenarioSet{
		"prompt-agent-model-not-in-project-deployments": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, prompt("gpt-b", "hi"))), sdk.StatusFail, "prompt-model-deployment:")
		},
		"prompt-agent-blank-instructions": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, prompt("gpt-a", "'  '"))), sdk.StatusFail, "prompt-instructions:")
		},
		"hosted-agent-project-dir-missing": func(t *testing.T) {
			// Input carries no file tree, so the directory check is skipped with a named capability.
			rs := eval(t, id, yamlInput(t, hosted("    project: src/gone\n")))
			wantNoFindings(t, rs)
			if s := wantSkip(t, rs, "unresolved-value:file-tree-unavailable"); s.MissingCapability != "project file tree" {
				t.Fatalf("%+v", s)
			}
			wantFinding(t, eval(t, id, yamlInput(t, hosted(""))), sdk.StatusFail, "hosted-source:")
		},
		"hosted-agent-code-configuration-and-image": func(t *testing.T) {
			y := hosted("    project: src/h\n    image: reg.example/x:1\n    codeConfiguration:\n      runtime: python_3_12\n      entryPoint: main.py\n")
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "code-and-image:")
		},
		"hosted-agent-cpu-above-4": func(t *testing.T) {
			y := hosted("    image: reg.example/x:1\n    container:\n      resources:\n        cpu: \"8\"\n        memory: 16Gi\n")
			rs := eval(t, id, yamlInput(t, y))
			wantFinding(t, rs, sdk.StatusFail, "cpu-range:")
			wantFinding(t, rs, sdk.StatusFail, "memory-range:")
			ok := hosted("    image: reg.example/x:1\n    container:\n      resources:\n        cpu: 500m\n        memory: 512Mi\n")
			wantPass(t, eval(t, id, yamlInput(t, ok))) // 0.5 cpu is inside the range; 512Mi is exactly 0.5Gi
		},
		"code-configuration-missing-entrypoint": func(t *testing.T) {
			y := hosted("    project: src/h\n    codeConfiguration:\n      runtime: python_3_12\n")
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "code-entryPoint:")
		},
		"prompt-agent-model-matches-deployment": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, prompt("gpt-a", "hi"))))
		},
		"hosted-agent-prebuilt-image": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, hosted("    image: reg.example/x:1\n"))))
		},
		"hosted-agent-code-deploy": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, hosted("    project: src/h\n    codeConfiguration:\n      runtime: python_3_12\n      entryPoint: main.py\n"))))
		},
		"project-uses-existing-endpoint": func(t *testing.T) {
			y := strings.Replace(prompt("gpt-b", "hi"), "    deployments:\n      - name: gpt-a\n", "    endpoint: https://a.services.ai.azure.com/api/projects/p\n", 1)
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:existing-project-endpoint")
		},
		"deployments-in-ref-file-unreadable": func(t *testing.T) {
			y := strings.Replace(prompt("gpt-b", "hi"), "      - name: gpt-a\n", "      - $ref: ./deployments.yaml\n", 1)
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:deployments-ref-file")
		},
	}
}

// ---- FND-CFG-005 ----

func scenarios005() scenarioSet {
	const id = "FND-CFG-005"
	conn := func(auth, creds string) string {
		return "name: d\nservices:\n  conn:\n    host: azure.ai.connection\n    category: RemoteTool\n    authType: " + auth + "\n" + creds
	}
	return scenarioSet{
		"connection-credentials-literal-api-key": func(t *testing.T) {
			secret := canary()
			rs := eval(t, id, yamlInput(t, conn("ApiKey", "    credentials:\n      key: "+secret+"\n")))
			f := wantFinding(t, rs, sdk.StatusFail, "credential:conn:credentials.key")
			if f.Confidence != sdk.ConfidenceCertain {
				t.Fatalf("known token shape should be certain, got %s", f.Confidence)
			}
			// A name-only match is likely.
			rs = eval(t, id, yamlInput(t, conn("CustomKeys", "    credentials:\n      Authorization: hunter2\n")))
			if f := wantFinding(t, rs, sdk.StatusFail, "credential:conn:credentials.Authorization"); f.Confidence != sdk.ConfidenceLikely {
				t.Fatalf("got %s", f.Confidence)
			}
		},
		"service-env-literal-token": func(t *testing.T) {
			y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n    env:\n      GITHUB_TOKEN: abc123\n      KEY_VAULT_NAME: myvault\n      LOG_LEVEL: info\n"
			rs := eval(t, id, yamlInput(t, y))
			wantFinding(t, rs, sdk.StatusFail, "env:a:GITHUB_TOKEN")
			for _, r := range withStatus(rs, sdk.StatusFail) {
				if strings.Contains(r.Finding.Key, "KEY_VAULT_NAME") || strings.Contains(r.Finding.Key, "LOG_LEVEL") {
					t.Fatalf("false positive %s", r.Finding.Key)
				}
			}
		},
		"target-url-with-sas-signature": func(t *testing.T) {
			y := conn("None", "    target: https://acct.blob.core.windows.net/c?sv=2024-01-01&sig=abc\n")
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "url-sas:conn:target:sig")
		},
		"target-url-with-userinfo": func(t *testing.T) {
			y := conn("None", "    target: https://user:pw@example.com/mcp\n")
			f := wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "url-userinfo:conn:target")
			if strings.Contains(f.Evidence, "pw") {
				t.Fatal("evidence echoes the password")
			}
		},
		"dotenv-tracked-in-git": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, "name: d\nservices:\n  a:\n    host: azure.ai.agent\n"))
			if s := wantSkip(t, rs, "unresolved-value:git-tracked-files-unavailable"); s.MissingCapability != project.CapabilityTrackedFiles {
				t.Fatalf("%+v", s)
			}
		},
		"credentials-use-env-reference": func(t *testing.T) {
			wantNoFindings(t, eval(t, id, yamlInput(t, conn("ApiKey", "    credentials:\n      key: ${SEARCH_KEY}\n    target: https://u:${P}@example.com\n"))))
		},
		"foundry-server-side-expression-passthrough": func(t *testing.T) {
			y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n    env:\n      API_TOKEN: ${{project.token}}\n      SECRET_KEY: $${{x}}\n"
			// $${{x}} is the escaped Foundry form; it still contains no literal secret text of its own.
			rs := eval(t, id, yamlInput(t, y))
			for _, r := range withStatus(rs, sdk.StatusFail) {
				if strings.Contains(r.Finding.Key, "API_TOKEN") {
					t.Fatal("a ${{...}} expression is not a raw secret")
				}
			}
		},
		"managed-identity-connection-no-credentials": func(t *testing.T) {
			wantNoFindings(t, eval(t, id, yamlInput(t, conn("ManagedIdentity", "    target: https://example.com\n"))))
		},
		"bicep-cli-missing": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, "name: d\nservices:\n  a:\n    host: azure.ai.agent\n", withIaC(model.IaCBicep)))
			wantSkip(t, rs, "tool-missing:bicep")
		},
		"checkov-not-configured": func(t *testing.T) {
			avail := withTools(sdk.ToolStatus{Name: "bicep", State: sdk.ToolAvailable})
			rs := eval(t, id, yamlInput(t, "name: d\nservices:\n  a:\n    host: azure.ai.agent\n", withIaC(model.IaCBicep), avail))
			wantSkip(t, rs, "tool-missing:checkov")
			for _, r := range withStatus(rs, sdk.StatusSkipped) {
				if r.Skip.Reason == "tool-missing:bicep" {
					t.Fatal("bicep is available")
				}
			}
		},
	}
}

// ---- FND-CFG-006 ----

func scenarios006() scenarioSet {
	const id = "FND-CFG-006"
	withEnvKeys := withEnv(map[string]string{"OTHER": "x"})
	svc := func(env string) string {
		return "name: d\nservices:\n  bot:\n    host: azure.ai.agent\n    env:\n" + env
	}
	return scenarioSet{
		"reference-missing-from-env-and-outputs": func(t *testing.T) {
			f := wantFinding(t, eval(t, id, yamlInput(t, svc("      A: ${MISSING_VAR}\n"), withEnvKeys)), sdk.StatusFail, "ref:MISSING_VAR")
			if f.Confidence != sdk.ConfidenceLikely || f.Resource.Name != "bot" || f.Location.Line == 0 {
				t.Fatalf("%+v", f)
			}
		},
		"env-declares-foundry-prefixed-variable": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, svc("      FOUNDRY_PROJECT_ENDPOINT: x\n"), withEnvKeys)), sdk.StatusFail, "reserved-env:bot:FOUNDRY_PROJECT_ENDPOINT")
		},
		"agent-endpoint-reference-for-undeclared-service": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, svc("      A: ${AGENT_GHOST_ENDPOINT}\n      B: ${AGENT_BOT_ENDPOINT}\n"), withEnvKeys))
			wantFinding(t, rs, sdk.StatusFail, "ref:AGENT_GHOST_ENDPOINT")
			for _, r := range withStatus(rs, sdk.StatusFail) {
				if r.Finding.Key == "ref:AGENT_BOT_ENDPOINT" {
					t.Fatal("declared agent service has a producer")
				}
			}
		},
		"reference-with-inline-default": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, svc("      A: ${LOG_LEVEL:-info}\n      B: ${AZURE_LOCATION}\n"), withEnvKeys)))
		},
		"reference-produced-by-bicep-output": func(t *testing.T) {
			in := yamlInput(t, svc("      A: ${AZURE_AI_ACCOUNT_NAME}\n"), withEnvKeys, withARM(t, "rules/cfg/arm-location-westus3.json"))
			wantPass(t, eval(t, id, in))
		},
		"foundry-server-side-expression-untouched": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, svc("      A: ${{project.endpoint}}\n      B: $${{project.endpoint}}\n"), withEnvKeys)))
		},
		"escaped-dollar-reference": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, svc("      A: $${LITERAL}\n      B: $NOT_EXPANDED\n"), withEnvKeys)))
		},
		"no-environment-selected": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, svc("      A: ${MISSING_VAR}\n")))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:no-environment-selected")
		},
		"bicep-not-compiled": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, svc("      A: ${MISSING_VAR}\n"), withEnvKeys, withIaC(model.IaCBicep)))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:compiled-arm-unavailable")
			rs = eval(t, id, yamlInput(t, svc("      A: ${MISSING_VAR}\n"), withEnvKeys, withIaC(model.IaCSynthetic)))
			wantSkip(t, rs, rules.ReasonSyntheticInfrastructure)
		},
	}
}

// ---- FND-CFG-007 ----

func scenarios007() scenarioSet {
	const id = "FND-CFG-007"
	conn := func(target string) string {
		return "name: d\nservices:\n  conn:\n    host: azure.ai.connection\n    category: RemoteTool\n    target: " + target + "\n"
	}
	return scenarioSet{
		"connection-target-http": func(t *testing.T) {
			f := wantFinding(t, eval(t, id, yamlInput(t, conn("http://api.example.com/mcp"))), sdk.StatusFail, "scheme:conn:target")
			if strings.Contains(f.Evidence, "api.example.com") {
				t.Fatal("evidence echoes the URL")
			}
		},
		"mcp-tool-url-localhost": func(t *testing.T) {
			y := "name: d\nservices:\n  tb:\n    host: azure.ai.toolbox\n    tools:\n      - type: mcp\n        server_url: https://localhost:8088/mcp\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "host:tb:tools[0].server_url:loopback")
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://[::1]:8080/x"))), sdk.StatusFail, "host:conn:target:loopback")
		},
		"target-metadata-ip-169-254-169-254": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://169.254.169.254/latest"))), sdk.StatusFail, "host:conn:target:metadata")
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://metadata.google.internal/x"))), sdk.StatusFail, "host:conn:target:metadata")
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://169.254.1.1/x"))), sdk.StatusFail, "host:conn:target:link-local")
		},
		"target-private-ip-literal": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://10.0.0.5/mcp"))), sdk.StatusFail, "host:conn:target:private")
			wantFinding(t, eval(t, id, yamlInput(t, conn("https://192.168.1.9/mcp"))), sdk.StatusFail, "host:conn:target:private")
		},
		"connection-target-https": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, conn("https://api.githubcopilot.com/mcp"))))
		},
		"target-is-arm-resource-id": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, conn("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/st"))))
		},
		"target-is-env-reference-only": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, conn("${MCP_URL}")))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:url-is-env-reference")
		},
		"target-resolved-only-at-deploy": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, conn("'${{project.endpoint}}'")))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:url-is-env-reference")
			// A reference in the host leaves the scheme judgeable.
			wantFinding(t, eval(t, id, yamlInput(t, conn("http://${HOST}/mcp"))), sdk.StatusFail, "scheme:conn:target")
		},
	}
}

// ---- FND-CFG-011 ----

func scenarios011() scenarioSet {
	const id = "FND-CFG-011"
	known := withVersions(model.Versions{Extensions: map[string]string{"azure.ai.agents": "1.0.0-beta.18", "azure.ai.projects": "1.0.0-beta.13"}})
	agent := func(body string) string {
		return "name: d\nservices:\n  bot:\n    host: azure.ai.agent\n    project: src/bot\n" + body
	}
	return scenarioSet{
		"legacy-config-env": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, agent("    config:\n      env:\n        A: b\n"), known)), sdk.StatusFail, "config:bot")
		},
		"microsoft-foundry-host": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, "name: d\nservices:\n  p:\n    host: microsoft.foundry\n", known)), sdk.StatusFail, "host-microsoft-foundry:p")
		},
		"prompt-voice-kind": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, agent("    kind: prompt-voice\n"), known)), sdk.StatusFail, "kind-prompt-voice:bot")
		},
		"env-declares-foundry-endpoint": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, agent("    env:\n      FOUNDRY_PROJECT_ENDPOINT: x\n"), known)), sdk.StatusFail, "reserved-env:bot:FOUNDRY_PROJECT_ENDPOINT")
			wantFinding(t, eval(t, id, yamlInput(t, agent("    kind: hosted\n    displayName: Bot\n"), known)), sdk.StatusFail, "ignored-field:bot:displayName")
		},
		"requiredversions-below-minimum": func(t *testing.T) {
			y := "name: d\nrequiredVersions:\n  extensions:\n    azure.ai.agents: \">=1.0.0-beta.2\"\n    azure.ai.projects: \">=1.0.0-beta.4 <2.0.0\"\nservices:\n  bot:\n    host: azure.ai.agent\n    project: src/bot\n"
			rs := eval(t, id, yamlInput(t, y, known))
			wantFinding(t, rs, sdk.StatusFail, "required-version:azure.ai.agents")
			for _, r := range withStatus(rs, sdk.StatusFail) {
				if r.Finding.Key == "required-version:azure.ai.projects" {
					t.Fatal("the minimum itself is allowed")
				}
			}
			old := withVersions(model.Versions{Extensions: map[string]string{"azure.ai.agents": "1.0.0-beta.7"}})
			wantFinding(t, eval(t, id, yamlInput(t, agent(""), old)), sdk.StatusFail, "installed-version:azure.ai.agents")
		},
		"unified-service-level-env": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, agent("    env:\n      LOG_LEVEL: info\n"), known)))
		},
		"kind-voice": func(t *testing.T) {
			wantPass(t, eval(t, id, yamlInput(t, agent("    kind: voice\n    model: gpt-a\n"), known)))
		},
		"installed-extension-versions-unavailable": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, agent("")))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:installed-extension-versions-unavailable")
		},
	}
}

// ---- FND-CFG-012 ----

func scenarios012() scenarioSet {
	const id = "FND-CFG-012"
	y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n"
	env := func(loc string) inputOpt { return withEnv(map[string]string{"AZURE_LOCATION": loc}) }
	return scenarioSet{
		"location-with-space-and-capitals": func(t *testing.T) {
			f := wantFinding(t, eval(t, id, yamlInput(t, y, env("East US"))), sdk.StatusFail, "format:AZURE_LOCATION")
			if !strings.Contains(f.Evidence, "uppercase") || !strings.Contains(f.Evidence, "whitespace") {
				t.Fatal(f.Evidence)
			}
		},
		"location-not-in-subscription-list": func(t *testing.T) {
			// Membership needs the control plane, which Input does not carry: skipped, never passed.
			rs := eval(t, id, yamlInput(t, y, env("atlantis")))
			wantNoFindings(t, rs)
			if s := wantSkip(t, rs, rules.ReasonControlPlaneUnavailable); s.MissingCapability != "Subscriptions_ListLocations result" {
				t.Fatalf("%+v", s)
			}
		},
		"azure-location-differs-from-parameter-file": func(t *testing.T) {
			in := yamlInput(t, y, env("eastus2"), withARM(t, "rules/cfg/arm-location-westus3.json"))
			wantFinding(t, eval(t, id, in), sdk.StatusFail, "location-mismatch")
		},
		"lowercase-region-in-subscription-list": func(t *testing.T) {
			in := yamlInput(t, y, env("westus3"), withARM(t, "rules/cfg/arm-location-westus3.json"))
			rs := eval(t, id, in)
			wantNoFindings(t, rs)
			wantSkip(t, rs, rules.ReasonControlPlaneUnavailable)
		},
		"control-plane-unavailable": func(t *testing.T) {
			wantSkip(t, eval(t, id, yamlInput(t, y, env("eastus2"))), rules.ReasonControlPlaneUnavailable)
			wantSkip(t, eval(t, id, yamlInput(t, y)), "unresolved-value:no-location-source")
		},
		"location-is-env-reference": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, y, env("${LOC}")))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:location-is-env-reference")
			in := yamlInput(t, y, withARM(t, "rules/cfg/arm-location-expression.json"))
			rs = eval(t, id, in)
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:arm-expression")
		},
	}
}

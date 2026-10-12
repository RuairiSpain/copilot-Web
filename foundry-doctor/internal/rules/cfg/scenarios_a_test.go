package cfg_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type scenarioSet map[string]func(*testing.T)

// ---- FND-CFG-001 ----

func scenarios001() scenarioSet {
	const id = "FND-CFG-001"
	failWith := func(file, prefix string) func(*testing.T) {
		return func(t *testing.T) {
			rs := eval(t, id, fixtureInput(t, "azureyaml/"+file))
			f := wantFinding(t, rs, sdk.StatusFail, prefix)
			if f.Location.Line == 0 || f.Location.File != "azure.yaml" {
				t.Fatalf("location %+v", f.Location)
			}
		}
	}
	passWith := func(file string) func(*testing.T) {
		return func(t *testing.T) { wantNoFindings(t, eval(t, id, fixtureInput(t, "azureyaml/"+file))) }
	}
	return scenarioSet{
		"duplicate-service-key":                       failWith("duplicate-service-key.azure.yaml", "duplicate:"),
		"unknown-host-missing-host-field":             failWith("unknown-host-missing-host-field.azure.yaml", "schema:missing-required:"),
		"agent-without-project":                       failWith("agent-without-project.azure.yaml", "schema:missing-required:"),
		"prompt-agent-missing-model-and-instructions": failWith("prompt-agent-missing-model-and-instructions.azure.yaml", "schema:missing-required:"),
		"project-service-with-project-path":           failWith("project-service-with-project-path.azure.yaml", "schema:forbidden-property:"),
		"voice-modeltype-hosted-agent":                failWith("voice-modeltype-hosted-agent.azure.yaml", "schema:unsupported-shape:"),
		"minimal-hosted-agent-project": func(t *testing.T) {
			wantPass(t, eval(t, id, fixtureInput(t, "azureyaml/minimal-hosted-agent-project.azure.yaml")))
		},
		"full-example-from-reference":    passWith("full-example-from-reference.azure.yaml"),
		"unknown-extra-property-allowed": passWith("unknown-extra-property-allowed.azure.yaml"),
		"schema-version-not-vendored-for-declared-extension": func(t *testing.T) {
			in := fixtureInput(t, "azureyaml/agent-without-project.azure.yaml",
				withVersions(model.Versions{Extensions: map[string]string{"azure.ai.agents": "1.0.0-beta.19"}}))
			rs := eval(t, id, in)
			wantNoFindings(t, rs)
			wantSkip(t, rs, rules.ReasonUnsupportedVersion)
		},
		"azure-yaml-uses-ref-file-missing": func(t *testing.T) {
			rs := eval(t, id, yamlInput(t, "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    $ref: ./missing.yaml\n"))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:ref-file")
		},
	}
}

// ---- FND-CFG-002 ----

const dupAgents = `name: demo
services:
  ai-project:
    host: azure.ai.project
  one:
    host: azure.ai.agent
    project: src/one
    uses: [ai-project]
    name: same
  two:
    host: azure.ai.agent
    project: src/two
    uses: [ai-project]
    name: same
`

func scenarios002() scenarioSet {
	const id = "FND-CFG-002"
	return scenarioSet{
		"two-agents-same-name-field": func(t *testing.T) {
			f := wantFinding(t, eval(t, id, yamlInput(t, dupAgents)), sdk.StatusFail, "dup-name:azure.ai.agent:same")
			if f.Resource.Name != "two" || f.Confidence != sdk.ConfidenceCertain {
				t.Fatalf("%+v", f)
			}
		},
		"connection-payload-name-collides-with-other-service-key": func(t *testing.T) {
			y := "name: d\nservices:\n  a:\n    host: azure.ai.connection\n    name: ' b '\n  b:\n    host: azure.ai.connection\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "dup-name:azure.ai.connection:b")
		},
		"two-routines-resolving-same-name": func(t *testing.T) {
			// A routine's name is its service key, so only a repeated key collides. The reader keeps both
			// entries in Services; build that shape by repeating the service in the model.
			in := yamlInput(t, "name: d\nservices:\n  nightly:\n    host: azure.ai.routine\n")
			in.AzureYAML.Services = append(in.AzureYAML.Services, in.AzureYAML.Services[0])
			wantFinding(t, eval(t, id, in), sdk.StatusFail, "dup-name:azure.ai.routine:nightly")
		},
		"distinct-agent-names": func(t *testing.T) {
			y := "name: d\nservices:\n  one:\n    host: azure.ai.agent\n    project: a\n    name: x\n  two:\n    host: azure.ai.agent\n    project: b\n    name: y\n"
			wantPass(t, eval(t, id, yamlInput(t, y)))
		},
		"same-name-different-kinds": func(t *testing.T) {
			y := "name: d\nservices:\n  one:\n    host: azure.ai.agent\n    project: a\n    name: shared\n  shared:\n    host: azure.ai.toolbox\n"
			wantPass(t, eval(t, id, yamlInput(t, y)))
		},
		"name-is-unresolved-env-reference": func(t *testing.T) {
			y := "name: d\nservices:\n  one:\n    host: azure.ai.agent\n    project: a\n    name: ${A_NAME}\n  two:\n    host: azure.ai.agent\n    project: b\n    name: ${B_NAME}\n"
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:name-env-reference")
			// The same unexpanded text twice is reported as uncertain.
			same := "name: d\nservices:\n  one:\n    host: azure.ai.agent\n    name: ${N}\n  two:\n    host: azure.ai.agent\n    name: ${N}\n"
			wantFinding(t, eval(t, id, yamlInput(t, same)), sdk.StatusUncertain, "dup-name:")
		},
		"service-body-in-missing-ref-file": func(t *testing.T) {
			y := "name: d\nservices:\n  one:\n    host: azure.ai.agent\n    $ref: ./gone.yaml\n  two:\n    host: azure.ai.agent\n    name: x\n"
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:service-body-in-ref-file")
		},
	}
}

// ---- FND-CFG-003 ----

const refBase = `name: d
services:
  ai-project:
    host: azure.ai.project
  conn:
    host: azure.ai.connection
    uses: [ai-project]
  tools:
    host: azure.ai.toolbox
    uses: [ai-project, conn]
    tools:
      - type: mcp
        connection: conn
`

func scenarios003() scenarioSet {
	const id = "FND-CFG-003"
	agent := func(extra string) string {
		return refBase + "  bot:\n    host: azure.ai.agent\n    project: src/bot\n    kind: hosted\n" + extra
	}
	return scenarioSet{
		"uses-unknown-service": func(t *testing.T) {
			f := wantFinding(t, eval(t, id, yamlInput(t, agent("    uses: [ai-project, nope]\n"))), sdk.StatusFail, "uses:bot:nope")
			if f.Location.Line == 0 {
				t.Fatal("no line")
			}
		},
		"toolbox-in-toolboxes-but-not-in-uses": func(t *testing.T) {
			wantFinding(t, eval(t, id, yamlInput(t, agent("    uses: [ai-project]\n    toolboxes: [tools]\n"))), sdk.StatusFail, "toolbox-uses:bot:tools")
		},
		"toolbox-tool-names-missing-connection": func(t *testing.T) {
			y := "name: d\nservices:\n  tools:\n    host: azure.ai.toolbox\n    tools:\n      - type: mcp\n        connection: absent\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "tool-connection:tools:absent")
		},
		"routine-without-uses-agent": func(t *testing.T) {
			y := agent("    uses: [ai-project]\n") + "  nightly:\n    host: azure.ai.routine\n    uses: [ai-project]\n    action:\n      type: invoke_agent_responses_api\n      agent_name: bot\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "routine-uses:nightly:bot")
		},
		"voice-conversation-engine-unknown-target": func(t *testing.T) {
			y := "name: d\nservices:\n  v:\n    host: azure.ai.agent\n    kind: voice\n    conversationEngine:\n      type: hosted_agent\n      name: ghost\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "engine:v:ghost")
		},
		"uses-cycle": func(t *testing.T) {
			y := "name: d\nservices:\n  a:\n    host: azure.ai.toolbox\n    uses: [b]\n  b:\n    host: azure.ai.toolbox\n    uses: [a]\n"
			wantFinding(t, eval(t, id, yamlInput(t, y)), sdk.StatusFail, "cycle:a,b")
		},
		"full-example-from-reference": func(t *testing.T) {
			wantNoFindings(t, eval(t, id, fixtureInput(t, "azureyaml/full-example-from-reference.azure.yaml")))
		},
		"uses-names-resources-entry": func(t *testing.T) {
			y := "name: d\nresources:\n  kv:\n    type: keyvault\nservices:\n  a:\n    host: azure.ai.toolbox\n    uses: [kv]\n"
			wantPass(t, eval(t, id, yamlInput(t, y)))
		},
		"memory-model-with-existing-project-endpoint": func(t *testing.T) {
			y := "name: d\nservices:\n  p:\n    host: azure.ai.project\n    endpoint: https://x.services.ai.azure.com/api/projects/y\n  a:\n    host: azure.ai.agent\n    project: s\n    uses: [p]\n    memoryStores:\n      - name: m\n        chatModel: gpt-x\n"
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:existing-project-endpoint")
			// With declared deployments a missing model fails.
			y2 := "name: d\nservices:\n  p:\n    host: azure.ai.project\n    deployments:\n      - name: gpt-a\n  a:\n    host: azure.ai.agent\n    project: s\n    uses: [p]\n    memoryStores:\n      - name: m\n        chatModel: gpt-b\n        embeddingModel: gpt-a\n"
			wantFinding(t, eval(t, id, yamlInput(t, y2)), sdk.StatusFail, "memory-chatModel:a:gpt-b")
		},
		"service-defined-in-missing-ref-file": func(t *testing.T) {
			y := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n    $ref: ./gone.yaml\n"
			rs := eval(t, id, yamlInput(t, y))
			wantNoFindings(t, rs)
			wantSkip(t, rs, "unresolved-value:service-body-in-ref-file")
		},
	}
}

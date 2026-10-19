package cfg

import "testing"

// FND-CFG-008 version 3: toolbox services with inline MCP tools are evaluated
// (the toolbox article documents the allowed_tools filter), unconfirmed
// spellings are named instead of being reported as an omission, and the
// omission stays uncertain because no vendored schema defines the key.
func TestCFG008Breadth(t *testing.T) {
	toolbox := func(tools string) string {
		return "services:\n  tb:\n    host: azure.ai.toolbox\n" + tools
	}
	agent := func(tools string) string {
		return "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n" + tools
	}
	runReviewCases(t, []reviewCase{
		{name: "toolbox-inline-mcp-empty-allowed-tools-is-finding", id: "FND-CFG-008",
			yaml: toolbox("    tools:\n      - type: mcp\n        connection: c\n        allowed_tools: []\n"), want: 1, contains: "empty allowed_tools"},
		{name: "toolbox-inline-mcp-nonempty-allowed-tools-passes", id: "FND-CFG-008",
			yaml: toolbox("    tools:\n      - type: mcp\n        connection: c\n        allowed_tools: [search]\n")},
		{name: "toolbox-inline-mcp-omitting-allowed-tools-is-uncertain", id: "FND-CFG-008",
			yaml: toolbox("    tools:\n      - type: mcp\n        connection: c\n"), skip: uncertainPrefix + "direct inline MCP tool omits allowed_tools"},
		{name: "toolbox-non-mcp-tools-not-applicable", id: "FND-CFG-008",
			yaml: toolbox("    tools:\n      - type: web_search\n"), skip: "not-applicable"},
		{name: "toolbox-endpoint-reuse-mode-skipped", id: "FND-CFG-008",
			yaml: toolbox("    endpoint: https://x.services.ai.azure.com/api/projects/p/toolboxes/t/versions/1/mcp\n"), skip: "toolbox endpoint-reuse mode"},
		{name: "toolbox-tool-from-ref-skipped", id: "FND-CFG-008",
			yaml: toolbox("    tools:\n      - $ref: ./tool.yaml\n"), skip: "tool definition comes from $ref"},
		{name: "agent-allowedTools-camelCase-not-reported-as-omitted", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: mcp\n        allowedTools: [a]\n"), skip: "tools[0] uses allowedTools, a spelling no verified source confirms"},
		{name: "agent-allowedTools-empty-is-not-a-finding", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: mcp\n        allowedTools: []\n"), skip: uncertainPrefix},
		{name: "agent-allowed_tools-wins-over-alias", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: mcp\n        allowed_tools: [a]\n        allowedTools: []\n")},
		{name: "agent-mcp-with-empty-list-still-finding", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: mcp\n        allowed_tools: []\n"), want: 1, contains: "empty allowed_tools"},
		{name: "agent-mcp-mixed-case-type", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: MCP\n        allowed_tools: []\n"), want: 1},
		{name: "agent-allowed-tools-reference-unresolved", id: "FND-CFG-008",
			yaml: agent("    tools:\n      - type: mcp\n        allowed_tools: ['${T}']\n"), skip: "allowed_tools is unresolved"},
	})
}

func TestCFG008KeyStable(t *testing.T) {
	y := "services:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n    tools:\n      - type: mcp\n        allowed_tools: []\n"
	res := run(t, find(Register(), "FND-CFG-008"), inputFor(t, y))
	if len(res.Findings) != 1 || res.Findings[0].Key != "mcp-allowed-tools-empty:tools[0]" {
		t.Fatalf("unexpected %+v", res.Findings)
	}
}

package cfg

import "testing"

// FND-CFG-007 version 3: every URL-typed key the vendored schemas and the
// catalogue sources name is read, and RFC 1918 literals are accepted when a
// project injects the agent runtime into a customer subnet.
func TestCFG007URLKeys(t *testing.T) {
	conn := func(extra string) string {
		return "name: app\nservices:\n  c:\n    host: azure.ai.connection\n" + extra
	}
	agent := func(extra string) string {
		return "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n" + extra
	}
	runReviewCases(t, []reviewCase{
		{name: "authorizationUrl-http", id: "FND-CFG-007", yaml: conn("    authorizationUrl: http://login.example.com/authorize\n"), want: 1, contains: "authorizationUrl"},
		{name: "tokenUrl-http", id: "FND-CFG-007", yaml: conn("    tokenUrl: http://login.example.com/token\n"), want: 1, contains: "tokenUrl"},
		{name: "refreshUrl-http", id: "FND-CFG-007", yaml: conn("    refreshUrl: http://login.example.com/refresh\n"), want: 1, contains: "refreshUrl"},
		{name: "oauth-urls-https-pass", id: "FND-CFG-007", yaml: conn("    authorizationUrl: https://a.example.com/a\n    tokenUrl: https://a.example.com/t\n    refreshUrl: https://a.example.com/r\n")},
		{name: "oauth-url-reference-skipped", id: "FND-CFG-007", yaml: conn("    tokenUrl: ${TOKEN_URL}\n")},
		{name: "tool-server_url-http", id: "FND-CFG-007", yaml: agent("    tools:\n      - type: mcp\n        server_url: http://mcp.example.com/sse\n"), want: 1, contains: "tools[0].server_url"},
		{name: "tool-server_url-https-passes", id: "FND-CFG-007", yaml: agent("    tools:\n      - type: mcp\n        server_url: https://mcp.example.com/sse\n")},
		{name: "tool-server_url-localhost", id: "FND-CFG-007", yaml: agent("    tools:\n      - type: mcp\n        server_url: https://localhost/sse\n"), want: 1, contains: "local/private"},
		{name: "toolConnections-target-http", id: "FND-CFG-007", yaml: agent("    toolConnections:\n      - name: c\n        target: http://svc.example.com\n"), want: 1, contains: "toolConnections[0].target"},
		{name: "toolConnections-target-https-passes", id: "FND-CFG-007", yaml: agent("    toolConnections:\n      - name: c\n        target: https://svc.example.com\n")},
		{name: "toolConnections-arm-id-passes", id: "FND-CFG-007", yaml: agent("    toolConnections:\n      - name: c\n        target: /subscriptions/s/resourceGroups/r\n")},
		{name: "publish-developerWebsiteUrl-http", id: "FND-CFG-007", yaml: agent("    activity:\n      publish:\n        developerWebsiteUrl: http://example.com\n"), want: 1, contains: "activity.publish.developerWebsiteUrl"},
		{name: "publish-privacyUrl-http", id: "FND-CFG-007", yaml: agent("    activity:\n      publish:\n        privacyUrl: http://example.com/p\n"), want: 1, contains: "privacyUrl"},
		{name: "publish-termsOfUseUrl-http", id: "FND-CFG-007", yaml: agent("    activity:\n      publish:\n        termsOfUseUrl: http://example.com/t\n"), want: 1, contains: "termsOfUseUrl"},
		{name: "publish-urls-https-pass", id: "FND-CFG-007", yaml: agent("    activity:\n      publish:\n        privacyUrl: https://example.com/p\n")},
	})
}

func TestCFG007PrivateNetworkExemption(t *testing.T) {
	const inj = "    network:\n      peSubnet: {vnet: v, name: pe}\n      agentSubnet: {vnet: v, name: ag}\n"
	const peOnly = "    network:\n      peSubnet: {vnet: v, name: pe}\n"
	sub := func(network, target string) string {
		return "name: app\nservices:\n  p:\n    host: azure.ai.project\n" + network +
			"  c:\n    host: azure.ai.connection\n    uses: [p]\n    target: " + target + "\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "rfc1918-no-network-is-finding", id: "FND-CFG-007", yaml: sub("", "https://10.0.0.4/mcp"), want: 1, contains: "local/private"},
		{name: "rfc1918-with-pe-only-is-finding", id: "FND-CFG-007", yaml: sub(peOnly, "https://10.0.0.4/mcp"), want: 1, contains: "local/private"},
		{name: "rfc1918-with-agent-subnet-injection-passes", id: "FND-CFG-007", yaml: sub(inj, "https://10.0.0.4/mcp")},
		{name: "rfc1918-http-with-injection-still-non-https", id: "FND-CFG-007", yaml: sub(inj, "http://10.0.0.4/mcp"), want: 1, contains: "non-https"},
		{name: "localhost-with-injection-still-a-finding", id: "FND-CFG-007", yaml: sub(inj, "https://localhost/mcp"), want: 1, contains: "local/private"},
		{name: "metadata-ip-with-injection-still-a-finding", id: "FND-CFG-007", yaml: sub(inj, "https://169.254.169.254/x"), want: 1, contains: "local/private"},
		{name: "loopback-with-injection-still-a-finding", id: "FND-CFG-007", yaml: sub(inj, "https://127.0.0.1/x"), want: 1, contains: "local/private"},
	})
}

func TestCFG007KeysStable(t *testing.T) {
	y := "services:\n  c:\n    host: azure.ai.connection\n    target: http://x.example.com\n"
	res := run(t, find(Register(), "FND-CFG-007"), inputFor(t, y))
	if len(res.Findings) != 1 || res.Findings[0].Key != "non-https:target" {
		t.Fatalf("unexpected findings %+v", res.Findings)
	}
}

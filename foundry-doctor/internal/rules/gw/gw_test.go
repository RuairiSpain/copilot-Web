package gw

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// model fills a verified API version on API Management resources that do not
// set one, so tests read like authored templates; rawModel does not.
type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource {
	out := make([]sdk.ARMResource, len(m))
	for i, r := range m {
		if r.APIVersion == "" && strings.HasPrefix(strings.ToLower(r.Type), "microsoft.apimanagement/") {
			r.APIVersion = "2024-05-01"
		}
		out[i] = r
	}
	return out
}

type rawModel []sdk.ARMResource

func (m rawModel) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

func findRule(id string) sdk.Rule {
	for _, r := range Register() {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func mustEval(t *testing.T, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	out, err := findRule(id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func res(typ, name string, props map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, Properties: props}
}

func TestRegisterIDs(t *testing.T) {
	want := []string{"FND-GW-001", "FND-GW-002", "FND-GW-003", "FND-GW-004", "FND-GW-005", "FND-GW-006"}
	got := Register()
	if len(got) != len(want) {
		t.Fatalf("got %d rules", len(got))
	}
	for i, r := range got {
		if r.ID() != want[i] {
			t.Fatalf("rule[%d]=%s want %s", i, r.ID(), want[i])
		}
	}
}

func TestGW001JWTValidation(t *testing.T) {
	api := res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
		"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
	})
	invalidPolicy := res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
		"format": "xml",
		"value":  `<policies><inbound><validate-jwt><audiences><audience>a</audience></audiences><openid-config url="https://login.microsoftonline.com/common/v2.0/.well-known/openid-configuration" /></validate-jwt></inbound></policies>`,
	})
	got := mustEval(t, "FND-GW-001", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil), api, invalidPolicy,
	}})
	if len(got.Findings) == 0 {
		t.Fatalf("expected findings, got %+v", got)
	}

	validPolicy := res("Microsoft.ApiManagement/service/apis/operations/policies", "apim/chat/send/policy", map[string]any{
		"format": "xml",
		"value":  `<policies><inbound><base /><validate-jwt><audiences><audience>api://gw</audience></audiences><openid-config url="https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0/.well-known/openid-configuration" /></validate-jwt></inbound></policies>`,
	})
	clean := mustEval(t, "FND-GW-001", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/send", nil),
		validPolicy,
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestGW002TokenGovernancePolicies(t *testing.T) {
	api := res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
		"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
	})
	missing := mustEval(t, "FND-GW-002", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{"publisherEmail": "x"}),
		api,
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml", "value": `<policies><inbound /></policies>`,
		}),
	}})
	if len(missing.Findings) < 2 {
		t.Fatalf("expected token-governance findings, got %+v", missing)
	}

	tokenPolicies := `<policies><inbound><llm-token-limit counter-key="tenant" estimate-prompt-tokens="true" tokens-per-minute="1200" /><llm-emit-token-metric><dimension name="tenant" /></llm-emit-token-metric></inbound></policies>`
	productOnly := mustEval(t, "FND-GW-002", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{"publisherEmail": "x"}),
		api,
		res("Microsoft.ApiManagement/service/products/apis", "apim/product/chat", nil),
		res("Microsoft.ApiManagement/service/products/policies", "apim/product/policy", map[string]any{"format": "xml", "value": tokenPolicies}),
	}})
	if len(productOnly.Findings) != 0 || productOnly.Skipped == nil || !strings.Contains(productOnly.Skipped.Reason, "depends on the product") {
		t.Fatalf("expected product-dependent coverage to be uncertain, got %+v", productOnly)
	}

	inherited := mustEval(t, "FND-GW-002", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{"publisherEmail": "x"}),
		api,
		res("Microsoft.ApiManagement/service/policies", "apim/policy", map[string]any{"format": "xml", "value": tokenPolicies}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{"format": "xml", "value": `<policies><inbound><base /></inbound></policies>`}),
	}})
	if len(inherited.Findings) != 0 || inherited.Skipped != nil {
		t.Fatalf("expected pass through <base/> from global scope, got %+v", inherited)
	}

	legacy := mustEval(t, "FND-GW-002", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{"format": "xml", "value": `<policies><inbound><azure-openai-token-limit counter-key="x" tokens-per-minute="1" /><llm-emit-token-metric /></inbound></policies>`}),
	}})
	if len(legacy.Findings) != 0 || legacy.Skipped == nil || !strings.Contains(legacy.Skipped.Reason, "legacy") {
		t.Fatalf("expected legacy policy name to be uncertain, got %+v", legacy)
	}
}

func TestGW003BackendResolution(t *testing.T) {
	api := res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"})
	bad := mustEval(t, "FND-GW-003", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml", "value": `<policies><inbound><set-backend-service backend-id="missing" /></inbound></policies>`,
		}),
	}})
	if len(bad.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", bad)
	}

	clean := mustEval(t, "FND-GW-003", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml", "value": `<policies><inbound><set-backend-service base-url="https://acct.openai.azure.com/openai" /></inbound></policies>`,
		}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestGW004RoutePathsUnique(t *testing.T) {
	got := mustEval(t, "FND-GW-004", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "/foundry"}),
		res("Microsoft.ApiManagement/service/apis", "apim/embeddings", map[string]any{"path": "foundry/"}),
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected one duplicate-path finding, got %+v", got)
	}
	clean := mustEval(t, "FND-GW-004", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "/chat"}),
		res("Microsoft.ApiManagement/service/apis", "apim/embeddings", map[string]any{"path": "embed"}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestGW005ManagedIdentityAndRBAC(t *testing.T) {
	api := res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
		"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
	})
	policy := res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
		"format": "xml",
		"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" client-id="app-client-id" /><set-header name="api-key"><value>x</value></set-header></inbound></policies>`,
	})
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{}),
		api,
		policy,
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
	}})
	if len(got.Findings) < 3 {
		t.Fatalf("expected findings, got %+v", got)
	}

	clean := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		{
			Type: "Microsoft.ApiManagement/service",
			Name: "apim",
			Identity: map[string]any{
				"type": "SystemAssigned,UserAssigned",
				"userAssignedIdentities": map[string]any{
					"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/apim-uai": map[string]any{},
				},
			},
			Properties: map[string]any{},
		},
		api,
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" client-id="11111111-1111-1111-1111-111111111111" /></inbound></policies>`,
		}),
		res("Microsoft.ManagedIdentity/userAssignedIdentities", "apim-uai", map[string]any{
			"clientId":    "11111111-1111-1111-1111-111111111111",
			"principalId": "22222222-2222-2222-2222-222222222222",
		}),
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
		{
			Type:  "Microsoft.Authorization/roleAssignments",
			Name:  "ra1",
			Scope: "/providers/Microsoft.CognitiveServices/accounts/acct",
			Properties: map[string]any{
				"principalId":      "22222222-2222-2222-2222-222222222222",
				"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/5e0bd9bd-7b93-4f28-af87-19fc36ad61bd",
			},
		},
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected clean result, got %+v", clean)
	}
}

func TestGW005BackendAuthorizationIsNotManagedIdentity(t *testing.T) {
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{
			"identity": map[string]any{"type": "SystemAssigned", "principalId": "11111111-1111-1111-1111-111111111111"},
		}),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
			"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
		}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><set-backend-service backend-id="foundry" /></inbound></policies>`,
		}),
		res("Microsoft.ApiManagement/service/backends", "apim/foundry", map[string]any{
			"url": "https://acct.openai.azure.com/openai",
			"credentials": map[string]any{
				"authorization": map[string]any{"scheme": "Bearer", "parameter": "{{token}}"},
			},
		}),
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
	}})
	if len(got.Findings) < 2 {
		t.Fatalf("expected findings for backend auth credentials, got %+v", got)
	}
}

func TestGW005BackendAuthorizationStillFailsAlongsideManagedIdentity(t *testing.T) {
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		{
			Type: "Microsoft.ApiManagement/service",
			Name: "apim",
			Identity: map[string]any{
				"type":        "SystemAssigned",
				"principalId": "22222222-2222-2222-2222-222222222222",
			},
			Properties: map[string]any{},
		},
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
			"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
		}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" /><set-backend-service backend-id="foundry" /></inbound></policies>`,
		}),
		res("Microsoft.ApiManagement/service/backends", "apim/foundry", map[string]any{
			"url": "https://acct.openai.azure.com/openai",
			"credentials": map[string]any{
				"authorization": map[string]any{"scheme": "Bearer", "parameter": "{{token}}"},
			},
		}),
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
		{
			Type:  "Microsoft.Authorization/roleAssignments",
			Name:  "ra1",
			Scope: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct",
			Properties: map[string]any{
				"principalId":      "22222222-2222-2222-2222-222222222222",
				"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/5e0bd9bd-7b93-4f28-af87-19fc36ad61bd",
			},
		},
	}})
	found := false
	for _, f := range got.Findings {
		if strings.Contains(f.Evidence, "backend authorization credentials are header-based") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected backend authorization finding, got %+v", got)
	}
}

func TestScopeCoversTargetStrictAncestorOnly(t *testing.T) {
	tests := []struct {
		name   string
		scope  string
		target string
		want   bool
	}{
		{"exact resource", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", true},
		{"subscription ancestor", "/subscriptions/sub", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", true},
		{"resource group ancestor", "/subscriptions/sub/resourceGroups/rg", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", true},
		{"different subscription", "/subscriptions/other", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", false},
		{"sibling resource group", "/subscriptions/sub/resourceGroups/other", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", false},
		{"suffix collision", "/providers/Microsoft.CognitiveServices/accounts/acct", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", false},
		{"qualified suffix exact resource", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", "/providers/Microsoft.CognitiveServices/accounts/acct", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopeCoversTarget(tt.scope, tt.target); got != tt.want {
				t.Fatalf("scopeCoversTarget(%q, %q) = %v, want %v", tt.scope, tt.target, got, tt.want)
			}
		})
	}
}

func TestGW005BroadScopeWithoutQualifiedTargetIsUncertain(t *testing.T) {
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		{
			Type: "Microsoft.ApiManagement/service",
			Name: "apim",
			Identity: map[string]any{
				"type":        "SystemAssigned",
				"principalId": "22222222-2222-2222-2222-222222222222",
			},
			Properties: map[string]any{},
		},
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
			"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
		}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" /></inbound></policies>`,
		}),
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
		{
			Type:  "Microsoft.Authorization/roleAssignments",
			Name:  "ra1",
			Scope: "/subscriptions/sub/resourceGroups/rg",
			Properties: map[string]any{
				"principalId":      "22222222-2222-2222-2222-222222222222",
				"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/5e0bd9bd-7b93-4f28-af87-19fc36ad61bd",
			},
		},
	}})
	if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain result, got %+v", got)
	}
}

func TestGW005ManagementGroupScopeWithoutQualifiedTargetIsUncertain(t *testing.T) {
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		{
			Type: "Microsoft.ApiManagement/service",
			Name: "apim",
			Identity: map[string]any{
				"type":        "SystemAssigned",
				"principalId": "22222222-2222-2222-2222-222222222222",
			},
			Properties: map[string]any{},
		},
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
			"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
		}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" /></inbound></policies>`,
		}),
		res("Microsoft.CognitiveServices/accounts", "acct", nil),
		{
			Type:  "Microsoft.Authorization/roleAssignments",
			Name:  "ra1",
			Scope: "/providers/Microsoft.Management/managementGroups/demo-mg",
			Properties: map[string]any{
				"principalId":      "22222222-2222-2222-2222-222222222222",
				"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/5e0bd9bd-7b93-4f28-af87-19fc36ad61bd",
			},
		},
	}})
	if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain result, got %+v", got)
	}
}

func TestGW005BackendPoolRequiresRoleOnEveryFoundryTarget(t *testing.T) {
	got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
		{
			Type: "Microsoft.ApiManagement/service",
			Name: "apim",
			Identity: map[string]any{
				"type":        "SystemAssigned",
				"principalId": "22222222-2222-2222-2222-222222222222",
			},
			Properties: map[string]any{},
		},
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
			"path": "chat",
		}),
		res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><authentication-managed-identity resource="https://cognitiveservices.azure.com" /><set-backend-service backend-id="router" /></inbound></policies>`,
		}),
		res("Microsoft.ApiManagement/service/backends", "apim/router", map[string]any{
			"pool": map[string]any{
				"services": []any{
					map[string]any{"id": "/providers/Microsoft.ApiManagement/service/apim/backends/foundry-a"},
					map[string]any{"id": "/providers/Microsoft.ApiManagement/service/apim/backends/foundry-b"},
				},
			},
		}),
		res("Microsoft.ApiManagement/service/backends", "apim/foundry-a", map[string]any{
			"url": "https://acct-a.openai.azure.com/openai",
		}),
		res("Microsoft.ApiManagement/service/backends", "apim/foundry-b", map[string]any{
			"url": "https://acct-b.openai.azure.com/openai",
		}),
		res("Microsoft.CognitiveServices/accounts", "acct-a", nil),
		res("Microsoft.CognitiveServices/accounts", "acct-b", nil),
		{
			Type:  "Microsoft.Authorization/roleAssignments",
			Name:  "ra1",
			Scope: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct-a",
			Properties: map[string]any{
				"principalId":      "22222222-2222-2222-2222-222222222222",
				"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/5e0bd9bd-7b93-4f28-af87-19fc36ad61bd",
			},
		},
	}})
	found := false
	for _, f := range got.Findings {
		if strings.Contains(f.Evidence, "lacks a Cognitive Services role") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing-role finding for uncovered pool target, got %+v", got)
	}
}

func TestGW006TelemetrySink(t *testing.T) {
	api := res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
		"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
	})
	policy := res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
		"format": "xml",
		"value":  `<policies><inbound><llm-emit-token-metric><dimension name="tenant" /></llm-emit-token-metric></inbound></policies>`,
	})
	got := mustEval(t, "FND-GW-006", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil), api, policy,
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", got)
	}

	metricPath := mustEval(t, "FND-GW-006", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		policy,
		res("Microsoft.ApiManagement/service/loggers", "apim/appinsights", map[string]any{
			"loggerType": "applicationInsights",
			"resourceId": "/providers/Microsoft.Insights/components/appi",
		}),
		res("Microsoft.ApiManagement/service/diagnostics", "apim/applicationinsights", map[string]any{
			"loggerId": "/providers/Microsoft.ApiManagement/service/apim/loggers/appinsights",
			"metrics":  true,
		}),
		res("Microsoft.Insights/components", "appi", nil),
	}})
	if len(metricPath.Findings) != 0 || metricPath.Skipped == nil || !strings.Contains(metricPath.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain clean result, got %+v", metricPath)
	}

	llmLogs := mustEval(t, "FND-GW-006", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		api,
		policy,
		{
			Type:  "Microsoft.Insights/diagnosticSettings",
			Name:  "diag",
			Scope: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ApiManagement/service/apim",
			Properties: map[string]any{
				"workspaceId": "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.OperationalInsights/workspaces/ws",
			},
		},
	}})
	if len(llmLogs.Findings) != 0 {
		t.Fatalf("expected clean result, got %+v", llmLogs)
	}
}

func chatAPI() sdk.ARMResource {
	return res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{
		"path": "chat", "serviceUrl": "https://acct.openai.azure.com/openai",
	})
}

func policyRes(typ, name, value string) sdk.ARMResource {
	return res(typ, name, map[string]any{"format": "xml", "value": value})
}

const validJWTPolicy = `<policies><inbound><validate-jwt><audiences><audience>api://gw</audience></audiences><openid-config url="https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0/.well-known/openid-configuration" /></validate-jwt><base /></inbound></policies>`

func TestGW001OperationDroppingBaseFails(t *testing.T) {
	got := mustEval(t, "FND-GW-001", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		chatAPI(),
		policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", validJWTPolicy),
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/send", nil),
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/list", nil),
		policyRes("Microsoft.ApiManagement/service/apis/operations/policies", "apim/chat/send/policy", `<policies><inbound><set-header name="x-op" exists-action="override"><value>1</value></set-header></inbound></policies>`),
		policyRes("Microsoft.ApiManagement/service/apis/operations/policies", "apim/chat/list/policy", `<policies><inbound><base /></inbound></policies>`),
	}})
	if len(got.Findings) != 1 || got.Findings[0].Key != "operation:send:no-token-validation" {
		t.Fatalf("expected exactly the unprotected operation to fail, got %+v", got)
	}
	if !strings.Contains(got.Findings[0].Evidence, "operation send") || !strings.Contains(got.Findings[0].Evidence, "api-version 2024-05-01") {
		t.Fatalf("evidence does not name the operation and resource version: %q", got.Findings[0].Evidence)
	}
}

func TestGW001ProductDroppingBaseIsUncertain(t *testing.T) {
	got := mustEval(t, "FND-GW-001", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		chatAPI(),
		policyRes("Microsoft.ApiManagement/service/policies", "apim/policy", validJWTPolicy),
		res("Microsoft.ApiManagement/service/products/apis", "apim/open/chat", nil),
		policyRes("Microsoft.ApiManagement/service/products/policies", "apim/open/policy", `<policies><inbound><rate-limit calls="5" renewal-period="60" /></inbound></policies>`),
	}})
	if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "depends on the product") {
		t.Fatalf("expected product-dependent uncertainty, got %+v", got)
	}
}

func TestGW001UnresolvedPolicyIsUncertain(t *testing.T) {
	rawxml := res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{
		"format": "rawxml",
		"value":  `<policies><inbound><set-variable name="x" value="@(context.Request.Headers.GetValueOrDefault("X-Header-Name", ""))" /><base /></inbound></policies>`,
	})
	link := res("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", map[string]any{"format": "rawxml-link", "value": "https://example.invalid/policy.xml"})
	fragment := policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><include-fragment fragment-id="auth" /></inbound></policies>`)
	choose := policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><choose><when condition="@(true)"><validate-jwt><audiences><audience>a</audience></audiences></validate-jwt></when></choose></inbound></policies>`)
	type tc struct {
		name  string
		extra sdk.ARMResource
		want  string
	}
	tests := []tc{
		{"rawxml with expression", rawxml, "does not parse as XML"},
		{"external link", link, "external link"},
		{"fragment outside template", fragment, "policy fragment auth is not defined in the template"},
		{"conditional validation", choose, "<choose>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arm := model{res("Microsoft.ApiManagement/service", "apim", nil), chatAPI(), tt.extra}
			got := mustEval(t, "FND-GW-001", &sdk.Input{ARM: arm})
			if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, tt.want) {
				t.Fatalf("expected uncertain mentioning %q, got %+v", tt.want, got)
			}
		})
	}
}

func TestGW001FragmentInTemplateIsResolved(t *testing.T) {
	got := mustEval(t, "FND-GW-001", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		chatAPI(),
		policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><include-fragment fragment-id="auth" /></inbound></policies>`),
		policyRes("Microsoft.ApiManagement/service/policyFragments", "apim/auth", `<fragment><validate-jwt><audiences><audience>api://gw</audience></audiences><openid-config url="https://login.microsoftonline.com/contoso.onmicrosoft.com/v2.0/.well-known/openid-configuration" /></validate-jwt></fragment>`),
	}})
	if len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("expected the fragment to be resolved, got %+v", got)
	}
}

func TestGWUnknownAPIVersionIsUncertain(t *testing.T) {
	api := chatAPI()
	api.APIVersion = "2023-05-01-preview"
	svc := res("Microsoft.ApiManagement/service", "apim", nil)
	svc.APIVersion = "2024-05-01"
	pol := policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound /></policies>`)
	pol.APIVersion = "2024-05-01"
	for _, id := range []string{"FND-GW-001", "FND-GW-002", "FND-GW-003", "FND-GW-004", "FND-GW-005"} {
		got := mustEval(t, id, &sdk.Input{ARM: rawModel{svc, api, pol}})
		if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "uncertain:") || !strings.Contains(got.Skipped.Reason, "2023-05-01-preview") {
			t.Fatalf("%s: expected uncertain for an unverified API version, got %+v", id, got)
		}
	}
}

func TestGW004UnresolvedPathAndNoAPIs(t *testing.T) {
	got := mustEval(t, "FND-GW-004", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "[parameters('apiPath')]"}),
	}})
	if got.Skipped == nil || !strings.Contains(got.Skipped.Reason, "path-from-unresolved-parameter") {
		t.Fatalf("expected uncertain path, got %+v", got)
	}
	none := mustEval(t, "FND-GW-004", &sdk.Input{ARM: model{res("Microsoft.Storage/storageAccounts", "st", nil)}})
	if none.Skipped == nil || none.Skipped.Reason != "no-apis" {
		t.Fatalf("expected no-apis skip, got %+v", none)
	}
}

func apimWithSystemIdentity() sdk.ARMResource {
	return sdk.ARMResource{
		Type:       "Microsoft.ApiManagement/service",
		Name:       "apim",
		Identity:   map[string]any{"type": "SystemAssigned"},
		Properties: map[string]any{},
	}
}

func miPolicy(attrs string) sdk.ARMResource {
	return policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><authentication-managed-identity `+attrs+` /></inbound></policies>`)
}

func TestGW005RealisticTemplateMatchesByReference(t *testing.T) {
	ra := sdk.ARMResource{
		Type:  "Microsoft.Authorization/roleAssignments",
		Name:  "ra",
		Scope: "[resourceId('Microsoft.CognitiveServices/accounts', 'acct')]",
		Properties: map[string]any{
			"principalId":      "[reference(resourceId('Microsoft.ApiManagement/service', 'apim'), '2024-05-01', 'full').identity.principalId]",
			"roleDefinitionId": "[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '5e0bd9bd-7b93-4f28-af87-19fc36ad61bd')]",
		},
	}
	base := model{apimWithSystemIdentity(), chatAPI(), miPolicy(`resource="https://cognitiveservices.azure.com"`), res("Microsoft.CognitiveServices/accounts", "acct", nil)}

	noRA := mustEval(t, "FND-GW-005", &sdk.Input{ARM: base})
	if len(noRA.Findings) != 0 || noRA.Skipped == nil || !strings.Contains(noRA.Skipped.Reason, "another deployment") {
		t.Fatalf("expected uncertain without a visible role assignment, got %+v", noRA)
	}

	withRA := append(append(model{}, base...), ra)
	if got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: withRA}); len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("expected pass when principal and scope reference the APIM identity and the account, got %+v", got)
	}

	paramScope := ra
	paramScope.Scope = "[resourceId('Microsoft.CognitiveServices/accounts', parameters('accountName'))]"
	if got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: append(append(model{}, base...), paramScope)}); len(got.Findings) != 0 || got.Skipped == nil {
		t.Fatalf("expected uncertain for a parameter-built scope, got %+v", got)
	}

	literalScope := ra
	literalScope.Scope = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct"
	clean := mustEval(t, "FND-GW-005", &sdk.Input{ARM: append(append(model{}, base...), literalScope)})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass when the role assignment references the APIM identity, got %+v", clean)
	}

	foundryUser := literalScope
	foundryUser.Properties = map[string]any{
		"principalId":      literalScope.Properties["principalId"],
		"roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/53ca6127-db72-4b80-b1b0-d745d6d5456d",
	}
	unverifiedRole := mustEval(t, "FND-GW-005", &sdk.Input{ARM: append(append(model{}, base...), foundryUser)})
	if len(unverifiedRole.Findings) != 0 || unverifiedRole.Skipped == nil {
		t.Fatalf("expected uncertain for the unverified Foundry User role, got %+v", unverifiedRole)
	}
}

func TestGW005UnprovableIdentityIsUncertain(t *testing.T) {
	uai := sdk.ARMResource{
		Type: "Microsoft.ApiManagement/service",
		Name: "apim",
		Identity: map[string]any{
			"type":                   "UserAssigned",
			"userAssignedIdentities": map[string]any{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/apim-uai": map[string]any{}},
		},
		Properties: map[string]any{},
	}
	type tc struct {
		name  string
		attrs string
		want  string
	}
	tests := []tc{
		{"named value client-id", `resource="https://cognitiveservices.azure.com" client-id="{{apim-uai-client-id}}"`, "named value"},
		{"runtime client-id", `resource="https://cognitiveservices.azure.com" client-id="11111111-1111-1111-1111-111111111111"`, "runtime values"},
		{"other audience", `resource="https://ai.azure.com"`, "only https://cognitiveservices.azure.com is verified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustEval(t, "FND-GW-005", &sdk.Input{ARM: model{
				uai, chatAPI(), miPolicy(tt.attrs),
				res("Microsoft.ManagedIdentity/userAssignedIdentities", "apim-uai", nil),
				res("Microsoft.CognitiveServices/accounts", "acct", nil),
			}})
			if len(got.Findings) != 0 || got.Skipped == nil || !strings.Contains(got.Skipped.Reason, tt.want) {
				t.Fatalf("expected uncertain mentioning %q, got %+v", tt.want, got)
			}
		})
	}
}

func TestGW006LargeLanguageModelLogs(t *testing.T) {
	metricPolicy := policyRes("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><llm-emit-token-metric><dimension name="tenant" /></llm-emit-token-metric></inbound></policies>`)
	diag := func(logs string) sdk.ARMResource {
		return res("Microsoft.ApiManagement/service/diagnostics", "apim/azuremonitor", map[string]any{
			"loggerId":           "/providers/Microsoft.ApiManagement/service/apim/loggers/azuremonitor",
			"largeLanguageModel": map[string]any{"logs": logs},
		})
	}
	enabled := mustEval(t, "FND-GW-006", &sdk.Input{ARM: model{res("Microsoft.ApiManagement/service", "apim", nil), chatAPI(), metricPolicy, diag("enabled")}})
	if len(enabled.Findings) != 0 || enabled.Skipped != nil {
		t.Fatalf("expected pass with largeLanguageModel.logs enabled, got %+v", enabled)
	}
	disabled := mustEval(t, "FND-GW-006", &sdk.Input{ARM: model{res("Microsoft.ApiManagement/service", "apim", nil), chatAPI(), metricPolicy, diag("disabled")}})
	if len(disabled.Findings) != 1 {
		t.Fatalf("expected a finding with largeLanguageModel.logs disabled, got %+v", disabled)
	}
}

func TestHasTenantHint(t *testing.T) {
	type tc struct {
		issuer string
		want   bool
	}
	for _, tt := range []tc{
		{"https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0", true},
		{"https://sts.windows.net/11111111-1111-1111-1111-111111111111/", true},
		{"https://login.microsoftonline.com/contoso.onmicrosoft.com/v2.0/.well-known/openid-configuration", true},
		{"https://login.microsoftonline.com/common/v2.0", false},
		{"https://login.microsoftonline.com/organizations/v2.0/.well-known/openid-configuration", false},
	} {
		if got := hasTenantHint(tt.issuer); got != tt.want {
			t.Fatalf("hasTenantHint(%q) = %v, want %v", tt.issuer, got, tt.want)
		}
	}
}

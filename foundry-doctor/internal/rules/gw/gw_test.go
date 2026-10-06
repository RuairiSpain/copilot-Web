package gw

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

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

	clean := mustEval(t, "FND-GW-002", &sdk.Input{ARM: model{
		res("Microsoft.ApiManagement/service", "apim", map[string]any{"publisherEmail": "x"}),
		api,
		res("Microsoft.ApiManagement/service/products/apis", "apim/product/chat", nil),
		res("Microsoft.ApiManagement/service/products/policies", "apim/product/policy", map[string]any{
			"format": "xml",
			"value":  `<policies><inbound><llm-token-limit counter-key="tenant" estimate-prompt-tokens="true" tokens-per-minute="1200" /><llm-emit-token-metric><dimension name="tenant" /></llm-emit-token-metric></inbound></policies>`,
		}),
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
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
	if len(clean.Findings) != 0 || (clean.Skipped != nil && !strings.Contains(clean.Skipped.Reason, "backend-entity")) {
		t.Fatalf("expected clean result, got %+v", clean)
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

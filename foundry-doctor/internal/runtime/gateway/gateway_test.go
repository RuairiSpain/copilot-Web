package gateway

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

func res(typ, name string, props map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, APIVersion: "2024-05-01", Properties: props}
}

func pol(typ, name, value string) sdk.ARMResource {
	return res(typ, name, map[string]any{"format": "xml", "value": value})
}

func names(els []PolicyElement) string {
	var out []string
	for _, el := range els {
		out = append(out, el.Name)
	}
	return strings.Join(out, ",")
}

func service(t *testing.T, resources ...sdk.ARMResource) (*Service, *API) {
	t.Helper()
	snap := BuildSnapshot(&sdk.Input{ARM: model(resources)})
	svc := snap.Services["apim"]
	if svc == nil || svc.APIs["chat"] == nil {
		t.Fatalf("snapshot = %+v", snap)
	}
	return svc, svc.APIs["chat"]
}

func TestParsePolicyRejectsMalformedXML(t *testing.T) {
	if _, err := ParsePolicy(`<policies><inbound><set-variable name="x" value="@(a && b)" /></inbound></policies>`); err == nil {
		t.Fatal("expected a parse error for raw policy expressions")
	}
	got, err := ParsePolicy(`<policies><inbound><base /><rate-limit calls="1" renewal-period="1" /></inbound></policies>`)
	if err != nil || !got.HasInbound || names(got.Inbound) != "base,rate-limit" {
		t.Fatalf("parsed = %+v err = %v", got, err)
	}
	noInbound, err := ParsePolicy(`<policies><outbound /></policies>`)
	if err != nil || noInbound.HasInbound {
		t.Fatalf("parsed = %+v err = %v", noInbound, err)
	}
}

func TestEffectivePoliciesHonourBasePerOperation(t *testing.T) {
	svc, api := service(t,
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
		pol("Microsoft.ApiManagement/service/policies", "apim/policy", `<policies><inbound><cors /></inbound></policies>`),
		pol("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><base /><validate-jwt /></inbound></policies>`),
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/keep", nil),
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/drop", nil),
		pol("Microsoft.ApiManagement/service/apis/operations/policies", "apim/chat/keep/policy", `<policies><inbound><base /><set-header name="a" /></inbound></policies>`),
		pol("Microsoft.ApiManagement/service/apis/operations/policies", "apim/chat/drop/policy", `<policies><inbound><set-header name="b" /></inbound></policies>`),
		res("Microsoft.ApiManagement/service/apis/operations", "apim/chat/plain", nil),
	)
	got := map[string]string{}
	for _, v := range EffectivePoliciesForAPI(svc, api) {
		if v.External || v.Product != "" {
			t.Fatalf("unexpected variant %+v", v)
		}
		got[v.Operation] = names(v.Inbound)
	}
	want := map[string]string{"drop": "set-header", "keep": "cors,validate-jwt,set-header", "plain": "cors,validate-jwt"}
	for op, w := range want {
		if got[op] != w {
			t.Fatalf("operation %s inbound = %q, want %q (all %v)", op, got[op], w, got)
		}
	}
}

func TestEffectivePoliciesProductVariants(t *testing.T) {
	svc, api := service(t,
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
		pol("Microsoft.ApiManagement/service/policies", "apim/policy", `<policies><inbound><validate-jwt /></inbound></policies>`),
		res("Microsoft.ApiManagement/service/products/apis", "apim/open/chat", nil),
		pol("Microsoft.ApiManagement/service/products/policies", "apim/open/policy", `<policies><inbound><rate-limit /></inbound></policies>`),
	)
	variants := EffectivePoliciesForAPI(svc, api)
	if len(variants) != 2 || variants[0].Product != "" || variants[1].Product != "open" {
		t.Fatalf("variants = %+v", variants)
	}
	if names(variants[0].Inbound) != "validate-jwt" || names(variants[1].Inbound) != "rate-limit" {
		t.Fatalf("a product scope without <base/> must not inherit the global policy: %+v", variants)
	}
}

func TestEffectivePoliciesFragmentsAndUnresolved(t *testing.T) {
	svc, api := service(t,
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
		pol("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><include-fragment fragment-id="limits" /></inbound></policies>`),
		pol("Microsoft.ApiManagement/service/policyFragments", "apim/limits", `<fragment><llm-token-limit counter-key="k" estimate-prompt-tokens="true" tokens-per-minute="1" /></fragment>`),
	)
	variants := EffectivePoliciesForAPI(svc, api)
	if len(variants) != 1 || variants[0].External || names(variants[0].Inbound) != "llm-token-limit" {
		t.Fatalf("variants = %+v", variants)
	}

	svc, api = service(t,
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
		pol("Microsoft.ApiManagement/service/apis/policies", "apim/chat/policy", `<policies><inbound><include-fragment fragment-id="missing" /></inbound></policies>`),
	)
	variants = EffectivePoliciesForAPI(svc, api)
	if len(variants) != 1 || !variants[0].External || !strings.Contains(strings.Join(variants[0].Unresolved, ";"), "missing") {
		t.Fatalf("variants = %+v", variants)
	}
	merged, ok := EffectivePolicyForAPI(svc, api)
	if !ok || !merged.External {
		t.Fatalf("API-level view must be external when a variant is unresolved: %+v", merged)
	}
}

func TestEffectivePolicyForAPIWithoutDocuments(t *testing.T) {
	svc, api := service(t,
		res("Microsoft.ApiManagement/service", "apim", nil),
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
	)
	if _, ok := EffectivePolicyForAPI(svc, api); ok {
		t.Fatal("expected ok=false without policy documents")
	}
}

func TestConditionalAndTopLevel(t *testing.T) {
	parsed, err := ParsePolicy(`<policies><inbound><choose><when condition="x"><validate-jwt /></when></choose></inbound></policies>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(TopLevel(parsed.Inbound, "validate-jwt")) != 0 || !Conditional(parsed.Inbound, "validate-jwt") {
		t.Fatalf("inbound = %+v", parsed.Inbound)
	}
}

func TestBuildSnapshotReadsLargeLanguageModelLogsAndVersions(t *testing.T) {
	resources := model{
		res("Microsoft.ApiManagement/service/apis", "apim/chat", map[string]any{"path": "chat"}),
		res("Microsoft.ApiManagement/service/diagnostics", "apim/azuremonitor", map[string]any{"largeLanguageModel": map[string]any{"logs": "enabled"}}),
		res("Microsoft.ApiManagement/service/diagnostics", "apim/other", map[string]any{"largeLanguageModel": map[string]any{"enabled": true}}),
		{Type: "Microsoft.ApiManagement/service", Name: "apim", APIVersion: "2023-05-01-preview"},
	}
	snap := BuildSnapshot(&sdk.Input{ARM: resources})
	svc := snap.Services["apim"]
	if svc == nil || svc.APIs["chat"] == nil {
		t.Fatalf("a service declared after its children must keep them: %+v", snap)
	}
	if len(svc.Diagnostics) != 2 || !svc.Diagnostics[0].LargeLanguageModel || svc.Diagnostics[1].LargeLanguageModel {
		t.Fatalf("diagnostics = %+v", svc.Diagnostics)
	}
	issues := svc.UnverifiedVersions(svc.APIs["chat"])
	if len(issues) != 1 || !strings.Contains(issues[0], "2023-05-01-preview") {
		t.Fatalf("unverified versions = %v", issues)
	}
}

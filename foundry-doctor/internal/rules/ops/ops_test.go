package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

type pol map[string]any

func (p pol) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func findRule(rs []sdk.Rule, id string) sdk.Rule {
	for _, r := range rs {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func eval(t *testing.T, id string, p Providers, in *sdk.Input) sdk.Result {
	t.Helper()
	out, err := findRule(RegisterWith(p), id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func doc(t *testing.T, y string) sdk.AzureYAMLView {
	t.Helper()
	d, err := azureyaml.Parse([]byte(y), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRegisterIDs(t *testing.T) {
	want := []string{"FND-OPS-001", "FND-OPS-002", "FND-OPS-003", "FND-OPS-004", "FND-OPS-005", "FND-OPS-006", "FND-OPS-007", "FND-OPS-008", "FND-OPS-009", "FND-OPS-010"}
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

func TestOPS001DiagnosticSettings(t *testing.T) {
	got := eval(t, "FND-OPS-001", Providers{}, &sdk.Input{ARM: model{
		{Type: "Microsoft.CognitiveServices/accounts", Name: "acct"},
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-OPS-001", Providers{}, &sdk.Input{ARM: model{
		{Type: "Microsoft.CognitiveServices/accounts", Name: "acct"},
		{Type: "Microsoft.Insights/diagnosticSettings", Name: "acct/to-law", Scope: "/providers/Microsoft.CognitiveServices/accounts/acct", Properties: map[string]any{
			"workspaceId": "/providers/Microsoft.OperationalInsights/workspaces/law",
			"logs":        []any{map[string]any{"categoryGroup": "audit", "enabled": true}},
		}},
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestOPS005NamingConstraints(t *testing.T) {
	got := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{
		{Type: "Microsoft.Storage/storageAccounts", Name: "bad-name"},
	}})
	if len(got.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{
		{Type: "Microsoft.Storage/storageAccounts", Name: "goodname123"},
	}})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestOPS007PipelineExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "azure-dev.yml"), []byte("steps:\n- run: azd up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := &sdk.Input{Profile: "foundry-prod", AzureYAML: doc(t, "name: d\nservices:\n  p:\n    host: azure.ai.project\n")}
	clean := eval(t, "FND-OPS-007", Providers{Root: root}, in)
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
	bad := eval(t, "FND-OPS-007", Providers{Root: t.TempDir()}, in)
	if len(bad.Findings) != 1 {
		t.Fatalf("expected finding, got %+v", bad)
	}
}

func TestOPS010ModelLists(t *testing.T) {
	got := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{
		Policy: pol{"models.allow": []string{"OpenAI/gpt-4"}, "models.deny": []string{"OpenAI/gpt-4"}},
		ARM:    model{{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/dep", Properties: map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4", "version": "1"}}}},
	})
	if len(got.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", got)
	}
	clean := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{
		Policy: pol{"models.allow": []string{"OpenAI/gpt-4"}, "models.deny": []string{}},
		ARM:    model{{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/dep", Properties: map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-4", "version": "1"}}}},
	})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

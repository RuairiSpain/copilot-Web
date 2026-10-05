// Package reporttest holds the shared fixtures and golden-file helper for the reporter tests.
// It is imported only by test files.
package reporttest

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Canary is planted in policy values under credential-like keys; no reporter may print it.
const Canary = "CANARY-POLICY-VALUE-7f3a"

// Golden compares got with testdata/<name> and rewrites it under -update.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(here), "..", "testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s (run with -update): %v", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s differs (run with -update to accept)\n--- got ---\n%s", name, got)
	}
}

// FullReport returns a report with every field populated, in a deliberately unsorted order.
func FullReport() *sdk.Report {
	return &sdk.Report{
		SchemaVersion: sdk.ReportSchemaVersion,
		Tool:          sdk.ToolInfo{Name: "foundry-doctor", Version: "0.1.0-test"},
		Profile:       "foundry-prod",
		EffectivePolicy: map[string]any{
			"resourceScope":         "same-resource-group",
			"allowedExternalScopes": []any{"rg-shared", "rg-hub"},
			"validation":            map[string]any{"bicep": "required", "azure": "optional"},
			"apiToken":              Canary,
			"nested":                map[string]any{"clientSecret": Canary, "retries": 3},
		},
		Tools: []sdk.ToolStatus{
			{Name: "psrule", Version: "", State: sdk.ToolMissing, Required: false, Detail: "not installed"},
			{Name: "bicep", Path: "bicep", Version: "0.47.16", State: sdk.ToolAvailable, Required: true},
		},
		Findings: []sdk.Finding{
			{
				RuleID: "FND-NET-001", RuleVersion: 2, Severity: sdk.SeverityWarning, Category: sdk.CategoryNiceToHave,
				Pillar: "security", Basis: []string{"product-opinion"}, Profile: "foundry-prod",
				Resource:       sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.CognitiveServices/accounts", Name: "aiservices"},
				Location:       sdk.Location{File: "infra/main.bicep", Line: 40, Column: 3},
				Evidence:       "Pipe | in text and <b>html</b> and [link](http://evil.example)",
				Recommendation: "Review the setting.", Confidence: sdk.ConfidenceLikely,
				Fingerprint: "fp1:0123456789abcdef0123456789abcdef", Key: "second",
			},
			{
				RuleID: "FND-SEC-002", RuleVersion: 1, Severity: sdk.SeverityError, Category: sdk.CategoryMustHave,
				Pillar: "security", Basis: []string{"platform", "docs"}, Profile: "foundry-prod",
				Resource:       sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.CognitiveServices/accounts", Name: "aiservices", ID: "/subscriptions/x/resourceGroups/rg/providers/p/n", Pointer: "/properties/disableLocalAuth"},
				Location:       sdk.Location{File: "infra/main.bicep", Line: 12, Column: 5, EndLine: 14, EndColumn: 2, Pointer: "/resources/0"},
				Evidence:       "disableLocalAuth is false\nsecond line",
				Recommendation: "Set disableLocalAuth to true.", Fix: "disableLocalAuth: true",
				DocsURL: "https://learn.microsoft.com/azure/ai-services/", LastVerified: "2026-10-05",
				Confidence: sdk.ConfidenceCertain, Fingerprint: "fp1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Adapter: "native", Key: "disableLocalAuth",
			},
			{
				RuleID: "FND-SEC-002", RuleVersion: 1, Severity: sdk.SeverityError, Category: sdk.CategoryMustHave,
				Profile:        "foundry-prod",
				Location:       sdk.Location{File: "infra/other.bicep", Line: 3},
				Evidence:       "Unicode 日本語 and emoji \U0001F600 and ANSI \x1b[31mred\x1b[0m",
				Recommendation: "Fix it.", Confidence: sdk.ConfidenceCertain,
				Fingerprint: "fp1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Suppressed:  &sdk.Suppression{Reason: "accepted risk", Expires: "2027-01-01", Owner: "team-a", Source: ".foundry-doctor/suppressions.yaml"},
			},
			{
				RuleID: "FND-ENV-003", RuleVersion: 1, Severity: sdk.SeverityInfo, Category: sdk.CategoryNiceToHave,
				Profile: "foundry-prod", Resource: sdk.ResourceRef{Kind: "env-key", Name: "AZURE_LOCATION"},
				Evidence: "Key AZURE_LOCATION is unset", Recommendation: "Set it.", Confidence: sdk.ConfidenceUncertain,
				Fingerprint: "fp1:cccccccccccccccccccccccccccccccc", Baselined: true,
			},
			{
				RuleID: "FND-SYS-INVALID-AZURE-YAML", Severity: sdk.SeverityError, Profile: "foundry-prod",
				Location: sdk.Location{File: "azure.yaml"},
				Evidence: "azure.yaml failed to parse", Recommendation: "Fix the YAML.", Confidence: sdk.ConfidenceCertain,
				Fingerprint: "fp1:dddddddddddddddddddddddddddddddd",
			},
		},
		Skipped: []sdk.SkippedCheck{
			{RuleID: "FND-NET-009", RuleVersion: 1, Reason: "missing-input", Detail: "no compiled ARM", MissingCapability: "compiled ARM template", Resource: sdk.ResourceRef{Kind: "project", Name: "demo"}},
			{RuleID: "FND-IDN-004", RuleVersion: 1, Reason: "profile-key-missing:policy.allowedExternalScopes", MissingCapability: "profile key policy.allowedExternalScopes"},
			{RuleID: "FND-IDN-005", RuleVersion: 3, Reason: "profile-key-missing:policy.allowedExternalScopes"},
		},
		Summary: sdk.Summary{
			Info: 0, Warning: 1, Error: 2, Suppressed: 1, Baselined: 1, Skipped: 3, Passed: 40, Hidden: 4,
			SkippedByReason: map[string]int{"missing-input": 1, "profile-key-missing:policy.allowedExternalScopes": 2},
		},
		ExitCode: sdk.ExitFindings,
	}
}

// Huge returns a report with n distinct findings spread over files, severities and rules.
func Huge(n int) *sdk.Report {
	r := &sdk.Report{Tool: sdk.ToolInfo{Name: "foundry-doctor", Version: "t"}, Profile: "foundry-dev"}
	sev := []sdk.Severity{sdk.SeverityInfo, sdk.SeverityWarning, sdk.SeverityError}
	for i := 0; i < n; i++ {
		r.Findings = append(r.Findings, sdk.Finding{
			RuleID: fmt.Sprintf("FND-NET-%03d", i%50), RuleVersion: 1, Severity: sev[i%3], Profile: "foundry-dev",
			Location: sdk.Location{File: fmt.Sprintf("infra/f%d.bicep", i%97), Line: i%500 + 1, Column: 1},
			Evidence: fmt.Sprintf("evidence %d", i), Recommendation: "r", Confidence: sdk.ConfidenceCertain,
			Fingerprint: fmt.Sprintf("fp1:%032x", i),
		})
	}
	return r
}

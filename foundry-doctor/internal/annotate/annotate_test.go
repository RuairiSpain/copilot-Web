package annotate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestCollectAndGitHubWorkflowCommands(t *testing.T) {
	finds := []sdk.Finding{
		{
			RuleID:         "FND-CFG-001",
			Severity:       sdk.SeverityError,
			Category:       sdk.CategoryMustHave,
			Location:       sdk.Location{File: "azure.yaml", Line: 3, Column: 1},
			Evidence:       "line1\nline2 :: redacted-marker",
			Recommendation: "Use % managed identity",
			Fingerprint:    "fp-1",
		},
		{
			RuleID:      "FND-NET-001",
			Severity:    sdk.SeverityWarning,
			Location:    sdk.Location{File: "../secret.txt", Line: 1},
			Evidence:    "outside",
			Fingerprint: "fp-2",
		},
		{
			RuleID:      "FND-BCP-001",
			Severity:    sdk.SeverityWarning,
			Location:    sdk.Location{File: "infra/main.bicep", Line: 4, Column: 2},
			Evidence:    "rule-derived bicep finding",
			Fingerprint: "fp-3",
		},
	}
	diags := []bicep.Diagnostic{{File: "infra/main.bicep", Line: 9, Column: 2, Severity: bicep.SeverityWarning, Code: "BCP057", Message: "bad % value :: detail"}}
	entries := Collect(finds, diags)
	if len(entries) != 4 {
		t.Fatalf("entries=%d", len(entries))
	}
	var inlineFound, outsideFound, ruleBicepManifestOnly bool
	for _, entry := range entries {
		switch entry.RuleID {
		case "FND-CFG-001":
			inlineFound = entry.Inline && entry.InlineReason == ""
		case "FND-NET-001":
			outsideFound = !entry.Inline && entry.InlineReason != "" && entry.File == ""
		case "FND-BCP-001":
			ruleBicepManifestOnly = !entry.Inline && entry.InlineReason == "untrusted-bicep-location"
		}
	}
	if !inlineFound {
		t.Fatalf("yaml finding should be inline: %+v", entries)
	}
	if !outsideFound {
		t.Fatalf("unsafe path should be manifest-only: %+v", entries)
	}
	if !ruleBicepManifestOnly {
		t.Fatalf("rule-derived bicep finding should stay manifest-only: %+v", entries)
	}
	var out bytes.Buffer
	if err := GitHubWorkflowCommands(&out, entries); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "\nline2") || strings.Contains(text, ":: ghp") || strings.Contains(text, "line1\nline2") {
		t.Fatalf("workflow output not sanitised:\n%s", text)
	}
	if !strings.Contains(text, "::error file=azure.yaml,line=3,col=1,title=FND-CFG-001::") || !strings.Contains(text, "::warning file=infra/main.bicep,line=9,col=2,title=BCP057::") {
		t.Fatalf("workflow output missing entries:\n%s", text)
	}
}

func TestCollectTruncatesAndEscapesMessages(t *testing.T) {
	entries := Collect([]sdk.Finding{{
		RuleID:         "FND-CFG-001",
		Severity:       sdk.SeverityError,
		Location:       sdk.Location{File: "azure.yaml", Line: 1, Column: 1},
		Evidence:       strings.Repeat("abc::%\n", 80),
		Fingerprint:    "fp-1",
		Recommendation: strings.Repeat("x", 20),
	}}, nil)
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	if got := entries[0].Message; len(got) > maxMessageBytes || strings.Contains(got, "\n") || strings.Contains(got, "::") {
		t.Fatalf("message not bounded/sanitised: len=%d %q", len(got), got)
	}
}

func TestWriteReview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\nservices:\n  api:\n    host: containerapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "infra", "main.bicep"), []byte("param location string = 'westeurope'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := WriteReview(t.Context(), ReviewRequest{
		RootDir: root, OutDir: "review/sample.review", AzureYAML: "azure.yaml", InfraPath: "infra",
		Entries: []Entry{
			{ID: "f1", RuleID: "FND-CFG-001", Severity: sdk.SeverityError, Category: "must-have", File: "azure.yaml", Line: 1, Message: "config problem", Inline: true},
			{ID: "f2", RuleID: "BCP057", Severity: sdk.SeverityWarning, File: "infra/main.bicep", Line: 1, Message: "compile issue", Inline: true},
			{ID: "f3", RuleID: "FND-NET-001", Severity: sdk.SeverityWarning, File: "../outside.txt", Line: 1, Message: "outside", Inline: false, InlineReason: "outside-project"},
		},
		Profile: "foundry-test", ToolVersion: "0.1.0-dev", WriteDiff: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Summary.Total != 3 || result.Manifest.Summary.Inline != 2 || result.Manifest.Summary.ManifestOnly != 1 {
		t.Fatalf("manifest summary %+v", result.Manifest.Summary)
	}
	manifestPath := filepath.Join(root, "review", "sample.review", "annotations-manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != ManifestSchemaVersion || len(manifest.Files) != 2 {
		t.Fatalf("manifest %+v", manifest)
	}
	origYAML, _ := os.ReadFile(filepath.Join(root, "azure.yaml"))
	if string(origYAML) != "name: demo\nservices:\n  api:\n    host: containerapp\n" {
		t.Fatalf("original azure.yaml changed:\n%s", origYAML)
	}
	origBicep, _ := os.ReadFile(filepath.Join(root, "infra", "main.bicep"))
	if string(origBicep) != "param location string = 'westeurope'\n" {
		t.Fatalf("original bicep changed:\n%s", origBicep)
	}
	yamlCopy, _ := os.ReadFile(filepath.Join(root, "review", "sample.review", "azure.yaml"))
	if !strings.Contains(string(yamlCopy), "# Foundry Doctor: FND-CFG-001 error must-have (ref f1). config problem") {
		t.Fatalf("yaml review copy missing annotation:\n%s", yamlCopy)
	}
	bicepCopy, _ := os.ReadFile(filepath.Join(root, "review", "sample.review", "infra", "main.bicep"))
	if !strings.Contains(string(bicepCopy), "// Foundry Doctor: BCP057 warning (ref f2). compile issue") {
		t.Fatalf("bicep review copy missing annotation:\n%s", bicepCopy)
	}
	if _, err := os.Stat(filepath.Join(root, "review", "sample.review", "annotations.diff")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteReviewRejectsUnsafeOut(t *testing.T) {
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "..\\oops.review", AzureYAML: "azure.yaml"}); err == nil {
		t.Fatal("want error")
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "oops", AzureYAML: "azure.yaml"}); err == nil {
		t.Fatal("want .review error")
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{RootDir: t.TempDir(), OutDir: "infra\\nested.review", AzureYAML: "azure.yaml", InfraPath: "infra"}); err == nil {
		t.Fatal("want nested review path error")
	}
}

func TestWriteReviewZeroFindingManifestUsesArrays(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReview(t.Context(), ReviewRequest{
		RootDir: root, OutDir: "review/empty.review", AzureYAML: "azure.yaml", ToolVersion: "0.1.0-dev",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "review", "empty.review", "annotations-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"files": []`) || !strings.Contains(text, `"annotations": []`) {
		t.Fatalf("manifest should emit arrays, got:\n%s", text)
	}
}

func TestWriteReviewRerunIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "azure.yaml"), []byte("name: demo\nservices:\n  api:\n    host: containerapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := ReviewRequest{
		RootDir: root, OutDir: "review/idempotent.review", AzureYAML: "azure.yaml",
		Entries:     []Entry{{ID: "f1", RuleID: "FND-CFG-001", Severity: sdk.SeverityError, File: "azure.yaml", Line: 1, Message: "config problem", Inline: true}},
		ToolVersion: "0.1.0-dev",
	}
	if _, err := WriteReview(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteReview(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "review", "idempotent.review", "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "# Foundry Doctor: FND-CFG-001 error"); got != 1 {
		t.Fatalf("annotation count=%d want 1\n%s", got, data)
	}
}

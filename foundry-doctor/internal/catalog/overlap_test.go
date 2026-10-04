package catalog

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func overlapFixture() []Rule {
	return []Rule{
		{ID: "FND-SEC-002", Group: "SEC", Overlap: Overlap{Decision: "native", Coverage: "none"}},
		{ID: "FND-SEC-001", Group: "SEC", Overlap: Overlap{Decision: "adapt", Coverage: "partial",
			PSRule: []string{"Azure.Search.LocalAuth"}, AzurePolicy: []string{"6300012e"}, Checkov: []string{"CKV_AZURE_1|2"}}},
		{ID: "FND-CFG-001", Group: "CFG", Overlap: Overlap{Decision: "wrap", Coverage: "full", BicepLinter: []string{"no-hardcoded-env-urls"}}},
		{ID: "FND-CFG-002", Group: "CFG"}, // unresearched: renders dashes
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s (run: go test ./internal/catalog -update)\n--- got ---\n%s", golden, got)
	}
}

func TestOverlapMatrixGolden(t *testing.T) {
	checkGolden(t, "overlap-matrix.golden", OverlapMatrix(overlapFixture()))
}

func TestMarkdownGolden(t *testing.T) {
	rules := []Rule{
		{ID: "FND-SEC-001", Group: "SEC", Title: "Local auth | disabled", Status: StatusVerified, Phases: []string{"1", "2"},
			Severity: Severity{Dev: "warning", Test: "error", Prod: "error"}, Overlap: Overlap{Decision: "adapt"},
			Sources: []Source{{URL: "https://example.com/a(b)|c", LastVerified: "2026-10-04"}}},
		{ID: "FND-CFG-001", Group: "CFG", Title: "Opinion", Status: StatusProductOpinion, Phases: []string{"later"}, Overlap: Overlap{Decision: "native"}},
		{ID: "FND-CFG-002", Group: "CFG", Title: "Seed", Status: StatusProposed, Phases: []string{"1"}},
	}
	checkGolden(t, "rule-catalog.golden", Markdown(rules))
}

func TestMatrixTotalsReconcile(t *testing.T) {
	md := OverlapMatrix(overlapFixture())
	if !strings.Contains(md, "| **all** | 0 | 1 | 1 | 1 | 0 | 1 | 4 |") {
		t.Fatalf("totals row does not reconcile with the 4 rules (incl. 1 unresearched):\n%s", md)
	}
}

func TestOverlapMatrixIndependentOfInputOrder(t *testing.T) {
	a := overlapFixture()
	b := []Rule{a[3], a[2], a[1], a[0]}
	if OverlapMatrix(a) != OverlapMatrix(b) {
		t.Fatal("output depends on input order")
	}
}

package catalog

import (
	"flag"
	"os"
	"path/filepath"
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

func TestOverlapMatrixGolden(t *testing.T) {
	got := OverlapMatrix(overlapFixture())
	golden := filepath.Join("testdata", "overlap-matrix.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("overlap matrix differs from %s (run: go test ./internal/catalog -update)\n--- got ---\n%s", golden, got)
	}
}

func TestOverlapMatrixIndependentOfInputOrder(t *testing.T) {
	a := overlapFixture()
	b := []Rule{a[3], a[2], a[1], a[0]}
	if OverlapMatrix(a) != OverlapMatrix(b) {
		t.Fatal("output depends on input order")
	}
}

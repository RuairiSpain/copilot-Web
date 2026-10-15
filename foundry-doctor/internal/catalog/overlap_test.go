package catalog

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestStructuredMatrixDoesNotLabelMissingDecisionCountAsResearchState(t *testing.T) {
	rules := overlapFixture()
	rules[0].Overlap.Research.PSRule.State = ResearchUnresearched
	md := OverlapMatrix(rules)
	if strings.Contains(md, "| unresearched | total |") {
		t.Fatalf("decision summary conflates missing decisions with per-tool research state:\n%s", md)
	}
	if !strings.Contains(md, "| no decision | total |") {
		t.Fatalf("decision summary does not identify its missing-decision column:\n%s", md)
	}
}
func TestOverlapMatrixIndependentOfInputOrder(t *testing.T) {
	a := overlapFixture()
	b := []Rule{a[3], a[2], a[1], a[0]}
	if OverlapMatrix(a) != OverlapMatrix(b) {
		t.Fatal("output depends on input order")
	}
	if Markdown(a) != Markdown(b) {
		t.Fatal("Markdown output depends on input order")
	}
}

func TestMarkdownEscapesHTMLMarkdownAndCode(t *testing.T) {
	rules := []Rule{{
		ID:    "FND-X-001<script>",
		Group: "X<img>",
		Title: "a | <script>& `tick`",
		Sources: []Source{{
			URL:          "https://example.test/a>b?x=1&y=2",
			LastVerified: "2026-10-04",
		}},
		Overlap: Overlap{PSRule: []string{"`</code><script>"}},
	}}
	md := Markdown(rules) + OverlapMatrix(rules)
	for _, unsafe := range []string{"<script>", "<img>", "</code>"} {
		if strings.Contains(md, unsafe) {
			t.Fatalf("output contains unescaped %q:\n%s", unsafe, md)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;", "&#124;", "&#96;", "%3E", "&amp;"} {
		if !strings.Contains(md, escaped) {
			t.Fatalf("output lacks escaped form %q:\n%s", escaped, md)
		}
	}
}

func TestMarkdownTableEscapingAdversarialInput(t *testing.T) {
	hostile := "pipe| slash\\ ticks``` lines\r\nnext <b>& \u202e \u2066 \x00 \x1f \x7f"
	rules := []Rule{{
		ID:      hostile,
		Group:   hostile,
		Title:   hostile,
		Phases:  []string{hostile},
		Overlap: Overlap{Decision: hostile, PSRule: []string{hostile}},
		Sources: []Source{{URL: "https://example.test/" + hostile, LastVerified: hostile}},
	}}
	md := Markdown(rules) + OverlapMatrix(rules)
	for _, raw := range []string{"| slash", "\\", "```", "<b>", "\r", "\nnext", "\u202e", "\u2066", "\x00", "\x1f", "\x7f"} {
		if strings.Contains(md, raw) {
			t.Fatalf("output contains unsafe raw sequence %q:\n%s", raw, md)
		}
	}
	for _, escaped := range []string{"&#124;", "&#92;", "&#96;", "&lt;b&gt;", "&amp;", "&#xfffd;", "%7C", "%5C"} {
		if !strings.Contains(md, escaped) {
			t.Fatalf("output lacks escaped sequence %q:\n%s", escaped, md)
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasSuffix(line, " |") {
			t.Fatalf("table row was split by hostile input: %q", line)
		}
	}
}

func TestRealCatalogueInvariantsAndGeneratedDocs(t *testing.T) {
	root := filepath.Join("..", "..")
	rules, err := Load(context.Background(), os.DirFS(root), "rules/catalog")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(rules, Options{Now: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	groupCounts := map[string]int{"CFG": 12, "COST": 4, "DEP": 12, "ENV": 4, "GW": 6, "IDN": 6, "IQ": 12, "NET": 11, "OPS": 10, "REL": 9, "RUN": 8, "SEC": 14}
	if len(rules) != 108 {
		t.Fatalf("catalogue has %d rules, want canonical 108", len(rules))
	}
	seen := map[string]int{}
	for _, r := range rules {
		seen[r.Group]++
	}
	if len(seen) != len(groupCounts) {
		t.Fatalf("groups = %v, want %v", seen, groupCounts)
	}
	for group, count := range groupCounts {
		if seen[group] != count {
			t.Errorf("group %s has %d rules, want %d", group, seen[group], count)
		}
		for n := 1; n <= count; n++ {
			id := fmt.Sprintf("FND-%s-%03d", group, n)
			if _, ok := slices.BinarySearchFunc(rules, id, func(r Rule, id string) int { return strings.Compare(r.ID, id) }); !ok {
				t.Errorf("canonical id %s is missing", id)
			}
		}
	}
	for name, got := range map[string]string{
		"docs/rule-catalog.md":     Markdown(rules),
		"docs/overlap-analysis.md": OverlapMatrix(rules),
	} {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s is stale; run rulecatalog generation", name)
		}
	}
}

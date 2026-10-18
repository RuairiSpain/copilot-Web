package mermaid

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

var update = flag.Bool("update", false, "update golden files")

func goldenGraph() view.Graph {
	return view.Graph{
		Mode: view.ModeCombined,
		Nodes: []view.Node{
			{ID: "n1", Label: "proj", Kind: "project", Origin: view.OriginLocal},
			{ID: "n2", Label: "gpt-4o-mini", Kind: "model", Origin: view.OriginBoth, Severity: "error", Unhealthy: true},
			{ID: "n3", Label: "storage", Kind: "storage", Origin: view.OriginAzure},
			{ID: "n4", Label: `x"]-->evil #1 <b>`, Kind: "missing", Origin: view.OriginMissing, Missing: true, Baselined: 1},
		},
		Edges: []view.Edge{
			{From: "n1", To: "n2", Kind: "deploys"},
			{From: "n1", To: "n3", Kind: "uses"},
			{From: "n3", To: "n4", Kind: "targets", Missing: true},
		},
		Summary: view.Summary{Mode: view.ModeCombined, NodeCount: 4, EdgeCount: 3},
	}
}

func TestRenderGolden(t *testing.T) {
	got := Render(goldenGraph())
	checkGolden(t, "graph.mmd.golden", got)
	assertValidMermaid(t, string(got))
}

func TestRenderOneClassStatementPerClass(t *testing.T) {
	got := string(Render(goldenGraph()))
	for _, want := range []string{"  class n2 both\n", "  class n2 unhealthy\n", "  class n4 missing\n", "  class n4 baselined\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "both,unhealthy") || strings.Contains(got, "missing,baselined") {
		t.Fatalf("class names must not be comma-joined:\n%s", got)
	}
}

func TestRenderEscapesHostileLabels(t *testing.T) {
	labels := []string{
		`x"]-->evil`,
		"a\nb\r\nc",
		"`backtick` | pipe",
		`<script>alert(1)</script>`,
		`#quot; already encoded & amp`,
		`click n1 call evil()`,
		`end"] ; classDef x fill:red`,
	}
	for _, l := range labels {
		g := view.Graph{Mode: view.ModeSource, Nodes: []view.Node{
			{ID: "n1", Label: l, Kind: "service", Origin: view.OriginLocal},
			{ID: "n2", Label: "b", Kind: "service", Origin: view.OriginLocal, Unhealthy: true, Baselined: 2},
		}, Edges: []view.Edge{{From: "n1", To: "n2", Kind: "uses|evil"}}}
		got := string(Render(g))
		assertValidMermaid(t, got)
		if strings.Contains(got, `\"`) {
			t.Errorf("backslash escapes are not Mermaid syntax: %s", got)
		}
	}
}

func TestRenderDisconnectedNodes(t *testing.T) {
	g, err := view.Build(view.Input{Source: view.SourceInput{Resources: []coregraph.Resource{
		{ID: "/resources/0", Type: "Microsoft.Storage/storageAccounts", Name: "a"},
		{ID: "/resources/1", Type: "Microsoft.Search/searchServices", Name: "b"},
	}}}, view.Options{Mode: view.ModeSource})
	if err != nil {
		t.Fatal(err)
	}
	got := string(Render(g))
	assertValidMermaid(t, got)
	for _, want := range []string{"  n1[\"b\"]\n", "  n2[\"a\"]\n", "  class n1,n2 local\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestEscapeLabelEntityCodes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a"b`, "a#quot;b"},
		{"#1", "#35;1"},
		{"<b>", "#lt;b#gt;"},
		{"a&b", "a#amp;b"},
		{"`x`", "#96;x#96;"},
		{"a|b", "a#124;b"},
		{"a\r\nb", "a b"},
	}
	for _, c := range cases {
		if got := escapeLabel(c.in); got != c.want {
			t.Errorf("escapeLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

var (
	reNode     = regexp.MustCompile(`^  (n[0-9]+)\["([^"\n]*)"\]$`)
	reEdge     = regexp.MustCompile(`^  (n[0-9]+) (-->|-\.->|==>)\|([^|"\n]*)\| (n[0-9]+)$`)
	reClassDef = regexp.MustCompile(`^  classDef ([a-z]+) [^\n]+$`)
	reClass    = regexp.MustCompile(`^  class (n[0-9]+(?:,n[0-9]+)*) ([a-z]+)$`)
	reAcc      = regexp.MustCompile(`^  acc(Title|Descr): [^\n;]*$`)
	reEntity   = regexp.MustCompile(`#[a-z0-9]+;`)
)

// assertValidMermaid checks the output against the restricted flowchart
// subset Render promises: every line is a header, accessibility, node, edge,
// classDef or class statement; labels contain no raw quote and every '#' in a
// label starts an entity code; every class name used has a classDef and every
// referenced node is declared.
func assertValidMermaid(t *testing.T, out string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) == 0 || lines[0] != "flowchart LR" {
		t.Fatalf("missing flowchart header: %q", out)
	}
	declared := map[string]bool{}
	defs := map[string]bool{}
	var classUses [][2]string
	var edgeEnds []string
	for _, line := range lines[1:] {
		switch {
		case reAcc.MatchString(line):
		case reNode.MatchString(line):
			m := reNode.FindStringSubmatch(line)
			declared[m[1]] = true
			if strings.Count(m[2], "#") != len(reEntity.FindAllString(m[2], -1)) {
				t.Errorf("label has a bare '#': %q", line)
			}
		case reEdge.MatchString(line):
			m := reEdge.FindStringSubmatch(line)
			edgeEnds = append(edgeEnds, m[1], m[4])
		case reClassDef.MatchString(line):
			defs[reClassDef.FindStringSubmatch(line)[1]] = true
		case reClass.MatchString(line):
			m := reClass.FindStringSubmatch(line)
			for _, id := range strings.Split(m[1], ",") {
				classUses = append(classUses, [2]string{id, m[2]})
			}
		default:
			t.Errorf("line outside the supported Mermaid subset: %q", line)
		}
	}
	for _, id := range edgeEnds {
		if !declared[id] {
			t.Errorf("edge references undeclared node %s", id)
		}
	}
	for _, u := range classUses {
		if !declared[u[0]] {
			t.Errorf("class references undeclared node %s", u[0])
		}
		if !defs[u[1]] {
			t.Errorf("class %q has no classDef", u[1])
		}
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: %v", name, err)
	}
	norm := func(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }
	if norm(want) != norm(got) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

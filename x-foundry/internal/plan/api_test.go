package plan_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

const minimalYAML = "x-foundry:\n  topology: {mode: standalone}\n  security: {roles: {admins: [Admins]}}\n  projects: [{name: finance}]\n"

func TestBuildAcceptsFilesTextAndMappings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "azure.yaml")
	if err := os.WriteFile(file, []byte(minimalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := plan.BuildFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fromText, err := plan.BuildText(minimalYAML)
	if err != nil {
		t.Fatal(err)
	}
	fromMapping, err := plan.BuildMapping(Doc(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(fromFile.Order(), ",") != strings.Join(fromText.Order(), ",") || fromMapping.Config.Projects[0].Name != "finance" {
		t.Fatal("the three entry points must agree")
	}
}

func TestBuildReturnsAFailureListingEveryError(t *testing.T) {
	_, err := plan.BuildMapping(Doc(t, `projects: [{name: aa}, {name: aa}]`, `defaults: {location: atlantis}`))
	var f *diag.Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v", err)
	}
	codes := map[string]bool{}
	for _, d := range f.Diagnostics {
		codes[d.Code] = true
	}
	if len(codes) != 2 || !codes["XF001"] || !codes["XF023"] || !strings.Contains(err.Error(), "validation failed with 2 error(s)") {
		t.Fatalf("diagnostics:\n%s", Lines(f.Diagnostics))
	}
}

func TestAnalysisStopsAtTheFirstFailingPhase(t *testing.T) {
	web := `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`
	cases := []struct {
		name string
		yaml []string
		want string
	}{
		{"schema", y(`projects: []`), "XF102"},
		{"declared", y(`projects: [{name: aa}, {name: aa}]`, web, Public, `search: {enabled: false}`), "XF001"},
		{"normalise", y(Public, `search: {enabled: false}`, web), "XF020"},
		{"effective", y(`models: {default: nope, allowed: [other]}`), "XF006"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Run(t, c.yaml...)
			if a.OK() {
				t.Fatal("expected failure")
			}
			OnlyCodes(t, a, c.want)
		})
	}
}

func TestAnalyseFileAndTextErrors(t *testing.T) {
	a, err := plan.AnalyseFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || a.OK() || a.Diagnostics[0].Code != "XF100" {
		t.Fatalf("%v %+v", err, a)
	}
	var f *diag.Failure
	if !errors.As(a.Err(), &f) {
		t.Fatal("Err must return a *diag.Failure")
	}
	if a, _ := plan.AnalyseText("x-foundry: [", "<t>"); a.OK() {
		t.Fatal("invalid YAML")
	}
	if _, err := plan.BuildText("x-foundry: ["); err == nil {
		t.Fatal("BuildText must fail")
	}
	good := Run(t)
	if good.Err() != nil {
		t.Fatal("a plan has no error")
	}
}

func TestWarningsTravelWithThePlan(t *testing.T) {
	p := MustPlan(t, `security: {roles: {admins: []}}`)
	if len(p.Warnings) != 1 || p.Warnings[0].Code != "XF114" || p.Warnings[0].Severity != diag.Warning {
		t.Fatalf("warnings = %+v", p.Warnings)
	}
}

func TestPlanJSONIsCamelCaseAndRoundTrips(t *testing.T) {
	p, err := plan.BuildFile("../../examples/hub-spoke.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.JSON("  ")
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	cfg := data["config"].(map[string]any)
	if data["schemaVersion"] != "1.0" || cfg["topologyMode"] != "hub-spoke" || cfg["agentSetup"] != "basic" {
		t.Fatalf("json = %v", cfg)
	}
	nodes := data["nodes"].([]any)
	if len(nodes) != len(p.Nodes) || nodes[0].(map[string]any)["id"] != p.Nodes[0].ID {
		t.Fatal("nodes")
	}
	compact, _ := p.JSON("")
	if strings.Contains(string(compact), "\n") {
		t.Fatal("compact JSON must be one line")
	}
}

func TestPlanNodeLookup(t *testing.T) {
	p := MustPlan(t)
	n, ok := p.Node("resource-group")
	if !ok || len(n.DependsOn) != 0 {
		t.Fatalf("%+v", n)
	}
	if _, ok := p.Node("ghost"); ok {
		t.Fatal("unknown node")
	}
}

func TestDiagnosticHelpers(t *testing.T) {
	w := diag.Warn("XF999", "x-foundry.a", "careful %d", 1)
	if w.String() != "warning XF999 at x-foundry.a: careful 1" || diag.Err("XF1", "", "m").String() != "error XF1: m" {
		t.Fatal(w.String())
	}
	ds := []diag.Diagnostic{w, diag.Err("XF1", "", "m"), diag.Err("XF1", "", "m")}
	if len(diag.Errors(ds)) != 2 || !diag.HasErrors(ds) || diag.HasErrors(ds[:1]) || len(diag.Dedupe(ds)) != 2 {
		t.Fatal("diag helpers")
	}
	if !strings.Contains(diag.Fail(ds...).Error(), "2 error(s)") {
		t.Fatal("Failure.Error")
	}
}

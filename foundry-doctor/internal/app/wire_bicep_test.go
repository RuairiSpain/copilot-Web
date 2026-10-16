package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func diag(code string, sev bicep.Severity, file string, line, col int, msg string) bicep.Diagnostic {
	return bicep.Diagnostic{File: file, Line: line, Column: col, Severity: sev, Code: code, Message: msg}
}

func TestBicepFindingsMapping(t *testing.T) {
	t.Parallel()
	ds := []bicep.Diagnostic{
		diag("BCP057", bicep.SeverityError, "infra/main.bicep", 9, 21, "The name does not exist."),
		diag("no-hardcoded-env-urls", bicep.SeverityWarning, "infra/main.bicep", 3, 11, "Hardcoded URL."),
		diag("custom-info", bicep.SeverityInfo, "infra/main.bicep", 1, 1, "Note."),
	}
	got := bicepFindings(ds, "infra/main.bicep")
	if len(got) != 3 {
		t.Fatalf("got %d findings", len(got))
	}
	want := []struct {
		id  string
		sev sdk.Severity
	}{{"bicep/BCP057", sdk.SeverityError}, {"bicep/no-hardcoded-env-urls", sdk.SeverityWarning}, {"bicep/custom-info", sdk.SeverityInfo}}
	for i, w := range want {
		f := got[i]
		if f.RuleID != w.id || f.Severity != w.sev || f.Adapter != "bicep" {
			t.Errorf("finding %d = %+v", i, f)
		}
		if f.Category != "" || len(f.Basis) != 0 || f.Pillar != "" {
			t.Errorf("adapter finding must carry no catalogue metadata: %+v", f)
		}
		if f.Fingerprint == "" || f.Key == "" || f.Location.File != "infra/main.bicep" || f.Evidence == "" {
			t.Errorf("incomplete finding %+v", f)
		}
	}
	if got[0].Location.Line != 9 || got[0].Location.Column != 21 {
		t.Errorf("location = %+v", got[0].Location)
	}
}

func TestBicepFindingsKeyAndFingerprint(t *testing.T) {
	t.Parallel()
	a := diag("no-unused-vars", bicep.SeverityWarning, "infra/main.bicep", 4, 5, "Variable \"x\" is unused.")
	b := diag("no-unused-vars", bicep.SeverityWarning, "infra/main.bicep", 8, 5, "Variable \"y\" is unused.")
	c := diag("no-unused-vars", bicep.SeverityWarning, "infra/main.bicep", 12, 5, "Variable \"x\" is unused.")
	fs := bicepFindings([]bicep.Diagnostic{a, b, c}, "")
	seen := map[string]bool{}
	for _, f := range fs {
		if seen[f.Fingerprint] {
			t.Fatalf("fingerprint collision for %+v", f)
		}
		seen[f.Fingerprint] = true
	}
	if !strings.HasSuffix(fs[2].Key, ":2") {
		t.Errorf("second identical diagnostic needs an occurrence suffix, key=%q", fs[2].Key)
	}
	// Moving a diagnostic to another line must not change its fingerprint.
	moved := a
	moved.Line = 40
	if got := bicepFindings([]bicep.Diagnostic{moved}, "")[0]; got.Fingerprint != fs[0].Fingerprint {
		t.Errorf("fingerprint changed with the line: %s != %s", got.Fingerprint, fs[0].Fingerprint)
	}
	// A different file differs.
	other := a
	other.File = "infra/other.bicep"
	if got := bicepFindings([]bicep.Diagnostic{other}, "")[0]; got.Fingerprint == fs[0].Fingerprint {
		t.Error("same diagnostic in another file must differ")
	}
}

func TestBicepFindingsRedactAndEntryFallback(t *testing.T) {
	t.Parallel()
	secret := "pass" + "word=" + "abc123def456"
	fs := bicepFindings([]bicep.Diagnostic{diag("BCP999", bicep.SeverityWarning, "", 0, 0, "value "+secret)}, "infra/main.bicep")
	if len(fs) != 1 || strings.Contains(fs[0].Evidence, "abc123def456") {
		t.Fatalf("secret leaked: %+v", fs)
	}
	if fs[0].Location.File != "infra/main.bicep" {
		t.Errorf("entry fallback: %+v", fs[0].Location)
	}
	if got := bicepFindings(nil, "x"); len(got) != 0 {
		t.Errorf("no diagnostics must give no findings: %v", got)
	}
}

func TestFilterAdapterFindings(t *testing.T) {
	t.Parallel()
	fs := bicepFindings([]bicep.Diagnostic{
		diag("BCP057", bicep.SeverityError, "a.bicep", 1, 1, "m"),
		diag("no-unused-params", bicep.SeverityWarning, "a.bicep", 2, 1, "n"),
	}, "")
	tests := []struct {
		name               string
		selectors, exclude []string
		want               int
	}{
		{"no selectors", nil, nil, 2},
		{"default category selectors", []string{"must-have", "nice-to-have"}, nil, 2},
		{"specific rule selected leaves them out", []string{"FND-CFG-001"}, nil, 0},
		{"group selected leaves them out", []string{"must-have", "CFG"}, nil, 0},
		{"exact exclude", nil, []string{"bicep/BCP057"}, 1},
		{"glob exclude", nil, []string{"bicep/*"}, 0},
		{"unrelated exclude", nil, []string{"FND-CFG-*"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := filterAdapterFindings(fs, tt.selectors, tt.exclude); len(got) != tt.want {
				t.Fatalf("got %d findings, want %d", len(got), tt.want)
			}
		})
	}
}

func TestAdapterStoreTakeOnce(t *testing.T) {
	t.Parallel()
	var nilStore *adapterStore
	nilStore.put("d", []sdk.Finding{{RuleID: "x"}})
	if nilStore.take("d") != nil {
		t.Fatal("nil store must be inert")
	}
	s := &adapterStore{}
	s.put("d", []sdk.Finding{{RuleID: "x"}})
	if len(s.take("d")) != 1 || s.take("d") != nil {
		t.Fatal("findings must be taken exactly once")
	}
	s.put("d", []sdk.Finding{{RuleID: "x"}})
	s.put("d", nil)
	if s.take("d") != nil {
		t.Fatal("empty put must clear")
	}
}

// sarifRunner serves a compiled template and a recorded SARIF document. The
// SARIF fixtures were recorded from `bicep build --diagnostics-format sarif`
// (Bicep CLI 0.48.1); {{ROOT}} stands for the project directory.
type sarifRunner struct {
	arm    []byte
	sarif  []byte
	exit   int
	called [][]string
}

func (r *sarifRunner) Run(_ context.Context, _ string, args []string, _ string) (bicep.RunResult, error) {
	r.called = append(r.called, args)
	if args[0] == "--version" {
		return bicep.RunResult{Stdout: []byte("Bicep CLI version 0.48.1 (test)")}, nil
	}
	if args[0] == "build" {
		return bicep.RunResult{ExitCode: r.exit, Stdout: r.arm, Stderr: r.sarif}, nil
	}
	return bicep.RunResult{ExitCode: 2}, nil
}

func recordedSARIF(t *testing.T, name, root string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "bicepdiag", name))
	if err != nil {
		t.Fatal(err)
	}
	slash := filepath.ToSlash(root)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash // Windows drive paths: file:///C:/...
	}
	return []byte(strings.ReplaceAll(string(b), "{{ROOT}}", slash))
}

// doctorWithSARIF runs the production pipeline (project, config, bicep, engine,
// baseline, suppress, report) against a scripted compiler.
func doctorWithSARIF(t *testing.T, sarifFile string, exit int, req DoctorRequest, suppressions string) (int, string, string, *sarifRunner) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "// compiled by the scripted runner\n")
	if suppressions != "" {
		writeFile(t, dir, ".foundry-doctor/suppressions.yaml", suppressions)
	}
	run := &sarifRunner{arm: readTestdata(t, "params/main.json"), sarif: recordedSARIF(t, sarifFile, dir), exit: exit}
	exe := filepath.Join(t.TempDir(), "bicep")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr, out bytes.Buffer
	svc := NewServices(&stderr, Options{
		Getenv:      func(string) string { return "" },
		Bicep:       bicep.DiscoverOptions{Path: exe, Runner: run},
		BicepRunner: run,
		AzdVersion:  func(context.Context) string { return "1.35.0" },
	})
	req.Dir, req.Format = dir, "json"
	code, err := Doctor(context.Background(), svc, req, &out)
	if exit == 0 && err != nil {
		t.Fatalf("doctor: %v\n%s", err, stderr.String())
	}
	return code, out.String(), stderr.String(), run
}

type jsonFinding struct {
	RuleID      string `json:"ruleId"`
	Severity    string `json:"severity"`
	Adapter     string `json:"adapter"`
	Fingerprint string `json:"fingerprint"`
	Evidence    string `json:"evidence"`
	DocsURL     string `json:"docsUrl"`
	Location    struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
	Suppressed *struct{} `json:"suppressed"`
}

func allReportFindings(t *testing.T, out string) []jsonFinding {
	t.Helper()
	var doc struct {
		Findings []jsonFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	return doc.Findings
}

func bicepReportFindings(t *testing.T, out string) []jsonFinding {
	t.Helper()
	var fs []jsonFinding
	for _, f := range allReportFindings(t, out) {
		if strings.HasPrefix(f.RuleID, "bicep/") {
			fs = append(fs, f)
		}
	}
	return fs
}

func TestDoctorReportsCompilerWarningsAsFindings(t *testing.T) {
	code, out, _, run := doctorWithSARIF(t, "warnings.sarif.json", 0, DoctorRequest{}, "")
	// The scripted template trips catalogue rules, so the exit code can be 1;
	// what matters is that no compiler warning is an error finding.
	var catalogueErrors int
	for _, f := range allReportFindings(t, out) {
		if f.Severity == "error" {
			if strings.HasPrefix(f.RuleID, "bicep/") {
				t.Fatalf("compiler warning reported as error: %+v", f)
			}
			catalogueErrors++
		}
	}
	if (catalogueErrors > 0) != (code == 1) {
		t.Fatalf("exit=%d with %d catalogue error findings", code, catalogueErrors)
	}
	fs := bicepReportFindings(t, out)
	byID := map[string]jsonFinding{}
	for _, f := range fs {
		byID[f.RuleID] = f
	}
	for _, id := range []string{"bicep/no-unused-params", "bicep/secure-secrets-in-params", "bicep/no-hardcoded-env-urls"} {
		f, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing from %+v", id, fs)
		}
		if f.Severity != "warning" || f.Adapter != "bicep" || f.Location.File != "infra/main.bicep" || f.Location.Line == 0 {
			t.Errorf("%s = %+v", id, f)
		}
	}
	if byID["bicep/no-hardcoded-env-urls"].DocsURL != "https://aka.ms/bicep/linter-diagnostics#no-hardcoded-env-urls" {
		t.Errorf("docs URL = %q", byID["bicep/no-hardcoded-env-urls"].DocsURL)
	}
	if strings.Contains(byID["bicep/no-hardcoded-env-urls"].Evidence, "aka.ms") {
		t.Error("docs URL must not be repeated in evidence")
	}
	// Offline guarantee: restore is never requested.
	var build []string
	for _, a := range run.called {
		if a[0] == "build" {
			build = a
		}
	}
	if !contains(build, "--no-restore") || !contains(build, "sarif") {
		t.Errorf("build args = %v", build)
	}
}

func TestDoctorWarningsFailAtWarningThreshold(t *testing.T) {
	code, _, _, _ := doctorWithSARIF(t, "warnings.sarif.json", 0, DoctorRequest{FailOn: sdk.SeverityWarning}, "")
	if code != 1 {
		t.Fatalf("--fail-on warning with compiler warnings: exit=%d, want 1", code)
	}
}

func TestDoctorCompileErrorFindingsMakeRunFail(t *testing.T) {
	code, out, stderr, _ := doctorWithSARIF(t, "error.sarif.json", 1, DoctorRequest{Profile: "dev"}, "")
	if code != 1 {
		t.Fatalf("compile error finding must fail the run via findings, exit=%d\n%s", code, stderr)
	}
	var sawErr bool
	for _, f := range bicepReportFindings(t, out) {
		if f.RuleID == "bicep/BCP057" && f.Severity == "error" && f.Location.File == "infra/main.bicep" && f.Location.Line == 9 {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("BCP057 not reported:\n%s", out)
	}
	if !strings.Contains(stderr, "failed to compile") {
		t.Errorf("the model-unavailable warning must remain: %s", stderr)
	}
}

func TestDoctorAdapterFindingsHonourSuppressions(t *testing.T) {
	sup := "version: 1\nentries:\n  - ruleId: bicep/no-hardcoded-env-urls\n    reason: sovereign cloud not used\n    owner: platform\n    expires: 2999-01-01\n"
	_, out, _, _ := doctorWithSARIF(t, "warnings.sarif.json", 0, DoctorRequest{}, sup)
	var n int
	for _, f := range bicepReportFindings(t, out) {
		if f.RuleID == "bicep/no-hardcoded-env-urls" {
			n++
			if f.Suppressed == nil {
				t.Error("finding must be suppressed")
			}
		} else if f.Suppressed != nil {
			t.Errorf("%s must not be suppressed", f.RuleID)
		}
	}
	if n != 1 {
		t.Fatalf("suppressed finding missing: %d", n)
	}
}

func TestDoctorRuleSelectionExcludesAdapterFindings(t *testing.T) {
	_, out, _, _ := doctorWithSARIF(t, "warnings.sarif.json", 0, DoctorRequest{Rules: []string{"FND-CFG-*"}}, "")
	if fs := bicepReportFindings(t, out); len(fs) != 0 {
		t.Fatalf("explicit rule selection must leave compiler findings out: %+v", fs)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

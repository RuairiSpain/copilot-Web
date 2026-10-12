package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fake struct {
	id string
	fn func(r rules.Rule) []rules.Result
}

func (f fake) ID() string   { return f.id }
func (f fake) Version() int { return 1 }
func (f fake) Evaluate(context.Context, *model.Input) ([]rules.Result, error) {
	return f.fn(f), nil
}

func catalogue(t *testing.T) *engine.Catalogue {
	t.Helper()
	pass := func(rules.Rule) []rules.Result { return []rules.Result{rules.Pass()} }
	c, err := engine.NewCatalogue(context.Background(), []rules.Rule{
		fake{"FND-CFG-001", func(r rules.Rule) []rules.Result {
			return []rules.Result{rules.Fail(rules.NewFinding(r, sdk.ResourceRef{Kind: "service", Name: "api"}, "e", ""))}
		}},
		fake{"FND-CFG-002", pass},
		fake{"FND-NET-001", pass}, // needs ARM: skipped on a project without infrastructure
		fake{"FND-ENV-002", func(rules.Rule) []rules.Result { panic("boom") }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testDeps(t *testing.T, dir string) deps {
	t.Helper()
	return deps{
		Dir: dir, Now: func() time.Time { return time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) },
		Getenv: func(string) string { return "" }, IsTTY: func(io.Writer) bool { return false },
		Version: "v0.0.0-test", Commit: "abc", Date: "2026-10-12", Catalogue: catalogue(t),
	}
}

func project(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte("name: sample\nservices:\n  api:\n    host: containerapp\n    project: ./src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exec(t *testing.T, d deps, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = execute(context.Background(), args, &out, &errb, d)
	return code, out.String(), errb.String()
}

func TestExitCodes_0(t *testing.T) {
	d := testDeps(t, project(t))
	if code, out, errs := exec(t, d, "doctor", "--rules", "FND-CFG-002"); code != 0 || !strings.Contains(out, "foundry-doctor v0.0.0-test") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
}

func TestExitCodes_1(t *testing.T) {
	d := testDeps(t, project(t))
	if code, out, errs := exec(t, d, "doctor", "--rules", "FND-CFG-001"); code != 1 || !strings.Contains(out, "FND-CFG-001") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
}

func TestExitCodes_2(t *testing.T) {
	empty, _ := filepath.EvalSymlinks(t.TempDir())
	for name, tc := range map[string]struct {
		dir  string
		args []string
	}{
		"no project":      {empty, []string{"doctor"}},
		"bad profile":     {project(t), []string{"doctor", "--profile", "staging"}},
		"unknown flag":    {project(t), []string{"doctor", "--nope"}},
		"unknown command": {project(t), []string{"frobnicate"}},
		"extra argument":  {project(t), []string{"doctor", "extra"}},
		"explain no arg":  {project(t), []string{"explain"}},
		"explain unknown": {project(t), []string{"explain", "FND-NOPE-999"}},
		"compare one arg": {project(t), []string{"compare", "dev"}},
	} {
		d := testDeps(t, tc.dir)
		if code, _, errs := exec(t, d, tc.args...); code != 2 || errs == "" {
			t.Errorf("%s: exit %d stderr %q", name, code, errs)
		}
	}
}

func TestExitCodes_3(t *testing.T) {
	d := testDeps(t, project(t))
	if code, _, errs := exec(t, d, "doctor", "--rules", "FND-NET-001", "--strict"); code != 3 {
		t.Fatalf("exit %d %s", code, errs)
	}
	if code, _, _ := exec(t, d, "doctor", "--rules", "FND-NET-001"); code != 0 {
		t.Fatalf("without --strict: exit %d", code)
	}
}

func TestExitCodes_4(t *testing.T) {
	d := testDeps(t, project(t))
	if code, _, errs := exec(t, d, "doctor", "--rules", "FND-ENV-002"); code != 4 || !strings.Contains(errs, "FND-ENV-002") {
		t.Fatalf("exit %d %s", code, errs)
	}
	// 4 beats 1.
	if code, _, _ := exec(t, d, "doctor", "--rules", "FND-ENV-002,FND-CFG-001"); code != 4 {
		t.Fatalf("precedence: exit %d", code)
	}
}

func TestFlags_OutNotOutput(t *testing.T) {
	d := testDeps(t, project(t))
	code, _, errs := exec(t, d, "doctor", "--output", "x.json")
	if code != 2 || !strings.Contains(errs, "unknown flag: --output") {
		t.Fatalf("exit %d %q", code, errs)
	}
	out := filepath.Join(d.Dir, "r.json")
	if code, _, errs := exec(t, d, "doctor", "--rules", "FND-CFG-002", "--format", "json", "--out", out); code != 0 {
		t.Fatalf("exit %d %s", code, errs)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedLaterPhaseFlags(t *testing.T) {
	d := testDeps(t, project(t))
	for _, f := range []string{"--preflight", "--runtime", "--security", "--what-if", "--export-tool-config", "--llm-explain"} {
		code, out, errs := exec(t, d, "doctor", f, "--rules", "FND-CFG-002")
		if code != 2 || !strings.Contains(errs, f+": not available in this version") || out != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q", f, code, out, errs)
		}
	}
	// --local is accepted, explicitly or not.
	if code, _, errs := exec(t, d, "doctor", "--local", "--rules", "FND-CFG-002"); code != 0 {
		t.Fatalf("--local: exit %d %s", code, errs)
	}
	// The flags stay in the help text so it is stable.
	_, help, _ := exec(t, d, "doctor", "--help")
	for _, f := range []string{"--preflight", "--runtime", "--security", "--what-if", "--export-tool-config", "--llm-explain"} {
		contains(t, "help", help, f)
	}
}

func contains(t *testing.T, name, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", name, w, got)
		}
	}
}

func TestHelpHasExamples(t *testing.T) {
	d := testDeps(t, project(t))
	for _, cmd := range []string{"doctor", "explain", "compare", "version"} {
		code, out, _ := exec(t, d, cmd, "--help")
		if code != 0 {
			t.Fatalf("%s --help: exit %d", cmd, code)
		}
		contains(t, cmd+" help", out, "Examples:", "foundry-doctor "+cmd)
	}
	code, out, _ := exec(t, d, "--help")
	contains(t, "root help", out, "doctor", "explain", "compare", "version", "Exit codes:", "--debug", "--no-color")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if code, out, _ := exec(t, d, "help", "doctor"); code != 0 || !strings.Contains(out, "Examples:") {
		t.Fatalf("help doctor: %d", code)
	}
	if code, out, _ := exec(t, d); code != 0 || !strings.Contains(out, "Available Commands") {
		t.Fatalf("bare invocation prints help: %d", code)
	}
}

func TestVersion(t *testing.T) {
	d := testDeps(t, project(t))
	want := "foundry-doctor v0.0.0-test (commit abc, built 2026-10-12)\n"
	for _, args := range [][]string{{"--version"}, {"version"}} {
		if code, out, _ := exec(t, d, args...); code != 0 || out != want {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
}

func TestNonInteractive(t *testing.T) {
	// No command reads standard input: a closed or empty stdin must not matter, and nothing prompts.
	d := testDeps(t, project(t))
	done := make(chan int, 1)
	go func() { c, _, _ := exec(t, d, "doctor", "--rules", "FND-CFG-002"); done <- c }()
	select {
	case c := <-done:
		if c != 0 {
			t.Fatalf("exit %d", c)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the command blocked")
	}
}

func TestColor(t *testing.T) {
	d := testDeps(t, project(t))
	d.IsTTY = func(io.Writer) bool { return true }
	args := []string{"doctor", "--rules", "FND-CFG-001"}
	if _, out, _ := exec(t, d, args...); !strings.Contains(out, "\x1b[") {
		t.Fatal("a terminal gets colour")
	}
	if _, out, _ := exec(t, d, append(args, "--no-color")...); strings.Contains(out, "\x1b[") {
		t.Fatal("--no-color ignored")
	}
	d.Getenv = func(k string) string {
		if k == "NO_COLOR" {
			return "1"
		}
		return ""
	}
	if _, out, _ := exec(t, d, args...); strings.Contains(out, "\x1b[") {
		t.Fatal("NO_COLOR ignored")
	}
	d.Getenv, d.IsTTY = nil, nil
	if _, out, _ := exec(t, d, args...); strings.Contains(out, "\x1b[") {
		t.Fatal("no terminal, no colour")
	}
}

func TestOsDeps(t *testing.T) {
	d := osDeps()
	if d.Dir == "" || d.Now == nil || d.Getenv == nil || d.Version == "" {
		t.Fatalf("%+v", d)
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if d.IsTTY(f) {
		t.Fatal("a regular file is not a terminal")
	}
	if d.IsTTY(&bytes.Buffer{}) {
		t.Fatal("a buffer is not a terminal")
	}
}

func TestExplainAndCompareCommands(t *testing.T) {
	d := testDeps(t, project(t))
	if code, out, _ := exec(t, d, "explain", "FND-CFG-001"); code != 0 || !strings.Contains(out, "FND-CFG-001") {
		t.Fatalf("explain: %d", code)
	}
	if code, out, _ := exec(t, d, "explain", "FND-CFG-001", "--format", "markdown"); code != 0 || !strings.HasPrefix(out, "# FND-CFG-001") {
		t.Fatalf("explain markdown: %d", code)
	}
	if err := os.MkdirAll(filepath.Join(d.Dir, ".azure", "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Dir, ".azure", "dev", ".env"), []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// compare reaches the application layer; whatever the comparer says, an unknown environment is exit 2.
	if code, _, _ := exec(t, d, "compare", "dev", "ghost"); code != 2 {
		t.Fatalf("compare unknown: %d", code)
	}
}

func TestRootHasNoStrayBusinessLogic(t *testing.T) {
	// main.go only wires the process; the check is structural: the file stays tiny.
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n > 25 {
		t.Fatalf("main.go has %d lines; keep it to wiring", n)
	}
}

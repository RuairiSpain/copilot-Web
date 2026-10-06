package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

const minimalAzureYAML = "name: demo\nservices:\n  api:\n    host: containerapp\n    project: ./src\n"

func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte(minimalAzureYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exec(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	// Offline: no Azure credentials are present or required.
	t.Setenv("AZURE_CLIENT_SECRET", "")
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	code := run(context.Background(), args, &out, &errb, app.DefaultServices(&errb))
	return code, out.String(), errb.String()
}

func TestExitCodes(t *testing.T) {
	dir := project(t)
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"offline all skipped optional", []string{"doctor", "--local", "--dir", dir}, 0},
		{"strict skip", []string{"doctor", "--local", "--strict", "--dir", dir}, 3},
		{"explicit rule lacking input cannot run", []string{"doctor", "--local", "--rules", "FND-NET-001", "--dir", dir}, 2},
		{"missing azure.yaml", []string{"doctor", "--local", "--dir", t.TempDir()}, 2},
		{"bad format", []string{"doctor", "--format", "xml", "--dir", dir}, 2},
		{"bad severity", []string{"doctor", "--min-severity", "loud", "--dir", dir}, 2},
		{"bad profile", []string{"doctor", "--profile", "staging", "--dir", dir}, 2},
		{"fail-on below min-severity", []string{"doctor", "--min-severity", "error", "--fail-on", "info", "--dir", dir}, 2},
		{"unknown flag", []string{"doctor", "--nope"}, 2},
		{"unknown selector", []string{"doctor", "--rules", "bogus", "--dir", dir}, 2},
		{"missing explicit baseline", []string{"doctor", "--baseline", "nope.json", "--dir", dir}, 2},
		{"explain unknown", []string{"explain", "FND-NOPE-999"}, 2},
		{"explain no arg", []string{"explain"}, 2},
		{"unknown command", []string{"frobnicate"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, _, errs := exec(t, tc.args...); got != tc.want {
				t.Fatalf("exit = %d, want %d (stderr=%q)", got, tc.want, errs)
			}
		})
	}
}

func TestHelpAndFlags(t *testing.T) {
	code, out, _ := exec(t, "doctor", "--help")
	if code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	for _, f := range []string{"--profile", "--local", "--rules", "--min-severity", "--fail-on",
		"--baseline", "--suppressions", "--strict", "--format", "--out"} {
		if !strings.Contains(out, f) {
			t.Errorf("doctor help missing %s", f)
		}
	}
	// -o and -e are reserved by azd and must not be declared as shorthands.
	for _, bad := range []string{" -o,", " -e,", "--environment"} {
		if strings.Contains(out, bad) {
			t.Errorf("doctor help must not declare %q", bad)
		}
	}
	for _, cmd := range []string{"annotate", "explain", "compare"} {
		if c, _, _ := exec(t, cmd, "--help"); c != 0 {
			t.Errorf("%s --help exit = %d", cmd, c)
		}
	}
	if c, explainHelp, _ := exec(t, "explain", "--help"); c != 0 || !strings.Contains(explainHelp, "--llm-explain") {
		t.Fatalf("explain help missing llm flags: exit=%d out=%q", c, explainHelp)
	}
}

func TestOutputFormatsAreDeterministic(t *testing.T) {
	dir := project(t)
	for _, f := range []string{"console", "json", "markdown", "sarif"} {
		t.Run(f, func(t *testing.T) {
			c1, o1, _ := exec(t, "doctor", "--local", "--format", f, "--dir", dir)
			c2, o2, _ := exec(t, "doctor", "--local", "--format", f, "--dir", dir)
			if c1 != 0 || c2 != 0 {
				t.Fatalf("exit %d/%d", c1, c2)
			}
			if o1 != o2 || o1 == "" {
				t.Fatalf("non-deterministic or empty %s output", f)
			}
		})
	}
}

func TestOutFile(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "sub", "r.json")
	code, stdout, _ := exec(t, "doctor", "--local", "--format", "json", "--out", out, "--dir", dir)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty when --out is used, got %q", stdout)
	}
	if b, err := os.ReadFile(out); err != nil || len(b) == 0 {
		t.Fatalf("out file: %v len=%d", err, len(b))
	}
}

func TestExplainRealCatalogue(t *testing.T) {
	code, out, errs := exec(t, "explain", "FND-CFG-001")
	if code != 0 || !strings.Contains(out, "FND-CFG-001") {
		t.Fatalf("exit=%d out=%q err=%q", code, out, errs)
	}
}

func TestExplainLLMFallbackIsNonFatal(t *testing.T) {
	code, out, errs := exec(t, "explain", "FND-CFG-001", "--llm-explain", "--audience", "owner")
	if code != 0 || !strings.Contains(out, "Deterministic rule documentation remains authoritative") {
		t.Fatalf("exit=%d out=%q err=%q", code, out, errs)
	}
}

func TestCompare(t *testing.T) {
	dir := project(t)
	if code, _, errs := exec(t, "compare", "dev", "prod", "--dir", dir); code != 0 {
		t.Fatalf("compare exit=%d err=%q", code, errs)
	}
	if code, _, _ := exec(t, "compare", "dev"); code != 2 {
		t.Fatalf("compare with one arg exit=%d, want 2", code)
	}
}

func TestAnnotate(t *testing.T) {
	dir := project(t)
	out := "review\\sample.review"
	if code, _, errs := exec(t, "annotate", "--out", out, "--dir", dir); code != 0 && code != 1 {
		t.Fatalf("annotate exit=%d err=%q", code, errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "review", "sample.review", "annotations-manifest.json")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := exec(t, "annotate", "--format", "review", "--dir", dir); code != 2 {
		t.Fatalf("annotate without out exit=%d, want 2", code)
	}
	if code, _, errs := exec(t, "annotate", "--format", "github", "--dir", dir); code != 0 || errs != "" {
		t.Fatalf("github annotate exit=%d err=%q", code, errs)
	}
}

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const seed = "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"

// setup creates a catalogue with one proposed rule under a temp dir and makes it the working directory.
func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

func exec(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestExitCodes(t *testing.T) {
	setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed})
	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"no args", nil, 2, "usage"},
		{"help", []string{"--help"}, 0, "usage"},
		{"unknown command", []string{"bogus"}, 2, "unknown command"},
		{"unknown flag", []string{"validate", "--nope"}, 2, "not defined"},
		{"positional argument", []string{"validate", "x"}, 2, "unexpected arguments"},
		{"phase0 on generate", []string{"generate-docs", "--phase0"}, 2, "not valid for"},
		{"check on validate", []string{"validate", "--check"}, 2, "not valid for"},
		{"validate ok", []string{"validate"}, 0, "catalogue valid: 1 rules"},
		{"validate phase0 fails on proposed", []string{"validate", "--phase0"}, 1, "still proposed"},
		{"docs check before generate is stale", []string{"generate-docs", "--check"}, 1, "docs/rule-catalog.md is stale; run: go run ./cmd/rulecatalog generate-docs"},
		{"generate docs", []string{"generate-docs"}, 0, "wrote docs/rule-catalog.md"},
		{"docs check after generate", []string{"generate-docs", "--check"}, 0, ""},
		{"overlap check before generate is stale", []string{"generate-overlap", "--check"}, 1, "generate-overlap"},
		{"generate overlap", []string{"generate-overlap"}, 0, "wrote docs/overlap-analysis.md"},
		{"overlap check after generate", []string{"generate-overlap", "--check"}, 0, ""},
		{"missing catalogue dir is an I/O error", []string{"validate", "--dir", "missing"}, 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, o, e := exec(tc.args...)
			if got != tc.want || !strings.Contains(o+e, tc.msg) {
				t.Fatalf("run(%v) = %d, want %d containing %q\nstdout: %s\nstderr: %s", tc.args, got, tc.want, tc.msg, o, e)
			}
		})
	}
}

func TestMalformedCatalogueIsExit1(t *testing.T) {
	setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nbogus: 1\n"})
	if got, _, e := exec("validate"); got != 1 || !strings.Contains(e, "bogus") {
		t.Fatalf("exit %d, stderr %q; want 1 for invalid content", got, e)
	}
}

func TestAbsoluteDirWorksFromAnywhere(t *testing.T) {
	dir := setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed})
	t.Chdir(t.TempDir())
	if got, o, e := exec("validate", "--dir", filepath.Join(dir, "rules", "catalog")); got != 0 {
		t.Fatalf("exit %d: %s %s", got, o, e)
	}
}

func TestUnwritableOutputIsExit2(t *testing.T) {
	setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed, "blocker": "a file, not a directory"})
	if got, _, e := exec("generate-docs", "--out", "blocker/out.md"); got != 2 {
		t.Fatalf("exit %d, stderr %q; want 2", got, e)
	}
}

func TestUnreadableCheckTargetIsExit2(t *testing.T) {
	setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed})
	if err := os.MkdirAll("docs/rule-catalog.md", 0o755); err != nil { // a directory where a file is expected
		t.Fatal(err)
	}
	if got, _, e := exec("generate-docs", "--check"); got != 2 {
		t.Fatalf("exit %d, stderr %q; want 2 for a read error that is not 'not found'", got, e)
	}
}

func TestGenerateCreatesOutputDirectory(t *testing.T) {
	setup(t, map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": seed})
	if got, _, e := exec("generate-docs", "--out", "new/dir/catalog.md"); got != 0 {
		t.Fatalf("exit %d: %s", got, e)
	}
	if _, err := os.Stat("new/dir/catalog.md"); err != nil {
		t.Fatal(err)
	}
}

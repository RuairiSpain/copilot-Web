package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	dir := t.TempDir()
	cat := filepath.Join(dir, "rules", "catalog", "cfg")
	if err := os.MkdirAll(cat, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"
	if err := os.WriteFile(filepath.Join(cat, "FND-CFG-001.yaml"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		args []string
		want int
	}{
		{nil, 2},
		{[]string{"bogus"}, 2},
		{[]string{"validate"}, 0},
		{[]string{"validate", "--phase0"}, 1},
		{[]string{"generate-docs", "--check"}, 1}, // not generated yet
		{[]string{"generate-docs"}, 0},
		{[]string{"generate-docs", "--check"}, 0},
		{[]string{"validate", "--dir", "missing"}, 2},
	}
	for _, tc := range tests {
		var out, errb bytes.Buffer
		if got := run(tc.args, &out, &errb); got != tc.want {
			t.Errorf("run(%v) = %d, want %d (stderr: %s)", tc.args, got, tc.want, strings.TrimSpace(errb.String()))
		}
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotate(t *testing.T) {
	dir := project(t)
	out := "review\\sample.review"
	if code, _, errs := exec(t, "annotate", "--out", out, "--dir", dir); code != 0 && code != 1 {
		t.Fatalf("annotate exit=%d err=%q", code, errs)
	}
	review := filepath.Join(dir, "review", "sample.review")
	if _, err := os.Stat(filepath.Join(review, "annotations-manifest.json")); err != nil {
		t.Fatal(err)
	}
	// azd discovers a project by the file name azure.yaml; the copy is renamed
	// and always carries the do-not-deploy banner.
	if _, err := os.Stat(filepath.Join(review, "azure.yaml")); !os.IsNotExist(err) {
		t.Fatalf("review copy must not contain azure.yaml: %v", err)
	}
	copyData, err := os.ReadFile(filepath.Join(review, "azure.review.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(copyData), "# Foundry Doctor review copy (.review). Do not deploy this file.\n") {
		t.Fatalf("review copy banner missing:\n%s", copyData)
	}
	orig, err := os.ReadFile(filepath.Join(dir, "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(orig) != minimalAzureYAML {
		t.Fatalf("original azure.yaml changed:\n%s", orig)
	}
	// A rerun replaces the tool-owned copy.
	if code, _, errs := exec(t, "annotate", "--out", out, "--dir", dir); code != 0 && code != 1 {
		t.Fatalf("annotate rerun exit=%d err=%q", code, errs)
	}
	if code, _, _ := exec(t, "annotate", "--format", "review", "--dir", dir); code != 2 {
		t.Fatalf("annotate without out exit=%d, want 2", code)
	}
	// The fixture has no inline-eligible location, so empty GitHub output is
	// correct; every line that is written must be a workflow command. Stderr is
	// not asserted: it carries host-dependent notes (azd/bicep detection).
	code, ghOut, errs := exec(t, "annotate", "--format", "github", "--dir", dir)
	if code != 0 {
		t.Fatalf("github annotate exit=%d err=%q", code, errs)
	}
	for _, line := range strings.Split(strings.TrimSpace(ghOut), "\n") {
		if line != "" && !strings.HasPrefix(line, "::") {
			t.Fatalf("github output has a non-command line %q", line)
		}
	}
	if code, out, errs := exec(t, "annotate", "--format", "sarif", "--dir", dir); code != 0 && code != 1 {
		t.Fatalf("sarif annotate exit=%d err=%q", code, errs)
	} else {
		var v map[string]any
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("invalid sarif json: %v\n%s", err, out)
		}
		if v["version"] != "2.1.0" {
			t.Fatalf("sarif version = %v", v["version"])
		}
	}
	if code, _, _ := exec(t, "annotate", "--format", "sarif", "--out", "azure.yaml", "--dir", dir); code != 2 {
		t.Fatalf("sarif --out onto azure.yaml exit=%d, want 2", code)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "azure.yaml")); string(after) != minimalAzureYAML {
		t.Fatalf("azure.yaml was overwritten:\n%s", after)
	}
}

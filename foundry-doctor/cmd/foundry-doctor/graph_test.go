package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

func TestGraphHelpListsFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--help"}, &out, &errb, noBicep(io.Discard)); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"--source", "--deployed", "--combined", "--findings-report", "--subscription", "--resource-group", "--redact-ids", "read-only Azure inventory"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestGraphValidation(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--deployed", "--dir", sample("good")}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
	if code := run(context.Background(), []string{"graph", "--source", "--deployed", "--dir", sample("good")}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
}

// TestGraphSampleAzureYAMLOnly keeps samples/graph/azure-yaml-only.mmd
// reproducible: it is the CLI's source-mode Mermaid output for
// samples/azure-yaml-only.
func TestGraphSampleAzureYAMLOnly(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--source", "--dir", sample("azure-yaml-only"), "--format", "mermaid"}, &out, &errb, noBicep(io.Discard)); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "samples", "graph", "azure-yaml-only.mmd"))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("samples/graph/azure-yaml-only.mmd drifted from the CLI output\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGraphDiffFlagConflicts(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--source", "--diff-base", "a.json", "--diff-target", "b.json"}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
	if code := run(context.Background(), []string{"graph", "--diff-base", "a.json"}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
}

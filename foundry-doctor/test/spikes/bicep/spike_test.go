//go:build spike

// Package bicepspike runs the ADR-002 Bicep CLI spike against the fixtures in
// this directory. It is skipped unless built with -tags spike and BICEP_PATH
// points at a bicep executable. It never parses Bicep source; it only runs the
// compiler and inspects ARM JSON and diagnostics.
//
//	BICEP_PATH=/path/to/bicep go test -tags spike ./test/spikes/bicep/...
package bicepspike

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func bicep(t *testing.T, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	path := os.Getenv("BICEP_PATH")
	if path == "" {
		t.Skip("BICEP_PATH not set")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("BICEP_PATH must be an absolute path, got %q", path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// Minimal environment: the compiler needs no Azure credentials, so none are passed through.
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1"}
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		exit = ee.ExitCode()
	default:
		t.Fatalf("cannot run bicep: %v", err)
	}
	return o.String(), e.String(), exit
}

var diagRe = regexp.MustCompile(`(?m)^(.+)\((\d+),(\d+)\) : (Error|Warning|Info) ([A-Za-z0-9-]+): `)

func TestVersion(t *testing.T) {
	out, _, code := bicep(t, "--version")
	if code != 0 || !strings.Contains(out, "Bicep CLI version") {
		t.Fatalf("unexpected version output %q (exit %d)", out, code)
	}
	t.Log(strings.TrimSpace(out))
}

func TestBuildMainARMShape(t *testing.T) {
	out, _, code := bicep(t, "build", "main.bicep", "--stdout")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var arm map[string]any
	if err := json.Unmarshal([]byte(out), &arm); err != nil {
		t.Fatal(err)
	}
	res, ok := arm["resources"].([]any)
	if !ok {
		t.Fatalf("ARM output has no resources array: %v", arm["resources"])
	}
	var sawCond, sawCopy, sawModule bool
	for _, r := range res {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("resource is not an object: %v", r)
		}
		if _, ok := m["condition"]; ok {
			sawCond = true
		}
		if _, ok := m["copy"]; ok {
			sawCopy = true
		}
		if m["type"] == "Microsoft.Resources/deployments" {
			sawModule = true
		}
		for _, k := range []string{"sourceMap", "line", "range"} {
			if _, ok := m[k]; ok {
				t.Errorf("unexpected source location key %q in ARM resource", k)
			}
		}
	}
	if !sawCond || !sawCopy || !sawModule {
		t.Errorf("cond=%v copy=%v module=%v", sawCond, sawCopy, sawModule)
	}
	if _, ok := arm["languageVersion"]; ok {
		t.Error("default output should use the array resource form (no languageVersion)")
	}
	if strings.Contains(out, "sourceMap") {
		t.Error("ARM output unexpectedly carries a source map")
	}
}

func TestSymbolicNameCodegenNeedsExperimentalFlag(t *testing.T) {
	// Without bicepconfig experimental flags resources are an array (no symbolic names).
	out, _, _ := bicep(t, "build", "main.bicep", "--stdout")
	var arm struct {
		Resources json.RawMessage `json:"resources"`
	}
	_ = json.Unmarshal([]byte(out), &arm)
	if !bytes.HasPrefix(bytes.TrimSpace(arm.Resources), []byte("[")) {
		t.Errorf("expected resources array, got %.20s", arm.Resources)
	}
}

func TestDiagnostics(t *testing.T) {
	cases := []struct {
		file string
		exit int
		code string
		line string
	}{
		{"error.bicep", 1, "BCP057", "11"},
		{"error.bicep", 1, "BCP037", "8"},
		{"lint-warning.bicep", 0, "no-unused-params", "1"},
		{"lint-warning.bicep", 0, "no-unused-vars", "2"},
		{"secure-output.bicep", 0, "outputs-should-not-contain-secrets", "8"},
		{"secure-output.bicep", 0, "outputs-should-not-contain-secrets", "11"},
	}
	for _, c := range cases {
		t.Run(c.file+"/"+c.code+":"+c.line, func(t *testing.T) {
			_, stderr, code := bicep(t, "build", c.file, "--stdout")
			if code != c.exit {
				t.Errorf("exit = %d, want %d", code, c.exit)
			}
			found := false
			for _, m := range diagRe.FindAllStringSubmatch(stderr, -1) {
				if m[5] == c.code && m[2] == c.line {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s at line %s in:\n%s", c.code, c.line, stderr)
			}
		})
	}
}

func TestLintSARIF(t *testing.T) {
	out, _, code := bicep(t, "lint", "error.bicep", "--diagnostics-format", "sarif")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	var s struct {
		Runs []struct {
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					Physical struct {
						Region struct {
							StartLine  int `json:"startLine"`
							CharOffset int `json:"charOffset"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("SARIF not on stdout for lint: %v", err)
	}
	if len(s.Runs) != 1 || len(s.Runs[0].Results) < 2 {
		t.Fatalf("unexpected SARIF: %s", out)
	}
	for _, r := range s.Runs[0].Results {
		if r.RuleID != "BCP057" {
			continue
		}
		if len(r.Locations) == 0 || r.Level != "error" || r.Locations[0].Physical.Region.StartLine != 11 {
			t.Errorf("BCP057 = %+v", r)
		}
	}
}

func TestBuildStdoutWithSARIFSplitsStreams(t *testing.T) {
	out, errOut, _ := bicep(t, "build", "secure-output.bicep", "--stdout", "--diagnostics-format", "sarif")
	if !json.Valid([]byte(out)) || !strings.Contains(out, "deploymentTemplate.json") {
		t.Error("expected ARM JSON on stdout")
	}
	if !strings.Contains(errOut, `"version": "2.1.0"`) {
		t.Error("expected SARIF on stderr")
	}
}

func TestBuildParams(t *testing.T) {
	out, _, code := bicep(t, "build-params", "main.bicepparam", "--stdout")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r map[string]string
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"parametersJson", "templateJson"} {
		if r[k] == "" {
			t.Errorf("missing %s", k)
		}
	}
}

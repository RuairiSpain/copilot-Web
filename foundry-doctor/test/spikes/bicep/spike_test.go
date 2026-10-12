//go:build spike

// Package bicepspike runs the ADR-002 Bicep CLI spike against the fixtures in
// this directory. It only runs when built with -tags spike and BICEP_PATH
// points at the pinned bicep executable. It never parses Bicep source; it only
// runs the compiler and inspects ARM JSON and diagnostics.
//
//	BICEP_PATH=/path/to/bicep go test -tags spike ./test/spikes/bicep/...
package bicepspike

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func bicep(t *testing.T, args ...string) commandResult {
	t.Helper()
	path := os.Getenv("BICEP_PATH")
	if path == "" {
		t.Fatalf("BICEP_PATH is required for the opt-in spike (missing required CLI is exit code 2)")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("BICEP_PATH must be an absolute path, got %q", path)
	}
	// Every compile/lint operation is an offline validation. This prevents an
	// external module reference added to a fixture from silently reaching a registry.
	if len(args) > 0 && args[0] != "--version" {
		args = append(args, "--no-restore")
	}
	// Use the same bounded runner exercised by the ordinary process tests.
	// The explicit environment does not pass Azure credentials or proxies.
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1",
	}
	if runtime.GOOS == "windows" {
		env = append(env,
			"SystemRoot="+os.Getenv("SystemRoot"),
			"USERPROFILE="+home,
			"DOTNET_BUNDLE_EXTRACT_BASE_DIR="+filepath.Join(home, ".net"),
		)
	}
	return (commandRunner{
		executable: path,
		env:        env,
		timeout:    60 * time.Second,
		maxOutput:  maxSpikeOutput,
	}).run(context.Background(), args...)
}

func commandFailure(t *testing.T, operation string, result commandResult) {
	t.Helper()
	t.Fatalf("%s failed (exit %d); stderr: %s; stdout: %s",
		operation,
		result.exitCode,
		safeCompilerSummary(result.stderr, maxLogSummary),
		safeCompilerSummary(result.stdout, maxLogSummary))
}

func TestVersion(t *testing.T) {
	result := bicep(t, "--version")
	if result.exitCode != 0 || result.stdoutTruncated {
		commandFailure(t, "Bicep version detection", result)
	}
	version, err := parseBicepVersion(string(result.stdout))
	if err != nil || version != (semVersion{major: 0, minor: 48, patch: 1}) {
		t.Fatalf("spike requires official Bicep CLI 0.48.1; output: %s",
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	t.Log(safeCompilerSummary(result.stdout, maxLogSummary))
}

func TestBuildMainARMShape(t *testing.T) {
	result := bicep(t, "build", "main.bicep", "--stdout")
	if result.exitCode != 0 {
		commandFailure(t, "build main.bicep", result)
	}
	arm, err := parseARMResult(result.stdout, result.stdoutTruncated)
	if err != nil {
		t.Fatalf("build main.bicep protocol failure: %v; output: %s",
			err, safeCompilerSummary(result.stdout, maxLogSummary))
	}
	res, ok := arm["resources"].([]any)
	if !ok {
		t.Fatal("ARM output has no resources array")
	}
	var sawCond, sawCopy, sawModule bool
	for _, r := range res {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatal("ARM resource is not an object")
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
	if strings.Contains(string(result.stdout), "sourceMap") {
		t.Error("ARM output unexpectedly carries a source map")
	}
}

func TestSymbolicNameCodegenNeedsExperimentalFlag(t *testing.T) {
	// Without bicepconfig experimental flags resources are an array (no symbolic names).
	result := bicep(t, "build", "main.bicep", "--stdout")
	if result.exitCode != 0 {
		commandFailure(t, "build main.bicep", result)
	}
	arm, err := parseARMResult(result.stdout, result.stdoutTruncated)
	if err != nil {
		t.Fatalf("ARM protocol failure: %v; output: %s", err,
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	if _, ok := arm["resources"].([]any); !ok {
		t.Error("expected resources array")
	}
}

func TestDiagnostics(t *testing.T) {
	cases := []struct {
		file string
		exit int
		code string
		line int
	}{
		{"error.bicep", 1, "BCP057", 11},
		{"error.bicep", 1, "BCP037", 8},
		{"lint-warning.bicep", 0, "no-unused-params", 1},
		{"lint-warning.bicep", 0, "no-unused-vars", 2},
		{"secure-output.bicep", 0, "outputs-should-not-contain-secrets", 8},
		{"secure-output.bicep", 0, "outputs-should-not-contain-secrets", 11},
	}
	for _, c := range cases {
		t.Run(c.file+"/"+c.code+":"+strconv.Itoa(c.line), func(t *testing.T) {
			result := bicep(t, "build", c.file, "--stdout", "--diagnostics-format", "sarif")
			if result.exitCode != c.exit {
				commandFailure(t, "build diagnostics", result)
			}
			findings, err := parseSARIFResult(result.stderr, result.stderrTruncated)
			if err != nil {
				t.Fatalf("SARIF protocol failure: %v; diagnostics: %s", err,
					safeCompilerSummary(result.stderr, maxLogSummary))
			}
			found := false
			for _, finding := range findings {
				if finding.RuleID == c.code && finding.Line == c.line &&
					finding.Confidence == "exact" {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s at proven line %d in %d normalized findings",
					c.code, c.line, len(findings))
			}
		})
	}
}

func TestLintSARIF(t *testing.T) {
	result := bicep(t, "lint", "error.bicep", "--diagnostics-format", "sarif")
	if result.exitCode != 1 {
		commandFailure(t, "lint error.bicep", result)
	}
	findings, err := parseSARIFResult(result.stdout, result.stdoutTruncated)
	if err != nil {
		t.Fatalf("SARIF protocol failure: %v; diagnostics: %s", err,
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	found := false
	for _, finding := range findings {
		if finding.RuleID != "BCP057" {
			continue
		}
		if finding.Severity != "error" || finding.Line != 11 || finding.Confidence != "exact" {
			t.Errorf("BCP057 metadata = severity %q, line %d, confidence %q",
				finding.Severity, finding.Line, finding.Confidence)
		}
		found = true
	}
	if !found {
		t.Errorf("BCP057 not found in %d normalized findings", len(findings))
	}
}

func TestBuildStdoutWithSARIFSplitsStreams(t *testing.T) {
	result := bicep(t, "build", "secure-output.bicep", "--stdout", "--diagnostics-format", "sarif")
	if result.exitCode != 0 {
		commandFailure(t, "build secure-output.bicep", result)
	}
	if _, err := parseARMResult(result.stdout, result.stdoutTruncated); err != nil {
		t.Fatalf("ARM protocol failure: %v; output: %s", err,
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	findings, err := parseSARIFResult(result.stderr, result.stderrTruncated)
	if err != nil || len(findings) == 0 {
		t.Fatalf("SARIF protocol failure: %v; diagnostics: %s", err,
			safeCompilerSummary(result.stderr, maxLogSummary))
	}
}

func TestBuildParams(t *testing.T) {
	result := bicep(t, "build-params", "main.bicepparam", "--stdout")
	if result.exitCode != 0 {
		commandFailure(t, "build-params main.bicepparam", result)
	}
	if result.stdoutTruncated {
		t.Fatalf("build-params protocol failure: output truncated; output: %s",
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	var r map[string]string
	if err := json.Unmarshal(result.stdout, &r); err != nil {
		t.Fatalf("build-params protocol failure: %v; output: %s", err,
			safeCompilerSummary(result.stdout, maxLogSummary))
	}
	for _, k := range []string{"parametersJson", "templateJson"} {
		if r[k] == "" {
			t.Errorf("missing %s", k)
		}
	}
}

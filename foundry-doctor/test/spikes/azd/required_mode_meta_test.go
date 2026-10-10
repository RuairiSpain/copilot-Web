//go:build spike

package azdspike

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequiredModeHasNoDirectSkipSites prevents a future dependency check from
// bypassing spikeDependencyUnavailable. This is a static complement to the
// external run: in REQUIRE_SPIKE_DEPS=1 that single helper calls Fatal before
// its Skip statement, so no dependency-related subtest can become SKIP.
func TestRequiredModeHasNoDirectSkipSites(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Clean(entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, finding := range directSkipSites(path, source) {
			t.Error(finding)
		}
	}
}

func directSkipSites(path string, source []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return []string{fmt.Sprintf("parse %s: %v", path, err)}
	}
	var findings []string
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Skip" && selector.Sel.Name != "Skipf" && selector.Sel.Name != "SkipNow") {
				return true
			}
			if function.Name.Name != "spikeDependencyUnavailable" {
				findings = append(findings, fmt.Sprintf("%s:%d calls %s directly; route dependency skips through spikeDependencyUnavailable",
					path, fset.Position(call.Pos()).Line, selector.Sel.Name))
			}
			return true
		})
	}
	return findings
}

// This deliberately broken source proves that the meta-test is not vacuous:
// introducing a direct skip is detected before it can silently weaken required
// mode.
func TestRequiredModeMetaTestRejectsDirectSkip(t *testing.T) {
	broken := []byte(`package azdspike
import "testing"
func TestBroken(t *testing.T) { t.Skip("missing dependency") }
`)
	findings := directSkipSites("deliberately_broken_test.go", broken)
	if len(findings) != 1 || !strings.Contains(findings[0], "calls Skip directly") {
		t.Fatalf("deliberately broken input was not rejected: %v", findings)
	}
}

// TestRequiredModeTurnsMissingDependenciesIntoFailure exercises the behavior
// through `go test`, rather than merely inspecting the helper's source.
func TestRequiredModeTurnsMissingDependenciesIntoFailure(t *testing.T) {
	if os.Getenv("FOUNDRY_DOCTOR_REQUIRED_MODE_PROBE") == "1" {
		spikeDependencyUnavailable(t, "deliberately unavailable dependency")
		return
	}
	cmd := exec.Command("go", "test", "-tags", "spike", "-run",
		"^TestRequiredModeTurnsMissingDependenciesIntoFailure$", ".")
	cmd.Env = append(os.Environ(),
		"FOUNDRY_DOCTOR_REQUIRED_MODE_PROBE=1",
		"REQUIRE_SPIKE_DEPS=1",
		"AZURE_DEV_DIR=")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("REQUIRE_SPIKE_DEPS=1 converted a missing dependency into success:\n%s", output)
	}
	text := string(output)
	if strings.Contains(text, "--- SKIP:") || !strings.Contains(text, "--- FAIL:") {
		t.Fatalf("required dependency must be FAIL, never SKIP:\n%s", output)
	}
}

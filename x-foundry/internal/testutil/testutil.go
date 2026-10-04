// Package testutil builds azure.yaml documents for tests from small YAML overrides.
package testutil

import (
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
)

// Base is the smallest valid x-foundry section.
const Base = `
topology: {mode: standalone}
security: {roles: {admins: [Admins]}}
projects: [{name: finance}]
`

// Private is a security block with private networking.
const Private = `security: {network: {mode: private}, roles: {admins: [a]}}`

// Public is a security block with public networking.
const Public = `security: {network: {mode: public}, roles: {admins: [a]}}`

// Doc returns an azure.yaml mapping: Base with each override's top-level keys replacing
// the base keys.
func Doc(t testing.TB, overrides ...string) map[string]any {
	t.Helper()
	body := mustMap(t, Base)
	for _, o := range overrides {
		for k, v := range mustMap(t, o) {
			body[k] = v
		}
	}
	return map[string]any{"name": "test", "x-foundry": body}
}

// HubDoc is Doc with a hub-spoke topology and a minimal hub, then the overrides.
func HubDoc(t testing.TB, overrides ...string) map[string]any {
	t.Helper()
	return Doc(t, append([]string{"topology: {mode: hub-spoke}\nhub: {name: shared}"}, overrides...)...)
}

func mustMap(t testing.TB, text string) map[string]any {
	t.Helper()
	v, err := parser.LoadYAML(text)
	if err != nil {
		t.Fatalf("bad test YAML: %v\n%s", err, text)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("test YAML is not a mapping:\n%s", text)
	}
	return m
}

// Analyse runs the whole pipeline on a mapping.
func Analyse(t testing.TB, doc map[string]any) plan.Analysis {
	t.Helper()
	a, err := plan.AnalyseMapping(doc, "<test>")
	if err != nil {
		t.Fatalf("unexpected internal error: %v", err)
	}
	return a
}

// Run builds Doc(overrides...) and analyses it.
func Run(t testing.TB, overrides ...string) plan.Analysis { return Analyse(t, Doc(t, overrides...)) }

// RunHub builds HubDoc(overrides...) and analyses it.
func RunHub(t testing.TB, overrides ...string) plan.Analysis {
	return Analyse(t, HubDoc(t, overrides...))
}

// MustPlan analyses and fails the test unless a plan is produced.
func MustPlan(t testing.TB, overrides ...string) *plan.Plan {
	t.Helper()
	return MustOK(t, Run(t, overrides...))
}

// MustOK fails the test unless a has a plan.
func MustOK(t testing.TB, a plan.Analysis) *plan.Plan {
	t.Helper()
	if !a.OK() {
		t.Fatalf("expected a plan, got:\n%s", Lines(a.Diagnostics))
	}
	return a.Plan
}

// Lines renders diagnostics one per line.
func Lines(ds []diag.Diagnostic) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString("  " + d.String() + "\n")
	}
	return b.String()
}

// Codes returns the distinct diagnostic codes with the given severity (all when empty).
func Codes(a plan.Analysis, severity diag.Severity) map[string]bool {
	out := map[string]bool{}
	for _, d := range a.Diagnostics {
		if severity == "" || d.Severity == severity {
			out[d.Code] = true
		}
	}
	return out
}

// Expect asserts a diagnostic with the code (and, when non-empty, path and message parts).
func Expect(t testing.TB, a plan.Analysis, code, path, message string) {
	t.Helper()
	var found []diag.Diagnostic
	for _, d := range a.Diagnostics {
		if d.Code == code {
			found = append(found, d)
		}
	}
	if len(found) == 0 {
		t.Fatalf("expected %s, got:\n%s", code, Lines(a.Diagnostics))
	}
	for _, d := range found {
		if strings.Contains(d.Path, path) && strings.Contains(d.Message, message) {
			return
		}
	}
	t.Fatalf("no %s matches path %q and message %q:\n%s", code, path, message, Lines(found))
}

// ExpectCase runs Run(overrides...) and asserts the diagnostic.
func ExpectCase(t testing.TB, code, path, message string, overrides ...string) {
	t.Helper()
	Expect(t, Run(t, overrides...), code, path, message)
}

// OnlyCodes asserts the exact set of codes reported (any severity).
func OnlyCodes(t testing.TB, a plan.Analysis, want ...string) {
	t.Helper()
	got := Codes(a, "")
	if len(got) != len(want) {
		t.Fatalf("codes = %v, want %v:\n%s", got, want, Lines(a.Diagnostics))
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("codes = %v, want %v:\n%s", got, want, Lines(a.Diagnostics))
		}
	}
}

// OK asserts a plan with no diagnostics at all.
func OK(t testing.TB, a plan.Analysis) {
	t.Helper()
	if !a.OK() || len(a.Diagnostics) > 0 {
		t.Fatalf("expected a clean plan, got:\n%s", Lines(a.Diagnostics))
	}
}

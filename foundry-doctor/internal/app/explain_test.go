package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func explain(t *testing.T, o app.ExplainOptions) result {
	t.Helper()
	var out, errb bytes.Buffer
	err := app.Explain(context.Background(), o, &out, &errb)
	return result{code: app.CodeOf(err), stdout: out.String(), stderr: errb.String()}
}

func TestExplain_Rule(t *testing.T) {
	cat := catalogue(t)
	r := explain(t, app.ExplainOptions{RuleID: "FND-CFG-001", Catalogue: cat})
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
	contains(t, "console", r.stdout,
		"FND-CFG-001  ", "Version: 1", "Status:", "Severity: foundry-dev=error, foundry-test=error, foundry-prod=error",
		"Rationale:", "Evidence:", "Remediation:", "Fix:", "Docs: https://", "Last verified: 20", "Owner:",
		"Compatible azd: >=1.34.2", "Compatible extension azure.ai.agents", "Source 1: https://", "Notes:")
	// A case-insensitive ID is accepted.
	if r2 := explain(t, app.ExplainOptions{RuleID: " fnd-cfg-001 ", Catalogue: cat}); r2.stdout != r.stdout {
		t.Fatal("ID lookup must ignore case and blanks")
	}
	md := explain(t, app.ExplainOptions{RuleID: "FND-SEC-001", Format: "markdown", Catalogue: cat})
	contains(t, "markdown", md.stdout, "# FND-SEC-001", "- **Severity**: foundry-dev=warning", "**Remediation**", "```yaml")
	contains(t, "build note", md.stdout, "no Go implementation")
}

func TestExplain_PerProfileSeverity(t *testing.T) {
	r := explain(t, app.ExplainOptions{RuleID: "FND-NET-001", Catalogue: catalogue(t)})
	contains(t, "severity", r.stdout, "foundry-dev=info, foundry-test=warning, foundry-prod=error")
}

func TestExplain_SystemAndBicepIDs(t *testing.T) {
	r := explain(t, app.ExplainOptions{RuleID: "FND-SYS-SUPPRESSION-EXPIRED", Catalogue: catalogue(t)})
	contains(t, "system", r.stdout, "engine diagnostic", "error in every profile", "never baselined")
	r = explain(t, app.ExplainOptions{RuleID: "bicep/BCP035", Catalogue: catalogue(t), Format: "markdown"})
	contains(t, "bicep", r.stdout, "# bicep/BCP035", "Bicep compiler diagnostic", "Docs")
}

func TestExplain_Errors(t *testing.T) {
	cat := catalogue(t)
	for _, tc := range []struct {
		id, format, msg string
	}{
		{"FND-NOPE-123", "", "unknown rule"},
		{"FND-SYS-NOTLISTED", "", "unknown rule"},
		{"", "", "unknown rule"},
		{"FND-CFG-001", "json", "not supported by explain"},
	} {
		r := explain(t, app.ExplainOptions{RuleID: tc.id, Format: tc.format, Catalogue: cat})
		if r.code != 2 || !strings.Contains(r.stderr, tc.msg) || r.stdout != "" {
			t.Errorf("%+v: exit %d stderr %q", tc, r.code, r.stderr)
		}
	}
}

func TestExplain_DefaultCatalogue(t *testing.T) {
	if r := explain(t, app.ExplainOptions{RuleID: "FND-CFG-001"}); r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.stderr)
	}
}

// ---- compare ----

func compareRun(t *testing.T, o app.CompareOptions) result {
	t.Helper()
	var out, errb bytes.Buffer
	err := app.Compare(context.Background(), o, &out, &errb)
	return result{code: app.CodeOf(err), stdout: out.String(), stderr: errb.String()}
}

func fakeComparer(l, r model.Environment) (compare.Diff, error) {
	d := compare.Diff{Left: l.Name, Right: r.Name}
	for _, k := range l.Keys() {
		kind := compare.OnlyLeft
		if r.Has(k) {
			lv, _ := l.Get(k)
			rv, _ := r.Get(k)
			kind = compare.Same
			if lv.Reveal() != rv.Reveal() {
				kind = compare.Different
			}
		}
		d.Keys = append(d.Keys, compare.KeyDiff{Key: k, Kind: kind})
	}
	return d, nil
}

func TestCompare(t *testing.T) {
	secret := canary("key")
	dir := project(t, map[string]string{
		".azure/dev/.env":  "SAME=1\nDIFF=" + secret + "\nONLY_DEV=x\n",
		".azure/prod/.env": "SAME=1\nDIFF=other-" + secret + "\n",
	})
	cmp := func(_ context.Context, l, r model.Environment) (compare.Diff, error) { return fakeComparer(l, r) }
	for _, format := range []string{"", "console", "json", "markdown"} {
		r := compareRun(t, app.CompareOptions{Dir: dir, Left: "dev", Right: "prod", Format: format, Comparer: cmp})
		if r.code != 0 {
			t.Fatalf("%q: exit %d %s", format, r.code, r.stderr)
		}
		contains(t, format, r.stdout, "DIFF", "only-left", "SAME")
		notContains(t, format, r.stdout, secret, "other-", "fp-left", "fp-right")
		if format == "json" {
			var doc map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc["left"] != "dev" {
				t.Fatalf("json: %v %s", err, r.stdout)
			}
		}
	}
	contains(t, "console", compareRun(t, app.CompareOptions{Dir: dir, Left: "dev", Right: "prod", Comparer: cmp}).stdout, "different", "only-left")
}

func TestCompare_Errors(t *testing.T) {
	dir := project(t, map[string]string{".azure/dev/.env": "A=1\n"})
	cmp := func(_ context.Context, l, r model.Environment) (compare.Diff, error) { return fakeComparer(l, r) }
	for name, tc := range map[string]struct {
		o    app.CompareOptions
		code int
	}{
		"missing right": {app.CompareOptions{Dir: dir, Left: "dev", Right: "ghost", Comparer: cmp}, 2},
		"missing left":  {app.CompareOptions{Dir: dir, Left: "ghost", Right: "dev", Comparer: cmp}, 2},
		"unsafe name":   {app.CompareOptions{Dir: dir, Left: "../dev", Right: "dev", Comparer: cmp}, 2},
		"bad format":    {app.CompareOptions{Dir: dir, Left: "dev", Right: "dev", Format: "sarif", Comparer: cmp}, 2},
		"no project":    {app.CompareOptions{Dir: t.TempDir(), Left: "dev", Right: "dev", Comparer: cmp}, 2},
		"comparer fails": {app.CompareOptions{Dir: dir, Left: "dev", Right: "dev", Comparer: func(context.Context, model.Environment, model.Environment) (compare.Diff, error) {
			return compare.Diff{}, errors.New("boom")
		}}, 4},
	} {
		if r := compareRun(t, tc.o); r.code != tc.code || r.stdout != "" || r.stderr == "" {
			t.Errorf("%s: exit %d stdout %q stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	// The real comparer is the default.
	r := compareRun(t, app.CompareOptions{Dir: dir, Left: "dev", Right: "dev"})
	if r.code != 0 {
		t.Fatalf("default comparer: exit %d, stderr %q", r.code, r.stderr)
	}
}

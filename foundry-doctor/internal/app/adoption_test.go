package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func ids(rep sdk.Report) []string {
	var out []string
	for _, f := range rep.Findings {
		out = append(out, f.RuleID)
	}
	return out
}

func TestBaseline_HidesOnlyExactFingerprints(t *testing.T) {
	cat := catalogue(t, failing("FND-CFG-001", "first"))
	dir := project(t, map[string]string{})
	o := base(dir, cat, "FND-CFG-001")
	o.Formats = []string{"json"}

	// Without a baseline: exit 1.
	if r := run(t, o); r.code != 1 {
		t.Fatalf("exit %d", r.code)
	}
	// Write the baseline: the findings are accepted for this run.
	o.WriteBaseline = ".foundry-doctor/baseline.yaml"
	r := run(t, o)
	if r.code != 0 {
		t.Fatalf("write-baseline run: exit %d\n%s", r.code, r.stderr)
	}
	if rep := decode(t, r.stdout); rep.Summary.Baselined != 1 {
		t.Fatalf("summary %+v", rep.Summary)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".foundry-doctor", "baseline.yaml"))
	if err != nil || !strings.Contains(string(b), "fp1:") || !strings.Contains(string(b), "firstSeen: \"2026-10-12\"") && !strings.Contains(string(b), "firstSeen: 2026-10-12") {
		t.Fatalf("baseline file: %v\n%s", err, b)
	}
	// The default path is picked up without a flag.
	o.WriteBaseline = ""
	if r := run(t, o); r.code != 0 {
		t.Fatalf("default baseline: exit %d", r.code)
	}
	// A second finding of another rule is new: exit 1, only that one is live.
	cat2 := catalogue(t, failing("FND-CFG-001", "first"), failing("FND-CFG-002", "second"))
	o2 := base(dir, cat2, "FND-CFG-001,FND-CFG-002")
	o2.Formats, o2.Profile = []string{"json"}, "prod"
	r = run(t, o2)
	if r.code != 1 {
		t.Fatalf("new finding must give exit 1, got %d", r.code)
	}
	rep := decode(t, r.stdout)
	live := 0
	for _, f := range rep.Findings {
		if !f.Baselined && f.Suppressed == nil {
			live++
			if f.RuleID != "FND-CFG-002" {
				t.Errorf("live finding %s", f.RuleID)
			}
		}
	}
	if live != 1 || rep.Summary.Baselined != 1 {
		t.Fatalf("live %d, summary %+v", live, rep.Summary)
	}
	// The same rule with a different key has another fingerprint: not hidden.
	var r3 fakeRule
	r3 = fakeRule{id: "FND-CFG-001", fn: func(*model.Input) ([]rules.Result, error) {
		f := rules.NewFinding(r3, sdk.ResourceRef{Kind: "service", Name: "api"}, "first", "")
		f.Key = "another-key"
		return []rules.Result{rules.Fail(f)}, nil
	}}
	o3 := base(dir, catalogue(t, r3), "FND-CFG-001")
	if r := run(t, o3); r.code != 1 {
		t.Fatalf("different fingerprint must not be hidden, exit %d", r.code)
	}
}

func TestBaseline_Errors(t *testing.T) {
	cat := catalogue(t, failing("FND-CFG-001", "e"))
	t.Run("explicit file missing", func(t *testing.T) {
		o := base(project(t, map[string]string{}), cat, "FND-CFG-001")
		o.Baseline = "nope.yaml"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
	t.Run("invalid file", func(t *testing.T) {
		dir := project(t, map[string]string{".foundry-doctor/baseline.yaml": "version: 7\n"})
		if r := run(t, base(dir, cat, "FND-CFG-001")); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
	t.Run("write outside the project", func(t *testing.T) {
		o := base(project(t, map[string]string{}), cat, "FND-CFG-001")
		o.WriteBaseline = "../escape.yaml"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
}

const suppressionFile = `version: 1
suppressions:
  - ruleId: FND-CFG-001
    resource:
      kind: service
      name: api
    reason: accepted until the migration
    owner: platform-team
    expires: "%s"
`

func suppressionsFor(expires string) string {
	return strings.Replace(suppressionFile, "%s", expires, 1)
}

func TestSuppressions(t *testing.T) {
	cat := catalogue(t, failing("FND-CFG-001", "e"))
	run1 := func(t *testing.T, content string) (result, sdk.Report) {
		t.Helper()
		dir := project(t, map[string]string{".foundry-doctor/suppressions.yaml": content})
		o := base(dir, cat, "FND-CFG-001")
		o.Formats = []string{"json"}
		r := run(t, o)
		return r, decode(t, r.stdout)
	}
	t.Run("valid", func(t *testing.T) {
		r, rep := run1(t, suppressionsFor("2026-10-12"))
		if r.code != 0 || rep.Summary.Suppressed != 1 || rep.Findings[0].Suppressed == nil || rep.Findings[0].Suppressed.Owner != "platform-team" {
			t.Fatalf("exit %d summary %+v", r.code, rep.Summary)
		}
	})
	t.Run("expired", func(t *testing.T) {
		r, rep := run1(t, suppressionsFor("2026-10-11"))
		if r.code != 1 {
			t.Fatalf("expired suppression must give exit 1, got %d", r.code)
		}
		got := ids(rep)
		if len(got) != 2 || got[0] != "FND-CFG-001" && got[1] != "FND-CFG-001" || !strings.Contains(strings.Join(got, ","), "FND-SYS-SUPPRESSION-EXPIRED") {
			t.Fatalf("findings %v", got)
		}
		for _, f := range rep.Findings {
			if f.Fingerprint == "" || f.Profile != "foundry-dev" {
				t.Errorf("diagnostic not stamped: %+v", f)
			}
			if f.RuleID == "FND-CFG-001" && f.Suppressed != nil {
				t.Error("an expired suppression must not hide the finding")
			}
		}
	})
	t.Run("invalid file", func(t *testing.T) {
		r, rep := run1(t, "version: 1\nsuppressions:\n  - ruleId: FND-CFG-001\n")
		if r.code != 1 || !strings.Contains(strings.Join(ids(rep), ","), "FND-SYS-SUPPRESSION-INVALID") {
			t.Fatalf("exit %d findings %v", r.code, ids(rep))
		}
	})
	t.Run("missing reason or expiry is invalid", func(t *testing.T) {
		r, rep := run1(t, "version: 1\nsuppressions:\n  - ruleId: FND-CFG-001\n    resource: {kind: service, name: api}\n    owner: me\n")
		if r.code != 1 || !strings.Contains(strings.Join(ids(rep), ","), "FND-SYS-SUPPRESSION-INVALID") {
			t.Fatalf("exit %d findings %v", r.code, ids(rep))
		}
	})
	t.Run("explicit file missing", func(t *testing.T) {
		o := base(project(t, map[string]string{}), cat, "FND-CFG-001")
		o.Suppressions = "nope.yaml"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("exit %d", r.code)
		}
	})
	t.Run("no clock leak: the date comes from the injected clock", func(t *testing.T) {
		dir := project(t, map[string]string{".foundry-doctor/suppressions.yaml": suppressionsFor("2026-10-12")})
		o := base(dir, cat, "FND-CFG-001")
		o.Now = func() time.Time { return fixedNow.AddDate(0, 0, 1) }
		if r := run(t, o); r.code != 1 {
			t.Fatalf("exit %d, want 1 once the day has passed", r.code)
		}
	})
}

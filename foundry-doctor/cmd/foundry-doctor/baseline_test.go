package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/baseline"
)

// runRaw runs the CLI and returns exit code, stdout and stderr.
func runRaw(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb, noBicep(&errb))
	return code, out.String(), errb.String()
}

type summaryDoc struct {
	Summary struct {
		Error, Warning, Info, Baselined, Hidden, Skipped int
		SkippedByReason                                  map[string]int `json:"skippedByReason"`
	} `json:"summary"`
	Findings []struct {
		RuleID    string `json:"ruleId"`
		Severity  string `json:"severity"`
		Baselined bool   `json:"baselined"`
	} `json:"findings"`
}

func (d summaryDoc) ids() []string {
	var s []string
	for _, f := range d.Findings {
		s = append(s, f.RuleID)
	}
	slices.Sort(s)
	return s
}

func doctorJSON(t *testing.T, args ...string) (int, summaryDoc) {
	t.Helper()
	code, stdout, stderr := runRaw(t, append([]string{"doctor", "--local", "--format", "json"}, args...)...)
	var d summaryDoc
	if err := json.Unmarshal([]byte(stdout), &d); err != nil {
		t.Fatalf("invalid JSON (exit %d): %v\nstdout=%s\nstderr=%s", code, err, stdout, stderr)
	}
	return code, d
}

func TestWriteBaselineRoundTrip(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	code, full := doctorJSON(t, "--dir", dir)
	if code != 1 || len(full.Findings) < 2 {
		t.Fatalf("fixture must produce several findings: exit=%d %v", code, full.ids())
	}

	// Baseline only one rule: it must hide exactly its findings.
	const hiddenRule = "FND-CFG-001"
	code, stdout, stderr := runRaw(t, "doctor", "--local", "--dir", dir, "--rules", hiddenRule, "--write-baseline", baseline.DefaultPath)
	if code != 0 {
		t.Fatalf("write-baseline exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "Wrote baseline") || !strings.Contains(stdout, baseline.DefaultPath) {
		t.Fatalf("stdout = %q", stdout)
	}
	path := filepath.Join(dir, filepath.FromSlash(baseline.DefaultPath))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Parse(data); err != nil {
		t.Fatalf("written baseline does not load: %v", err)
	}

	// The conventional path is discovered without a flag; an explicit
	// --baseline resolves against the working directory, so pass it absolute.
	code, withBase := doctorJSON(t, "--dir", dir)
	_, explicit := doctorJSON(t, "--dir", dir, "--baseline", path)
	if !slices.Equal(withBase.ids(), explicit.ids()) {
		t.Fatalf("auto-discovered baseline %v differs from --baseline %v", withBase.ids(), explicit.ids())
	}
	if code != 1 {
		t.Fatalf("remaining findings must still fail: exit=%d", code)
	}
	var wantVisible, wantHidden []string
	for _, f := range full.Findings {
		if f.RuleID == hiddenRule {
			wantHidden = append(wantHidden, f.RuleID)
		} else {
			wantVisible = append(wantVisible, f.RuleID)
		}
	}
	if len(wantHidden) == 0 {
		t.Fatalf("fixture has no %s finding", hiddenRule)
	}
	slices.Sort(wantVisible)
	var gotVisible []string
	baselined := 0
	for _, f := range withBase.Findings {
		if f.Baselined {
			baselined++
			if f.RuleID != hiddenRule {
				t.Errorf("baseline hid %s, which was not baselined", f.RuleID)
			}
			continue
		}
		gotVisible = append(gotVisible, f.RuleID)
	}
	slices.Sort(gotVisible)
	if !slices.Equal(gotVisible, wantVisible) {
		t.Fatalf("visible after baseline = %v, want %v", gotVisible, wantVisible)
	}
	if baselined != len(wantHidden) {
		t.Fatalf("baselined = %d, want %d", baselined, len(wantHidden))
	}

	// A baseline of everything hides everything and the run is clean.
	code, _, stderr = runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", "all.yaml")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	code, all := doctorJSON(t, "--dir", dir, "--baseline", filepath.Join(dir, "all.yaml"))
	if code != 0 {
		t.Fatalf("full baseline must make the run pass: exit=%d %v", code, all.ids())
	}
	if all.Summary.Error+all.Summary.Warning+all.Summary.Info != 0 || all.Summary.Baselined != len(full.Findings) {
		t.Fatalf("summary = %+v, want all %d findings baselined", all.Summary, len(full.Findings))
	}
}

func TestWriteBaselineIsDeterministicAndLeakFree(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	for _, name := range []string{"one.yaml", "two.yaml"} {
		if code, _, stderr := runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", name); code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr)
		}
	}
	a, _ := os.ReadFile(filepath.Join(dir, "one.yaml"))
	b, _ := os.ReadFile(filepath.Join(dir, "two.yaml"))
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Fatalf("baselines differ:\n%s\n---\n%s", a, b)
	}
	// Only the documented keys appear: no evidence, no locations, no paths.
	for _, line := range strings.Split(strings.TrimSpace(string(a)), "\n") {
		trim := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		key, _, _ := strings.Cut(trim, ":")
		if !slices.Contains([]string{"version", "entries", "fingerprint", "ruleId"}, key) {
			t.Errorf("unexpected line in baseline: %q", line)
		}
	}
	if strings.Contains(string(a), dir) || strings.Contains(string(a), "azure.yaml") {
		t.Fatalf("baseline leaks a path:\n%s", a)
	}
}

func TestWriteBaselineRefusesOverwriteWithoutForce(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	target := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", "b.yaml")
	if code != 2 || !strings.Contains(stderr, "--force") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("file was modified: %q", got)
	}
	code, _, stderr = runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", "b.yaml", "--force")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if got, _ := os.ReadFile(target); !strings.HasPrefix(string(got), "version: 1") {
		t.Fatalf("force did not write a baseline: %q", got)
	}
}

func TestWriteBaselineRefusesSymlinksAndEscapes(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	outside := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", "linked/baseline.yaml", "--force")
		if code != 2 || !strings.Contains(stderr, "symbolic link") {
			t.Fatalf("exit=%d stderr=%s", code, stderr)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 0 {
			t.Fatalf("a file was written through a symlink: %v", entries)
		}
	}
	for _, p := range []string{"../escape.yaml", filepath.Join(outside, "abs.yaml")} {
		code, _, stderr := runRaw(t, "doctor", "--local", "--dir", dir, "--write-baseline", p, "--force")
		if code != 2 {
			t.Fatalf("%s: exit=%d stderr=%s", p, code, stderr)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 1 {
		t.Fatalf("a file escaped the project: %v", entries)
	}
}

func TestWriteBaselineFlagValidation(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	for name, args := range map[string][]string{
		"with --baseline":     {"--write-baseline", "x.yaml", "--baseline", "y.yaml"},
		"--force on its own":  {"--force"},
		"missing project dir": {"--write-baseline", "x.yaml", "--dir", filepath.Join(dir, "missing")},
	} {
		code, _, _ := runRaw(t, append([]string{"doctor", "--local", "--dir", dir}, args...)...)
		if code != 2 {
			t.Errorf("%s: exit=%d, want 2", name, code)
		}
	}
}

func TestMinSeverityHiddenCountInSummary(t *testing.T) {
	dir := copyTree(t, sample("bad"))
	_, all := doctorJSON(t, "--dir", dir)
	if all.Summary.Hidden != 0 {
		t.Fatalf("hidden = %d with the default filter", all.Summary.Hidden)
	}
	var below int
	for _, f := range all.Findings {
		if f.Severity != "error" {
			below++
		}
	}
	if below == 0 {
		t.Skip("fixture has no warning or info finding to hide")
	}
	_, onlyErrors := doctorJSON(t, "--dir", dir, "--min-severity", "error")
	if onlyErrors.Summary.Hidden != below {
		t.Fatalf("hidden = %d, want %d (findings below error)", onlyErrors.Summary.Hidden, below)
	}
	for _, f := range onlyErrors.Findings {
		if f.Severity != "error" {
			t.Fatalf("%s %s must be hidden", f.Severity, f.RuleID)
		}
	}
	if len(onlyErrors.Findings)+onlyErrors.Summary.Hidden != len(all.Findings) {
		t.Fatalf("shown %d + hidden %d != total %d", len(onlyErrors.Findings), onlyErrors.Summary.Hidden, len(all.Findings))
	}
	if onlyErrors.Summary.Skipped == 0 || len(onlyErrors.Summary.SkippedByReason) == 0 {
		t.Fatalf("skippedByReason missing: %+v", onlyErrors.Summary)
	}
	total := 0
	for _, n := range onlyErrors.Summary.SkippedByReason {
		total += n
	}
	if total != onlyErrors.Summary.Skipped {
		t.Fatalf("skippedByReason sums to %d, skipped = %d", total, onlyErrors.Summary.Skipped)
	}
	_, console, _ := runRaw(t, "doctor", "--local", "--dir", dir, "--min-severity", "error")
	if !strings.Contains(console, "hidden by --min-severity") {
		t.Fatalf("console summary lacks the hidden count:\n%s", console)
	}
}

func TestDoctorHelpNamesRealDefaultsAndBaselinePath(t *testing.T) {
	_, stdout, _ := runRaw(t, "doctor", "--help")
	if !strings.Contains(stdout, "default foundry-prod") {
		t.Errorf("help must print the actual default profile:\n%s", stdout)
	}
	if strings.Contains(stdout, "baseline.json") || strings.Contains(stdout, ".foundry/") {
		t.Errorf("help still names the old baseline path:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--write-baseline") || !strings.Contains(stdout, "--force") {
		t.Errorf("help must document baseline generation:\n%s", stdout)
	}
}

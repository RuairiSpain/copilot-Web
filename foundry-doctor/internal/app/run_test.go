package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func decode(t *testing.T, s string) sdk.Report {
	t.Helper()
	var r sdk.Report
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatalf("not a JSON report: %v\n%s", err, s)
	}
	return r
}

func TestRun_ExitCodes(t *testing.T) {
	cat := catalogue(t, passing("FND-CFG-001"), failing("FND-ENV-001", "evidence"), panicking("FND-ENV-002"), passing("FND-NET-001"))
	tests := []struct {
		name string
		sel  string
		mod  func(*app.Options)
		want int
	}{
		{"clean", "FND-CFG-001", nil, sdk.ExitOK},
		{"finding at fail-on", "FND-ENV-001", func(o *app.Options) { o.Profile = "prod" }, sdk.ExitFindings},
		{"finding below fail-on", "FND-ENV-001", nil, sdk.ExitOK}, // info in dev
		{"fail-on info", "FND-ENV-001", func(o *app.Options) { o.FailOn = "info" }, sdk.ExitFindings},
		{"skipped without strict", "FND-NET-001", nil, sdk.ExitOK},
		{"skipped with strict", "FND-NET-001", func(o *app.Options) { o.Strict = true }, sdk.ExitSkippedStrict},
		{"strict does not beat findings", "FND-ENV-001,FND-NET-001", func(o *app.Options) { o.Strict = true; o.Profile = "prod" }, sdk.ExitFindings},
		{"rule panic", "FND-ENV-002", nil, sdk.ExitInternal},
		{"panic beats findings", "FND-ENV-002,FND-ENV-001", func(o *app.Options) { o.Profile = "prod" }, sdk.ExitInternal},
		{"bad profile", "FND-CFG-001", func(o *app.Options) { o.Profile = "staging" }, sdk.ExitCannotRun},
		{"bad min-severity", "FND-CFG-001", func(o *app.Options) { o.MinSeverity = "loud" }, sdk.ExitCannotRun},
		{"bad fail-on", "FND-CFG-001", func(o *app.Options) { o.FailOn = "fatal" }, sdk.ExitCannotRun},
		{"bad format", "FND-CFG-001", func(o *app.Options) { o.Formats = []string{"html"} }, sdk.ExitCannotRun},
		{"unknown rule", "FND-NOPE-999", nil, sdk.ExitCannotRun},
		{"rule without implementation", "FND-SEC-001", nil, sdk.ExitCannotRun},
		{"bad selector", "a,,b", nil, sdk.ExitCannotRun},
		{"later-phase flag", "FND-CFG-001", func(o *app.Options) { o.Unavailable = []string{"--preflight"} }, sdk.ExitCannotRun},
		{"no clock", "FND-CFG-001", func(o *app.Options) { o.Now = nil }, sdk.ExitInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := base(project(t, map[string]string{}), cat, tc.sel)
			if tc.mod != nil {
				tc.mod(&o)
			}
			r := run(t, o)
			if r.code != tc.want {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, tc.want, r.stdout, r.stderr)
			}
		})
	}
}

func TestRun_UnavailableMessage(t *testing.T) {
	o := base(project(t, map[string]string{}), catalogue(t, passing("FND-CFG-001")), "FND-CFG-001")
	o.Unavailable = []string{"--runtime", "--preflight"}
	r := run(t, o)
	if r.code != 2 || !strings.Contains(r.stderr, "--preflight, --runtime: not available in this version") {
		t.Fatalf("%d %q", r.code, r.stderr)
	}
	if r.stdout != "" {
		t.Fatalf("no report expected, got %q", r.stdout)
	}
}

func TestRun_NoProjectIsExit2(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	r := run(t, base(dir, catalogue(t, passing("FND-CFG-001")), "FND-CFG-001"))
	if r.code != 2 || !strings.Contains(r.stderr, "azure.yaml") {
		t.Fatalf("%d %q", r.code, r.stderr)
	}
	r = run(t, base(filepath.Join(dir, "missing"), catalogue(t, passing("FND-CFG-001")), "FND-CFG-001"))
	if r.code != 2 {
		t.Fatalf("missing dir: %d %q", r.code, r.stderr)
	}
}

func TestRun_InvalidAzureYAMLIsAFinding(t *testing.T) {
	dir := project(t, map[string]string{"azure.yaml": "name: [unclosed\n"})
	r := run(t, base(dir, catalogue(t, passing("FND-CFG-001")), "FND-CFG-001"))
	if r.code != 1 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	contains(t, "stdout", r.stdout, "FND-SYS-INVALID-AZURE-YAML")
}

func TestRun_ProfilesMap(t *testing.T) {
	cat := catalogue(t, failing("FND-ENV-001", "e"))
	for in, want := range map[string]string{"": "foundry-dev", "dev": "foundry-dev", "test": "foundry-test", "prod": "foundry-prod", "foundry-prod": "foundry-prod"} {
		o := base(project(t, map[string]string{}), cat, "FND-ENV-001")
		o.Profile, o.Formats = in, []string{"json"}
		r := run(t, o)
		rep := decode(t, r.stdout)
		if rep.Profile != want {
			t.Errorf("--profile %q: report profile %q, want %q", in, rep.Profile, want)
		}
		if len(rep.Findings) != 1 || rep.Findings[0].Profile != want {
			t.Errorf("--profile %q: finding profile %+v", in, rep.Findings)
		}
	}
}

func TestRun_MinSeverityFiltersDisplayNotExit(t *testing.T) {
	cat := catalogue(t, failing("FND-ENV-001", "e"))
	o := base(project(t, map[string]string{}), cat, "FND-ENV-001")
	o.Profile, o.Formats, o.MinSeverity, o.FailOn = "test", []string{"json"}, "error", "warning"
	r := run(t, o)
	if r.code != 1 {
		t.Fatalf("exit %d, want 1: fail-on is independent of min-severity", r.code)
	}
	rep := decode(t, r.stdout)
	if len(rep.Findings) != 0 || rep.Summary.Hidden != 1 {
		t.Fatalf("findings %d hidden %d", len(rep.Findings), rep.Summary.Hidden)
	}
}

func TestRun_StrictOnlyWhenNothingElse(t *testing.T) {
	cat := catalogue(t, passing("FND-NET-001"), failing("FND-ENV-001", "e"))
	// skipped (NET-001 needs ARM; synthetic project) + warning finding below the default threshold.
	o := base(project(t, map[string]string{}), cat, "FND-NET-001,FND-ENV-001")
	o.Strict, o.Profile = true, "test"
	if r := run(t, o); r.code != 3 {
		t.Fatalf("exit %d, want 3\n%s", r.code, r.stdout)
	}
	o.FailOn = "warning"
	if r := run(t, o); r.code != 1 {
		t.Fatalf("exit %d, want 1 (findings beat strict)", r.code)
	}
}

func TestRun_NotImplementedRulesAreSkippedNotDropped(t *testing.T) {
	cat := catalogue(t, passing("FND-CFG-001"))
	o := base(project(t, map[string]string{}), cat, "")
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	rep := decode(t, r.stdout)
	if rep.Summary.Skipped < 10 || rep.Summary.SkippedByReason[app.ReasonNotImplemented] != rep.Summary.Skipped {
		t.Fatalf("skipped %d by reason %v", rep.Summary.Skipped, rep.Summary.SkippedByReason)
	}
	if rep.Summary.Passed != 1 {
		t.Fatalf("passed %d", rep.Summary.Passed)
	}
	o.Strict = true
	if r := run(t, o); r.code != 3 {
		t.Fatalf("strict exit %d, want 3", r.code)
	}
}

func TestRun_ConfigErrors(t *testing.T) {
	cat := catalogue(t, passing("FND-CFG-001"))
	t.Run("unknown key", func(t *testing.T) {
		dir := project(t, map[string]string{".foundry-doctor/config.yaml": "version: 1\nbogus: true\n"})
		if r := run(t, base(dir, cat, "FND-CFG-001")); r.code != 2 || !strings.Contains(r.stderr, "bogus") {
			t.Fatalf("%d %q", r.code, r.stderr)
		}
	})
	t.Run("missing --config", func(t *testing.T) {
		o := base(project(t, map[string]string{}), cat, "FND-CFG-001")
		o.ConfigPath = "nope.yaml"
		if r := run(t, o); r.code != 2 {
			t.Fatalf("%d %q", r.code, r.stderr)
		}
	})
	t.Run("--config overrides", func(t *testing.T) {
		dir := project(t, map[string]string{"other.yaml": "version: 1\nprofile: prod\n", ".foundry-doctor/config.yaml": "version: 1\nprofile: test\n"})
		o := base(dir, cat, "FND-CFG-001")
		o.ConfigPath, o.Formats = "other.yaml", []string{"json"}
		if rep := decode(t, run(t, o).stdout); rep.Profile != "foundry-prod" {
			t.Fatalf("profile %q", rep.Profile)
		}
		o.ConfigPath = ""
		if rep := decode(t, run(t, o).stdout); rep.Profile != "foundry-test" {
			t.Fatalf("profile %q", rep.Profile)
		}
		o.Profile = "dev" // flag beats file
		if rep := decode(t, run(t, o).stdout); rep.Profile != "foundry-dev" {
			t.Fatalf("profile %q", rep.Profile)
		}
	})
	t.Run("policy header", func(t *testing.T) {
		dir := project(t, map[string]string{".foundry-doctor/config.yaml": "version: 1\npolicy:\n  logRetention:\n    minimumDays: 90\n"})
		o := base(dir, cat, "FND-CFG-001")
		o.Formats = []string{"json"}
		rep := decode(t, run(t, o).stdout)
		v, _ := rep.EffectivePolicy["policy.logRetention.minimumDays"].(string)
		if !strings.Contains(v, "90") || !strings.Contains(v, "[repository]") {
			t.Fatalf("policy header %v", rep.EffectivePolicy)
		}
	})
}

func TestRun_Environment(t *testing.T) {
	var seen []string
	probe := fakeRule{id: "FND-ENV-001", fn: func(in *model.Input) ([]rules.Result, error) {
		if in.Environment == nil {
			seen = append(seen, "none")
		} else {
			seen = append(seen, in.Environment.Name)
		}
		return nil, nil
	}}
	cat := catalogue(t, probe)
	files := map[string]string{
		".azure/config.json": `{"defaultEnvironment":"dev"}`,
		".azure/dev/.env":    "A=1\n",
		".azure/prod/.env":   "B=2\n",
	}
	dir := project(t, files)
	o := base(dir, cat, "FND-ENV-001")
	run(t, o)
	o.Environment = "prod"
	run(t, o)
	o.Environment = "ghost"
	if r := run(t, o); r.code != 2 {
		t.Fatalf("explicit missing environment: exit %d", r.code)
	}
	if strings.Join(seen, ",") != "dev,prod" {
		t.Fatalf("environments seen %v", seen)
	}
	// An implicit environment that cannot be read is not fatal.
	dir2 := project(t, map[string]string{".azure/config.json": `{"defaultEnvironment":"dev"}`})
	seen = nil
	if r := run(t, base(dir2, cat, "FND-ENV-001")); r.code != 0 || seen[0] != "none" {
		t.Fatalf("exit %d seen %v", r.code, seen)
	}
}

func TestRun_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := base(project(t, map[string]string{}), catalogue(t, passing("FND-CFG-001")), "FND-CFG-001")
	var out, errb strings.Builder
	err := app.Run(ctx, o, &out, &errb)
	if app.CodeOf(err) != 2 {
		t.Fatalf("exit %d: %v", app.CodeOf(err), err)
	}
}

func TestRun_DefaultCatalogue(t *testing.T) {
	o := base(project(t, map[string]string{}), nil, "")
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 0 && r.code != 1 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
}

func TestExitError(t *testing.T) {
	e := &app.ExitError{Code: 3}
	if e.Error() != "exit 3" || e.Unwrap() != nil || e.ExitCode() != 3 {
		t.Fatal("bare exit error")
	}
	cause := errors.New("why")
	e = &app.ExitError{Code: 2, Err: cause}
	if e.Error() != "why" || !errors.Is(e, cause) {
		t.Fatal("wrapped exit error")
	}
	if app.CodeOf(nil) != 0 || app.CodeOf(e) != 2 || app.CodeOf(errors.New("x")) != 4 {
		t.Fatal("CodeOf")
	}
}

func TestRun_SkippedHasReasonAndCapability(t *testing.T) {
	cat := catalogue(t, passing("FND-NET-001"))
	o := base(project(t, map[string]string{}), cat, "FND-NET-001")
	o.Formats = []string{"json"}
	rep := decode(t, run(t, o).stdout)
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != rules.ReasonSyntheticInfrastructure || rep.Skipped[0].MissingCapability == "" {
		t.Fatalf("%+v", rep.Skipped)
	}
}

func TestRun_FilesStayUntouched(t *testing.T) {
	dir := project(t, map[string]string{})
	before, _ := os.ReadDir(dir)
	run(t, base(dir, catalogue(t, failing("FND-CFG-001", "e")), "FND-CFG-001"))
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Fatalf("the doctor created files: %v -> %v", before, after)
	}
}

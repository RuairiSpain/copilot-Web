package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func bicepProject(t *testing.T, cfg string) string {
	t.Helper()
	files := map[string]string{
		"infra/main.bicep": "targetScope = 'resourceGroup'\n",
	}
	if cfg != "" {
		files[".foundry-doctor/config.yaml"] = cfg
	}
	return project(t, files)
}

func armProbe(got *[]string) fakeRule {
	return fakeRule{id: "FND-SEC-001", fn: func(in *model.Input) ([]rules.Result, error) {
		if in.ARM == nil {
			*got = append(*got, "no-arm")
		} else {
			*got = append(*got, "arm:"+in.ARM.File)
			for _, r := range in.ARM.Resources {
				*got = append(*got, r.Type)
			}
		}
		*got = append(*got, "bicep="+in.Versions.Bicep)
		return nil, nil
	}}
}

func TestBicep_CompilesAndNormalises(t *testing.T) {
	var got []string
	dir := bicepProject(t, "")
	fb := &fakeBicep{arm: armOneResource}
	o := withBicep(t, base(dir, catalogue(t, armProbe(&got)), "FND-SEC-001"), fb)
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	want := "arm:infra/main.bicep,Microsoft.Storage/storageAccounts,bicep=0.30.3"
	if strings.Join(got, ",") != want {
		t.Fatalf("rule saw %v", got)
	}
	rep := decode(t, r.stdout)
	if len(rep.Tools) != 1 || rep.Tools[0].Name != "bicep" || rep.Tools[0].State != sdk.ToolAvailable || !rep.Tools[0].Required {
		t.Fatalf("tools %+v", rep.Tools)
	}
	if strings.Contains(rep.Tools[0].Path, "/") {
		t.Fatalf("tool path must be a base name: %q", rep.Tools[0].Path)
	}
	// The compiler ran with the no-restore, stdout form and a scrubbed environment.
	last := fb.specs[len(fb.specs)-1]
	if strings.Join(last.Args[:1], "") != "build" || !strings.Contains(strings.Join(last.Args, " "), "--no-restore") {
		t.Fatalf("args %v", last.Args)
	}
}

func TestBicep_RequiredButMissingIsExit2(t *testing.T) {
	var got []string
	o := base(bicepProject(t, ""), catalogue(t, armProbe(&got)), "FND-SEC-001")
	o.Bicep.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 2 || !strings.Contains(r.stderr, "Bicep CLI is required") {
		t.Fatalf("exit %d stderr %q", r.code, r.stderr)
	}
	rep := decode(t, r.stdout)
	if rep.ExitCode != 2 || rep.Tools[0].State != sdk.ToolMissing {
		t.Fatalf("report %+v", rep)
	}
}

func TestBicep_OptionalMissingSkipsWithToolMissing(t *testing.T) {
	var got []string
	o := base(bicepProject(t, "version: 1\nvalidation:\n  bicep: optional\n"), catalogue(t, armProbe(&got)), "FND-SEC-001")
	o.Bicep.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	rep := decode(t, r.stdout)
	if len(rep.Skipped) != 1 || rep.Skipped[0].Reason != "tool-missing:bicep" || rep.Skipped[0].MissingCapability == "" {
		t.Fatalf("skipped %+v", rep.Skipped)
	}
	if len(got) != 0 {
		t.Fatal("the rule must not run without ARM")
	}
	o.Strict = true
	if r := run(t, o); r.code != 3 {
		t.Fatalf("strict exit %d", r.code)
	}
}

func TestBicep_Disabled(t *testing.T) {
	var got []string
	o := base(bicepProject(t, "version: 1\nvalidation:\n  bicep: disabled\n"), catalogue(t, armProbe(&got)), "FND-SEC-001")
	o.Formats = []string{"json"}
	o.Bicep.LookPath = func(string) (string, error) { t.Error("must not look for bicep"); return "", errors.New("x") }
	r := run(t, o)
	rep := decode(t, r.stdout)
	if r.code != 0 || rep.Tools[0].State != sdk.ToolDisabled || len(rep.Skipped) != 1 {
		t.Fatalf("exit %d tools %+v skipped %+v", r.code, rep.Tools, rep.Skipped)
	}
}

func TestBicep_NamedExecutable(t *testing.T) {
	var asked string
	o := base(bicepProject(t, "version: 1\nadvanced:\n  bicep:\n    executable: bicep-nightly\n"), catalogue(t, passing("FND-CFG-001")), "FND-CFG-001")
	o.Bicep.LookPath = func(n string) (string, error) { asked = n; return "", errors.New("x") }
	run(t, o)
	if asked != "bicep-nightly" {
		t.Fatalf("looked up %q", asked)
	}
}

func TestBicep_CompileErrorBecomesFinding(t *testing.T) {
	var got []string
	fb := &fakeBicep{exitCode: 1, stderr: "/work/infra/main.bicep(3,5) : Error BCP035: The specified \"resource\" declaration is missing the following required properties: \"name\".\n"}
	o := withBicep(t, base(bicepProject(t, ""), catalogue(t, armProbe(&got)), "FND-SEC-001"), fb)
	o.Formats = []string{"json"}
	r := run(t, o)
	if r.code != 1 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	rep := decode(t, r.stdout)
	found := false
	for _, f := range rep.Findings {
		if f.RuleID == "bicep/BCP035" {
			found = true
			if f.Fingerprint == "" || f.Adapter != "bicep" {
				t.Errorf("finding %+v", f)
			}
		}
	}
	if !found || len(got) != 0 {
		t.Fatalf("findings %v, rule ran: %v", ids(rep), got)
	}
}

func TestBicep_ProtocolFailureIsExit4(t *testing.T) {
	fb := &fakeBicep{exitCode: 1, stderr: "something unparsable"}
	o := withBicep(t, base(bicepProject(t, ""), catalogue(t, passing("FND-CFG-001")), "FND-CFG-001"), fb)
	if r := run(t, o); r.code != 4 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
}

func TestARMJSONProject(t *testing.T) {
	var got []string
	dir := project(t, map[string]string{"infra/main.json": armOneResource})
	o := base(dir, catalogue(t, armProbe(&got)), "FND-SEC-001")
	if r := run(t, o); r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	if len(got) < 2 || got[0] != "arm:infra/main.json" {
		t.Fatalf("rule saw %v", got)
	}
	// An unreadable template is reported, not a crash; ARM rules are skipped.
	dir = project(t, map[string]string{"infra/other.json": armOneResource})
	got = nil
	o = base(dir, catalogue(t, armProbe(&got)), "FND-SEC-001")
	o.Formats = []string{"json"}
	o.Debug = true
	r := run(t, o)
	if r.code != 0 || len(got) != 0 {
		t.Fatalf("exit %d got %v", r.code, got)
	}
	if rep := decode(t, r.stdout); len(rep.Skipped) != 1 {
		t.Fatalf("skipped %+v", rep.Skipped)
	}
	contains(t, "debug", r.stderr, "no infra/main.json ARM template")
}

func TestDebugOutput(t *testing.T) {
	var got []string
	fb := &fakeBicep{arm: armOneResource}
	o := withBicep(t, base(bicepProject(t, ""), catalogue(t, armProbe(&got)), "FND-SEC-001"), fb)
	o.Debug = true
	r := run(t, o)
	contains(t, "stderr", r.stderr, "debug: profile foundry-dev", "debug: policy policy.", "debug: tool bicep state=available version=0.30.3", "debug: adapter command: bicep build <entry.bicep> --stdout --no-restore")
	notContains(t, "stderr", r.stderr, "/bicep ", "main.bicep")
	_ = app.UnavailableMessage
}

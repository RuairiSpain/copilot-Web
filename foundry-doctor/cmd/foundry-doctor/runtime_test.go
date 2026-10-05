package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type recordingRuntime struct {
	in  app.RuntimeInput
	out app.RunOutput
	n   int
}

func (r *recordingRuntime) Run(_ context.Context, in app.RuntimeInput) (app.RunOutput, error) {
	r.n++
	r.in = in
	return r.out, nil
}

func runtimeSvc(rt app.RuntimeEngine) app.Services {
	svc := noBicep(io.Discard)
	svc.Runtime = rt
	return svc
}

func TestRuntimeHelpListsFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"runtime", "--help"}, &out, &errb, runtimeSvc(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"--subscription", "--resource-group", "--account", "--project", "--vantage", "--timeout", "metadata-only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestRuntimeFlagsReachEngine(t *testing.T) {
	rt := &recordingRuntime{}
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{
		"runtime", "--dir", sample("good"),
		"--subscription", "sub", "--resource-group", "rg", "--account", "acct", "--project", "proj",
		"--vantage", "vnet", "--timeout", "45s",
	}, &out, &errb, runtimeSvc(rt))
	if code != 0 || rt.n != 1 {
		t.Fatalf("exit %d runs %d stderr %s", code, rt.n, errb.String())
	}
	if rt.in.Target.SubscriptionID != "sub" || rt.in.Target.ResourceGroup != "rg" || rt.in.Target.Account != "acct" || rt.in.Target.Project != "proj" {
		t.Errorf("target not passed: %+v", rt.in)
	}
	if rt.in.Vantage != runtime.VantageVNet || rt.in.Timeout != 45*time.Second {
		t.Errorf("options not passed: %+v", rt.in)
	}
}

func TestRuntimeSkippedStrictAndValidation(t *testing.T) {
	skip := app.RunOutput{Skipped: []sdk.Skip{{RuleID: "FND-RUN-002", Reason: "input-unavailable: re-run from inside the VNet"}}}
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"runtime", "--dir", sample("good")}, &out, &errb, runtimeSvc(&recordingRuntime{out: skip})); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Errorf("readiness summary missing: %s", out.String())
	}
	out.Reset()
	errb.Reset()
	if code := run(context.Background(), []string{"runtime", "--strict", "--dir", sample("good")}, &out, &errb, runtimeSvc(&recordingRuntime{out: skip})); code != app.ExitStrictSkip {
		t.Fatalf("strict exit = %d, want %d", code, app.ExitStrictSkip)
	}
	if code := run(context.Background(), []string{"runtime", "--vantage", "bad", "--dir", sample("good")}, &out, &errb, runtimeSvc(&recordingRuntime{})); code != app.ExitUnavailable {
		t.Fatalf("bad vantage exit = %d, want %d", code, app.ExitUnavailable)
	}
	if code := run(context.Background(), []string{"runtime", "--dir", sample("good")}, &out, &errb, runtimeSvc(nil)); code != app.ExitUnavailable {
		t.Fatalf("missing engine exit = %d, want %d", code, app.ExitUnavailable)
	}
}

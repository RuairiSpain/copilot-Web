package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type recordingPreflight struct {
	in  app.PreflightInput
	out app.RunOutput
	n   int
}

func (r *recordingPreflight) Run(_ context.Context, in app.PreflightInput) (app.RunOutput, error) {
	r.n++
	r.in = in
	return r.out, nil
}

func preflightSvc(pf app.PreflightEngine) app.Services {
	svc := noBicep(io.Discard)
	svc.Preflight = pf
	return svc
}

func TestPreflightHelpListsFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"preflight", "--help"}, &out, &errb, preflightSvc(nil)); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"--what-if", "--preflight", "--approved-scope", "--allow-delete", "--subscription", "--resource-group", "--location", "--tenant", "never guarantees"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestPreflightFlagsReachEngine(t *testing.T) {
	pf := &recordingPreflight{}
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{
		"preflight", "--dir", sample("good"), "--what-if",
		"--subscription", "sub", "--resource-group", "rg", "--location", "eastus", "--tenant", "ten",
		"--approved-scope", "/subscriptions/sub/resourceGroups/shared",
		"--allow-delete", "/subscriptions/sub/resourceGroups/rg/providers/X/y/z",
	}, &out, &errb, preflightSvc(pf))
	if code != 0 || pf.n != 1 {
		t.Fatalf("exit %d runs %d stderr %s", code, pf.n, errb.String())
	}
	if !pf.in.WhatIf || pf.in.Target.SubscriptionID != "sub" || pf.in.Target.ResourceGroup != "rg" ||
		pf.in.Target.Location != "eastus" || pf.in.Target.TenantID != "ten" {
		t.Errorf("target/what-if not passed: %+v", pf.in)
	}
	if len(pf.in.ApprovedScopes) != 1 || len(pf.in.AllowedDeletes) != 1 {
		t.Errorf("scopes not passed: %+v", pf.in)
	}
}

func TestPreflightWhatIfIsOptIn(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"default off", nil, false},
		{"what-if", []string{"--what-if"}, true},
		{"preflight alias", []string{"--preflight"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pf := &recordingPreflight{}
			t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
			t.Setenv("AZURE_ENV_NAME", "")
			args := append([]string{"preflight", "--dir", sample("good")}, tc.args...)
			var out, errb bytes.Buffer
			if code := run(context.Background(), args, &out, &errb, preflightSvc(pf)); code != 0 {
				t.Fatalf("exit %d: %s", code, errb.String())
			}
			if pf.in.WhatIf != tc.want {
				t.Fatalf("WhatIf = %v, want %v", pf.in.WhatIf, tc.want)
			}
		})
	}
}

func TestPreflightSkippedStrictAndReadiness(t *testing.T) {
	skip := app.RunOutput{Skipped: []sdk.Skip{{RuleID: "FND-DEP-005", Reason: "input-unavailable: capability quota usage read; missing permission Microsoft.CognitiveServices/locations/usages/read"}}}
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"preflight", "--dir", sample("good")}, &out, &errb, preflightSvc(&recordingPreflight{out: skip})); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Errorf("readiness summary missing: %s", out.String())
	}
	out.Reset()
	errb.Reset()
	if code := run(context.Background(), []string{"preflight", "--strict", "--dir", sample("good")}, &out, &errb, preflightSvc(&recordingPreflight{out: skip})); code != app.ExitStrictSkip {
		t.Fatalf("strict exit = %d, want %d", code, app.ExitStrictSkip)
	}
}

func TestPreflightRejectsBadInput(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"preflight", "--min-severity", "loud"}, &out, &errb, preflightSvc(&recordingPreflight{})); code == 0 {
		t.Fatal("invalid severity accepted")
	}
	if code := run(context.Background(), []string{"preflight", "extra"}, &out, &errb, preflightSvc(&recordingPreflight{})); code == 0 {
		t.Fatal("positional argument accepted")
	}
	if code := run(context.Background(), []string{"preflight", "--dir", sample("good")}, &out, &errb, preflightSvc(nil)); code != app.ExitUnavailable {
		t.Fatalf("missing engine exit = %d, want %d", code, app.ExitUnavailable)
	}
}

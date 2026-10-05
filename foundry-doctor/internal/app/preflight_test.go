package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakePreflight struct {
	out RunOutput
	err error
	in  PreflightInput
}

func (f *fakePreflight) Run(_ context.Context, in PreflightInput) (RunOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestPreflight(t *testing.T) {
	restricted := RunOutput{Skipped: []sdk.Skip{{
		RuleID: "FND-DEP-005",
		Reason: "input-unavailable: capability quota usage read; missing permission Microsoft.CognitiveServices/locations/usages/read; denied",
	}}}
	tests := []struct {
		name     string
		pf       *fakePreflight
		nilPF    bool
		req      PreflightRequest
		wantExit int
		wantErr  error
		wantOut  string
	}{
		{name: "clean", pf: &fakePreflight{}, wantExit: ExitOK},
		{name: "restricted identity skip is reported", pf: &fakePreflight{out: restricted}, wantExit: ExitOK, wantOut: "skipped=1"},
		{name: "strict skip", pf: &fakePreflight{out: restricted}, req: PreflightRequest{DoctorRequest: DoctorRequest{Strict: true}}, wantExit: ExitStrictSkip},
		{name: "finding fails", pf: &fakePreflight{out: RunOutput{Findings: []sdk.Finding{{RuleID: "FND-DEP-001", Severity: sdk.SeverityError, Fingerprint: "x"}}}}, wantExit: ExitFindings},
		{name: "engine error", pf: &fakePreflight{err: errors.New("boom")}, wantExit: ExitInternal, wantErr: errors.New("boom")},
		{name: "nil engine unavailable", nilPF: true, wantExit: ExitUnavailable, wantErr: ErrUnavailable},
		{name: "bad format", pf: &fakePreflight{}, req: PreflightRequest{DoctorRequest: DoctorRequest{Format: "nope"}}, wantExit: ExitUnavailable, wantErr: errors.New("invalid format")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			if !tt.nilPF {
				svc.Preflight = tt.pf
			}
			var out bytes.Buffer
			code, err := Preflight(context.Background(), svc, tt.req, &out)
			if code != tt.wantExit {
				t.Fatalf("exit = %d, want %d (err %v)", code, tt.wantExit, err)
			}
			if (err != nil) != (tt.wantErr != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == ErrUnavailable && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("output %q missing %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestPreflightReadinessIgnoresFilters(t *testing.T) {
	warn := sdk.Finding{RuleID: "FND-DEP-010", Severity: sdk.SeverityWarning, Fingerprint: "w"}
	cases := []struct {
		name string
		req  PreflightRequest
	}{
		{"warning below --min-severity is hidden", PreflightRequest{DoctorRequest: DoctorRequest{MinSeverity: sdk.SeverityError}}},
		{"warning at default severity", PreflightRequest{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			svc.Preflight = &fakePreflight{out: RunOutput{Findings: []sdk.Finding{warn}, Evaluated: []string{"FND-DEP-010", "FND-DEP-011"}}}
			var got *report.Readiness
			var shown int
			svc.Reporter = reporterFunc(func(_ io.Writer, _ string, r Report) error {
				got, shown = r.Readiness, len(r.Findings)
				return nil
			})
			if _, err := Preflight(context.Background(), svc, c.req, io.Discard); err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got.Blocked) != 1 || got.Blocked[0] != "FND-DEP-010" {
				t.Fatalf("a failing rule must be blocked (shown findings %d): %+v", shown, got)
			}
			if len(got.Ready) != 1 || got.Ready[0] != "FND-DEP-011" {
				t.Fatalf("ready = %v", got.Ready)
			}
		})
	}
}

func TestPreflightPassesThroughAndDefaultsSelector(t *testing.T) {
	pf := &fakePreflight{}
	svc, cfg := newSvc(&fakeEngine{})
	svc.Preflight = pf
	req := PreflightRequest{
		Target:         PreflightTarget{SubscriptionID: "sub", ResourceGroup: "rg"},
		WhatIf:         true,
		ApprovedScopes: []string{"/subscriptions/sub/resourceGroups/shared"},
		AllowedDeletes: []string{"id1"},
	}
	if _, err := Preflight(context.Background(), svc, req, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.last.Rules) != 1 || cfg.last.Rules[0] != "FND-DEP-*" {
		t.Errorf("default selector = %v", cfg.last.Rules)
	}
	if !pf.in.WhatIf || pf.in.Target.SubscriptionID != "sub" || len(pf.in.ApprovedScopes) != 1 || len(pf.in.AllowedDeletes) != 1 {
		t.Errorf("flags not passed through: %+v", pf.in)
	}
}

func TestPreflightWhatIfOffByDefault(t *testing.T) {
	pf := &fakePreflight{}
	svc, _ := newSvc(&fakeEngine{})
	svc.Preflight = pf
	if _, err := Preflight(context.Background(), svc, PreflightRequest{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if pf.in.WhatIf {
		t.Error("what-if must be opt-in")
	}
}

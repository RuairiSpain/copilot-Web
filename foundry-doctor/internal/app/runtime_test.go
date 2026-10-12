package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeRuntime struct {
	out RunOutput
	err error
	in  RuntimeInput
}

func (f *fakeRuntime) Run(_ context.Context, in RuntimeInput) (RunOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestRuntime(t *testing.T) {
	restricted := RunOutput{Skipped: []sdk.Skip{{
		RuleID: "FND-RUN-002",
		Reason: "input-unavailable: private DNS resolves only from inside the VNet",
	}}}
	tests := []struct {
		name     string
		rt       *fakeRuntime
		nilRT    bool
		req      RuntimeRequest
		wantExit int
		wantErr  error
		wantOut  string
	}{
		{name: "clean", rt: &fakeRuntime{}, wantExit: ExitOK},
		{name: "restricted identity skip is reported as unavailable", rt: &fakeRuntime{out: restricted}, wantExit: ExitUnavailable, wantOut: "skipped=1"},
		{name: "strict required skip still exits unavailable", rt: &fakeRuntime{out: restricted}, req: RuntimeRequest{DoctorRequest: DoctorRequest{Strict: true}}, wantExit: ExitUnavailable},
		{name: "finding fails", rt: &fakeRuntime{out: RunOutput{Findings: []sdk.Finding{{RuleID: "FND-RUN-001", Severity: sdk.SeverityError, Fingerprint: "x"}}}}, wantExit: ExitFindings},
		{name: "engine error", rt: &fakeRuntime{err: errors.New("boom")}, wantExit: ExitInternal, wantErr: errors.New("boom")},
		{name: "nil engine unavailable", nilRT: true, wantExit: ExitUnavailable, wantErr: ErrUnavailable},
		{name: "bad format", rt: &fakeRuntime{}, req: RuntimeRequest{DoctorRequest: DoctorRequest{Format: "nope"}}, wantExit: ExitUnavailable, wantErr: errors.New("invalid format")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			if !tt.nilRT {
				svc.Runtime = tt.rt
			}
			var out bytes.Buffer
			code, err := Runtime(context.Background(), svc, tt.req, &out)
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

func TestRuntimeReadinessIgnoresFilters(t *testing.T) {
	warn := sdk.Finding{RuleID: "FND-RUN-004", Severity: sdk.SeverityWarning, Fingerprint: "w"}
	cases := []struct {
		name string
		req  RuntimeRequest
	}{
		{"warning below --min-severity is hidden", RuntimeRequest{DoctorRequest: DoctorRequest{MinSeverity: sdk.SeverityError}}},
		{"warning at default severity", RuntimeRequest{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			svc.Runtime = &fakeRuntime{out: RunOutput{Findings: []sdk.Finding{warn}, Evaluated: []string{"FND-RUN-004", "FND-RUN-005"}}}
			var got *report.Readiness
			svc.Reporter = reporterFunc(func(_ io.Writer, _ string, r Report) error {
				got = r.Readiness
				return nil
			})
			if _, err := Runtime(context.Background(), svc, c.req, io.Discard); err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got.Blocked) != 1 || got.Blocked[0] != "FND-RUN-004" {
				t.Fatalf("blocked = %+v", got)
			}
			if len(got.Ready) != 1 || got.Ready[0] != "FND-RUN-005" {
				t.Fatalf("ready = %v", got.Ready)
			}
		})
	}
}

func TestRuntimePassesThroughAndDefaultsSelector(t *testing.T) {
	rt := &fakeRuntime{}
	svc, cfg := newSvc(&fakeEngine{})
	svc.Runtime = rt
	req := RuntimeRequest{
		Target:  RuntimeTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct", Project: "proj"},
		Vantage: runtime.VantageVNet,
		Timeout: 45 * time.Second,
	}
	if _, err := Runtime(context.Background(), svc, req, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.last.Rules) != 1 || cfg.last.Rules[0] != "FND-RUN-*" {
		t.Errorf("default selector = %v", cfg.last.Rules)
	}
	if rt.in.Target.Account != "acct" || rt.in.Target.Project != "proj" || rt.in.Vantage != runtime.VantageVNet || rt.in.Timeout != 45*time.Second {
		t.Errorf("flags not passed through: %+v", rt.in)
	}
}

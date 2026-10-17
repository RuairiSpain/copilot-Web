package app

import (
	"context"
	"io"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// TestPreflightExitCodePrecedence pins the exit-code precedence (4, 2, 3, 1, 0)
// for the preflight command with uncertain and required skips.
func TestPreflightExitCodePrecedence(t *testing.T) {
	finding := sdk.Finding{RuleID: "FND-DEP-001", Severity: sdk.SeverityError, Fingerprint: "x"}
	uncertain := sdk.Skip{RuleID: "FND-DEP-002", Reason: "uncertain: the registration of Microsoft.Bing is not evaluated"}
	required := sdk.Skip{RuleID: "FND-DEP-005", Reason: "input-unavailable: capability quota usage read; missing permission Microsoft.CognitiveServices/locations/usages/read; denied"}
	cases := []struct {
		name   string
		out    RunOutput
		strict bool
		want   int
	}{
		{"finding and uncertain skip, not strict: 1", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{uncertain}}, false, ExitFindings},
		{"finding and uncertain skip, strict: 3", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{uncertain}}, true, ExitStrictSkip},
		{"uncertain skip only, not strict: 0", RunOutput{Skipped: []sdk.Skip{uncertain}}, false, ExitOK},
		{"uncertain skip only, strict: 3", RunOutput{Skipped: []sdk.Skip{uncertain}}, true, ExitStrictSkip},
		{"required skip and uncertain skip: 2", RunOutput{Skipped: []sdk.Skip{required, uncertain}}, false, ExitUnavailable},
		{"required skip, uncertain skip and finding: 2", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{required, uncertain}}, false, ExitUnavailable},
		{"strict does not outrank a required skip: 2", RunOutput{Skipped: []sdk.Skip{required, uncertain}}, true, ExitUnavailable},
		{"strict outranks a finding: 3 not 1", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{uncertain}}, true, ExitStrictSkip},
		{"finding alone: 1", RunOutput{Findings: []sdk.Finding{finding}}, true, ExitFindings},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			svc.Preflight = &fakePreflight{out: c.out}
			code, err := Preflight(context.Background(), svc, PreflightRequest{DoctorRequest: DoctorRequest{Strict: c.strict}}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if code != c.want {
				t.Fatalf("exit = %d, want %d", code, c.want)
			}
		})
	}
}

package app

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/network/dnsstub"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Runtime exit codes follow the preflight rule: a skip is Required (exit 2)
// only when a check could not run; an uncertain skip exits 0 and 3 under
// --strict; by-design skips never change the code.
func TestRuntimeExitCodesFollowThePreflightRule(t *testing.T) {
	finding := sdk.Finding{RuleID: "FND-RUN-001", Severity: sdk.SeverityError, Fingerprint: "x"}
	uncertain := sdk.Skip{RuleID: "FND-RUN-005", Reason: "uncertain: agent support is not listed on the project endpoint"}
	internal := sdk.Skip{RuleID: "FND-RUN-004", Reason: "uncertain: internal error: a request was refused by the read-only allow-list"}
	unavailable := sdk.Skip{RuleID: "FND-RUN-001", Reason: "input-unavailable: capability roleAssignments; missing permission Microsoft.Authorization/roleAssignments/read"}
	byDesign := sdk.Skip{RuleID: "FND-RUN-008", Reason: sdk.SkipNotImplemented}
	cases := []struct {
		name   string
		out    RunOutput
		strict bool
		want   int
	}{
		{"uncertain only", RunOutput{Skipped: []sdk.Skip{uncertain}}, false, ExitOK},
		{"uncertain only, strict", RunOutput{Skipped: []sdk.Skip{uncertain}}, true, ExitStrictSkip},
		{"input-unavailable", RunOutput{Skipped: []sdk.Skip{unavailable}}, false, ExitUnavailable},
		{"input-unavailable, strict does not outrank it", RunOutput{Skipped: []sdk.Skip{unavailable}}, true, ExitUnavailable},
		{"finding and uncertain", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{uncertain}}, false, ExitFindings},
		{"finding and uncertain, strict", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{uncertain}}, true, ExitStrictSkip},
		{"unavailable outranks a finding", RunOutput{Findings: []sdk.Finding{finding}, Skipped: []sdk.Skip{unavailable, uncertain}}, false, ExitUnavailable},
		{"an internal error is a check that did not run", RunOutput{Skipped: []sdk.Skip{internal}}, false, ExitUnavailable},
		{"by-design skip, strict, exits 0", RunOutput{Skipped: []sdk.Skip{byDesign}}, true, ExitOK},
		{"by-design plus uncertain, not strict", RunOutput{Skipped: []sdk.Skip{byDesign, uncertain}}, false, ExitOK},
		{"finding alone", RunOutput{Findings: []sdk.Finding{finding}}, true, ExitFindings},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			svc.Runtime = &fakeRuntime{out: c.out}
			code, err := Runtime(context.Background(), svc, RuntimeRequest{DoctorRequest: DoctorRequest{Strict: c.strict}}, &bytes.Buffer{})
			if err != nil || code != c.want {
				t.Fatalf("exit = %d (err %v), want %d", code, err, c.want)
			}
		})
	}
}

func TestRuntimeSkipRequired(t *testing.T) {
	for reason, want := range map[string]bool{
		"uncertain: something could not be proven":                        false,
		"uncertain: internal error: a response did not have the expected": true,
		"input-unavailable: capability x":                                 true,
		sdk.SkipNotImplemented:                                            false,
		"":                                                                true,
	} {
		if got := runtimeSkipRequired(sdk.Skip{Reason: reason}); got != want {
			t.Errorf("runtimeSkipRequired(%q) = %v, want %v", reason, got, want)
		}
	}
}

// The production resolver must map real resolver errors: an NXDOMAIN answer is
// a finding for a private-endpoint-backed account, and a lookup that runs out
// of time is a skip (it proves nothing), never a finding.
func TestRuntimeProductionResolverMapsRealDNSErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte("name: demo\nservices:\n  support-agent:\n    host: azure.ai.agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, timeout time.Duration, nxdomain bool) (int, string) {
		t.Helper()
		hc := &http.Client{Transport: &pvRT{t: t, routes: pvRoutes()}}
		var stderr bytes.Buffer
		o := Options{
			Getenv:     func(string) string { return "" },
			AzdVersion: func(context.Context) string { return "1.35.0" },
			RuntimeClient: func() (azure.Client, error) {
				a, err := azure.New(azure.Options{Credential: tokenSentinelCred{}, Endpoint: "https://management.test", LogAnalyticsEndpoint: "https://la.test", HTTPClient: hc,
					Now: func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) error { return nil }})
				if err != nil {
					return azure.Client{}, err
				}
				return a.Client(), nil
			},
			RuntimeHTTP: hc, RuntimeCredential: tokenSentinelCred{},
		}
		if nxdomain {
			o.RuntimeNetResolver = dnsstub.NXDomain(t)
		} else {
			o.RuntimeNetResolver = dnsstub.Silent(t)
		}
		svc := NewServices(&stderr, o)
		var out bytes.Buffer
		code, err := Runtime(context.Background(), svc, RuntimeRequest{
			DoctorRequest: DoctorRequest{Dir: dir, Format: "json", Rules: []string{"FND-RUN-002"}},
			Target:        RuntimeTarget{SubscriptionID: pvSub, ResourceGroup: "rg", Account: "acct", Project: "proj"},
			Vantage:       runtime.VantageVNet, Timeout: timeout,
		}, &out)
		if err != nil {
			t.Fatalf("runtime: %v\n%s", err, stderr.String())
		}
		return code, out.String()
	}
	t.Run("NXDOMAIN is a finding", func(t *testing.T) {
		_, text := run(t, 5*time.Second, true)
		if !strings.Contains(text, "NXDOMAIN") {
			t.Fatalf("an NXDOMAIN answer for a private-endpoint-backed account must be reported:\n%s", text)
		}
	})
	t.Run("a timeout is a skip, never an NXDOMAIN", func(t *testing.T) {
		code, text := run(t, 300*time.Millisecond, false)
		if strings.Contains(text, "NXDOMAIN") {
			t.Fatalf("a timeout must not read as NXDOMAIN:\n%s", text)
		}
		if code != ExitUnavailable || !strings.Contains(text, "timeout") {
			t.Fatalf("exit = %d, want %d with a timeout skip:\n%s", code, ExitUnavailable, text)
		}
	})
}

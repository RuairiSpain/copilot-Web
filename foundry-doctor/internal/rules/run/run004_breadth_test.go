package run

import (
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// mapPolicy is an in-memory sdk.Policy.
type mapPolicy map[string]any

func (m mapPolicy) Get(k string) (any, bool) { v, ok := m[k]; return v, ok }

func inputWithPolicy(p mapPolicy) *sdk.Input {
	in := baseInput()
	if p != nil {
		in.Policy = p
	}
	return in
}

func statusOf(n429, n200 float64, dep string) azure.MetricResult {
	return azure.MetricResult{Series: []azure.MetricSeries{
		mseries(n429, "statuscode", "429", "modeldeploymentname", dep),
		mseries(n200, "statuscode", "200", "modeldeploymentname", dep),
	}}
}

var okDeployment = azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded"}

func TestRun004SustainedRateNotSingleTransient429(t *testing.T) {
	cases := []struct {
		name         string
		n429, n200   float64
		policy       mapPolicy
		wantFinding  bool
		wantNoteWith string // uncertain note when no finding; empty: clean pass
	}{
		{"one transient 429 among 499 requests passes", 1, 499, nil, false, ""},
		{"exactly at the default 5 percent passes", 25, 475, nil, false, ""},
		{"just above 5 percent fails", 26, 474, nil, true, ""},
		{"429s on a tiny sample are uncertain, not a failure", 5, 35, nil, false, "below the minimum of 100 requests"},
		{"a single 429 on a single request is uncertain", 1, 0, nil, false, "below the minimum of 100 requests"},
		{"policy raises the allowed rate", 100, 400, mapPolicy{PolicyThrottleMaxRate: 0.5}, false, ""},
		{"policy lowers the allowed rate", 10, 490, mapPolicy{PolicyThrottleMaxRate: 0.01}, true, ""},
		{"policy lowers minRequests so a small sample is judged", 20, 20, mapPolicy{PolicyThrottleMinRequests: 10}, true, ""},
		{"policy raises minRequests so a mid sample is not judged", 60, 440, mapPolicy{PolicyThrottleMinRequests: 1000}, false, "below the minimum of 1000 requests"},
		{"float policy values from YAML decoding are accepted", 60, 440, mapPolicy{PolicyThrottleMinRequests: float64(200), PolicyThrottleMaxRate: float64(0.2)}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := monitorFor(azure.MetricResult{}, statusOf(c.n429, c.n200, "gpt-main"), azure.MetricResult{})
			res, err := evalRule(t, "FND-RUN-004", deployDeps(m, okDeployment), inputWithPolicy(c.policy))
			if err != nil {
				t.Fatal(err)
			}
			if c.wantFinding {
				if len(res.Findings) != 1 || res.Findings[0].Key != "throttling-429" {
					t.Fatalf("want one throttling finding, got %+v", res)
				}
				ev := res.Findings[0].Evidence
				for _, want := range []string{"HTTP 429 responses of", "requests (", "minutes", "thresholds: rate above", "runtime.throttling.maxRate429", "runtime.throttling.minRequests"} {
					if !strings.Contains(ev, want) {
						t.Errorf("evidence %q lacks %q", ev, want)
					}
				}
				return
			}
			if len(res.Findings) != 0 {
				t.Fatalf("unexpected findings %+v", res.Findings)
			}
			if c.wantNoteWith == "" {
				if res.Skipped != nil {
					t.Fatalf("want pass, got skip %+v", res.Skipped)
				}
				return
			}
			if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.wantNoteWith) {
				t.Fatalf("skip = %+v, want uncertain containing %q", res.Skipped, c.wantNoteWith)
			}
		})
	}
}

func TestRun004WindowPolicyIsSentToTheMonitorQuery(t *testing.T) {
	var windows []time.Duration
	m := fakeMonitor{metrics: func(q azure.MetricQuery) (azure.MetricResult, error) {
		windows = append(windows, q.Window)
		if q.MetricName == "AzureOpenAIProvisionedManagedUtilizationV2" {
			return azure.MetricResult{}, nil
		}
		return statusOf(0, 500, "gpt-main"), nil
	}}
	for _, c := range []struct {
		name   string
		policy mapPolicy
		want   time.Duration
		note   string
	}{
		{"default is 60 minutes", nil, time.Hour, ""},
		{"policy window", mapPolicy{PolicyThrottleWindow: 15}, 15 * time.Minute, ""},
		{"too small falls back with a note", mapPolicy{PolicyThrottleWindow: 1}, time.Hour, PolicyThrottleWindow},
		{"too large falls back with a note", mapPolicy{PolicyThrottleWindow: 100000}, time.Hour, PolicyThrottleWindow},
		{"non-number falls back with a note", mapPolicy{PolicyThrottleWindow: "soon"}, time.Hour, PolicyThrottleWindow},
	} {
		t.Run(c.name, func(t *testing.T) {
			windows = nil
			res, err := evalRule(t, "FND-RUN-004", deployDeps(m, okDeployment), inputWithPolicy(c.policy))
			if err != nil || len(res.Findings) != 0 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if len(windows) == 0 {
				t.Fatal("no metric query was sent")
			}
			for _, w := range windows {
				if w != c.want {
					t.Errorf("window = %v, want %v", w, c.want)
				}
			}
			if c.note == "" {
				if res.Skipped != nil {
					t.Fatalf("want pass, got %+v", res.Skipped)
				}
			} else if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, c.note) {
				t.Fatalf("skip = %+v, want a note about %s", res.Skipped, c.note)
			}
		})
	}
}

func TestRun004InvalidThresholdPoliciesFallBackToDefaults(t *testing.T) {
	for name, p := range map[string]mapPolicy{
		"ratio zero":       {PolicyThrottleMaxRate: 0},
		"ratio above one":  {PolicyThrottleMaxRate: 5},
		"ratio not number": {PolicyThrottleMaxRate: "high"},
		"minRequests zero": {PolicyThrottleMinRequests: 0},
		"minRequests frac": {PolicyThrottleMinRequests: 10.5},
	} {
		t.Run(name, func(t *testing.T) {
			// 10 percent over 500 requests: fails under the defaults, so the fallback is visible.
			m := monitorFor(azure.MetricResult{}, statusOf(50, 450, "gpt-main"), azure.MetricResult{})
			res, err := evalRule(t, "FND-RUN-004", deployDeps(m, okDeployment), inputWithPolicy(p))
			if err != nil || len(res.Findings) != 1 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
		})
	}
	pol := readThrottlePolicy(inputWithPolicy(mapPolicy{PolicyThrottleMaxRate: 2}))
	if len(pol.Notes) != 1 || pol.MaxRate != DefaultMaxRate429 {
		t.Errorf("policy = %+v", pol)
	}
	if def := readThrottlePolicy(nil); def.MaxRate != 0.05 || def.WindowMin != 60 || def.MinRequests != 100 || len(def.Notes) != 0 {
		t.Errorf("defaults = %+v", def)
	}
}

func TestRun004UtilizationPeakAloneIsUncertainButWithSustained429IsAFinding(t *testing.T) {
	util := azure.MetricResult{Series: []azure.MetricSeries{mseries(100, "modeldeploymentname", "gpt-main")}}
	// peak only, healthy request mix
	res, err := evalRule(t, "FND-RUN-004", deployDeps(monitorFor(azure.MetricResult{}, statusOf(0, 500, "gpt-main"), util), okDeployment), baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) || !strings.Contains(res.Skipped.Reason, "a single peak is not treated as a failure") {
		t.Fatalf("peak only: res=%+v err=%v", res, err)
	}
	// peak below 100 percent is quiet
	low := azure.MetricResult{Series: []azure.MetricSeries{mseries(99, "modeldeploymentname", "gpt-main")}}
	res, err = evalRule(t, "FND-RUN-004", deployDeps(monitorFor(azure.MetricResult{}, statusOf(0, 500, "gpt-main"), low), okDeployment), baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped != nil {
		t.Fatalf("below 100: res=%+v err=%v", res, err)
	}
	// peak plus a sustained 429 rate: both findings
	res, err = evalRule(t, "FND-RUN-004", deployDeps(monitorFor(azure.MetricResult{}, statusOf(100, 400, "gpt-main"), util), okDeployment), baseInput())
	if err != nil || len(res.Findings) != 2 {
		t.Fatalf("peak with 429: res=%+v err=%v", res, err)
	}
	keys := res.Findings[0].Key + "," + res.Findings[1].Key
	if !strings.Contains(keys, "throttling-429") || !strings.Contains(keys, "utilization") {
		t.Errorf("keys = %s", keys)
	}
}

func TestRun004DeploymentStateSignals(t *testing.T) {
	healthy := monitorFor(azure.MetricResult{}, statusOf(0, 500, "gpt-main"), azure.MetricResult{})
	cases := []struct {
		name        string
		d           azure.AccountDeployment
		wantFinding string // Key
		wantNote    string
	}{
		{"running passes", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded", DeploymentState: "Running"}, "", ""},
		{"absent state passes", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded"}, "", ""},
		{"paused is a finding", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded", DeploymentState: "Paused"}, "deployment-state", ""},
		{"paused is case-insensitive", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded", DeploymentState: "paused"}, "deployment-state", ""},
		{"unknown state is uncertain", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Succeeded", DeploymentState: "Draining"}, "", "unrecognised deploymentState"},
		{"disabled provisioning fails", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Disabled"}, "provisioning-state", ""},
		{"creating is uncertain", azure.AccountDeployment{Name: "gpt-main", ProvisioningState: "Creating"}, "", "provisioningState is Creating"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-004", deployDeps(healthy, c.d), baseInput())
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.wantFinding != "":
				if len(res.Findings) != 1 || res.Findings[0].Key != c.wantFinding {
					t.Fatalf("res = %+v", res)
				}
			case c.wantNote != "":
				if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, c.wantNote) {
					t.Fatalf("res = %+v", res)
				}
			default:
				if len(res.Findings) != 0 || res.Skipped != nil {
					t.Fatalf("res = %+v", res)
				}
			}
		})
	}
}

func TestRun004KeysAreStableAcrossNumbers(t *testing.T) {
	get := func(n429, n200 float64) string {
		m := monitorFor(azure.MetricResult{}, statusOf(n429, n200, "gpt-main"), azure.MetricResult{})
		res, err := evalRule(t, "FND-RUN-004", deployDeps(m, okDeployment), baseInput())
		if err != nil || len(res.Findings) != 1 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		return res.Findings[0].Key + "|" + res.Findings[0].Resource.ID
	}
	if a, b := get(60, 440), get(90, 410); a != b {
		t.Errorf("the key changed with the counts: %s vs %s", a, b)
	}
}

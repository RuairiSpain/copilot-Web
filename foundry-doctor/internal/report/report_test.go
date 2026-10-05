// Package report_test holds the cross-format tests: determinism, injection, canaries, unicode,
// empty and huge reports. Per-format golden tests live in the subpackages.
package report_test

import (
	"bytes"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/console"
	reportjson "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/json"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/markdown"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/reporttest"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/sarif"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func reporters() []sdk.Reporter {
	return []sdk.Reporter{console.New(console.Options{}), reportjson.New(), markdown.New(), sarif.New(sarif.Options{})}
}

func render(t *testing.T, rp sdk.Reporter, r *sdk.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := rp.Write(&b, r); err != nil {
		t.Fatalf("%s: %v", rp.Format(), err)
	}
	return b.String()
}

func shuffled(r *sdk.Report, seed int64) *sdk.Report {
	c := *r
	rng := rand.New(rand.NewSource(seed))
	c.Findings = append([]sdk.Finding(nil), r.Findings...)
	c.Skipped = append([]sdk.SkippedCheck(nil), r.Skipped...)
	c.Tools = append([]sdk.ToolStatus(nil), r.Tools...)
	rng.Shuffle(len(c.Findings), func(i, j int) { c.Findings[i], c.Findings[j] = c.Findings[j], c.Findings[i] })
	rng.Shuffle(len(c.Skipped), func(i, j int) { c.Skipped[i], c.Skipped[j] = c.Skipped[j], c.Skipped[i] })
	rng.Shuffle(len(c.Tools), func(i, j int) { c.Tools[i], c.Tools[j] = c.Tools[j], c.Tools[i] })
	return &c
}

func TestReportersDeterministic(t *testing.T) {
	for _, base := range []*sdk.Report{reporttest.FullReport(), reporttest.Huge(200)} {
		for _, rp := range reporters() {
			want := render(t, rp, base)
			for seed := int64(1); seed <= 8; seed++ {
				if got := render(t, rp, shuffled(base, seed)); got != want {
					t.Fatalf("%s: output depends on input order (seed %d)", rp.Format(), seed)
				}
			}
			for i := 0; i < 3; i++ {
				if got := render(t, rp, base); got != want {
					t.Fatalf("%s: repeated run differs", rp.Format())
				}
			}
		}
	}
}

func TestReportersConcurrentSafe(t *testing.T) {
	r := reporttest.FullReport()
	var wg sync.WaitGroup
	for _, rp := range reporters() {
		want := render(t, rp, r)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var b bytes.Buffer
				if err := rp.Write(&b, r); err != nil || b.String() != want {
					t.Errorf("%s: concurrent output differs: %v", rp.Format(), err)
				}
			}()
		}
	}
	wg.Wait()
}

func TestNoWallClockOrAbsolutePath(t *testing.T) {
	year := time.Now().Format("2006-")
	for _, rp := range reporters() {
		out := render(t, rp, reporttest.FullReport())
		if strings.Contains(out, "\\") && rp.Format() != "json" && rp.Format() != "markdown" && rp.Format() != "sarif" {
			t.Errorf("%s: backslash in output", rp.Format())
		}
		if strings.Contains(out, "T"+year) || strings.Contains(out, "file:///") {
			t.Errorf("%s: timestamp or absolute URI", rp.Format())
		}
	}
}

// Reporters print what they get; policy values under credential-like keys are the one place
// where a secret could arrive outside Evidence, and none of the formats may print them.
func TestCanaryPolicyValueNeverPrinted(t *testing.T) {
	for _, rp := range reporters() {
		if out := render(t, rp, reporttest.FullReport()); strings.Contains(out, reporttest.Canary) {
			t.Errorf("%s printed the canary", rp.Format())
		}
	}
	// Redaction keeps the key visible so that the reader knows the setting exists.
	if out := render(t, console.New(console.Options{}), reporttest.FullReport()); !strings.Contains(out, "apiToken = [redacted]") {
		t.Errorf("redacted key not shown:\n%s", out)
	}
}

func injectionReport() *sdk.Report {
	hostile := "\x1b[2J\x1b[31mred\x1b]0;pwned\x07 \x00 ‮|\n<img src=x onerror=alert(1)>[x](javascript:alert(1))\r@everyone"
	return &sdk.Report{
		Tool: sdk.ToolInfo{Name: "foundry-doctor\x1b[1m", Version: "1"}, Profile: "p\x1b[0m",
		EffectivePolicy: map[string]any{"k\x1b[1m": hostile},
		Findings: []sdk.Finding{{
			RuleID: "FND-X-001" + "\x1b[1m", Severity: sdk.SeverityError, Profile: "p", Evidence: hostile, Recommendation: hostile, Fix: hostile,
			DocsURL: "javascript:alert(1)", Fingerprint: "fp1:ee", Confidence: sdk.ConfidenceCertain,
			Resource: sdk.ResourceRef{Name: hostile}, Location: sdk.Location{File: "a\x1b[1m.yaml", Line: 1},
			Suppressed: &sdk.Suppression{Reason: hostile, Expires: "2030-01-01"},
		}},
		Skipped: []sdk.SkippedCheck{{RuleID: "FND-Y-001", Reason: hostile, Detail: hostile, MissingCapability: hostile}},
	}
}

func TestInjectionStripped(t *testing.T) {
	for _, rp := range reporters() {
		if rp.Format() == "json" {
			continue // JSON is the report as is; encoding/json escapes control characters.
		}
		out := render(t, rp, injectionReport())
		for _, bad := range []string{"\x1b", "\x07", "\x00", "‮", "\r"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: output contains %q", rp.Format(), bad)
			}
		}
		if strings.Contains(out, "pwned") && rp.Format() != "sarif" {
			// The OSC payload is a title string and must disappear with its escape.
			t.Errorf("%s: OSC payload survived", rp.Format())
		}
		if rp.Format() == "markdown" {
			for _, bad := range []string{"<img", "](javascript:", "@everyone", "[x]("} {
				if strings.Contains(out, bad) {
					t.Errorf("markdown: raw %q survived:\n%s", bad, out)
				}
			}
		}
	}
	if out := render(t, reportjson.New(), injectionReport()); strings.ContainsAny(out, "\x1b\x07\x00\r") {
		t.Error("json contains raw control characters")
	}
}

func TestUnicodeSurvives(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "FND-U-001", Severity: sdk.SeverityInfo, Evidence: "日本語 café \U0001F600", Location: sdk.Location{File: "infra/日本.bicep", Line: 1}, Fingerprint: "fp1:u"}}}
	for _, rp := range reporters() {
		out := render(t, rp, r)
		if !strings.Contains(out, "日本語 café \U0001F600") && rp.Format() != "json" {
			t.Errorf("%s lost unicode evidence", rp.Format())
		}
		if !strings.Contains(out, "日本語") {
			t.Errorf("%s lost unicode", rp.Format())
		}
	}
}

func TestEmptyReports(t *testing.T) {
	for _, r := range []*sdk.Report{nil, {}} {
		for _, rp := range reporters() {
			if out := render(t, rp, r); out == "" {
				t.Errorf("%s: empty output", rp.Format())
			}
		}
	}
}

func TestHugeReport(t *testing.T) {
	r := reporttest.Huge(30000)
	for _, rp := range reporters() {
		if out := render(t, rp, r); len(out) < 1000 {
			t.Errorf("%s: short output", rp.Format())
		}
	}
	// SARIF caps at 25,000 and records the truncation.
	out := render(t, sarif.New(sarif.Options{}), r)
	if got := strings.Count(out, `"ruleIndex"`); got != sarif.DefaultMaxResults {
		t.Errorf("sarif results = %d", got)
	}
	if !strings.Contains(out, "Results truncated: 5000 of 30000") {
		t.Error("truncation notification missing")
	}
}

func TestFormats(t *testing.T) {
	got := map[string]bool{}
	for _, rp := range reporters() {
		got[rp.Format()] = true
	}
	for _, f := range []string{"console", "json", "markdown", "sarif"} {
		if !got[f] {
			t.Errorf("missing %s", f)
		}
	}
}

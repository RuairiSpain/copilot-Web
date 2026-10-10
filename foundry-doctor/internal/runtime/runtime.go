// Package runtime implements the Phase 3 runtime-diagnosis rules and the
// shared probe contract they use.
//
// Runtime probes are deterministic, time-bounded and read-only. They classify
// themselves as safe-local, control-plane, data-plane or vnet-only and report
// one of four states:
//   - pass: the probe ran and found healthy evidence
//   - fail: the probe ran and found an unhealthy condition
//   - skipped: the probe could not run (missing permission, unavailable
//     vantage point, missing input)
//   - uncertain: the probe ran but could not prove the result
//
// Skipped and uncertain are never treated as pass.
package runtime

import (
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Class identifies where and how a probe runs.
type Class string

const (
	ClassSafeLocal    Class = "safe-local"
	ClassControlPlane Class = "control-plane"
	ClassDataPlane    Class = "data-plane"
	ClassVNetOnly     Class = "vnet-only"
)

// State is the outcome of one runtime probe.
type State string

const (
	StatePass      State = "pass"
	StateFail      State = "fail"
	StateSkipped   State = "skipped"
	StateUncertain State = "uncertain"
)

// UncertainPrefix marks the Skip.Reason of an uncertain runtime outcome.
const UncertainPrefix = "uncertain: "

// Vantage declares where the runtime command is running from.
type Vantage string

const (
	VantageNone  Vantage = "none"
	VantageLocal Vantage = "local"
	VantageVNet  Vantage = "vnet"
)

// Correlation ties runtime evidence back to a source node and a deployed node.
type Correlation struct {
	Resource sdk.ResourceRef
	Source   sdk.Location
}

// Result is the normalised outcome of one probe.
type Result struct {
	State       State
	Class       Class
	Correlation Correlation
	Message     string
	Details     []string
}

// RedactedMessage returns Message redacted and flattened to one line.
func (r Result) RedactedMessage() string { return clean(findings.Redact(r.Message)) }

// RedactedDetails returns a redacted copy of Details.
func (r Result) RedactedDetails() []string {
	if len(r.Details) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Details))
	for _, d := range r.Details {
		out = append(out, clean(findings.Redact(d)))
	}
	return out
}

// Timeout returns d when it is positive, else fallback.
func Timeout(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}

// Package rules defines the contract between the engine and the native rules (ADR-001).
// Rule implementations live in the sub-packages cfg, sec, net, idn, env, ops, rel and cost.
// The engine, not the rule, stamps Severity, Profile and Fingerprint (ADR-008); report and
// adapter packages must not import any rule package.
package rules

import (
	"context"
	"fmt"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Rule is one catalogue rule implemented in Go.
type Rule interface {
	// ID is the catalogue ID, for example FND-SEC-001.
	ID() string
	// Version is the catalogue version the code implements. The engine refuses a mismatch.
	Version() int
	// Evaluate inspects the input and returns zero or more results. It must be read-only,
	// deterministic and free of I/O beyond the Input. A returned error is an internal error (exit 4);
	// "cannot tell" is a Skip or Uncertain result, never an error.
	Evaluate(ctx context.Context, in *model.Input) ([]Result, error)
}

// Result is one outcome of a rule. Exactly one of Finding (fail, uncertain) or Skip (skipped) is set,
// and neither for pass. Rules never set Severity, Profile, Category, Pillar, Basis, DocsURL,
// LastVerified or Fingerprint on a Finding; they set RuleID, RuleVersion, Resource, Location,
// Evidence, Recommendation, Fix, Confidence and Key.
type Result struct {
	Status  sdk.Status
	Finding *sdk.Finding
	Skip    *sdk.SkippedCheck
}

// Pass returns a pass result.
func Pass() Result { return Result{Status: sdk.StatusPass} }

// Fail returns a fail result for f.
func Fail(f sdk.Finding) Result { return Result{Status: sdk.StatusFail, Finding: &f} }

// Uncertain returns an uncertain result for f; f.Confidence should be uncertain.
func Uncertain(f sdk.Finding) Result { return Result{Status: sdk.StatusUncertain, Finding: &f} }

// Skipped returns a skipped result.
func Skipped(s sdk.SkippedCheck) Result { return Result{Status: sdk.StatusSkipped, Skip: &s} }

// Skip reasons (ADR-007, ADR-012). Reasons with a parameter are built with the helper functions.
const (
	ReasonSyntheticInfrastructure = "synthetic-infrastructure"
	ReasonControlPlaneUnavailable = "control-plane-unavailable"
	ReasonUnsupportedVersion      = "unsupported-version"
	ReasonNotApplicable           = "not-applicable"

	reasonProfileKeyMissing = "profile-key-missing:"
	reasonToolMissing       = "tool-missing:"
	reasonUnresolvedValue   = "unresolved-value:"
)

// ReasonProfileKeyMissing is "profile-key-missing:<key>"; use the model.Key* constants for key.
func ReasonProfileKeyMissing(key string) string { return reasonProfileKeyMissing + key }

// ReasonToolMissing is "tool-missing:<name>".
func ReasonToolMissing(name string) string { return reasonToolMissing + name }

// ReasonUnresolvedValue is "unresolved-value:<why>".
func ReasonUnresolvedValue(why string) string { return reasonUnresolvedValue + why }

// SkipFor builds a skipped result for a rule.
func SkipFor(r Rule, reason, detail, missing string, res sdk.ResourceRef) Result {
	return Skipped(sdk.SkippedCheck{
		RuleID: r.ID(), RuleVersion: r.Version(), Reason: reason,
		Detail: detail, MissingCapability: missing, Resource: res,
	})
}

// NewFinding starts a finding for a rule with the fields a rule owns.
func NewFinding(r Rule, res sdk.ResourceRef, evidence, recommendation string) sdk.Finding {
	return sdk.Finding{
		RuleID: r.ID(), RuleVersion: r.Version(), Resource: res,
		Evidence: evidence, Recommendation: recommendation, Confidence: sdk.ConfidenceCertain,
	}
}

// Validate checks that a result is well formed for its status.
func (r Result) Validate() error {
	switch r.Status {
	case sdk.StatusPass:
		if r.Finding != nil || r.Skip != nil {
			return fmt.Errorf("pass result must carry no finding or skip")
		}
	case sdk.StatusFail, sdk.StatusUncertain:
		if r.Finding == nil || r.Skip != nil {
			return fmt.Errorf("%s result needs a finding and no skip", r.Status)
		}
		f := r.Finding
		if f.Severity != "" || f.Profile != "" || f.Fingerprint != "" {
			return fmt.Errorf("rule %s set Severity, Profile or Fingerprint; the engine owns them", f.RuleID)
		}
	case sdk.StatusSkipped:
		if r.Skip == nil || r.Finding != nil || r.Skip.Reason == "" {
			return fmt.Errorf("skipped result needs a skip with a reason and no finding")
		}
	default:
		return fmt.Errorf("unknown status %q", r.Status)
	}
	return nil
}

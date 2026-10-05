package rules

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Profile names (ADR-010 decision 6). Reports and Config.Profile use the long form.
const (
	ProfileDev  = "foundry-dev"
	ProfileTest = "foundry-test"
	ProfileProd = "foundry-prod"
)

// NormalizeProfile accepts dev|test|prod or foundry-dev|foundry-test|foundry-prod and returns the long form.
func NormalizeProfile(s string) (string, error) {
	switch s {
	case "dev", ProfileDev:
		return ProfileDev, nil
	case "test", ProfileTest:
		return ProfileTest, nil
	case "prod", ProfileProd:
		return ProfileProd, nil
	}
	return "", fmt.Errorf("unknown profile %q: want dev, test or prod", s)
}

// Stamper is the engine's view of the findings package (ADR-008): it is consumed here and implemented
// there, so this package does not import it.
type Stamper interface {
	// Fingerprint computes the finding fingerprint. The executor calls it after every other field is stamped.
	Fingerprint(f sdk.Finding) string
	// Redact removes secret-shaped content from free text (evidence, recommendation, fix, panic messages).
	Redact(s string) string
}

// SeverityFor returns the severity of rule m under a long-form profile. Platform-basis rules are
// errors in every profile (PRD section 9).
func SeverityFor(m catalog.Rule, profile string) (sdk.Severity, error) {
	if slices.Contains(m.Basis, "platform") {
		return sdk.SeverityError, nil
	}
	var v string
	switch profile {
	case ProfileDev:
		v = m.Severity.Dev
	case ProfileTest:
		v = m.Severity.Test
	case ProfileProd:
		v = m.Severity.Prod
	default:
		return "", fmt.Errorf("unknown profile %q", profile)
	}
	sev, err := sdk.ParseSeverity(v)
	if err != nil {
		return "", fmt.Errorf("%s: %w", m.ID, err)
	}
	return sev, nil
}

// ExecConfig configures Execute.
type ExecConfig struct {
	Profile string // long form
	Stamper Stamper
	// StrictVersions skips a rule with a version range when the tool's version was not detected.
	// By default an undetected version does not gate the rule; only a detected version outside the range does.
	StrictVersions bool
}

// RuleError is an internal error: a rule returned an error, panicked or returned a malformed result.
// Any RuleError makes the run exit 4 (PRD section 7).
type RuleError struct {
	RuleID string
	Panic  bool
	Err    error
}

func (e RuleError) Error() string {
	kind := "error"
	if e.Panic {
		kind = "panic"
	}
	return fmt.Sprintf("internal %s in rule %s: %v", kind, e.RuleID, e.Err)
}

// Unwrap returns the underlying error.
func (e RuleError) Unwrap() error { return e.Err }

// Outcome is the deterministic result of Execute. Findings and Skipped are sorted.
type Outcome struct {
	Findings []sdk.Finding      // stamped
	Skipped  []sdk.SkippedCheck // never counted as passed
	Passed   int                // number of Pass results returned by rules
	Executed int                // rules whose Evaluate ran
	Errors   []RuleError        // internal errors, in rule order
	NotRun   []string           // rules not reached because the context ended, in rule order
	// Interrupted is the context error when the run was cancelled or timed out.
	Interrupted error
}

// Execute runs the entries in ID order, sequentially, so output does not depend on scheduling. Each rule
// is gated (version, input availability), evaluated with panic recovery, validated and stamped.
// The error return is for invalid configuration only; rule failures are in Outcome.Errors.
func Execute(ctx context.Context, in *model.Input, entries []Entry, cfg ExecConfig) (Outcome, error) {
	var out Outcome
	if cfg.Stamper == nil {
		return out, errors.New("execute: nil Stamper")
	}
	if in == nil {
		return out, errors.New("execute: nil input")
	}
	if long, err := NormalizeProfile(cfg.Profile); err != nil || long != cfg.Profile {
		return out, fmt.Errorf("execute: profile must be the long form foundry-dev|foundry-test|foundry-prod, got %q", cfg.Profile)
	}
	sorted := slices.Clone(entries)
	slices.SortStableFunc(sorted, func(a, b Entry) int { return strings.Compare(a.Meta.ID, b.Meta.ID) })

	for i, e := range sorted {
		if !e.Executable() {
			continue
		}
		if err := ctx.Err(); err != nil {
			out.Interrupted = err
			for _, rest := range sorted[i:] {
				if rest.Executable() {
					out.NotRun = append(out.NotRun, rest.Meta.ID)
				}
			}
			break
		}
		if skip := gate(in, e, cfg.StrictVersions); skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		out.Executed++
		results, rerr := safeEvaluate(ctx, e.Impl, in)
		if rerr != nil {
			rerr.Err = redactErr(cfg.Stamper, rerr.Err)
			out.Errors = append(out.Errors, *rerr)
			continue
		}
		if err := ctx.Err(); err != nil {
			// The rule may have returned partial work after cancellation; discard it.
			out.Interrupted = err
			out.Executed--
			for _, rest := range sorted[i:] {
				if rest.Executable() {
					out.NotRun = append(out.NotRun, rest.Meta.ID)
				}
			}
			break
		}
		if err := absorb(&out, e, results, cfg); err != nil {
			out.Errors = append(out.Errors, RuleError{RuleID: e.Meta.ID, Err: redactErr(cfg.Stamper, err)})
		}
	}
	SortFindings(out.Findings)
	SortSkipped(out.Skipped)
	return out, nil
}

func redactErr(st Stamper, err error) error { return errors.New(st.Redact(err.Error())) }

func safeEvaluate(ctx context.Context, r Rule, in *model.Input) (res []Result, rerr *RuleError) {
	defer func() {
		if p := recover(); p != nil {
			res = nil
			rerr = &RuleError{RuleID: r.ID(), Panic: true, Err: fmt.Errorf("panic: %v", p)}
		}
	}()
	res, err := r.Evaluate(ctx, in)
	if err != nil {
		return nil, &RuleError{RuleID: r.ID(), Err: fmt.Errorf("evaluate: %w", err)}
	}
	return res, nil
}

// absorb validates the results of one rule and appends them to out. Results of a rule with any
// malformed result are dropped as a whole and the rule counts as an internal error.
func absorb(out *Outcome, e Entry, results []Result, cfg ExecConfig) error {
	sev, err := SeverityFor(e.Meta, cfg.Profile)
	if err != nil {
		return err
	}
	var findings []sdk.Finding
	var skips []sdk.SkippedCheck
	passed := 0
	for i, r := range results {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("result %d: %w", i, err)
		}
		switch r.Status {
		case sdk.StatusPass:
			passed++
		case sdk.StatusSkipped:
			s := *r.Skip
			if s.RuleID == "" {
				s.RuleID = e.Meta.ID
			}
			if s.RuleVersion == 0 {
				s.RuleVersion = e.Meta.Version
			}
			if s.RuleID != e.Meta.ID || s.RuleVersion != e.Meta.Version {
				return fmt.Errorf("result %d: skip names %s v%d, rule is %s v%d", i, s.RuleID, s.RuleVersion, e.Meta.ID, e.Meta.Version)
			}
			s.Detail = cfg.Stamper.Redact(s.Detail)
			skips = append(skips, s)
		default:
			f := *r.Finding
			if f.RuleID != e.Meta.ID || f.RuleVersion != e.Meta.Version {
				return fmt.Errorf("result %d: finding names %s v%d, rule is %s v%d", i, f.RuleID, f.RuleVersion, e.Meta.ID, e.Meta.Version)
			}
			findings = append(findings, stamp(f, r.Status, e.Meta, sev, cfg))
		}
	}
	out.Findings = append(out.Findings, findings...)
	out.Skipped = append(out.Skipped, skips...)
	out.Passed += passed
	return nil
}

// stamp sets the engine-owned fields (ADR-008) from the catalogue and the profile.
func stamp(f sdk.Finding, st sdk.Status, m catalog.Rule, sev sdk.Severity, cfg ExecConfig) sdk.Finding {
	f.Severity = sev
	f.Profile = cfg.Profile
	f.Category = sdk.Category(m.Category)
	f.Pillar = m.Pillar
	f.Basis = slices.Clone(m.Basis)
	if len(m.Sources) > 0 {
		f.DocsURL = m.Sources[0].URL
		for _, s := range m.Sources {
			if s.LastVerified > f.LastVerified {
				f.LastVerified = s.LastVerified
			}
		}
	}
	if f.Recommendation == "" {
		f.Recommendation = m.Recommendation
	}
	if f.Fix == "" {
		f.Fix = m.Fix
	}
	switch {
	case st == sdk.StatusUncertain && (f.Confidence == "" || f.Confidence == sdk.ConfidenceCertain):
		f.Confidence = sdk.ConfidenceUncertain
	case f.Confidence == "":
		f.Confidence = sdk.ConfidenceCertain
	}
	f.Evidence = cfg.Stamper.Redact(f.Evidence)
	f.Recommendation = cfg.Stamper.Redact(f.Recommendation)
	f.Fix = cfg.Stamper.Redact(f.Fix)
	f.Suppressed, f.Baselined, f.Adapter = nil, false, ""
	f.Fingerprint = cfg.Stamper.Fingerprint(f)
	return f
}

// Offline input planes (catalogue "inputs") that Phase 1 can read.
func offlinePlane(p string) bool { return p == "azure.yaml" || p == "bicep-arm" || p == "azd-env" }

// gate returns a skipped check when the rule must not run: its input is not available (ADR-012) or the
// detected tool versions are outside the catalogue ranges (ADR-007, PRD section 9).
func gate(in *model.Input, e Entry, strict bool) *sdk.SkippedCheck {
	m := e.Meta
	skip := func(reason, detail, missing string) *sdk.SkippedCheck {
		return &sdk.SkippedCheck{RuleID: m.ID, RuleVersion: m.Version, Reason: reason, Detail: detail, MissingCapability: missing}
	}
	if s := versionGate(in.Versions, m, strict); s != "" {
		return skip(ReasonUnsupportedVersion, s, "")
	}
	hasOffline, needsARMOnly := false, slices.Contains(m.Inputs, "bicep-arm")
	for _, p := range m.Inputs {
		if offlinePlane(p) {
			hasOffline = true
		}
		if p == "azure.yaml" || p == "azd-env" {
			needsARMOnly = false
		}
	}
	if !hasOffline {
		return skip(ReasonControlPlaneUnavailable, "inputs "+strings.Join(m.Inputs, ", ")+" are not read in offline mode", "live Azure access")
	}
	if needsARMOnly && in.ARM == nil {
		if in.Project.HasARM() {
			return skip(ReasonUnresolvedValue("compiled-arm-unavailable"), "the ARM template could not be compiled; see the engine diagnostics", "compiled ARM template")
		}
		return skip(ReasonSyntheticInfrastructure, "iac: "+string(in.Project.IaC), "compiled ARM template")
	}
	return nil
}

// versionGate returns a non-empty detail when a detected version is outside the rule's closed range.
func versionGate(v model.Versions, m catalog.Rule, strict bool) string {
	check := func(tool, have, rng string) string {
		if rng == "" {
			return ""
		}
		if have == "" {
			if strict {
				return tool + " version not detected; rule verified for " + rng
			}
			return ""
		}
		ok, err := InRange(have, rng)
		switch {
		case err != nil:
			return fmt.Sprintf("%s version %q cannot be compared with %q", tool, have, rng)
		case !ok:
			return fmt.Sprintf("%s %s is outside the verified range %s", tool, have, rng)
		}
		return ""
	}
	if d := check("azd", v.Azd, m.Compatibility.Azd); d != "" {
		return d
	}
	for _, name := range sortedKeys(m.Compatibility.Extensions) {
		if d := check("extension "+name, v.Extensions[name], m.Compatibility.Extensions[name]); d != "" {
			return d
		}
	}
	return ""
}

// SortFindings orders findings by rule, resource, key, location and fingerprint.
func SortFindings(fs []sdk.Finding) {
	slices.SortStableFunc(fs, func(a, b sdk.Finding) int {
		return firstNonZero(
			strings.Compare(a.RuleID, b.RuleID),
			compareResource(a.Resource, b.Resource),
			strings.Compare(a.Key, b.Key),
			strings.Compare(a.Location.File, b.Location.File),
			a.Location.Line-b.Location.Line,
			a.Location.Column-b.Location.Column,
			strings.Compare(a.Fingerprint, b.Fingerprint),
			strings.Compare(a.Evidence, b.Evidence),
		)
	})
}

// SortSkipped orders skipped checks by rule, reason, resource and detail.
func SortSkipped(ss []sdk.SkippedCheck) {
	slices.SortStableFunc(ss, func(a, b sdk.SkippedCheck) int {
		return firstNonZero(
			strings.Compare(a.RuleID, b.RuleID),
			strings.Compare(a.Reason, b.Reason),
			compareResource(a.Resource, b.Resource),
			strings.Compare(a.Detail, b.Detail),
		)
	})
}

func compareResource(a, b sdk.ResourceRef) int {
	return firstNonZero(
		strings.Compare(a.Kind, b.Kind), strings.Compare(a.Type, b.Type), strings.Compare(a.Name, b.Name),
		strings.Compare(a.ID, b.ID), strings.Compare(a.Pointer, b.Pointer),
	)
}

func firstNonZero(vs ...int) int {
	for _, v := range vs {
		if v != 0 {
			return v
		}
	}
	return 0
}

// SkipSummary counts skipped checks by their full reason string, for Summary.SkippedByReason. The map is
// never nil. Parameterised reasons keep their parameter (for example "profile-key-missing:policy.tags.required").
func SkipSummary(ss []sdk.SkippedCheck) map[string]int {
	m := make(map[string]int)
	for _, s := range ss {
		m[s.Reason]++
	}
	return m
}

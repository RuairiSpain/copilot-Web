// Package engine assembles a run: the default rule registry, engine diagnostics (ADR-009),
// fingerprint merging (ADR-008), adapter de-duplication (ADR-001) and the report with its
// exit code (ADR-010). Rule evaluation itself lives in internal/rules.
package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Engine diagnostic IDs (ADR-009). Adding one is an ADR amendment.
const (
	DiagSuppressionExpired    = "FND-SYS-SUPPRESSION-EXPIRED"
	DiagSuppressionInvalid    = "FND-SYS-SUPPRESSION-INVALID"
	DiagInvalidAzureYAML      = "FND-SYS-INVALID-AZURE-YAML"
	DiagUnsupportedHost       = "FND-SYS-UNSUPPORTED-HOST"
	DiagAdapterUnavailable    = "FND-SYS-ADAPTER-UNAVAILABLE"
	DiagDuplicateFingerprint  = "FND-SYS-DUPLICATE-FINGERPRINT"
	bicepPrefix               = "bicep/"
	diagnosticRuleVersion     = 1
	dateLayout                = "2006-01-02"
	engineDiagnosticsFileKind = "file"
)

// SystemDiagnostic describes one reserved engine ID.
type SystemDiagnostic struct {
	ID       string
	Severity sdk.Severity
	Meaning  string // the built-in text printed by explain
}

// SystemDiagnostics returns the reserved IDs of ADR-009 in ID order, with their fixed severities.
func SystemDiagnostics() []SystemDiagnostic {
	return []SystemDiagnostic{
		{DiagAdapterUnavailable, sdk.SeverityInfo, "An optional adapter is missing or disabled; dependent checks are skipped."},
		{DiagDuplicateFingerprint, sdk.SeverityWarning, "Two distinct findings collapsed to one fingerprint; the rule's key does not distinguish them."},
		{DiagInvalidAzureYAML, sdk.SeverityError, "azure.yaml failed to parse or violates the schema; dependent rules are skipped."},
		{DiagSuppressionExpired, sdk.SeverityError, "A suppression's expiry date has passed. The suppressed finding is visible again."},
		{DiagSuppressionInvalid, sdk.SeverityError, "A suppression lacks a reason or an expiry, or is malformed."},
		{DiagUnsupportedHost, sdk.SeverityWarning, "A service host the engine does not know; rules for it are skipped."},
	}
}

// IsSystemID reports whether id is a reserved engine ID (FND-SYS-*), listed or not.
func IsSystemID(id string) bool { return strings.HasPrefix(id, "FND-SYS-") }

// IsBicepID reports whether id is a compiler diagnostic ID (bicep/<code>).
func IsBicepID(id string) bool {
	return strings.HasPrefix(id, bicepPrefix) && len(id) > len(bicepPrefix)
}

// SystemSeverity returns the fixed severity of a listed engine ID.
func SystemSeverity(id string) (sdk.Severity, bool) {
	for _, d := range SystemDiagnostics() {
		if d.ID == id {
			return d.Severity, true
		}
	}
	return "", false
}

// Diagnostic is the input to NewDiagnostic.
type Diagnostic struct {
	ID             string       // a listed FND-SYS-* ID, or bicep/<code>
	Severity       sdk.Severity // used only for bicep/<code>; engine IDs have fixed severities
	Resource       sdk.ResourceRef
	Location       sdk.Location
	Evidence       string
	Recommendation string
	Key            string
	Confidence     sdk.Confidence
}

// NewDiagnostic builds an engine diagnostic as an ordinary finding (ADR-009 decision 4): Category and
// Basis empty, severity fixed by ID, free text redacted, fingerprint stamped. An unlisted FND-SYS ID is an error.
func NewDiagnostic(st rules.Stamper, profile string, d Diagnostic) (sdk.Finding, error) {
	if st == nil {
		return sdk.Finding{}, errors.New("diagnostic: nil Stamper")
	}
	sev := d.Severity
	switch {
	case IsSystemID(d.ID):
		fixed, ok := SystemSeverity(d.ID)
		if !ok {
			return sdk.Finding{}, fmt.Errorf("diagnostic: %s is not a listed engine ID (ADR-009)", d.ID)
		}
		sev = fixed
	case IsBicepID(d.ID):
		if !sev.Valid() {
			return sdk.Finding{}, fmt.Errorf("diagnostic %s: severity %q is not valid", d.ID, sev)
		}
	default:
		return sdk.Finding{}, fmt.Errorf("diagnostic: %q is neither FND-SYS-* nor bicep/<code>", d.ID)
	}
	conf := d.Confidence
	if conf == "" {
		conf = sdk.ConfidenceCertain
	}
	f := sdk.Finding{
		RuleID: d.ID, RuleVersion: diagnosticRuleVersion, Severity: sev, Profile: profile,
		Resource: d.Resource, Location: d.Location, Key: d.Key, Confidence: conf,
		Evidence: st.Redact(d.Evidence), Recommendation: st.Redact(d.Recommendation),
	}
	f.Fingerprint = st.Fingerprint(f)
	return f, nil
}

// FromModelDiagnostic maps a parser or compiler diagnostic (internal/model) to a finding. Source "bicep"
// gives bicep/<code> with the compiler's severity; any other source must use one of the kebab-case codes
// invalid-azure-yaml, unsupported-host, adapter-unavailable, duplicate-fingerprint.
func FromModelDiagnostic(st rules.Stamper, profile string, d model.Diagnostic) (sdk.Finding, error) {
	in := Diagnostic{
		Evidence: d.Message, Severity: d.Severity,
		Location: sdk.Location{File: d.File, Line: d.Pos.Line, Column: d.Pos.Column},
		Key:      d.Code,
	}
	if d.Source == "bicep" {
		if d.Code == "" {
			return sdk.Finding{}, errors.New("diagnostic: bicep diagnostic without a code")
		}
		in.ID = bicepPrefix + d.Code
		return NewDiagnostic(st, profile, in)
	}
	switch d.Code {
	case "invalid-azure-yaml", "unsupported-host", "adapter-unavailable", "duplicate-fingerprint":
		in.ID = "FND-SYS-" + strings.ToUpper(d.Code)
	default:
		return sdk.Finding{}, fmt.Errorf("diagnostic: no engine ID for source %q code %q", d.Source, d.Code)
	}
	return NewDiagnostic(st, profile, in)
}

// SuppressionState is the result of CheckSuppression.
type SuppressionState int

// SuppressionState values.
const (
	SuppressionActive SuppressionState = iota
	SuppressionExpired
	SuppressionInvalid
)

// CheckSuppression classifies a suppression on the given day. It never reads the clock: the caller
// passes today. A suppression needs a reason and a YYYY-MM-DD expiry; it is active through its expiry day.
func CheckSuppression(s sdk.Suppression, today time.Time) (SuppressionState, string) {
	if strings.TrimSpace(s.Reason) == "" {
		return SuppressionInvalid, "the suppression has no reason"
	}
	exp, err := time.Parse(dateLayout, s.Expires)
	if err != nil {
		return SuppressionInvalid, "the suppression expiry is missing or not a YYYY-MM-DD date"
	}
	if today.UTC().Truncate(24 * time.Hour).After(exp) {
		return SuppressionExpired, "the suppression expired on " + s.Expires
	}
	return SuppressionActive, ""
}

// SuppressionDiagnostic returns the FND-SYS diagnostic for a suppression that is not active, or false when
// it is active. entry names the suppression entry (for example the suppressed rule ID and fingerprint) and
// becomes the finding Key, so each broken entry has its own fingerprint.
func SuppressionDiagnostic(st rules.Stamper, profile string, s sdk.Suppression, entry string, today time.Time) (sdk.Finding, bool, error) {
	state, why := CheckSuppression(s, today)
	var id string
	switch state {
	case SuppressionActive:
		return sdk.Finding{}, false, nil
	case SuppressionExpired:
		id = DiagSuppressionExpired
	default:
		id = DiagSuppressionInvalid
	}
	f, err := NewDiagnostic(st, profile, Diagnostic{
		ID:             id,
		Resource:       sdk.ResourceRef{Kind: engineDiagnosticsFileKind, Name: s.Source},
		Evidence:       fmt.Sprintf("Suppression %s: %s.", entry, why),
		Recommendation: "Renew the suppression with a reason and a new expiry date, or fix the finding it hides.",
		Key:            entry,
	})
	return f, err == nil, err
}

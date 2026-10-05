// Package sdk holds the small, stable public contracts of Foundry Doctor: Finding, Adapter,
// Reporter and Report (ADR-011). Everything else is internal. Keep this package minimal:
// a change here is a change to the public API and to the JSON report schema.
package sdk

import "fmt"

// Severity is the importance of a finding after the profile has been applied.
type Severity string

// Severity values, from least to most important.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Rank orders severities: info 1, warning 2, error 3. An unknown value ranks 0, below every valid one.
func (s Severity) Rank() int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityError:
		return 3
	}
	return 0
}

// Valid reports whether s is one of the three defined severities.
func (s Severity) Valid() bool { return s.Rank() > 0 }

// AtLeast reports whether s is as severe as, or more severe than, threshold.
// An invalid s is never at least any threshold.
func (s Severity) AtLeast(threshold Severity) bool { return s.Valid() && s.Rank() >= threshold.Rank() }

// CompareSeverity returns -1, 0 or 1 when a is less, equally or more severe than b.
func CompareSeverity(a, b Severity) int {
	switch {
	case a.Rank() < b.Rank():
		return -1
	case a.Rank() > b.Rank():
		return 1
	}
	return 0
}

// ParseSeverity converts a flag or config value to a Severity.
func ParseSeverity(s string) (Severity, error) {
	v := Severity(s)
	if !v.Valid() {
		return "", fmt.Errorf("unknown severity %q: want info, warning or error", s)
	}
	return v, nil
}

// Confidence states how sure the deterministic check is of its own result.
type Confidence string

// Confidence values.
const (
	ConfidenceCertain   Confidence = "certain"
	ConfidenceLikely    Confidence = "likely"
	ConfidenceUncertain Confidence = "uncertain"
)

// Status is the outcome of one rule evaluation. Skipped is never the same as pass.
type Status string

// Status values.
const (
	StatusPass      Status = "pass"
	StatusFail      Status = "fail"
	StatusSkipped   Status = "skipped"
	StatusUncertain Status = "uncertain"
)

// Category is the catalogue category of a rule.
type Category string

// Category values, matching the catalogue.
const (
	CategoryMustHave   Category = "must-have"
	CategoryNiceToHave Category = "nice-to-have"
)

// ResourceRef names the thing a finding is about by logical identity, not by position.
// Kind is one of "project", "service", "arm-resource", "env-key", "file" or "tool".
// Type is the Azure resource type or azure.yaml host. Name is the logical name. ID is the
// Azure resource ID when it is known and contains no secret. Pointer is a JSON pointer
// within the resource, for example /properties/networkAcls/defaultAction.
type ResourceRef struct {
	Kind    string `json:"kind,omitempty"`
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
	ID      string `json:"id,omitempty"`
	Pointer string `json:"pointer,omitempty"`
}

// Location is a source position. File is relative to the project root with forward slashes.
// Line and Column are 1-based; zero means unknown. Pointer is a JSON pointer for derived ARM matches.
type Location struct {
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	Pointer   string `json:"pointer,omitempty"`
}

// Suppression records why a finding is accepted. A suppression needs a reason and an expiry.
type Suppression struct {
	Reason  string `json:"reason"`
	Expires string `json:"expires"` // YYYY-MM-DD
	Owner   string `json:"owner,omitempty"`
	Source  string `json:"source,omitempty"` // suppression file the entry came from, relative to the project root
}

// Finding is one normalised result. Rules fill the evidence fields; the engine stamps
// Severity, Profile, Category, Pillar, Basis, DocsURL, LastVerified and Fingerprint (ADR-008).
// Evidence must never contain a secret, token, connection string, prompt or document content.
type Finding struct {
	RuleID         string       `json:"ruleId"`
	RuleVersion    int          `json:"ruleVersion"`
	Severity       Severity     `json:"severity"`
	Category       Category     `json:"category"`
	Pillar         string       `json:"pillar,omitempty"`
	Basis          []string     `json:"basis,omitempty"`
	Profile        string       `json:"profile"`
	Resource       ResourceRef  `json:"resource,omitzero"`
	Location       Location     `json:"location,omitzero"`
	Evidence       string       `json:"evidence"`
	Recommendation string       `json:"recommendation"`
	Fix            string       `json:"fix,omitempty"`
	DocsURL        string       `json:"docsUrl,omitempty"`
	LastVerified   string       `json:"lastVerified,omitempty"`
	Confidence     Confidence   `json:"confidence"`
	Suppressed     *Suppression `json:"suppressed,omitempty"`
	Baselined      bool         `json:"baselined"`
	Fingerprint    string       `json:"fingerprint"`
	Adapter        string       `json:"adapter,omitempty"`
	// Key distinguishes several findings of one rule on one resource, for example the
	// name of the failing sub-check or of the offending property. It is part of the
	// fingerprint (ADR-008), so it must be stable and carry no line number or secret.
	Key string `json:"key,omitempty"`
}

// SkippedCheck records a check that did not run. It is reported, never counted as a pass.
type SkippedCheck struct {
	RuleID      string `json:"ruleId"`
	RuleVersion int    `json:"ruleVersion"`
	Reason      string `json:"reason"`
	Detail      string `json:"detail,omitempty"`
	// MissingCapability names what would let the check run, for example "compiled ARM template".
	MissingCapability string      `json:"missingCapability,omitempty"`
	Resource          ResourceRef `json:"resource,omitzero"`
}

// Package sdk defines the public contracts shared by the Foundry Doctor core
// platform and by rule implementations: the Finding model (PRD section 7),
// the Rule interface and the inputs a rule may inspect.
//
// Rules are pure: given an Input they return findings and explicit skips. A
// rule that cannot evaluate because an input is unavailable MUST return a Skip;
// a skip is never converted to a pass by the platform.
package sdk

import (
	"context"
	"fmt"
)

// Severity is the finding severity.
type Severity string

// Severity values.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Rank orders severities: info < warning < error. Unknown values rank 0.
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

// Valid reports whether s is a known severity.
func (s Severity) Valid() bool { return s.Rank() > 0 }

// ParseSeverity parses a severity name.
func ParseSeverity(v string) (Severity, error) {
	s := Severity(v)
	if !s.Valid() {
		return "", fmt.Errorf("invalid severity %q (want info, warning or error)", v)
	}
	return s, nil
}

// Confidence is how sure a rule is that the finding is real.
type Confidence string

// Confidence values.
const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Category classifies a rule as must-have or nice-to-have.
type Category string

// Category values.
const (
	CategoryMustHave   Category = "must-have"
	CategoryNiceToHave Category = "nice-to-have"
)

// ResourceRef identifies the resource a finding is about.
type ResourceRef struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
}

// Location is a repo-relative, slash-separated source location. Line and
// Column are 1-based; zero means unknown.
type Location struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// Suppression records why a finding is suppressed.
type Suppression struct {
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires,omitempty"`
}

// Finding is a single result. See PRD section 7.
type Finding struct {
	RuleID         string       `json:"ruleId"`
	RuleVersion    int          `json:"ruleVersion"`
	Severity       Severity     `json:"severity"`
	Category       Category     `json:"category,omitempty"`
	Pillar         string       `json:"pillar,omitempty"`
	Basis          []string     `json:"basis,omitempty"`
	Profile        string       `json:"profile,omitempty"`
	Resource       ResourceRef  `json:"resource"`
	Location       Location     `json:"location"`
	Evidence       string       `json:"evidence,omitempty"`
	Recommendation string       `json:"recommendation,omitempty"`
	Fix            string       `json:"fix,omitempty"`
	DocsURL        string       `json:"docsUrl,omitempty"`
	LastVerified   string       `json:"lastVerified,omitempty"`
	Confidence     Confidence   `json:"confidence,omitempty"`
	Suppressed     *Suppression `json:"suppressed,omitempty"`
	Baselined      bool         `json:"baselined,omitempty"`
	Fingerprint    string       `json:"fingerprint"`
	Adapter        string       `json:"adapter,omitempty"`
}

// Skip reports a check that did not run. Skipped is never a pass.
type Skip struct {
	RuleID string `json:"ruleId"`
	Reason string `json:"reason"`
	// Required marks a skip of an input the user explicitly asked to validate.
	// A required skip makes the run exit 2 (PRD section 7).
	Required bool `json:"required,omitempty"`
}

// Skip reason constants understood by the platform.
const (
	SkipNotImplemented     = "not-implemented"
	SkipUnsupportedVersion = "unsupported-version"
	SkipInputUnavailable   = "input-unavailable"
	SkipProfileKeyPrefix   = "profile-key-missing:"
)

// SkipMissingPolicyKey builds the standard reason for a missing policy key.
func SkipMissingPolicyKey(key string) string { return SkipProfileKeyPrefix + key }

// Result is the outcome of evaluating one rule.
type Result struct {
	Findings []Finding
	// Skipped, when non-nil, means the rule did not evaluate. Findings must
	// then be empty.
	Skipped *Skip
}

// Rule is implemented by packet B (and later) rule packages. Implementations
// must be deterministic and free of I/O beyond the Input they are given.
type Rule interface {
	// ID returns the catalogue rule ID, for example FND-CFG-001.
	ID() string
	// Evaluate inspects in. The engine fills in catalogue metadata (version,
	// severity for the active profile, category, pillar, basis, docs URL,
	// confidence, fingerprint) on returned findings, so a rule only needs to
	// set Resource, Location and Evidence (and may override Recommendation/Fix).
	Evaluate(ctx context.Context, in *Input) (Result, error)
}

// Input is everything a rule may look at.
type Input struct {
	Profile     string
	Environment string
	AzdVersion  string
	// AzureYAML is nil when azure.yaml is unavailable.
	AzureYAML AzureYAMLView
	// ARM is nil when no compiled Bicep/ARM model is available (offline
	// without a Bicep CLI). Rules needing it must skip with SkipInputUnavailable.
	ARM ARMModel
	// Policy is the effective, merged policy (ADR-007 keys).
	Policy Policy
}

// ARMModel is the stub contract for the compiled Bicep/ARM model produced by
// packet B. Core only depends on this interface.
type ARMModel interface {
	// Resources returns resources in deterministic order.
	Resources() []ARMResource
}

// ARMOutput is one top-level output of the compiled template. Value is the
// raw value (possibly an unresolved ARM expression); rules use only the name.
type ARMOutput struct {
	Name string
	Type string
}

// ARMOutputs is an optional interface an ARMModel may also implement to expose
// the top-level template outputs. Rules type-assert for it; absence means the
// output set is unknown, not empty.
type ARMOutputs interface {
	Outputs() []ARMOutput
}

// ARMResource is one deployed resource in the ARM model.
type ARMResource struct {
	Type       string
	Name       string
	APIVersion string
	Properties map[string]any
	Location   Location
	// Region is the Azure region (the resource "location" value) as written
	// in the template. It may be an unresolved ARM expression ("[...]").
	Region string
	// Kind is the resource "kind" (for example AIServices); empty when absent.
	Kind string
	// SKUName is the resource sku.name when present and literal; empty otherwise.
	SKUName string
	// Identity is the top-level resource "identity" object (type,
	// userAssignedIdentities); nil when absent. Values may be unresolved ARM
	// expressions ("[...]").
	Identity map[string]any
	// Scope is the top-level "scope" of an extension resource such as a role
	// assignment; empty when absent. It may be an unresolved expression.
	Scope string
}

// AzureYAMLView is the read-only view of azure.yaml offered to rules.
type AzureYAMLView interface {
	// Path returns the repo-relative file path.
	Path() string
	// ServiceNames returns service names in file order.
	ServiceNames() []string
	// Lookup returns the scalar string at a key path (for example
	// "services", "web", "host") with its location.
	Lookup(path ...string) (value string, loc Location, ok bool)
}

// Policy is read-only access to effective policy values.
type Policy interface {
	// Get returns the value at a dotted key such as "network.publicAccess".
	Get(key string) (any, bool)
}

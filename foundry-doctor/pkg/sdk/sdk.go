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

// Rank orders confidences: low < medium < high. Unknown values rank 0.
func (c Confidence) Rank() int {
	switch c {
	case ConfidenceLow:
		return 1
	case ConfidenceMedium:
		return 2
	case ConfidenceHigh:
		return 3
	}
	return 0
}

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
	RuleID      string `json:"ruleId"`
	RuleVersion int    `json:"ruleVersion"`
	// Severity is filled by the engine from the catalogue. A rule MAY set it to
	// a LOWER severity than the catalogue value for this one finding (ADR-008
	// amendment 3); the engine ignores a value that is not lower and ignores
	// it for platform-basis rules. It never feeds the fingerprint.
	Severity Severity    `json:"severity"`
	Category Category    `json:"category,omitempty"`
	Pillar   string      `json:"pillar,omitempty"`
	Basis    []string    `json:"basis,omitempty"`
	Profile  string      `json:"profile,omitempty"`
	Resource ResourceRef `json:"resource"`
	// Key is a rule-chosen stable discriminator (resource key, variable name,
	// property path) that separates two findings of one rule on one resource.
	// It is part of the fingerprint (ADR-008). It must not contain secret values.
	Key            string   `json:"key,omitempty"`
	Location       Location `json:"location"`
	Evidence       string   `json:"evidence,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
	Fix            string   `json:"fix,omitempty"`
	DocsURL        string   `json:"docsUrl,omitempty"`
	LastVerified   string   `json:"lastVerified,omitempty"`
	// Confidence is filled by the engine from the catalogue evidence confidence.
	// A rule MAY set a LOWER confidence for this one finding (ADR-008
	// amendment 3); a value that is not lower is ignored. It never feeds the
	// fingerprint.
	Confidence  Confidence   `json:"confidence,omitempty"`
	Suppressed  *Suppression `json:"suppressed,omitempty"`
	Baselined   bool         `json:"baselined,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	Adapter     string       `json:"adapter,omitempty"`
}

// Skip reports a check that did not run. Skipped is never a pass.
type Skip struct {
	RuleID string `json:"ruleId"`
	Reason string `json:"reason"`
	// Required marks a skip of an input the user explicitly asked to validate.
	// A required skip makes the run exit 2 (PRD section 7).
	Required bool `json:"required,omitempty"`
	// MissingCapability names the input or capability whose absence caused the
	// skip, for example "compiled ARM template" or "azd version".
	MissingCapability string `json:"missingCapability,omitempty"`
}

// Skip reason constants understood by the platform.
const (
	SkipNotImplemented     = "not-implemented"
	SkipUnsupportedVersion = "unsupported-version"
	SkipInputUnavailable   = "input-unavailable"
	SkipProfileKeyPrefix   = "profile-key-missing:"
	// SkipAzdVersionUnknown: the rule has an azd compatibility range and the
	// azd version could not be determined.
	SkipAzdVersionUnknown = "azd-version-unknown"
	// SkipSyntheticInfrastructure: the rule needs a compiled ARM template and the
	// project has no on-disk infrastructure (ADR-004, ADR-012).
	SkipSyntheticInfrastructure = "synthetic-infrastructure"
	// SkipNotApplicable: the rule ran its applicability check and the project has
	// nothing it applies to. Not a hidden check.
	SkipNotApplicable = "not-applicable"
)

// SkipMissingPolicyKey builds the standard reason for a missing policy key.
func SkipMissingPolicyKey(key string) string { return SkipProfileKeyPrefix + key }

// Result is the outcome of evaluating one rule.
type Result struct {
	Findings []Finding
	// Skipped, when non-nil, means the rule did not evaluate. Findings must
	// then be empty.
	Skipped *Skip
	// Skips lists per-resource checks the rule could not evaluate while it
	// still evaluated others. It may accompany Findings (or be set alone when
	// the rule ran but nothing was reported). The engine reports each entry as
	// a skip of the rule; skips never alter a finding or its fingerprint. It
	// must not be combined with Skipped. Each Reason should start with a
	// reason head (for example "unresolved-expression") and name the resource.
	Skips []Skip
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
	// Tags is the top-level resource tags object when present.
	Tags     map[string]any
	Location Location
	// Region is the Azure region (the resource "location" value) as written
	// in the template. It may be an unresolved ARM expression ("[...]").
	Region string
	// Zones is the top-level resource zones array when present.
	Zones []string
	// Kind is the resource "kind" (for example AIServices); empty when absent.
	Kind string
	// Conditional is true when the resource has an `if (...)` condition.
	// Condition holds the raw condition expression ("" when unconditional).
	// A literal "false" means the resource is not deployed.
	Conditional bool
	Condition   string
	// Looped is true when the resource is deployed through a `for` copy loop.
	Looped bool
	// SKUName is the resource sku.name when present and literal after
	// parameter-file and module-parameter substitution (the same value as
	// SKU["name"]); empty when absent or still an unresolved expression.
	SKUName string
	// SKU is the top-level resource "sku" object (name, capacity, tier, ...).
	// It preserves unresolved expressions and non-name fields without changing
	// the meaning of Properties, which remains the ARM "properties" object.
	// SKU values go through the same parameter substitution and secret
	// scanning as Properties.
	SKU map[string]any
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

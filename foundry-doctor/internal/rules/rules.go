// Package rules provides the rule registry, selectors and evaluation engine.
//
// The engine combines implementations (pkg/sdk.Rule) with catalogue metadata
// (internal/catalog) and fills version, severity, category, pillar, basis,
// docs URL, confidence and fingerprint on every finding. A rule that cannot run
// is reported as a skip, never as a pass.
package rules

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Skip reasons produced by the engine in addition to the sdk constants.
const (
	// SkipRuleError is used when a rule returned an error or panicked.
	SkipRuleError = "rule-error"
	// SkipInvalidMetadata is used when catalogue severity for the profile is missing or invalid.
	SkipInvalidMetadata = "invalid-catalogue-metadata"
	// SkipUnsupportedProfile is used when the profile has no per-profile severity key.
	SkipUnsupportedProfile = "unsupported-profile"
)

// PlatformBasis is the catalogue basis value that forces error severity.
const PlatformBasis = "platform"

// Registry holds rule implementations by ID.
type Registry struct {
	impl map[string]sdk.Rule
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{impl: map[string]sdk.Rule{}} }

// Register adds a rule. Empty and duplicate IDs are rejected.
func (r *Registry) Register(rule sdk.Rule) error {
	if rule == nil {
		return errors.New("rules: nil rule")
	}
	id := rule.ID()
	if id == "" {
		return errors.New("rules: rule has empty ID")
	}
	if _, dup := r.impl[id]; dup {
		return fmt.Errorf("rules: duplicate rule ID %s", id)
	}
	r.impl[id] = rule
	return nil
}

// Get returns the implementation for id.
func (r *Registry) Get(id string) (sdk.Rule, bool) { x, ok := r.impl[id]; return x, ok }

// IDs returns registered IDs, sorted.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.impl))
	for id := range r.impl {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Selector filters catalogue rules. Empty fields match everything. Values in
// one field are ORed; fields are ANDed. Exclude removes by rule ID, group,
// category or pillar and always wins.
type Selector struct {
	IDs        []string
	Groups     []string
	Categories []string
	Pillars    []string
	Exclude    []string
}

// Matches reports whether r is selected.
func (s Selector) Matches(r catalog.Rule) bool {
	for _, x := range s.Exclude {
		if strings.EqualFold(x, r.ID) || strings.EqualFold(x, r.Group) || x == r.Category || strings.EqualFold(x, r.Pillar) {
			return false
		}
	}
	in := func(list []string, v string) bool {
		if len(list) == 0 {
			return true
		}
		return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, v) })
	}
	return in(s.IDs, r.ID) && in(s.Groups, r.Group) && in(s.Categories, r.Category) && in(s.Pillars, r.Pillar)
}

// Engine evaluates selected rules.
type Engine struct {
	Catalog  []catalog.Rule
	Registry *Registry
	Selector Selector
	// Required marks IDs whose skips make the run fail (user explicitly asked).
	Required []string
}

// Report is the outcome of a run, sorted deterministically.
type Report struct {
	Findings []sdk.Finding
	Skipped  []sdk.Skip
	// Evaluated lists IDs that actually ran (even with no findings).
	Evaluated []string
}

// Run evaluates all selected catalogue rules against in. in.Profile must be a
// profile name such as foundry-prod; its suffix (dev|test|prod) selects severity.
func (e *Engine) Run(ctx context.Context, in *sdk.Input) (*Report, error) {
	if in == nil {
		return nil, errors.New("rules: nil input")
	}
	key := ProfileKey(in.Profile)
	cat := slices.Clone(e.Catalog)
	slices.SortFunc(cat, func(a, b catalog.Rule) int { return strings.Compare(a.ID, b.ID) })
	rep := &Report{}
	for _, cr := range cat {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.Selector.Matches(cr) {
			continue
		}
		if cr.Status == catalog.StatusDropped || cr.Status == catalog.StatusDeprecated {
			continue
		}
		skip := func(reason string) {
			rep.Skipped = append(rep.Skipped, sdk.Skip{RuleID: cr.ID, Reason: reason, Required: slices.Contains(e.Required, cr.ID)})
		}
		impl, ok := e.Registry.Get(cr.ID)
		if !ok {
			skip(sdk.SkipNotImplemented)
			continue
		}
		sev, reason := SeverityFor(cr, key)
		if reason != "" {
			skip(reason)
			continue
		}
		if !cr.Compatibility.Azd.Empty() {
			if in.AzdVersion == "" {
				skip(sdk.SkipInputUnavailable)
				continue
			}
			if !cr.Compatibility.Azd.Contains(in.AzdVersion) {
				skip(sdk.SkipUnsupportedVersion)
				continue
			}
		}
		res, err := safeEvaluate(ctx, impl, in)
		if err != nil {
			skip(SkipRuleError)
			continue
		}
		if res.Skipped != nil {
			if len(res.Findings) > 0 {
				skip(SkipRuleError)
				continue
			}
			s := *res.Skipped
			s.RuleID = cr.ID
			if slices.Contains(e.Required, cr.ID) {
				s.Required = true
			}
			rep.Skipped = append(rep.Skipped, s)
			continue
		}
		rep.Evaluated = append(rep.Evaluated, cr.ID)
		for _, f := range res.Findings {
			rep.Findings = append(rep.Findings, Enrich(f, cr, in.Profile, sev))
		}
	}
	findings.Sort(rep.Findings)
	slices.SortFunc(rep.Skipped, func(a, b sdk.Skip) int {
		if c := strings.Compare(a.RuleID, b.RuleID); c != 0 {
			return c
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	return rep, nil
}

func safeEvaluate(ctx context.Context, r sdk.Rule, in *sdk.Input) (res sdk.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("rules: rule %s panicked", r.ID())
		}
	}()
	return r.Evaluate(ctx, in)
}

// ProfileKey maps foundry-dev|foundry-test|foundry-prod to dev|test|prod.
// Unknown names return "".
func ProfileKey(profile string) string {
	switch profile {
	case "foundry-dev", "dev":
		return "dev"
	case "foundry-test", "test":
		return "test"
	case "foundry-prod", "prod":
		return "prod"
	}
	return ""
}

// SeverityFor returns the effective severity of cr for a profile key. Platform
// basis rules are error in every profile. A non-empty reason means skip.
func SeverityFor(cr catalog.Rule, key string) (sdk.Severity, string) {
	var raw string
	switch key {
	case "dev":
		raw = cr.Severity.Dev
	case "test":
		raw = cr.Severity.Test
	case "prod":
		raw = cr.Severity.Prod
	default:
		return "", SkipUnsupportedProfile
	}
	if slices.Contains(cr.Basis, PlatformBasis) {
		return sdk.SeverityError, ""
	}
	if raw == "" {
		return "", sdk.SkipProfileKeyPrefix + "severity." + key
	}
	s, err := sdk.ParseSeverity(raw)
	if err != nil {
		return "", SkipInvalidMetadata
	}
	return s, ""
}

// ConfidenceFor maps catalogue confidence to SDK confidence.
func ConfidenceFor(c string) sdk.Confidence {
	switch c {
	case "certain":
		return sdk.ConfidenceHigh
	case "likely":
		return sdk.ConfidenceMedium
	}
	return sdk.ConfidenceLow
}

// Enrich fills catalogue metadata and the fingerprint on a rule finding.
func Enrich(f sdk.Finding, cr catalog.Rule, profile string, sev sdk.Severity) sdk.Finding {
	f.RuleID = cr.ID
	f.RuleVersion = cr.Version
	f.Severity = sev
	f.Category = sdk.Category(cr.Category)
	f.Pillar = cr.Pillar
	f.Basis = slices.Clone(cr.Basis)
	f.Profile = profile
	if f.Recommendation == "" {
		f.Recommendation = cr.Recommendation
	}
	if f.Fix == "" {
		f.Fix = cr.Fix
	}
	if len(cr.Sources) > 0 {
		f.DocsURL = cr.Sources[0].URL
		f.LastVerified = cr.Sources[0].LastVerified
	}
	f.Confidence = ConfidenceFor(cr.Evidence.Confidence)
	f.Evidence = findings.Redact(f.Evidence)
	f.Fingerprint = findings.Fingerprint(f)
	return f
}

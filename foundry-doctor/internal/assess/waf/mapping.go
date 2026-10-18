// Package waf maps catalogue rules to WAF-facing controls through the reviewed,
// versioned mapping file mapping.yaml.
package waf

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

//go:embed mapping.yaml
var mappingYAML []byte

// Mapping is the parsed mapping file.
type Mapping struct {
	Version      int        `yaml:"version"`
	LastReviewed string     `yaml:"lastReviewed"`
	PillarSource Source     `yaml:"pillarSource"`
	Mapped       []Mapped   `yaml:"mapped"`
	Excluded     []Excluded `yaml:"excluded"`
	Questions    []Question `yaml:"questions"`
	Costly       []Costly   `yaml:"costly"`
}

// Source is a cited source with its verification date.
type Source struct {
	URL          string `yaml:"url"`
	LastVerified string `yaml:"lastVerified"`
}

// Mapped puts one rule in the WAF view.
type Mapped struct {
	Rule   string `yaml:"rule"`
	Pillar string `yaml:"pillar"`
}

// Excluded keeps one rule out of the WAF view, with a reason.
type Excluded struct {
	Rule   string `yaml:"rule"`
	Reason string `yaml:"reason"`
}

// Question marks a control that needs business or operational context.
type Question struct {
	Rule         string `yaml:"rule"`
	Key          string `yaml:"key"`
	ReferenceKey string `yaml:"referenceKey"`
	Prompt       string `yaml:"prompt"`
}

// Costly marks an optional recommendation whose remediation adds Azure cost.
type Costly struct {
	Rule         string `yaml:"rule"`
	Basis        string `yaml:"basis"`
	Source       string `yaml:"source"`
	LastVerified string `yaml:"lastVerified"`
}

// Load parses the embedded mapping file.
func Load() (Mapping, error) {
	return parse(mappingYAML)
}

func parse(data []byte) (Mapping, error) {
	var m Mapping
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Mapping{}, fmt.Errorf("waf: parse mapping: %w", err)
	}
	return m, nil
}

// Validate checks the mapping against the catalogue. Every catalogue rule must
// appear exactly once as mapped or excluded; a rule scheduled in phases 1-4
// must be mapped; mapped pillars must equal the catalogue pillar; questions and
// costly entries must name mapped rules; costly entries need a basis and an
// https source with a date, and never name a must-have rule.
func Validate(m Mapping, rules []catalog.Rule) error {
	var errs []error
	byID := make(map[string]catalog.Rule, len(rules))
	for _, r := range rules {
		byID[r.ID] = r
	}
	if m.Version < 1 || m.LastReviewed == "" {
		errs = append(errs, errors.New("waf: mapping needs version and lastReviewed"))
	}
	if !httpsURL(m.PillarSource.URL) || m.PillarSource.LastVerified == "" {
		errs = append(errs, errors.New("waf: mapping needs pillarSource url and lastVerified"))
	}
	seen := map[string]string{}
	mapped := map[string]bool{}
	for _, e := range m.Mapped {
		r, ok := byID[e.Rule]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("waf: mapped rule %s is not in the catalogue", e.Rule))
			continue
		case seen[e.Rule] != "":
			errs = append(errs, fmt.Errorf("waf: rule %s listed twice (%s and mapped)", e.Rule, seen[e.Rule]))
			continue
		case strings.TrimSpace(e.Pillar) == "" || e.Pillar != r.Pillar:
			errs = append(errs, fmt.Errorf("waf: rule %s mapped to pillar %q, catalogue says %q", e.Rule, e.Pillar, r.Pillar))
		case r.Status == catalog.StatusDropped || r.Status == catalog.StatusDeprecated:
			errs = append(errs, fmt.Errorf("waf: rule %s has status %s and must be excluded, not mapped", e.Rule, r.Status))
		}
		seen[e.Rule] = "mapped"
		mapped[e.Rule] = true
	}
	for _, e := range m.Excluded {
		r, ok := byID[e.Rule]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("waf: excluded rule %s is not in the catalogue", e.Rule))
			continue
		case seen[e.Rule] != "":
			errs = append(errs, fmt.Errorf("waf: rule %s listed twice (%s and excluded)", e.Rule, seen[e.Rule]))
			continue
		case strings.TrimSpace(e.Reason) == "":
			errs = append(errs, fmt.Errorf("waf: excluded rule %s has no reason", e.Rule))
		case scheduledForAssessment(r):
			errs = append(errs, fmt.Errorf("waf: rule %s is scheduled in phases 1-4 and must be mapped, not excluded", e.Rule))
		}
		seen[e.Rule] = "excluded"
	}
	for _, r := range rules {
		if seen[r.ID] == "" {
			errs = append(errs, fmt.Errorf("waf: catalogue rule %s is neither mapped nor excluded", r.ID))
		}
	}
	qs := map[string]bool{}
	for _, q := range m.Questions {
		switch {
		case !mapped[q.Rule]:
			errs = append(errs, fmt.Errorf("waf: question rule %s is not mapped", q.Rule))
		case qs[q.Rule]:
			errs = append(errs, fmt.Errorf("waf: question rule %s listed twice", q.Rule))
		case strings.TrimSpace(q.Prompt) == "":
			errs = append(errs, fmt.Errorf("waf: question rule %s has no prompt", q.Rule))
		case q.ReferenceKey != "" && q.Key == "":
			errs = append(errs, fmt.Errorf("waf: question rule %s has a referenceKey without a key", q.Rule))
		}
		qs[q.Rule] = true
	}
	cs := map[string]bool{}
	for _, c := range m.Costly {
		switch {
		case !mapped[c.Rule]:
			errs = append(errs, fmt.Errorf("waf: costly rule %s is not mapped", c.Rule))
		case cs[c.Rule]:
			errs = append(errs, fmt.Errorf("waf: costly rule %s listed twice", c.Rule))
		case byID[c.Rule].Category == "must-have":
			errs = append(errs, fmt.Errorf("waf: costly rule %s is must-have; mandatory controls are not optional costly recommendations", c.Rule))
		case strings.TrimSpace(c.Basis) == "" || !httpsURL(c.Source) || c.LastVerified == "":
			errs = append(errs, fmt.Errorf("waf: costly rule %s needs basis, https source and lastVerified", c.Rule))
		}
		cs[c.Rule] = true
	}
	return errors.Join(errs...)
}

// scheduledForAssessment reports whether a live rule is scheduled in a phase
// whose evidence `assess waf` consumes (phases 1-4).
func scheduledForAssessment(r catalog.Rule) bool {
	switch r.Status {
	case catalog.StatusDropped, catalog.StatusDeprecated, catalog.StatusProposed:
		return false
	}
	return slices.ContainsFunc(r.Phases, func(p string) bool {
		return p == "1" || p == "2" || p == "3" || p == "4"
	})
}

func httpsURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// Definitions validates the mapping against rules and returns one control
// definition per mapped rule, sorted by rule ID.
func Definitions(rules []catalog.Rule) ([]assess.Definition, error) {
	m, err := Load()
	if err != nil {
		return nil, err
	}
	return definitionsFrom(m, rules)
}

func definitionsFrom(m Mapping, rules []catalog.Rule) ([]assess.Definition, error) {
	if err := Validate(m, rules); err != nil {
		return nil, err
	}
	byID := make(map[string]catalog.Rule, len(rules))
	for _, r := range rules {
		byID[r.ID] = r
	}
	questions := map[string]Question{}
	for _, q := range m.Questions {
		questions[q.Rule] = q
	}
	costly := map[string]Costly{}
	for _, c := range m.Costly {
		costly[c.Rule] = c
	}
	out := make([]assess.Definition, 0, len(m.Mapped))
	for _, e := range m.Mapped {
		r := byID[e.Rule]
		d := assess.Definition{
			RuleID:       r.ID,
			ControlID:    r.ID,
			ControlTitle: r.Title,
			Pillar:       e.Pillar,
		}
		if c, ok := costly[r.ID]; ok {
			d.Costly = true
			d.CostBasis = c.Basis
			d.CostSourceURL = c.Source
			d.CostVerified = c.LastVerified
		}
		if q, ok := questions[r.ID]; ok {
			d.NoEvidenceState = assess.StateQuestion
			d.QuestionKey = q.Key
			d.QuestionReferenceKey = q.ReferenceKey
			d.QuestionPrompt = q.Prompt
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b assess.Definition) int { return strings.Compare(a.RuleID, b.RuleID) })
	return out, nil
}

// ExcludedRules returns the excluded rule IDs with their reasons, sorted.
func ExcludedRules() ([]Excluded, error) {
	m, err := Load()
	if err != nil {
		return nil, err
	}
	out := slices.Clone(m.Excluded)
	slices.SortFunc(out, func(a, b Excluded) int { return strings.Compare(a.Rule, b.Rule) })
	return out, nil
}

package rules

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

// Selector grammar for --rules (comma separated, the flag may repeat):
//
//	FND-SEC-001        one rule ID
//	FND-SEC-*          glob over IDs (path.Match syntax: * ? [..])
//	sec | group:sec    every rule of a group (case-insensitive)
//	must-have          category (also nice-to-have, or category:must-have)
//	phase:1            catalogue phase
//	!<term>            exclusion; any term may be negated
//
// A rule is selected when it matches at least one positive term (or there are no positive terms)
// and matches no negative term. Terms are validated by Registry.Select, which rejects any term
// that matches no catalogue rule, so a typo is an error rather than an empty run.

type termKind int

const (
	kindID termKind = iota
	kindGlob
	kindGroup
	kindCategory
	kindPhase
)

type term struct {
	kind   termKind
	value  string // normalised: IDs and globs upper-case, groups upper-case, categories and phases as written
	raw    string // the term as the user wrote it, without "!"
	negate bool
}

var idRe = regexp.MustCompile(`^FND-[A-Z]+-[0-9]{3}$`)
var wordRe = regexp.MustCompile(`^[A-Za-z]+$`)
var phaseRe = regexp.MustCompile(`^[0-9]+$|^later$|^future$`)

// Selector is a parsed --rules expression. The zero value selects everything.
type Selector struct{ terms []term }

// IsZero reports whether the selector has no terms.
func (s Selector) IsZero() bool { return len(s.terms) == 0 }

// String returns the canonical comma-separated form.
func (s Selector) String() string {
	parts := make([]string, len(s.terms))
	for i, t := range s.terms {
		parts[i] = t.String()
	}
	return strings.Join(parts, ",")
}

func (t term) String() string {
	if t.negate {
		return "!" + t.raw
	}
	return t.raw
}

// ParseSelector parses one or more selector expressions (repeated flags) into one selector.
// An empty string is skipped. An empty term, such as "a,,b" or a lone "!", is an error.
func ParseSelector(exprs ...string) (Selector, error) {
	var sel Selector
	var errs []error
	for _, expr := range exprs {
		if strings.TrimSpace(expr) == "" {
			continue
		}
		for _, raw := range strings.Split(expr, ",") {
			t, err := parseTerm(strings.TrimSpace(raw))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			sel.terms = append(sel.terms, t)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Selector{}, err
	}
	return sel, nil
}

func parseTerm(raw string) (term, error) {
	var t term
	if strings.HasPrefix(raw, "!") {
		t.negate = true
		raw = strings.TrimSpace(raw[1:])
	}
	if raw == "" {
		return term{}, errors.New("rule selector: empty term")
	}
	t.raw = raw
	switch lower := strings.ToLower(raw); {
	case lower == sdkMustHave || lower == sdkNiceToHave:
		t.kind, t.value = kindCategory, lower
	case strings.HasPrefix(lower, "category:"):
		v := strings.TrimPrefix(lower, "category:")
		if v != sdkMustHave && v != sdkNiceToHave {
			return term{}, fmt.Errorf("rule selector %q: category must be must-have or nice-to-have", raw)
		}
		t.kind, t.value = kindCategory, v
	case strings.HasPrefix(lower, "phase:"):
		v := strings.TrimPrefix(lower, "phase:")
		if !phaseRe.MatchString(v) {
			return term{}, fmt.Errorf("rule selector %q: phase must be a number, later or future", raw)
		}
		t.kind, t.value = kindPhase, v
	case strings.HasPrefix(lower, "group:"):
		v := raw[len("group:"):]
		if !wordRe.MatchString(v) {
			return term{}, fmt.Errorf("rule selector %q: group must be letters only", raw)
		}
		t.kind, t.value = kindGroup, strings.ToUpper(v)
	case idRe.MatchString(strings.ToUpper(raw)):
		t.kind, t.value = kindID, strings.ToUpper(raw)
	case strings.ContainsAny(raw, "*?["):
		g := strings.ToUpper(raw)
		if _, err := path.Match(g, "FND-SEC-001"); err != nil {
			return term{}, fmt.Errorf("rule selector %q: bad glob: %w", raw, err)
		}
		t.kind, t.value = kindGlob, g
	case wordRe.MatchString(raw):
		t.kind, t.value = kindGroup, strings.ToUpper(raw)
	default:
		return term{}, fmt.Errorf("rule selector %q: not a rule ID, glob, group, category or phase:<n>", raw)
	}
	return t, nil
}

// The catalogue stores categories as plain strings.
const (
	sdkMustHave   = "must-have"
	sdkNiceToHave = "nice-to-have"
)

func (t term) matches(m catalog.Rule) bool {
	switch t.kind {
	case kindID:
		return m.ID == t.value
	case kindGlob:
		ok, err := path.Match(t.value, m.ID)
		return err == nil && ok
	case kindGroup:
		return strings.EqualFold(m.Group, t.value)
	case kindCategory:
		return m.Category == t.value
	case kindPhase:
		return slices.Contains(m.Phases, t.value)
	}
	return false
}

// Match reports whether the catalogue rule is selected.
func (s Selector) Match(m catalog.Rule) bool {
	positive, hit := false, false
	for _, t := range s.terms {
		if t.negate {
			if t.matches(m) {
				return false
			}
			continue
		}
		positive = true
		if t.matches(m) {
			hit = true
		}
	}
	return !positive || hit
}

// Select returns the executable entries the selector picks, in ID order. It returns an error when a
// term matches no catalogue rule, or when a positive ID term names a rule with no implementation in
// this build.
func (r *Registry) Select(sel Selector) ([]Entry, error) {
	if err := r.validateTerms(sel); err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range r.entries {
		if e.Executable() && sel.Match(e.Meta) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *Registry) validateTerms(sel Selector) error {
	var errs []error
	for _, t := range sel.terms {
		found := false
		for _, e := range r.entries {
			if !t.matches(e.Meta) {
				continue
			}
			found = true
			if !t.negate && t.kind == kindID && !e.Executable() {
				errs = append(errs, fmt.Errorf("rule selector %q: %s is catalogued (phases %v) but not implemented in this version", t.raw, e.Meta.ID, e.Meta.Phases))
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("rule selector %q matches no catalogue rule", t.raw))
		}
	}
	return errors.Join(errs...)
}

// Selection is the rules block of the configuration: packs to start from, selector terms to add and
// selector terms to remove. Entries are selector terms in the --rules grammar, without negation.
type Selection struct {
	Packs   []string
	Include []string
	Exclude []string
}

// Resolve returns the executable entries to run, in ID order.
//
// A non-zero cli selector replaces the configuration (flags win, PRD section 6) and is applied to
// every executable rule. Otherwise the result is: the union of the configured packs (DefaultPack when
// none), plus the rules matched by Include, minus the rules matched by Exclude. A pack rule with no
// implementation, an unknown pack, a negated term in Include or Exclude, and a term that matches no
// catalogue rule are errors.
func (r *Registry) Resolve(packs []Pack, cfg Selection, cli Selector) ([]Entry, error) {
	if !cli.IsZero() {
		return r.Select(cli)
	}
	names := cfg.Packs
	if len(names) == 0 {
		names = []string{DefaultPack}
	}
	chosen := map[string]bool{}
	var errs []error
	for _, name := range names {
		p, ok := PackByID(packs, name)
		if !ok {
			errs = append(errs, fmt.Errorf("rule pack %q not found", name))
			continue
		}
		for _, id := range p.Rules {
			e, ok := r.Get(id)
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("rule pack %q: %s is not in the catalogue", name, id))
			case !e.Executable() && e.Meta.Implementation.Owner != "native":
				// Delivered by an adapter or the compiler path, not by a Go rule.
			case !e.Executable():
				errs = append(errs, fmt.Errorf("rule pack %q: %s has no implementation in this build", name, id))
			default:
				chosen[id] = true
			}
		}
	}
	parse := func(kind string, terms []string) (Selector, error) {
		sel, err := ParseSelector(terms...)
		if err != nil {
			return sel, fmt.Errorf("rules.%s: %w", kind, err)
		}
		for _, t := range sel.terms {
			if t.negate {
				return Selector{}, fmt.Errorf("rules.%s: %q: do not use ! in a list; move the term to the other list", kind, t.String())
			}
		}
		return sel, r.validateTerms(sel)
	}
	inc, err := parse("include", cfg.Include)
	if err != nil {
		errs = append(errs, err)
	}
	exc, err := parse("exclude", cfg.Exclude)
	if err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range r.entries {
		if !e.Executable() {
			continue
		}
		in := chosen[e.Meta.ID] || (!inc.IsZero() && anyTerm(inc, e.Meta))
		if in && !(!exc.IsZero() && anyTerm(exc, e.Meta)) {
			out = append(out, e)
		}
	}
	return out, nil
}

func anyTerm(s Selector, m catalog.Rule) bool {
	for _, t := range s.terms {
		if t.matches(m) {
			return true
		}
	}
	return false
}

package rules

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

// Phase1 is the catalogue phase value of the rules this release implements.
const Phase1 = "1"

// Entry joins one catalogue rule to its Go implementation. Impl is nil when the rule is
// catalogued but not implemented in this build (a later phase, or an adapter-owned rule).
type Entry struct {
	Meta catalog.Rule
	Impl Rule
}

// Executable reports whether the entry has an implementation.
func (e Entry) Executable() bool { return e.Impl != nil }

// InPhase reports whether the catalogue rule is allocated to phase.
func InPhase(m catalog.Rule, phase string) bool { return slices.Contains(m.Phases, phase) }

// NeedsImplementation reports whether a catalogue rule must have a Go implementation in phase:
// it is allocated to the phase, is neither dropped nor deprecated, and is owned by native code.
// Rules owned by a tool (for example FND-SEC-014, a Bicep linter wrap) are delivered through the
// adapter or compiler-diagnostic path and have no Go Rule.
func NeedsImplementation(m catalog.Rule, phase string) bool {
	return InPhase(m, phase) && m.Status != catalog.StatusDropped && m.Status != catalog.StatusDeprecated &&
		m.Implementation.Owner == "native"
}

// PhaseRules returns the catalogue rules that need an implementation in phase, in ID order.
func PhaseRules(cat []catalog.Rule, phase string) []catalog.Rule {
	var out []catalog.Rule
	for _, m := range cat {
		if NeedsImplementation(m, phase) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b catalog.Rule) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// LoadCatalog loads and validates the catalogue in fsys below root.
func LoadCatalog(ctx context.Context, fsys fs.FS, root string) ([]catalog.Rule, error) {
	cat, err := catalog.Load(ctx, fsys, root)
	if err != nil {
		return nil, fmt.Errorf("load rule catalogue: %w", err)
	}
	if err := catalog.Validate(cat, catalog.Options{}); err != nil {
		return nil, fmt.Errorf("validate rule catalogue: %w", err)
	}
	return cat, nil
}

// CheckImplementations verifies the implementations against the catalogue for phase:
//   - every implementation names a catalogue rule, and that rule is in the phase;
//   - no rule has two implementations;
//   - every implementation's Version equals the catalogue version;
//   - when requireComplete is set, every rule of the phase has an implementation.
//
// All problems are returned together, in a stable order.
func CheckImplementations(cat []catalog.Rule, impls []Rule, phase string, requireComplete bool) error {
	byID := make(map[string]catalog.Rule, len(cat))
	for _, m := range cat {
		byID[m.ID] = m
	}
	var errs []error
	count := map[string]int{}
	for _, r := range impls {
		if r == nil {
			errs = append(errs, errors.New("nil rule implementation registered"))
			continue
		}
		id := r.ID()
		count[id]++
		m, ok := byID[id]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: implementation has no catalogue rule", id))
		case !NeedsImplementation(m, phase):
			errs = append(errs, fmt.Errorf("%s: implementation registered but the rule is not a phase %s rule (phases %v, status %s)", id, phase, m.Phases, m.Status))
		case r.Version() != m.Version:
			errs = append(errs, fmt.Errorf("%s: implementation is version %d, catalogue is version %d", id, r.Version(), m.Version))
		}
	}
	for _, id := range sortedKeys(count) {
		if count[id] > 1 {
			errs = append(errs, fmt.Errorf("%s: %d implementations registered, want exactly one", id, count[id]))
		}
	}
	if requireComplete {
		for _, m := range PhaseRules(cat, phase) {
			if count[m.ID] == 0 {
				errs = append(errs, fmt.Errorf("%s: phase %s rule has no implementation", m.ID, phase))
			}
		}
	}
	return errors.Join(errs...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Registry is the catalogue joined to the implementations. It is immutable after construction.
type Registry struct {
	entries []Entry // sorted by ID
	index   map[string]int
}

// NewRegistry joins cat and impls for phase. It fails on the problems CheckImplementations
// reports, except missing implementations: use Registry.CheckComplete for that, so a build
// with a partial rule set can still run the rules it has.
func NewRegistry(cat []catalog.Rule, impls []Rule, phase string) (*Registry, error) {
	if err := CheckImplementations(cat, impls, phase, false); err != nil {
		return nil, fmt.Errorf("rule registry: %w", err)
	}
	byID := make(map[string]Rule, len(impls))
	for _, r := range impls {
		byID[r.ID()] = r
	}
	reg := &Registry{index: make(map[string]int, len(cat))}
	sorted := slices.Clone(cat)
	slices.SortFunc(sorted, func(a, b catalog.Rule) int { return strings.Compare(a.ID, b.ID) })
	for i, m := range sorted {
		reg.entries = append(reg.entries, Entry{Meta: m, Impl: byID[m.ID]})
		reg.index[m.ID] = i
	}
	return reg, nil
}

// Entries returns every catalogue entry in ID order. The slice is a copy.
func (r *Registry) Entries() []Entry { return slices.Clone(r.entries) }

// Executable returns the entries that have an implementation, in ID order.
func (r *Registry) Executable() []Entry {
	var out []Entry
	for _, e := range r.entries {
		if e.Executable() {
			out = append(out, e)
		}
	}
	return out
}

// Get returns the entry for a catalogue ID.
func (r *Registry) Get(id string) (Entry, bool) {
	i, ok := r.index[id]
	if !ok {
		return Entry{}, false
	}
	return r.entries[i], true
}

// Catalog returns the catalogue rules in ID order.
func (r *Registry) Catalog() []catalog.Rule {
	out := make([]catalog.Rule, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Meta
	}
	return out
}

// CheckComplete reports an error naming every rule of phase that has no implementation.
func (r *Registry) CheckComplete(phase string) error {
	var errs []error
	for _, e := range r.entries {
		if NeedsImplementation(e.Meta, phase) && !e.Executable() {
			errs = append(errs, fmt.Errorf("%s: phase %s rule has no implementation", e.Meta.ID, phase))
		}
	}
	return errors.Join(errs...)
}

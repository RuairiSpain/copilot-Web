// Package env holds the native FND-ENV-* rules for azd environment values (key names only, never values).
//
// Cross-environment rules need more than one environment. model.Input carries only the selected
// environment, so the rules read their environments through a Source. Rules() uses DefaultSource,
// which can see only Input.Environment and therefore skips every cross-environment rule until the
// engine supplies sibling environments (see the hand-off note on Input.Environments). RulesWith
// injects a different Source, which the tests and the compare command use.
//
// No rule here puts an environment value into a Finding, a skip detail or an error. Evidence names
// keys, environments and azure.yaml paths only.
package env

import (
	"slices"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

// Source returns the azd environments visible to the rules for this input. The result need not be
// sorted; the rules sort by name and ignore duplicates.
type Source func(in *model.Input) []model.Environment

// DefaultSource returns the selected environment only, because model.Input has no sibling environments.
func DefaultSource(in *model.Input) []model.Environment {
	if in == nil || in.Environment == nil {
		return nil
	}
	return []model.Environment{*in.Environment}
}

// Rules returns the rules of this group in ID order.
func Rules() []rules.Rule { return RulesWith(DefaultSource) }

// RulesWith returns the rules of this group reading environments from src.
func RulesWith(src Source) []rules.Rule {
	if src == nil {
		src = DefaultSource
	}
	return []rules.Rule{
		sharedResourceGroup{src: src},
		copiedValues{src: src},
		prodReferencesNonProd{src: src},
		differenceSummary{src: src},
	}
}

// visible returns the environments sorted by name with duplicate names dropped.
func visible(src Source, in *model.Input) []model.Environment {
	envs := slices.Clone(src(in))
	slices.SortStableFunc(envs, func(a, b model.Environment) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return slices.CompactFunc(envs, func(a, b model.Environment) bool { return a.Name == b.Name })
}

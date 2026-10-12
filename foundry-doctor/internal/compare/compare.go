// Package compare diffs two azd environments.
//
// The result carries key names and booleans only. For every key it says whether the key is present
// on the left, on the right or on both sides and, when on both, whether the two values are equal.
// That equality is computed in-process and only the outcome (Same or Different) leaves this package.
//
// No value, no hash of a value, no length and no prefix of a value is ever emitted (ADR-008,
// CLAUDE.md). An unsalted hash of a low-entropy value, such as a region or a short name, can be
// brute-forced, so fingerprints of values are deliberately not part of the design. The scope
// summary (subscription and resource group) is likewise reported as a DiffKind by key name, never
// by the subscription ID or group name.
//
// Values are compared byte for byte after parsing, so an empty value is equal to another empty
// value and differs from a missing key. Line endings are normalised by the .env parser, not here.
package compare

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// DiffKind classifies one key.
type DiffKind string

// DiffKind values.
const (
	OnlyLeft  DiffKind = "only-left"
	OnlyRight DiffKind = "only-right"
	Different DiffKind = "different"
	Same      DiffKind = "same"
)

// Keys that identify the scope of an azd environment.
const (
	KeySubscription  = "AZURE_SUBSCRIPTION_ID"
	KeyResourceGroup = "AZURE_RESOURCE_GROUP"
)

// KeyDiff is the comparison of one key. It holds no value and no hash of a value. Bound is true when
// the key is environment-bound (see EnvironmentBound).
type KeyDiff struct {
	Key   string   `json:"key"`
	Kind  DiffKind `json:"kind"`
	Bound bool     `json:"bound,omitempty"`
}

// Diff is the deterministic result: Keys is sorted by key name. Subscription and ResourceGroup
// summarise the scope keys by name; they are empty when neither side sets the key.
type Diff struct {
	Left          string    `json:"left"`
	Right         string    `json:"right"`
	Subscription  DiffKind  `json:"subscription,omitempty"`
	ResourceGroup DiffKind  `json:"resourceGroup,omitempty"`
	Keys          []KeyDiff `json:"keys"`
}

// Count returns how many keys have the given kind.
func (d Diff) Count(k DiffKind) int {
	n := 0
	for _, kd := range d.Keys {
		if kd.Kind == k {
			n++
		}
	}
	return n
}

// Identical reports whether every key is present on both sides with an equal value.
func (d Diff) Identical() bool { return d.Count(Same) == len(d.Keys) }

// EnvironmentBound is the Foundry Doctor heuristic for keys whose value identifies the
// environment's own resources: AZURE_SUBSCRIPTION_ID, keys ending in _NAME, _ENDPOINT, _ID or
// _RESOURCE_GROUP, and values shaped like an ARM resource ID or a *.services.ai.azure.com endpoint.
// AZURE_TENANT_ID is excluded because a tenant is normally shared. It is not an azd property.
func EnvironmentBound(key string, v model.EnvValue) bool {
	if key == "AZURE_TENANT_ID" {
		return false
	}
	if key == KeySubscription {
		return true
	}
	for _, s := range []string{"_NAME", "_ENDPOINT", "_ID", "_RESOURCE_GROUP"} {
		if strings.HasSuffix(key, s) {
			return true
		}
	}
	raw := v.Reveal()
	return strings.HasPrefix(raw, "/subscriptions/") || strings.Contains(strings.ToLower(raw), ".services.ai.azure.com")
}

// Environments compares two environments. It honours ctx cancellation.
func Environments(ctx context.Context, left, right model.Environment) (Diff, error) {
	d := Diff{Left: left.Name, Right: right.Name}
	lk, rk := left.Keys(), right.Keys()
	all := make([]string, 0, len(lk)+len(rk))
	all = append(all, lk...)
	all = append(all, rk...)
	slices.Sort(all)
	all = slices.Compact(all)

	d.Keys = make([]KeyDiff, 0, len(all))
	for i, k := range all {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return Diff{}, fmt.Errorf("compare environments %q and %q: %w", left.Name, right.Name, err)
			}
		}
		lv, lok := left.Get(k)
		rv, rok := right.Get(k)
		kd := KeyDiff{Key: k}
		switch {
		case lok && !rok:
			kd.Kind, kd.Bound = OnlyLeft, EnvironmentBound(k, lv)
		case !lok && rok:
			kd.Kind, kd.Bound = OnlyRight, EnvironmentBound(k, rv)
		case lv.Reveal() == rv.Reveal():
			kd.Kind, kd.Bound = Same, EnvironmentBound(k, lv)
		default:
			kd.Kind, kd.Bound = Different, EnvironmentBound(k, lv) || EnvironmentBound(k, rv)
		}
		switch k {
		case KeySubscription:
			d.Subscription = kd.Kind
		case KeyResourceGroup:
			d.ResourceGroup = kd.Kind
		}
		d.Keys = append(d.Keys, kd)
	}
	return d, nil
}

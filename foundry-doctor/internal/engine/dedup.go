package engine

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// OverlapIndex maps an external tool's rule ID to the catalogue rules that list it in their overlap
// field. It is built from the catalogue, never from tool output.
type OverlapIndex struct {
	byTool map[string]map[string][]string // list name -> external ID (lower case) -> catalogue IDs
}

// NewOverlapIndex indexes the overlap lists of the catalogue rules.
func NewOverlapIndex(cat []catalog.Rule) OverlapIndex {
	idx := OverlapIndex{byTool: map[string]map[string][]string{}}
	add := func(list string, ids []string, rule string) {
		for _, id := range ids {
			if idx.byTool[list] == nil {
				idx.byTool[list] = map[string][]string{}
			}
			k := strings.ToLower(id)
			if !slices.Contains(idx.byTool[list][k], rule) {
				idx.byTool[list][k] = append(idx.byTool[list][k], rule)
			}
		}
	}
	for _, m := range cat {
		o := m.Overlap
		add("psrule", o.PSRule, m.ID)
		add("azure-policy", o.AzurePolicy, m.ID)
		add("defender", o.Defender, m.ID)
		add("advisor", o.Advisor, m.ID)
		add("bicep", o.BicepLinter, m.ID)
		add("checkov", o.Checkov, m.ID)
	}
	return idx
}

// Lookup returns the catalogue IDs a finding of the named adapter maps to, sorted. An adapter name
// that is not a known overlap list searches every list.
func (x OverlapIndex) Lookup(adapter, externalID string) []string {
	k := strings.ToLower(externalID)
	var out []string
	for list, m := range x.byTool {
		if _, known := x.byTool[adapter]; known && list != adapter {
			continue
		}
		for _, id := range m[k] {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Absorbed records an adapter finding that was dropped because a native finding covers it.
type Absorbed struct {
	Adapter           sdk.Finding // the dropped finding, kept as supporting provenance
	NativeRuleID      string
	NativeFingerprint string
}

// DedupAdapter removes adapter findings that a native finding already reports: the adapter finding's rule
// maps, through the catalogue overlap lists, to a native rule that has a finding on the same resource
// (kind, lower-cased type, name). The native finding wins. The result keeps the order of adapter.
func DedupAdapter(native, adapter []sdk.Finding, idx OverlapIndex) (kept []sdk.Finding, absorbed []Absorbed) {
	type key struct{ rule, kind, typ, name string }
	byKey := map[key]sdk.Finding{}
	for _, f := range native {
		if f.Adapter != "" {
			continue
		}
		k := key{f.RuleID, f.Resource.Kind, strings.ToLower(f.Resource.Type), f.Resource.Name}
		if _, ok := byKey[k]; !ok {
			byKey[k] = f
		}
	}
	for _, a := range adapter {
		hit := false
		ids := idx.Lookup(a.Adapter, a.RuleID)
		if len(ids) == 0 {
			ids = []string{a.RuleID} // the adapter already used a catalogue ID
		}
		for _, id := range ids {
			if n, ok := byKey[key{id, a.Resource.Kind, strings.ToLower(a.Resource.Type), a.Resource.Name}]; ok {
				absorbed = append(absorbed, Absorbed{Adapter: a, NativeRuleID: n.RuleID, NativeFingerprint: n.Fingerprint})
				hit = true
				break
			}
		}
		if !hit {
			kept = append(kept, a)
		}
	}
	return kept, absorbed
}

// MergeFingerprints applies ADR-008: findings with the same fingerprint and identical evidence merge into
// the first; with different evidence both stay and an FND-SYS-DUPLICATE-FINGERPRINT diagnostic is returned
// for each colliding fingerprint. The merged list is sorted.
func MergeFingerprints(st rules.Stamper, profile string, in []sdk.Finding) (merged, diags []sdk.Finding, err error) {
	rules.SortFindings(in)
	seen := map[string][]sdk.Finding{}
	reported := map[string]bool{}
	for _, f := range in {
		prev := seen[f.Fingerprint]
		if slices.ContainsFunc(prev, func(p sdk.Finding) bool { return p.Evidence == f.Evidence }) {
			continue
		}
		if len(prev) > 0 && f.Fingerprint != "" && !reported[f.Fingerprint] {
			reported[f.Fingerprint] = true
			d, derr := NewDiagnostic(st, profile, Diagnostic{
				ID: DiagDuplicateFingerprint, Resource: f.Resource, Key: f.Fingerprint,
				Evidence:       "Rule " + f.RuleID + " produced distinct findings with one fingerprint.",
				Recommendation: "Set a distinguishing Key on the findings of this rule.",
			})
			if derr != nil {
				return nil, nil, derr
			}
			diags = append(diags, d)
		}
		seen[f.Fingerprint] = append(prev, f)
		merged = append(merged, f)
	}
	rules.SortFindings(diags)
	return merged, diags, nil
}

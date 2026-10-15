// Package wara holds reliability-specific WARA references.
package wara

import (
	"slices"
)

var references = map[string][]string{
	"FND-REL-001": {"b376281d-bfec-4695-8f90-9a44544fdfa4", "dff62efe-c3a3-4621-98b3-c877c65cb195"},
	"FND-REL-002": {"e6c7e1cc-2f47-264d-aa50-1da421314472"},
	"FND-REL-003": {"921631f6-ed59-49a5-94c1-f0f3ececa580", "e544520b-8505-7841-9e77-1f1974ee86ec"},
	"FND-REL-007": {"dff62efe", "43663217-a1d3-844b-80ea-571a2ce37c6c", "9cabded7-a1fc-6e4a-944b-d7dd98ea31a2", "61187af4-7d36-4b48-b16e-de78bef143a0"},
	"FND-REL-008": {"0c193899-da60-4a52-b4a0-77d75ac8c5c5"},
	"FND-REL-009": {"baf3bfc0-32a2-4c0c-926d-c9bf0b49808e", "740f2c1c-8857-4648-80eb-47d2c56d5a50", "af4f88cb-35e8-4371-b29e-3a32b1d2f40a", "e35cf148-8eee-49d1-a1c9-956160f99e0b"},
}

// Lookup returns the WARA recommendation IDs associated with ruleID.
func Lookup(ruleID string) []string {
	return slices.Clone(references[ruleID])
}

// Map returns a cloned copy of the rule-to-WARA mapping.
func Map() map[string][]string {
	out := make(map[string][]string, len(references))
	for k, v := range references {
		out[k] = slices.Clone(v)
	}
	return out
}

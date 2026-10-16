package net

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// usable reports whether r can be evaluated: it is deployed. A resource that
// is deployed only under a parameter-driven condition or a loop is recorded as
// a skip, and the caller must not evaluate it. A literal false condition is
// silently not applicable.
func (o *outcome) usable(r sdk.ARMResource) bool {
	switch presence(r) {
	case armexpr.Deployed:
		return true
	case armexpr.MaybeDeployed:
		o.skip(foundrylink.SkipConditional + ": " + r.Name + " is deployed only under a condition or loop the template cannot resolve")
	}
	return false
}

// nameKey is a stable map key for name segments.
func nameKey(segs []armexpr.Segment) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.Text
	}
	return strings.Join(parts, "/")
}

// policyValue reads an effective-policy value. Keys are relative to "policy:"
// (ADR-007), for example "network.publicAccess".
func policyValue(in *sdk.Input, key string) (any, bool) {
	if in == nil || in.Policy == nil {
		return nil, false
	}
	return in.Policy.Get(key)
}

func policyBool(in *sdk.Input, key string) bool {
	v, ok := policyValue(in, key)
	b, isB := v.(bool)
	return ok && isB && b
}

func policyString(in *sdk.Input, key string) string {
	v, _ := policyValue(in, key)
	s, _ := v.(string)
	return lc(s)
}

// policyListHas reports whether a list-valued policy key contains item.
func policyListHas(in *sdk.Input, key, item string) bool {
	v, ok := policyValue(in, key)
	if !ok {
		return false
	}
	switch xs := v.(type) {
	case []string:
		for _, x := range xs {
			if strings.EqualFold(strings.TrimSpace(x), item) {
				return true
			}
		}
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok && strings.EqualFold(strings.TrimSpace(s), item) {
				return true
			}
		}
	}
	return false
}

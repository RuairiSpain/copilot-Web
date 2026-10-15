package rel

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The helpers in this file are a narrow duplicate of the helpers in the ops and
// cost rule packages (no shared package by design). They compare expression text
// only and never evaluate an expression.

// isExpr reports whether s is an ARM expression: it starts with "[" and is not
// the "[[" escape for a literal leading bracket.
func isExpr(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[")
}

func normExpr(s string) string {
	t := strings.TrimSpace(s)
	if isExpr(t) && strings.HasSuffix(t, "]") {
		t = t[1 : len(t)-1]
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t), " ", ""))
}

type deployState int

const (
	stateLive deployState = iota
	stateAbsent
	stateUncertain
)

// stateOf classifies a resource using Conditional/Condition/Looped. A literal
// false condition means the resource is not deployed; any other condition or a
// copy loop leaves deployment uncertain.
func stateOf(r sdk.ARMResource) deployState {
	if r.Conditional {
		switch normExpr(r.Condition) {
		case "false", "false()":
			return stateAbsent
		case "true", "true()":
			if !r.Looped {
				return stateLive
			}
		}
		return stateUncertain
	}
	if r.Looped {
		return stateUncertain
	}
	return stateLive
}

// view splits the model's resources by deployment state.
type view struct {
	live []sdk.ARMResource
	unc  []sdk.ARMResource
}

func newView(in *sdk.Input) *view {
	v := &view{}
	if in == nil || in.ARM == nil {
		return v
	}
	for _, r := range in.ARM.Resources() {
		switch stateOf(r) {
		case stateLive:
			v.live = append(v.live, r)
		case stateUncertain:
			v.unc = append(v.unc, r)
		}
	}
	return v
}

func newOutcome(in *sdk.Input) *outcome { return &outcome{v: newView(in)} }

// of returns the live resources of the given type and records a note for every
// uncertain resource of that type, so the rule cannot report a clean pass.
func (o *outcome) of(typ string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range o.v.live {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, r)
		}
	}
	for _, r := range o.v.unc {
		if strings.EqualFold(r.Type, typ) {
			o.notes = append(o.notes, r.Name+": conditional or looped deployment cannot be resolved offline")
		}
	}
	return out
}

func (o *outcome) hasUncertain(typ string) bool {
	for _, r := range o.v.unc {
		if strings.EqualFold(r.Type, typ) {
			return true
		}
	}
	return false
}

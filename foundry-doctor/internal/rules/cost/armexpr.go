package cost

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// The helpers in this file are a narrow duplicate of the helpers in the ops and
// rel rule packages (no shared package by design). They compare expression text
// only and never evaluate an expression.

// isExpr reports whether s is an ARM expression: it starts with "[" and is not
// the "[[" escape for a literal leading bracket.
func isExpr(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[")
}

func normCond(s string) string {
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
		switch normCond(r.Condition) {
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

// liveResources returns the resources that are certainly deployed. cond is true
// when at least one resource with deployment state "uncertain" was dropped.
func liveResources(m sdk.ARMModel) (live []sdk.ARMResource, uncertain []sdk.ARMResource) {
	if m == nil {
		return nil, nil
	}
	for _, r := range m.Resources() {
		switch stateOf(r) {
		case stateLive:
			live = append(live, r)
		case stateUncertain:
			uncertain = append(uncertain, r)
		}
	}
	return live, uncertain
}

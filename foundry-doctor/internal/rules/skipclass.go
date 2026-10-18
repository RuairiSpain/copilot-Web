package rules

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SkipClass says whether a skip hid a check that could have produced a finding.
type SkipClass int

const (
	// SkipHidesCheck: the rule could not look because an input or capability was
	// missing, the version was unknown or unsupported, or the rule failed.
	// `--strict` exits 3 for these. This is the default for an unknown reason so
	// that a new reason fails safe.
	SkipHidesCheck SkipClass = iota
	// SkipByDesign: nothing was hidden. The rule is not implemented in this
	// build, the project has nothing the rule applies to, the profile defines no
	// key for it, or the rule needs infrastructure the project does not have
	// on disk. The report still lists the skip; `--strict` ignores it.
	SkipByDesign
)

// byDesignHeads lists the reason heads (text before the first ':') that are
// by design. This list is the single classification of skip reasons for
// `--strict` (ADR-010 decision 9). Add a reason here, with a test, before a
// rule emits it.
var byDesignHeads = []string{
	sdk.SkipNotImplemented,
	sdk.SkipNotApplicable,
	sdk.SkipSyntheticInfrastructure,
	"profile-key-missing",
	// Applicability decisions that rule packages emit without the
	// not-applicable head today.
	"dev-profile",
	"policy-managed",
	"api-not-fronting-a-model-endpoint",
	"no-explicit-observability-or-server-tracing-requirement",
	"no-ai-gateway-apis",
	"no-apis",
	"environment-not-classified-as-dev",
	"non-foundry-backend-host",
	"no-supported-fixed-capacity-resources",
	// "Nothing to check" heads: the project has no resource the rule applies to.
	"project-has-no-agent-services",
	"no-emit-token-metric-policy",
	"no-resilience-objective-declared",
	"no-access-requirement-declared",
	"backend-does-not-target-foundry-or-azure-openai",
	"schedule-null",
	"no-split-skill-in-project; no-explicit-query-definitions",
}

// byDesignInputDetails lists input-unavailable details that mean "the project
// has no such resource" rather than "the input could not be read".
var byDesignInputDetails = []string{
	"project has no capability host",
	"project has no search connection with index metadata",
	"no dependency is proven private-endpoint backed",
	"no bound connection targets a search, storage or cosmos db resource",
	"account has no model deployments",
}

// ReasonHead returns the reason without detail: the text before the first ':'.
func ReasonHead(reason string) string {
	head, _, _ := strings.Cut(reason, ":")
	return strings.TrimSpace(head)
}

// ClassifySkip classifies one skip for `--strict`.
func ClassifySkip(s sdk.Skip) SkipClass {
	head := ReasonHead(s.Reason)
	if slices.Contains(byDesignHeads, head) {
		return SkipByDesign
	}
	if head == sdk.SkipInputUnavailable {
		_, detail, _ := strings.Cut(s.Reason, ":")
		detail = strings.ToLower(strings.TrimSpace(detail))
		for _, d := range byDesignInputDetails {
			if strings.HasPrefix(detail, d) {
				return SkipByDesign
			}
		}
	}
	return SkipHidesCheck
}

// AnyHiddenCheck reports whether any skip hid a check.
func AnyHiddenCheck(skips []sdk.Skip) bool {
	return slices.ContainsFunc(skips, func(s sdk.Skip) bool { return ClassifySkip(s) == SkipHidesCheck })
}

// SkippedByReason counts skips by reason head.
func SkippedByReason(skips []sdk.Skip) map[string]int {
	out := map[string]int{}
	for _, s := range skips {
		out[ReasonHead(s.Reason)]++
	}
	return out
}

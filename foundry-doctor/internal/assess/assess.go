// Package assess aggregates rule findings into WAF-oriented control views.
package assess

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// State is the WAF assessment state.
type State string

// Assessment states. PASS, INFO and FAIL/WARNING come only from deterministic
// rule evidence. ATTESTED comes only from a recorded team answer. Skipped is
// never a pass.
const (
	StatePass          State = "PASS"
	StateFail          State = "FAIL"
	StateWarning       State = "WARNING"
	StateUnknown       State = "UNKNOWN"
	StateQuestion      State = "QUESTION"
	StateSkipped       State = "SKIPPED"
	StateInfo          State = "INFO"
	StateAttested      State = "ATTESTED"
	StateNotApplicable State = "NOT_APPLICABLE"
)

// States lists every state in report order.
var States = []State{
	StateFail, StateWarning, StateUnknown, StateQuestion, StateSkipped,
	StateAttested, StateInfo, StateNotApplicable, StatePass,
}

// Evidence bases recorded on every control.
const (
	BasisDeterministic = "deterministic"
	BasisAttested      = "attested"
	BasisNone          = "none"
)

// Definition maps one catalogue rule to one WAF-facing control.
type Definition struct {
	RuleID          string
	ControlID       string
	ControlTitle    string
	Pillar          string
	Costly          bool
	CostBasis       string
	CostSourceURL   string
	CostVerified    string
	NoEvidenceState State
	// QuestionKey is the flattened policy key whose boolean value answers the
	// question. QuestionReferenceKey, when set, must hold a non-empty string
	// for a true answer to count as attested.
	QuestionKey          string
	QuestionReferenceKey string
	QuestionPrompt       string
}

// WARARef is one WARA (APRL) recommendation reference.
type WARARef struct {
	ID         string `json:"id"`
	Label      string `json:"label,omitempty"`
	PGVerified string `json:"pgVerified"`
	SourceURL  string `json:"sourceUrl,omitempty"`
}

// Input is the aggregation input.
type Input struct {
	Profile     string
	ProjectPath string
	Catalogue   []catalog.Rule
	Definitions []Definition
	Findings    []sdk.Finding
	Skipped     []sdk.Skip
	Evaluated   []string
	Policy      map[string]any
	ToolVersion string
	GeneratedAt string
	ScopeNote   string
	Limitations []string
	WARALookups map[string][]WARARef
}

// Assessment is the aggregate WAF result.
type Assessment struct {
	Framework   string       `json:"framework"`
	Profile     string       `json:"profile"`
	ToolVersion string       `json:"toolVersion,omitempty"`
	GeneratedAt string       `json:"generatedAt,omitempty"`
	ProjectPath string       `json:"projectPath,omitempty"`
	ScopeNote   string       `json:"scopeNote,omitempty"`
	Decision    string       `json:"decision"`
	Limitations []string     `json:"limitations"`
	TopActions  []Action     `json:"topActions"`
	Controls    []Control    `json:"controls"`
	Summary     Summary      `json:"summary"`
	Unassessed  []ControlRef `json:"unassessed"`
	Costly      []Action     `json:"costlyRecommendations"`
}

// Action is a prioritised next step.
type Action struct {
	ControlID      string `json:"controlId"`
	RuleID         string `json:"ruleId"`
	Title          string `json:"title"`
	Pillar         string `json:"pillar"`
	State          State  `json:"state"`
	Mandatory      bool   `json:"mandatory"`
	Recommendation string `json:"recommendation,omitempty"`
	SourceURL      string `json:"sourceUrl,omitempty"`
	LastVerified   string `json:"lastVerified,omitempty"`
	ProjectOpinion bool   `json:"projectOpinion"`
	Costly         bool   `json:"costly"`
	CostBasis      string `json:"costBasis,omitempty"`
	CostSourceURL  string `json:"costSourceUrl,omitempty"`
	CostVerified   string `json:"costLastVerified,omitempty"`
}

// ControlRef is a light-weight control reference.
type ControlRef struct {
	ControlID   string `json:"controlId"`
	RuleID      string `json:"ruleId"`
	Title       string `json:"title"`
	Pillar      string `json:"pillar"`
	State       State  `json:"state"`
	StateReason string `json:"stateReason,omitempty"`
}

// Summary is the overall state count summary.
type Summary struct {
	States  map[State]int            `json:"states"`
	Pillars map[string]SummaryPillar `json:"pillars"`
}

// SummaryPillar holds per-pillar counts.
type SummaryPillar struct {
	States map[State]int `json:"states"`
}

// Control is one WAF-facing control assessment.
type Control struct {
	ControlID      string        `json:"controlId"`
	RuleID         string        `json:"ruleId"`
	RuleVersion    int           `json:"ruleVersion,omitempty"`
	Title          string        `json:"title"`
	Pillar         string        `json:"pillar"`
	State          State         `json:"state"`
	Basis          string        `json:"basis,omitempty"`
	StateReason    string        `json:"stateReason,omitempty"`
	Mandatory      bool          `json:"mandatory"`
	Costly         bool          `json:"costly"`
	CostBasis      string        `json:"costBasis,omitempty"`
	CostSourceURL  string        `json:"costSourceUrl,omitempty"`
	CostVerified   string        `json:"costLastVerified,omitempty"`
	ProjectOpinion bool          `json:"projectOpinion"`
	SourceURL      string        `json:"sourceUrl,omitempty"`
	LastVerified   string        `json:"lastVerified,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
	Fix            string        `json:"fix,omitempty"`
	QuestionPrompt string        `json:"questionPrompt,omitempty"`
	QuestionAnswer string        `json:"questionAnswer,omitempty"`
	WARAReferences []WARARef     `json:"waraReferences,omitempty"`
	ExternalIDs    []string      `json:"externalOverlap,omitempty"`
	Findings       []sdk.Finding `json:"findings,omitempty"`
	Skipped        []sdk.Skip    `json:"skipped,omitempty"`
	Evaluated      bool          `json:"evaluated"`
}

// PillarName returns the Azure Well-Architected Framework pillar name for a
// catalogue pillar key (https://learn.microsoft.com/azure/well-architected/pillars).
func PillarName(p string) string {
	switch p {
	case "reliability":
		return "Reliability"
	case "security":
		return "Security"
	case "cost":
		return "Cost Optimization"
	case "operations":
		return "Operational Excellence"
	case "performance":
		return "Performance Efficiency"
	}
	return p
}

// Pillars lists catalogue pillar keys in report order.
var Pillars = []string{"security", "reliability", "operations", "cost", "performance"}

// BaseLimitations are printed on every assessment.
var BaseLimitations = []string{
	"This report maps available Foundry Doctor evidence to selected Azure Well-Architected aligned controls. It is not a certification and does not claim complete WAF compliance.",
	"Controls without complete deterministic evidence remain visible as UNKNOWN, QUESTION or SKIPPED rather than being treated as passing; a partly skipped evaluation is UNKNOWN, never PASS.",
	"ATTESTED controls rest on an answer the team recorded in Foundry Doctor configuration. Foundry Doctor did not verify the answer.",
	"Control IDs are Foundry Doctor rule IDs. They are not mapped to WAF checklist codes in this release. Pillar assignments come from the rule catalogue and are project judgement, not Microsoft guidance.",
	"A suppressed or baselined finding is shown as WARNING and stays visible in the evidence pack. It does not by itself produce a No-go decision, so the decision can be Proceed with conditions while accepted failures remain.",
	"The decision wording and the top-action ranking are project opinion, not Microsoft guidance.",
}

// Build aggregates findings into control views.
func Build(in Input) (Assessment, error) {
	rulesByID := make(map[string]catalog.Rule, len(in.Catalogue))
	for _, r := range in.Catalogue {
		rulesByID[r.ID] = r
	}
	findingsByRule := map[string][]sdk.Finding{}
	for _, f := range in.Findings {
		findingsByRule[f.RuleID] = append(findingsByRule[f.RuleID], f)
	}
	skipsByRule := map[string][]sdk.Skip{}
	for _, s := range in.Skipped {
		skipsByRule[s.RuleID] = append(skipsByRule[s.RuleID], s)
	}
	evaluated := map[string]bool{}
	for _, id := range in.Evaluated {
		evaluated[id] = true
	}

	out := Assessment{
		Framework:   "azure-waf",
		Profile:     in.Profile,
		ToolVersion: in.ToolVersion,
		GeneratedAt: in.GeneratedAt,
		ProjectPath: in.ProjectPath,
		ScopeNote:   in.ScopeNote,
		Limitations: append(slices.Clone(BaseLimitations), in.Limitations...),
		Summary:     Summary{States: map[State]int{}, Pillars: map[string]SummaryPillar{}},
	}

	controls := make([]Control, 0, len(in.Definitions))
	for _, def := range in.Definitions {
		rule, ok := rulesByID[def.RuleID]
		if !ok {
			return Assessment{}, fmt.Errorf("assess: rule %s missing from catalogue", def.RuleID)
		}
		ctrl := Control{
			ControlID:      choose(def.ControlID, rule.ID),
			RuleID:         rule.ID,
			RuleVersion:    rule.Version,
			Title:          choose(def.ControlTitle, rule.Title),
			Pillar:         choose(def.Pillar, rule.Pillar),
			Mandatory:      rule.Category == "must-have",
			Costly:         def.Costly,
			CostBasis:      def.CostBasis,
			CostSourceURL:  def.CostSourceURL,
			CostVerified:   def.CostVerified,
			ProjectOpinion: rule.Status == catalog.StatusProductOpinion || slices.Contains(rule.Basis, "opinion"),
			SourceURL:      firstSourceURL(rule),
			LastVerified:   firstLastVerified(rule),
			Recommendation: strings.TrimSpace(rule.Recommendation),
			Fix:            strings.TrimSpace(rule.Fix),
			QuestionPrompt: def.QuestionPrompt,
			WARAReferences: slices.Clone(in.WARALookups[rule.ID]),
			ExternalIDs:    externalIDs(rule),
			Findings:       slices.Clone(findingsByRule[rule.ID]),
			Skipped:        slices.Clone(skipsByRule[rule.ID]),
			Evaluated:      evaluated[rule.ID],
		}
		ctrl.State, ctrl.Basis, ctrl.StateReason, ctrl.QuestionAnswer = stateFor(ctrl, def, in.Policy)
		controls = append(controls, ctrl)
	}

	slices.SortFunc(controls, func(a, b Control) int {
		if a.Pillar != b.Pillar {
			return strings.Compare(a.Pillar, b.Pillar)
		}
		if a.ControlID != b.ControlID {
			return strings.Compare(a.ControlID, b.ControlID)
		}
		return strings.Compare(a.RuleID, b.RuleID)
	})
	out.Controls = controls
	for _, c := range controls {
		out.Summary.States[c.State]++
		p := out.Summary.Pillars[c.Pillar]
		if p.States == nil {
			p.States = map[State]int{}
		}
		p.States[c.State]++
		out.Summary.Pillars[c.Pillar] = p
		if Unassessed(c.State) {
			out.Unassessed = append(out.Unassessed, toRef(c))
		}
		if costlyRecommendation(c) {
			out.Costly = append(out.Costly, toAction(c))
		}
	}
	out.TopActions = topActions(controls)
	out.Decision = decisionFor(controls)
	return out, nil
}

// Unassessed reports whether a state means the control was not fully assessed.
func Unassessed(s State) bool {
	return s == StateUnknown || s == StateQuestion || s == StateSkipped
}

// Outstanding reports whether a state calls for action or review.
func Outstanding(s State) bool {
	return s == StateFail || s == StateWarning || s == StateUnknown || s == StateQuestion
}

// stateFor derives the state, its evidence basis, a short reason and the
// recorded answer (question controls only).
func stateFor(c Control, d Definition, policy map[string]any) (State, string, string, string) {
	activeErr, activeWarn, exceptions, info := false, false, false, false
	for _, f := range c.Findings {
		switch {
		case f.Severity == sdk.SeverityInfo:
			info = true
		case f.Suppressed != nil || f.Baselined:
			exceptions = true
		case f.Severity == sdk.SeverityError:
			activeErr = true
		default:
			activeWarn = true
		}
	}
	switch {
	case activeErr:
		return StateFail, BasisDeterministic, "An active error-severity finding remains for the selected profile.", ""
	case activeWarn:
		return StateWarning, BasisDeterministic, "An active warning-severity finding remains for the selected profile.", ""
	case exceptions:
		return StateWarning, BasisDeterministic, "Findings exist but are suppressed or baselined; they remain open debt.", ""
	}
	if d.QuestionKey != "" {
		return answerState(d, policy)
	}
	var hidden, delegated, undeclared, notApp []sdk.Skip
	for _, s := range c.Skipped {
		switch classifySkip(s) {
		case skipDelegated:
			delegated = append(delegated, s)
		case skipUndeclared:
			undeclared = append(undeclared, s)
		case skipNotApplicable:
			notApp = append(notApp, s)
		default:
			hidden = append(hidden, s)
		}
	}
	switch {
	case len(hidden) > 0 && c.Evaluated:
		return StateUnknown, BasisDeterministic, fmt.Sprintf("Partial evaluation: %d check(s) skipped (%s).", len(hidden), skipReasons(hidden)), ""
	case len(hidden) > 0:
		return StateSkipped, BasisNone, "Skipped: " + skipReasons(hidden) + ".", ""
	case len(undeclared) > 0:
		return StateQuestion, BasisNone, "No requirement or objective is declared (" + skipReasons(undeclared) + "); the control needs a team decision.", ""
	case len(delegated) > 0:
		return StateAttested, BasisAttested, "Delegated to Azure Policy by configuration; assignment and compliance not verified (" + skipReasons(delegated) + ").", ""
	case c.Evaluated && info:
		return StateInfo, BasisDeterministic, "Evaluated; informational at this profile's severity, see evidence.", ""
	case c.Evaluated && len(notApp) > 0:
		return StatePass, BasisDeterministic, "Evaluated with no remaining finding; other checks were not applicable (" + skipReasons(notApp) + ").", ""
	case c.Evaluated:
		return StatePass, BasisDeterministic, "Evaluated with no remaining finding and no skipped check.", ""
	case len(notApp) > 0:
		return StateNotApplicable, BasisDeterministic, "Not applicable: " + skipReasons(notApp) + ".", ""
	case d.NoEvidenceState != "":
		return d.NoEvidenceState, BasisNone, "No evidence was recorded for this control.", ""
	}
	return StateUnknown, BasisNone, "No evidence was recorded for this control.", ""
}

// answerState evaluates a recorded question answer. Only a boolean true with
// the required reference is ATTESTED; false is WARNING; anything else stays a
// QUESTION.
func answerState(d Definition, policy map[string]any) (State, string, string, string) {
	v, ok := policy[d.QuestionKey]
	if !ok {
		return StateQuestion, BasisNone, "No answer recorded (" + d.QuestionKey + ").", ""
	}
	answer := fmt.Sprint(v)
	b, isBool := v.(bool)
	switch {
	case !isBool:
		return StateQuestion, BasisNone, "Recorded answer for " + d.QuestionKey + " is not true or false.", answer
	case !b:
		return StateWarning, BasisAttested, "The team recorded " + d.QuestionKey + ": false.", answer
	}
	if d.QuestionReferenceKey != "" {
		ref, _ := policy[d.QuestionReferenceKey].(string)
		if strings.TrimSpace(ref) == "" {
			return StateQuestion, BasisNone, "Answer recorded without the required reference (" + d.QuestionReferenceKey + ").", answer
		}
		answer += "; reference: " + strings.TrimSpace(ref)
	}
	return StateAttested, BasisAttested, "Answered by team attestation; not verified by Foundry Doctor.", answer
}

type skipKind int

const (
	// skipHidden: the check did not run; the control was not assessed.
	skipHidden skipKind = iota
	// skipNotApplicable: the project has no resource or scope the rule checks.
	skipNotApplicable
	// skipDelegated: configuration says another mechanism (Azure Policy,
	// central DNS) owns the setting. Foundry Doctor did not verify it.
	skipDelegated
	// skipUndeclared: the rule needs a requirement or objective that the
	// team has not declared.
	skipUndeclared
)

// notApplicableHeads are reason heads that mean the project has nothing the
// rule checks. This list is the WAF view's own classification; it is
// deliberately separate from rules.ClassifySkip, which decides --strict exit
// codes (ADR-010 decision 9, ADR-026).
var notApplicableHeads = []string{
	sdk.SkipNotApplicable,
	"dev-profile",
	"environment-not-classified-as-dev",
	"no-supported-fixed-capacity-resources",
	"project-has-no-agent-services",
	"no-apis",
	"no-ai-gateway-apis",
	"no-emit-token-metric-policy",
	"api-not-fronting-a-model-endpoint",
	"non-foundry-backend-host",
	"backend-does-not-target-foundry-or-azure-openai",
	"schedule-null",
}

// notApplicableInputDetails are input-unavailable details that state the
// project has no such resource, as opposed to an input that could not be read.
var notApplicableInputDetails = []string{
	"project has no capability host",
	"project has no search connection with index metadata",
	"no bound connection targets a search, storage or cosmos db resource",
	"account has no model deployments",
}

// delegatedHeads are configuration delegations (ADR-026 item 3).
var delegatedHeads = []string{"policy-managed", "central-dns"}

func classifySkip(s sdk.Skip) skipKind {
	head, detail, _ := strings.Cut(s.Reason, ":")
	head = strings.TrimSpace(head)
	switch {
	case slices.Contains(delegatedHeads, head):
		return skipDelegated
	case strings.HasPrefix(head, "no-") && (strings.HasSuffix(head, "-declared") || strings.HasSuffix(head, "-requirement")):
		return skipUndeclared
	case slices.Contains(notApplicableHeads, head):
		return skipNotApplicable
	case head == sdk.SkipInputUnavailable:
		detail = strings.ToLower(strings.TrimSpace(detail))
		for _, d := range notApplicableInputDetails {
			if strings.HasPrefix(detail, d) {
				return skipNotApplicable
			}
		}
	}
	return skipHidden
}

func skipReasons(skips []sdk.Skip) string {
	seen := map[string]bool{}
	var parts []string
	for _, s := range skips {
		r := strings.TrimSpace(s.Reason)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		parts = append(parts, r)
	}
	if len(parts) == 0 {
		return "no reason recorded"
	}
	return strings.Join(parts, "; ")
}

// costlyRecommendation: an optional control whose remediation adds Azure cost
// and that is still outstanding or unassessed. Mandatory controls never land
// here (the mapping file forbids it); they stay in the top actions.
func costlyRecommendation(c Control) bool {
	if !c.Costly || c.Mandatory {
		return false
	}
	return Outstanding(c.State) || c.State == StateSkipped
}

func topActions(controls []Control) []Action {
	type ranked struct {
		Action
		rank int
	}
	var rows []ranked
	for _, c := range controls {
		if !Outstanding(c.State) || costlyRecommendation(c) {
			continue
		}
		rank := 90
		switch c.State {
		case StateFail:
			rank = 10
		case StateWarning:
			rank = 20
		case StateUnknown:
			rank = 30
		case StateQuestion:
			rank = 40
		}
		if !c.Mandatory {
			rank += 20
		}
		rows = append(rows, ranked{rank: rank, Action: toAction(c)})
	}
	slices.SortFunc(rows, func(a, b ranked) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		if a.Pillar != b.Pillar {
			return strings.Compare(a.Pillar, b.Pillar)
		}
		return strings.Compare(a.ControlID, b.ControlID)
	})
	if len(rows) > 5 {
		rows = rows[:5]
	}
	out := make([]Action, len(rows))
	for i, r := range rows {
		out[i] = r.Action
	}
	return out
}

func decisionFor(controls []Control) string {
	has := func(match func(Control) bool) bool {
		return slices.ContainsFunc(controls, match)
	}
	switch {
	case has(func(c Control) bool { return c.Mandatory && c.State == StateFail }):
		return "No-go within assessed scope: one or more mandatory controls failed."
	case has(func(c Control) bool {
		return c.Mandatory && (c.State == StateWarning || c.State == StateAttested || Unassessed(c.State))
	}):
		return "Proceed with conditions: mandatory controls need remediation or manual review."
	case has(func(c Control) bool { return Outstanding(c.State) }):
		return "Proceed with recommendations: no mandatory failures, but outstanding improvements remain."
	default:
		unassessed := 0
		for _, c := range controls {
			if Unassessed(c.State) {
				unassessed++
			}
		}
		if unassessed > 0 {
			return fmt.Sprintf("Proceed within assessed scope: no outstanding control failures were found, but %d controls were not assessed.", unassessed)
		}
		return "Proceed within assessed scope: no outstanding control failures were found in the assessed set."
	}
}

func firstSourceURL(r catalog.Rule) string {
	if len(r.Sources) == 0 {
		return ""
	}
	return r.Sources[0].URL
}

func firstLastVerified(r catalog.Rule) string {
	if len(r.Sources) == 0 {
		return ""
	}
	return r.Sources[0].LastVerified
}

func externalIDs(r catalog.Rule) []string {
	var out []string
	for _, id := range r.Overlap.PSRule {
		out = append(out, "PSRule "+id)
	}
	for _, id := range r.Overlap.AzurePolicy {
		out = append(out, "Azure Policy "+id)
	}
	return out
}

func choose(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func toRef(c Control) ControlRef {
	return ControlRef{ControlID: c.ControlID, RuleID: c.RuleID, Title: c.Title, Pillar: c.Pillar, State: c.State, StateReason: c.StateReason}
}

func toAction(c Control) Action {
	recommendation := c.Recommendation
	if c.State == StateUnknown || c.State == StateQuestion || c.State == StateSkipped {
		// No defect was detected for these controls, so the rule's fix text
		// would tell an owner to repair something that may be fine.
		recommendation = "Obtain evidence for this control before relying on it."
		if c.StateReason != "" {
			recommendation += " " + c.StateReason
		}
	}
	return Action{
		ControlID:      c.ControlID,
		RuleID:         c.RuleID,
		Title:          c.Title,
		Pillar:         c.Pillar,
		State:          c.State,
		Mandatory:      c.Mandatory,
		Recommendation: recommendation,
		SourceURL:      c.SourceURL,
		LastVerified:   c.LastVerified,
		ProjectOpinion: c.ProjectOpinion,
		Costly:         c.Costly,
		CostBasis:      c.CostBasis,
		CostSourceURL:  c.CostSourceURL,
		CostVerified:   c.CostVerified,
	}
}

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

// Assessment states.
const (
	StatePass     State = "PASS"
	StateFail     State = "FAIL"
	StateWarning  State = "WARNING"
	StateUnknown  State = "UNKNOWN"
	StateQuestion State = "QUESTION"
	StateSkipped  State = "SKIPPED"
)

// Definition maps one catalogue rule to one WAF-facing control.
type Definition struct {
	RuleID          string
	ControlID       string
	ControlTitle    string
	Pillar          string
	Costly          bool
	NoEvidenceState State
	QuestionKey     string
	QuestionPrompt  string
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
	ScopeNote   string
	Limitations []string
	WARALookups map[string][]string
}

// Assessment is the aggregate WAF result.
type Assessment struct {
	Framework   string       `json:"framework"`
	Profile     string       `json:"profile"`
	ToolVersion string       `json:"toolVersion,omitempty"`
	ProjectPath string       `json:"projectPath,omitempty"`
	ScopeNote   string       `json:"scopeNote,omitempty"`
	Decision    string       `json:"decision"`
	Limitations []string     `json:"limitations"`
	TopActions  []Action     `json:"topActions"`
	Controls    []Control    `json:"controls"`
	Summary     Summary      `json:"summary"`
	Unassessed  []ControlRef `json:"unassessed"`
	Costly      []ControlRef `json:"costlyRecommendations"`
}

// Action is a prioritised next step.
type Action struct {
	ControlID      string `json:"controlId"`
	RuleID         string `json:"ruleId"`
	Title          string `json:"title"`
	Pillar         string `json:"pillar"`
	State          State  `json:"state"`
	Recommendation string `json:"recommendation,omitempty"`
	SourceURL      string `json:"sourceUrl,omitempty"`
	LastVerified   string `json:"lastVerified,omitempty"`
	Costly         bool   `json:"costly"`
}

// ControlRef is a light-weight control reference.
type ControlRef struct {
	ControlID string `json:"controlId"`
	RuleID    string `json:"ruleId"`
	Title     string `json:"title"`
	Pillar    string `json:"pillar"`
	State     State  `json:"state"`
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
	Title          string        `json:"title"`
	Pillar         string        `json:"pillar"`
	State          State         `json:"state"`
	Mandatory      bool          `json:"mandatory"`
	Costly         bool          `json:"costly"`
	ProjectOpinion bool          `json:"projectOpinion"`
	SourceURL      string        `json:"sourceUrl,omitempty"`
	LastVerified   string        `json:"lastVerified,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
	Fix            string        `json:"fix,omitempty"`
	QuestionPrompt string        `json:"questionPrompt,omitempty"`
	QuestionAnswer string        `json:"questionAnswer,omitempty"`
	WARAReferences []string      `json:"waraReferences,omitempty"`
	Findings       []sdk.Finding `json:"findings,omitempty"`
	Skipped        []sdk.Skip    `json:"skipped,omitempty"`
	Evaluated      bool          `json:"evaluated"`
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
		ProjectPath: in.ProjectPath,
		ScopeNote:   in.ScopeNote,
		Limitations: append([]string{
			"This report maps available Foundry Doctor evidence to selected Azure Well-Architected aligned controls. It is not a certification and does not claim complete WAF compliance.",
			"Controls without deterministic evidence remain visible as UNKNOWN, QUESTION or SKIPPED rather than being treated as passing.",
		}, in.Limitations...),
		Summary: Summary{States: map[State]int{}, Pillars: map[string]SummaryPillar{}},
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
			Title:          choose(def.ControlTitle, rule.Title),
			Pillar:         choose(def.Pillar, rule.Pillar),
			Mandatory:      rule.Category == "must-have",
			Costly:         def.Costly,
			ProjectOpinion: rule.Status == catalog.StatusProductOpinion || slices.Contains(rule.Basis, "opinion"),
			SourceURL:      firstSourceURL(rule),
			LastVerified:   firstLastVerified(rule),
			Recommendation: strings.TrimSpace(rule.Recommendation),
			Fix:            strings.TrimSpace(rule.Fix),
			QuestionPrompt: def.QuestionPrompt,
			WARAReferences: append([]string(nil), in.WARALookups[rule.ID]...),
			Findings:       append([]sdk.Finding(nil), findingsByRule[rule.ID]...),
			Skipped:        append([]sdk.Skip(nil), skipsByRule[rule.ID]...),
			Evaluated:      evaluated[rule.ID],
		}
		ctrl.State = stateFor(ctrl, def, in.Policy)
		if def.QuestionKey != "" {
			if v, ok := in.Policy[def.QuestionKey]; ok {
				ctrl.QuestionAnswer = fmt.Sprint(v)
				if ctrl.State == StateQuestion {
					ctrl.State = StatePass
				}
			}
		}
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
		if c.State == StateUnknown || c.State == StateQuestion || c.State == StateSkipped {
			out.Unassessed = append(out.Unassessed, toRef(c))
		}
		if c.Costly {
			out.Costly = append(out.Costly, toRef(c))
		}
	}
	out.TopActions = topActions(controls)
	out.Decision = decisionFor(controls)
	return out, nil
}

func stateFor(c Control, d Definition, policy map[string]any) State {
	activeErr, activeWarn, exceptions := false, false, false
	for _, f := range c.Findings {
		switch {
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
		return StateFail
	case activeWarn || exceptions:
		return StateWarning
	case c.Evaluated:
		return StatePass
	}
	if d.QuestionKey != "" {
		if _, ok := policy[d.QuestionKey]; ok {
			return StatePass
		}
	}
	if d.NoEvidenceState != "" {
		return d.NoEvidenceState
	}
	if len(c.Skipped) != 0 {
		return StateSkipped
	}
	return StateUnknown
}

func topActions(controls []Control) []Action {
	type ranked struct {
		Action
		rank int
	}
	var rows []ranked
	for _, c := range controls {
		if c.State == StatePass || c.State == StateSkipped {
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
		if c.Costly {
			rank += 10
		}
		rows = append(rows, ranked{
			rank: rank,
			Action: Action{
				ControlID:      c.ControlID,
				RuleID:         c.RuleID,
				Title:          c.Title,
				Pillar:         c.Pillar,
				State:          c.State,
				Recommendation: c.Recommendation,
				SourceURL:      c.SourceURL,
				LastVerified:   c.LastVerified,
				Costly:         c.Costly,
			},
		})
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
		for _, c := range controls {
			if match(c) {
				return true
			}
		}
		return false
	}
	switch {
	case has(func(c Control) bool { return c.Mandatory && c.State == StateFail }):
		return "No-go within assessed scope: one or more mandatory controls failed."
	case has(func(c Control) bool {
		return c.Mandatory && (c.State == StateWarning || c.State == StateUnknown || c.State == StateQuestion || c.State == StateSkipped)
	}):
		return "Proceed with conditions: mandatory controls need remediation or manual review."
	case has(func(c Control) bool {
		return c.State == StateFail || c.State == StateWarning || c.State == StateUnknown || c.State == StateQuestion
	}):
		return "Proceed with recommendations: no mandatory failures, but outstanding improvements remain."
	default:
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

func choose(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func toRef(c Control) ControlRef {
	return ControlRef{ControlID: c.ControlID, RuleID: c.RuleID, Title: c.Title, Pillar: c.Pillar, State: c.State}
}

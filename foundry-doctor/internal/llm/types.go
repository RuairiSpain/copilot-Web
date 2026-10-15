package llm

import (
	"context"
	"time"
)

// Audience selects the tone and focus of generated advisory text.
type Audience string

const (
	AudienceOwner     Audience = "owner"
	AudienceDeveloper Audience = "developer"
)

// Config is the resolved provider configuration for one invocation.
type Config struct {
	Provider         string
	Endpoint         string
	Deployment       string
	Scope            string
	Timeout          time.Duration
	MaxPromptBytes   int
	MaxResponseBytes int
}

// Options are per-command overrides. Empty fields fall back to environment
// configuration and then built-in defaults.
type Options struct {
	Provider string
	Timeout  time.Duration
}

// Finding is the allow-listed, redacted DTO sent to an LLM provider.
type Finding struct {
	RuleID         string `json:"ruleId"`
	Explanation    string `json:"explanation"`
	Evidence       string `json:"evidence,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

// RuleInput is the minimized payload for a single-rule advisory explanation.
type RuleInput struct {
	Audience Audience `json:"audience"`
	Finding  Finding  `json:"finding"`
}

// AssessmentInput is the minimized payload for an assessment advisory.
type AssessmentInput struct {
	Audience Audience  `json:"audience"`
	Findings []Finding `json:"findings"`
}

// Prompt is the provider-neutral prompt contract.
type Prompt struct {
	Version string
	System  string
	User    string
	Schema  map[string]any
}

// Completion is one provider response before validation.
type Completion struct {
	Provider   string
	Model      string
	Deployment string
	Content    string
}

// Provider generates one advisory completion.
type Provider interface {
	Complete(ctx context.Context, cfg Config, prompt Prompt) (Completion, error)
}

// Metadata is rendered next to generated text so provenance is explicit.
type Metadata struct {
	Provider      string
	Model         string
	Deployment    string
	PromptVersion string
}

// Section is one validated narrative section.
type Section struct {
	Heading string   `json:"heading"`
	Summary string   `json:"summary"`
	Actions []string `json:"actions,omitempty"`
	RuleIDs []string `json:"ruleIds,omitempty"`
}

// Narrative is the validated advisory result.
type Narrative struct {
	Summary  string    `json:"summary"`
	Sections []Section `json:"sections"`
	Metadata Metadata  `json:"-"`
}

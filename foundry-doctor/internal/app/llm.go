package app

import (
	"context"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
)

// Narrator generates advisory text over deterministic rule and assessment
// data. Callers must keep findings and exit codes authoritative.
type Narrator interface {
	ExplainRule(ctx context.Context, ruleID string, audience llm.Audience, opt llm.Options) (llm.Narrative, error)
	ExplainAssessment(ctx context.Context, a assess.Assessment, audience llm.Audience, opt llm.Options) (llm.Narrative, error)
}

// ExplainRequest extends the rule explanation path with optional advisory LLM
// generation.
type ExplainRequest struct {
	RuleID      string
	Format      string
	LLMExplain  bool
	LLMAudience llm.Audience
	LLMProvider string
	LLMTimeout  time.Duration
}

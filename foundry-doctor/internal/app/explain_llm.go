package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
)

// ExplainRule renders rule documentation and optionally appends an advisory
// narrative. Unknown rules remain a usage error (exit 2).
func ExplainRule(ctx context.Context, svc Services, req ExplainRequest, stdout io.Writer) (int, error) {
	if req.Format == "" {
		req.Format = "console"
	}
	if req.Format != "console" && req.Format != "markdown" {
		err := Usagef("invalid --format %q for explain (want console or markdown)", req.Format)
		return ExitCodeForError(err), err
	}
	if req.RuleID == "" {
		err := Usagef("rule ID is required")
		return ExitCodeForError(err), err
	}
	if req.LLMExplain {
		if req.LLMAudience == "" {
			req.LLMAudience = llm.AudienceDeveloper
		}
		switch req.LLMAudience {
		case llm.AudienceOwner, llm.AudienceDeveloper:
		default:
			err := Usagef("invalid --audience %q (want owner or developer)", req.LLMAudience)
			return ExitCodeForError(err), err
		}
		if req.LLMTimeout != 0 && (req.LLMTimeout < llm.MinimumTimeout || req.LLMTimeout > llm.MaximumTimeout) {
			err := Usagef("--llm-timeout must be between %s and %s", llm.MinimumTimeout, llm.MaximumTimeout)
			return ExitCodeForError(err), err
		}
	}
	var buf bytes.Buffer
	if err := svc.Explainer.Explain(ctx, req.RuleID, req.Format, &buf); err != nil {
		return ExitCodeForError(err), err
	}
	out := buf.Bytes()
	if req.LLMExplain && svc.Narrator != nil {
		narrative, note, err := explainNarrative(ctx, svc, req)
		if err != nil {
			return ExitCodeForError(err), err
		}
		out = appendRenderedNarrative(out, req.Format, narrative, note)
	}
	if _, err := stdout.Write(out); err != nil {
		return ExitInternal, fmt.Errorf("write explanation: %w", err)
	}
	return ExitOK, nil
}

func explainNarrative(ctx context.Context, svc Services, req ExplainRequest) (*llm.Narrative, string, error) {
	narrative, err := svc.Narrator.ExplainRule(ctx, req.RuleID, req.LLMAudience, llm.Options{
		Provider: req.LLMProvider,
		Timeout:  req.LLMTimeout,
	})
	if err == nil {
		return &narrative, "", nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, "", err
	}
	return nil, "LLM advisory unavailable (" + findings.Redact(strings.TrimSpace(err.Error())) + "). Deterministic rule documentation remains authoritative.", nil
}

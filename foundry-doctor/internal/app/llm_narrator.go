package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm/prompts"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm/redact"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm/validate"
)

type lazyNarrator struct {
	cat       *lazyCatalog
	getenv    func(string) string
	providers map[string]llm.Provider
}

func newNarrator(cat *lazyCatalog, getenv func(string) string) Narrator {
	return lazyNarrator{
		cat:    cat,
		getenv: getenv,
		providers: map[string]llm.Provider{
			llm.DefaultProvider: llm.NewAzureOpenAIProvider(http.DefaultClient, azure.NewDefaultCredential(nil)),
		},
	}
}

func (n lazyNarrator) ExplainRule(ctx context.Context, ruleID string, audience llm.Audience, opt llm.Options) (llm.Narrative, error) {
	rule, err := n.catalogRule(ctx, ruleID)
	if err != nil {
		return llm.Narrative{}, err
	}
	cfg, provider, err := llm.ResolveConfig(n.getenv, opt, n.providers)
	if err != nil {
		return llm.Narrative{}, err
	}
	input := redact.Rule(rule, audience)
	prompt, err := prompts.Rule(input, cfg.MaxPromptBytes)
	if err != nil {
		return llm.Narrative{}, err
	}
	providerCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	completion, err := provider.Complete(providerCtx, cfg, prompt)
	if err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() == nil {
			return llm.Narrative{}, fmt.Errorf("%w: provider timed out", llm.ErrUnavailable)
		}
		return llm.Narrative{}, err
	}
	out, err := validate.Narrative(completion.Content, validate.Options{
		AllowedRuleIDs: allowedRuleIDs([]string{rule.ID}),
		AllowedActions: allowedActions([]string{input.Finding.Recommendation}),
	})
	if err != nil {
		return llm.Narrative{}, err
	}
	out.Metadata = llm.Metadata{
		Provider:      firstNonEmpty(completion.Provider, cfg.Provider),
		Model:         completion.Model,
		Deployment:    firstNonEmpty(completion.Deployment, cfg.Deployment),
		PromptVersion: prompt.Version,
	}
	return out, nil
}

func (n lazyNarrator) ExplainAssessment(ctx context.Context, a assess.Assessment, audience llm.Audience, opt llm.Options) (llm.Narrative, error) {
	rules, err := n.cat.get(ctx)
	if err != nil {
		return llm.Narrative{}, err
	}
	cfg, provider, err := llm.ResolveConfig(n.getenv, opt, n.providers)
	if err != nil {
		return llm.Narrative{}, err
	}
	input, err := redact.Assessment(rules, a, audience)
	if err != nil {
		return llm.Narrative{}, err
	}
	prompt, err := prompts.Assessment(input, cfg.MaxPromptBytes)
	if err != nil {
		return llm.Narrative{}, err
	}
	providerCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	completion, err := provider.Complete(providerCtx, cfg, prompt)
	if err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() == nil {
			return llm.Narrative{}, fmt.Errorf("%w: provider timed out", llm.ErrUnavailable)
		}
		return llm.Narrative{}, err
	}
	out, err := validate.Narrative(completion.Content, validate.Options{
		AllowedRuleIDs: allowedRuleIDs(ruleIDs(input.Findings)),
		AllowedActions: allowedActions(recommendations(input.Findings)),
	})
	if err != nil {
		return llm.Narrative{}, err
	}
	out.Metadata = llm.Metadata{
		Provider:      firstNonEmpty(completion.Provider, cfg.Provider),
		Model:         completion.Model,
		Deployment:    firstNonEmpty(completion.Deployment, cfg.Deployment),
		PromptVersion: prompt.Version,
	}
	return out, nil
}

func (n lazyNarrator) catalogRule(ctx context.Context, ruleID string) (catalog.Rule, error) {
	rules, err := n.cat.get(ctx)
	if err != nil {
		return catalog.Rule{}, err
	}
	for _, rule := range rules {
		if strings.EqualFold(rule.ID, ruleID) {
			return rule, nil
		}
	}
	return catalog.Rule{}, fmt.Errorf("%w: rule %q not found in catalogue", llm.ErrUnavailable, ruleID)
}

func ruleIDs(in []llm.Finding) []string {
	out := make([]string, 0, len(in))
	for _, f := range in {
		out = append(out, f.RuleID)
	}
	return out
}

func recommendations(in []llm.Finding) []string {
	out := make([]string, 0, len(in))
	for _, f := range in {
		out = append(out, f.Recommendation)
	}
	return out
}

func allowedActions(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, action := range in {
		action = strings.TrimSpace(action)
		if action != "" {
			out[action] = struct{}{}
		}
	}
	return out
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

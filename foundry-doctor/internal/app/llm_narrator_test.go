package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
)

type recordingProvider struct {
	content string
	prompt  llm.Prompt
	cfg     llm.Config
}

func (p *recordingProvider) Complete(_ context.Context, cfg llm.Config, prompt llm.Prompt) (llm.Completion, error) {
	p.cfg = cfg
	p.prompt = prompt
	return llm.Completion{Provider: cfg.Provider, Model: "mock-model", Deployment: cfg.Deployment, Content: p.content}, nil
}

func narratorEnv(k string) string {
	switch k {
	case llm.EnvEndpoint:
		return "https://demo.openai.azure.com"
	case llm.EnvDeployment:
		return "demo"
	default:
		return ""
	}
}

// End to end through the real redact -> prompts -> validate pipeline with a
// mock provider (no network).
func TestLazyNarratorValidatesProviderOutput(t *testing.T) {
	good := &recordingProvider{content: `{"summary":"Advisory explanation of this configuration rule.","sections":[{"heading":"Recommended remediation","summary":"Apply the documented recommendation for FND-CFG-001.","actions":[],"ruleIds":["FND-CFG-001"]}]}`}
	n := lazyNarrator{cat: &lazyCatalog{}, getenv: narratorEnv, providers: map[string]llm.Provider{llm.DefaultProvider: good}}
	out, err := n.ExplainRule(context.Background(), "FND-CFG-001", llm.AudienceDeveloper, llm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Sections) != 1 || out.Metadata.Model != "mock-model" || out.Metadata.PromptVersion == "" {
		t.Fatalf("out=%+v", out)
	}
	if !strings.Contains(good.prompt.User, "FND-CFG-001") || good.cfg.Scope != llm.DefaultScope || good.cfg.MaxOutputTokens == 0 {
		t.Fatalf("prompt/config not as expected: scope=%q max=%d", good.cfg.Scope, good.cfg.MaxOutputTokens)
	}

	for name, content := range map[string]string{
		"invented rule in summary": `{"summary":"FND-NET-999 is also failing.","sections":[{"heading":"Recommended remediation","summary":"Apply the fix.","actions":[],"ruleIds":["FND-CFG-001"]}]}`,
		"invented rule id":         `{"summary":"Advisory.","sections":[{"heading":"Recommended remediation","summary":"Apply the fix.","actions":[],"ruleIds":["FND-CFG-001","FND-NET-999"]}]}`,
		"severity claim":           `{"summary":"Severity was downgraded.","sections":[{"heading":"Recommended remediation","summary":"Apply the fix.","actions":[],"ruleIds":["FND-CFG-001"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			bad := &recordingProvider{content: content}
			n := lazyNarrator{cat: &lazyCatalog{}, getenv: narratorEnv, providers: map[string]llm.Provider{llm.DefaultProvider: bad}}
			if _, err := n.ExplainRule(context.Background(), "FND-CFG-001", llm.AudienceDeveloper, llm.Options{}); !errors.Is(err, llm.ErrRejected) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestLazyNarratorRejectsNonAllowListedScope(t *testing.T) {
	p := &recordingProvider{}
	env := func(k string) string {
		if k == llm.EnvScope {
			return "https://management.azure.com/.default"
		}
		return narratorEnv(k)
	}
	n := lazyNarrator{cat: &lazyCatalog{}, getenv: env, providers: map[string]llm.Provider{llm.DefaultProvider: p}}
	if _, err := n.ExplainRule(context.Background(), "FND-CFG-001", llm.AudienceOwner, llm.Options{}); !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if p.prompt.User != "" {
		t.Fatal("provider must not be called with a non-allow-listed scope")
	}
}

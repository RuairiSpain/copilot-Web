package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type noopProvider struct{}

func (noopProvider) Complete(_ context.Context, _ Config, _ Prompt) (Completion, error) {
	return Completion{}, nil
}

func TestResolveConfig(t *testing.T) {
	providers := map[string]Provider{"fake": noopProvider{}}
	cfg, _, err := ResolveConfig(func(k string) string {
		switch k {
		case EnvProvider:
			return "fake"
		case EnvEndpoint:
			return "https://demo.openai.azure.com"
		case EnvDeployment:
			return "demo"
		default:
			return ""
		}
	}, Options{Timeout: 5 * time.Second}, providers)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "fake" || cfg.Endpoint == "" || cfg.Deployment != "demo" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestResolveConfigRejectsInvalidTimeoutAndProvider(t *testing.T) {
	providers := map[string]Provider{DefaultProvider: noopProvider{}}
	_, _, err := ResolveConfig(func(k string) string {
		switch k {
		case EnvEndpoint:
			return "https://demo.openai.azure.com"
		case EnvDeployment:
			return "demo"
		default:
			return ""
		}
	}, Options{Provider: "bogus", Timeout: 45 * time.Second}, providers)
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveConfigScopeAllowList(t *testing.T) {
	providers := map[string]Provider{DefaultProvider: noopProvider{}}
	env := func(scope string) func(string) string {
		return func(k string) string {
			switch k {
			case EnvEndpoint:
				return "https://demo.openai.azure.com"
			case EnvDeployment:
				return "demo"
			case EnvScope:
				return scope
			default:
				return ""
			}
		}
	}
	for _, ok := range []string{"", DefaultScope, LegacyScope} {
		cfg, _, err := ResolveConfig(env(ok), Options{}, providers)
		if err != nil {
			t.Fatalf("scope %q: %v", ok, err)
		}
		if cfg.MaxOutputTokens != DefaultMaxCompletionTokens {
			t.Fatalf("max output tokens=%d", cfg.MaxOutputTokens)
		}
	}
	for _, bad := range []string{"https://management.azure.com/.default", "https://graph.microsoft.com/.default", "https://ai.azure.com"} {
		_, _, err := ResolveConfig(env(bad), Options{}, providers)
		if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), EnvScope) {
			t.Fatalf("scope %q: err=%v", bad, err)
		}
	}
}

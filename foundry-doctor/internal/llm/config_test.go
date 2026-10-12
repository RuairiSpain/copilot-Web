package llm

import (
	"context"
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

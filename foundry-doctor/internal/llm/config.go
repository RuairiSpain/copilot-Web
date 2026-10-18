package llm

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultProvider         = "azure-openai"
	DefaultScope            = "https://ai.azure.com/.default"
	LegacyScope             = "https://cognitiveservices.azure.com/.default"
	DefaultTimeout          = 10 * time.Second
	MinimumTimeout          = 1 * time.Second
	MaximumTimeout          = 30 * time.Second
	DefaultMaxPromptBytes   = 16 * 1024
	DefaultMaxResponseBytes = 12 * 1024

	// DefaultMaxCompletionTokens caps billed output (reasoning plus visible
	// tokens) for the single provider call a command makes.
	DefaultMaxCompletionTokens = 4096
)

// allowedScopes are the only token audiences the LLM provider may request.
// Both appear in Microsoft Learn guidance for the /openai/v1/ route; anything
// else (ARM, Graph, ...) would mint and forward an unrelated bearer token.
var allowedScopes = map[string]bool{
	DefaultScope: true,
	LegacyScope:  true,
}

const (
	EnvProvider   = "FOUNDRY_DOCTOR_LLM_PROVIDER"
	EnvEndpoint   = "FOUNDRY_DOCTOR_LLM_ENDPOINT"
	EnvDeployment = "FOUNDRY_DOCTOR_LLM_DEPLOYMENT"
	EnvScope      = "FOUNDRY_DOCTOR_LLM_SCOPE"
)

// ErrUnavailable reports that the requested LLM narrative cannot run with the
// current configuration or environment. Callers keep the deterministic report.
var ErrUnavailable = errors.New("llm unavailable")

// ErrRejected reports a provider response that was structurally invalid or
// contradicted the deterministic contract.
var ErrRejected = errors.New("llm response rejected")

// ResolveConfig combines environment configuration and one-shot flag
// overrides. It never enables a provider by itself; callers must already have
// explicit opt-in from the command line.
func ResolveConfig(getenv func(string) string, opt Options, allowlist map[string]Provider) (Config, Provider, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	providerName := strings.TrimSpace(opt.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(getenv(EnvProvider))
	}
	if providerName == "" {
		providerName = DefaultProvider
	}
	provider, ok := allowlist[providerName]
	if !ok {
		return Config{}, nil, fmt.Errorf("%w: provider %q is not allowed", ErrUnavailable, providerName)
	}
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < MinimumTimeout || timeout > MaximumTimeout {
		return Config{}, nil, fmt.Errorf("%w: timeout %s must be between %s and %s", ErrUnavailable, timeout, MinimumTimeout, MaximumTimeout)
	}
	endpoint := strings.TrimSpace(firstNonEmpty(getenv(EnvEndpoint), getenv("AZURE_OPENAI_ENDPOINT")))
	deployment := strings.TrimSpace(firstNonEmpty(getenv(EnvDeployment), getenv("AZURE_OPENAI_DEPLOYMENT")))
	scope := strings.TrimSpace(getenv(EnvScope))
	if scope == "" {
		scope = DefaultScope
	}
	if !allowedScopes[scope] {
		return Config{}, nil, fmt.Errorf("%w: %s must be %s or %s", ErrUnavailable, EnvScope, DefaultScope, LegacyScope)
	}
	if endpoint == "" {
		return Config{}, nil, fmt.Errorf("%w: set %s or AZURE_OPENAI_ENDPOINT", ErrUnavailable, EnvEndpoint)
	}
	if deployment == "" {
		return Config{}, nil, fmt.Errorf("%w: set %s or AZURE_OPENAI_DEPLOYMENT", ErrUnavailable, EnvDeployment)
	}
	return Config{
		Provider:         providerName,
		Endpoint:         endpoint,
		Deployment:       deployment,
		Scope:            scope,
		Timeout:          timeout,
		MaxPromptBytes:   DefaultMaxPromptBytes,
		MaxResponseBytes: DefaultMaxResponseBytes,
		MaxOutputTokens:  DefaultMaxCompletionTokens,
	}, provider, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

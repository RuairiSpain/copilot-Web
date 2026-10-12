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
	DefaultTimeout          = 10 * time.Second
	MinimumTimeout          = 1 * time.Second
	MaximumTimeout          = 30 * time.Second
	DefaultMaxPromptBytes   = 16 * 1024
	DefaultMaxResponseBytes = 12 * 1024
)

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

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// AzureOpenAIProvider calls the Azure OpenAI / Foundry OpenAI-compatible
// endpoint with Microsoft Entra ID authentication.
type AzureOpenAIProvider struct {
	Client     *http.Client
	Credential azure.TokenCredential
}

// NewAzureOpenAIProvider builds the production provider.
func NewAzureOpenAIProvider(client *http.Client, credential azure.TokenCredential) AzureOpenAIProvider {
	if client == nil {
		client = http.DefaultClient
	}
	if credential == nil {
		credential = azure.NewDefaultCredential(nil)
	}
	return AzureOpenAIProvider{Client: client, Credential: credential}
}

func (p AzureOpenAIProvider) Complete(ctx context.Context, cfg Config, prompt Prompt) (Completion, error) {
	endpoint, err := providerURL(cfg.Endpoint)
	if err != nil {
		return Completion{}, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	tok, err := p.Credential.Token(ctx, cfg.Scope)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Completion{}, err
		}
		return Completion{}, fmt.Errorf("%w: %s", ErrUnavailable, err)
	}
	body := map[string]any{
		"model":       cfg.Deployment,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": prompt.System},
			{"role": "user", "content": prompt.User},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "foundry_doctor_phase9",
				"strict": true,
				"schema": prompt.Schema,
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Completion{}, fmt.Errorf("marshal provider request: %w", err)
	}
	if len(payload) > cfg.MaxPromptBytes {
		return Completion{}, fmt.Errorf("%w: prompt exceeds %d bytes", ErrUnavailable, cfg.MaxPromptBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Completion{}, fmt.Errorf("build provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := p.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Completion{}, err
		}
		return Completion{}, fmt.Errorf("%w: provider request failed", ErrUnavailable)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, int64(cfg.MaxResponseBytes+1))
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Completion{}, fmt.Errorf("%w: provider response could not be read", ErrUnavailable)
	}
	if len(raw) > cfg.MaxResponseBytes {
		return Completion{}, fmt.Errorf("%w: provider response exceeded %d bytes", ErrRejected, cfg.MaxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Completion{}, parseProviderError(raw, resp.StatusCode)
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Completion{}, fmt.Errorf("%w: provider response is not valid JSON", ErrRejected)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return Completion{}, fmt.Errorf("%w: provider response contained no narrative content", ErrRejected)
	}
	return Completion{
		Provider:   cfg.Provider,
		Model:      strings.TrimSpace(out.Model),
		Deployment: cfg.Deployment,
		Content:    out.Choices[0].Message.Content,
	}, nil
}

func providerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("endpoint is not a URL")
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("endpoint must be an https URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("endpoint must not contain query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".openai.azure.com") && !strings.HasSuffix(host, ".services.ai.azure.com") {
		return "", fmt.Errorf("endpoint host must end with .openai.azure.com or .services.ai.azure.com")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch path {
	case "":
		if strings.HasSuffix(host, ".services.ai.azure.com") {
			return "", fmt.Errorf("endpoint path must be /openai/v1 for .services.ai.azure.com hosts")
		}
		return strings.TrimRight(u.String(), "/") + "/openai/v1/chat/completions", nil
	case "/openai/v1":
		return strings.TrimRight(u.String(), "/") + "/chat/completions", nil
	case "/openai/v1/chat/completions":
		return strings.TrimRight(u.String(), "/"), nil
	default:
		return "", fmt.Errorf("endpoint path must be empty or /openai/v1")
	}
}

func parseProviderError(raw []byte, status int) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		msg := strings.TrimSpace(body.Error.Message)
		code := strings.TrimSpace(body.Error.Code)
		switch {
		case msg != "" && code != "":
			return fmt.Errorf("%w: provider returned %s (%d)", ErrUnavailable, code, status)
		case code != "":
			return fmt.Errorf("%w: provider returned %s (%d)", ErrUnavailable, code, status)
		case msg != "":
			return fmt.Errorf("%w: provider returned status %d", ErrUnavailable, status)
		}
	}
	return fmt.Errorf("%w: provider returned status %d", ErrUnavailable, status)
}

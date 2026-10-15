package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type staticCredential struct{}

func (staticCredential) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "abc.def.ghi", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func TestAzureOpenAIProviderComplete(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.URL.String(), "https://demo.openai.azure.com/openai/v1/chat/completions"; got != want {
			t.Fatalf("url=%s want=%s", got, want)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Fatalf("auth=%q", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"response_format"`)) || !bytes.Contains(body, []byte(`"json_schema"`)) {
			t.Fatalf("body=%s", body)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-test","choices":[{"message":{"content":"{\"summary\":\"Advisory\",\"sections\":[{\"heading\":\"Priority\",\"summary\":\"Do the fix.\",\"actions\":[\"Rotate config\"],\"ruleIds\":[\"FND-IDN-001\"]}]}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	provider := NewAzureOpenAIProvider(client, staticCredential{})
	got, err := provider.Complete(context.Background(), Config{
		Provider: "azure-openai", Endpoint: "https://demo.openai.azure.com", Deployment: "dep",
		Scope: DefaultScope, MaxPromptBytes: 4096, MaxResponseBytes: 4096,
	}, Prompt{System: "sys", User: "user", Schema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-test" || !strings.Contains(got.Content, `"summary":"Advisory"`) {
		t.Fatalf("got=%+v", got)
	}
}

func TestAzureOpenAIProviderAcceptsDocumentedBaseURLs(t *testing.T) {
	cases := map[string]string{
		"https://demo.openai.azure.com":                "https://demo.openai.azure.com/openai/v1/chat/completions",
		"https://demo.openai.azure.com/openai/v1/":     "https://demo.openai.azure.com/openai/v1/chat/completions",
		"https://demo.services.ai.azure.com/openai/v1": "https://demo.services.ai.azure.com/openai/v1/chat/completions",
	}
	for raw, wantURL := range cases {
		t.Run(raw, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != wantURL {
					t.Fatalf("url=%s want=%s", r.URL.String(), wantURL)
				}
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-test","choices":[{"message":{"content":"{\"summary\":\"Advisory\",\"sections\":[{\"heading\":\"Priority\",\"summary\":\"Do the fix.\",\"actions\":[\"Rotate config\"],\"ruleIds\":[\"FND-IDN-001\"]}]}"}}]}`)),
					Header:     make(http.Header),
				}, nil
			})}
			provider := NewAzureOpenAIProvider(client, staticCredential{})
			if _, err := provider.Complete(context.Background(), Config{
				Provider: "azure-openai", Endpoint: raw, Deployment: "dep",
				Scope: DefaultScope, MaxPromptBytes: 4096, MaxResponseBytes: 4096,
			}, Prompt{System: "sys", User: "user", Schema: map[string]any{"type": "object"}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAzureOpenAIProviderRejectsBareServicesAIBaseURL(t *testing.T) {
	provider := NewAzureOpenAIProvider(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	})}, staticCredential{})
	_, err := provider.Complete(context.Background(), Config{
		Provider: "azure-openai", Endpoint: "https://demo.services.ai.azure.com", Deployment: "dep",
		Scope: DefaultScope, MaxPromptBytes: 4096, MaxResponseBytes: 4096,
	}, Prompt{System: "sys", User: "user", Schema: map[string]any{"type": "object"}})
	if err == nil || !strings.Contains(err.Error(), "/openai/v1") {
		t.Fatalf("err=%v", err)
	}
}

func TestAzureOpenAIProviderRejectsBadEndpoint(t *testing.T) {
	provider := NewAzureOpenAIProvider(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	})}, staticCredential{})
	_, err := provider.Complete(context.Background(), Config{
		Provider: "azure-openai", Endpoint: "http://example.com", Deployment: "dep",
		Scope: DefaultScope, MaxPromptBytes: 4096, MaxResponseBytes: 4096,
	}, Prompt{System: "sys", User: "user", Schema: map[string]any{"type": "object"}})
	if err == nil || !strings.Contains(err.Error(), "https URL") {
		t.Fatalf("err=%v", err)
	}
}

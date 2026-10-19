package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

const validCompletion = `{"model":"gpt-test","choices":[{"finish_reason":"stop","message":{"content":"{\"summary\":\"Advisory\",\"sections\":[]}"}}]}`

func okResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func testConfig() Config {
	return Config{
		Provider: "azure-openai", Endpoint: "https://demo.openai.azure.com", Deployment: "dep",
		Scope: DefaultScope, MaxPromptBytes: 4096, MaxResponseBytes: 4096, MaxOutputTokens: DefaultMaxCompletionTokens,
	}
}

func testPrompt() Prompt {
	return Prompt{System: "sys", User: "user", Schema: map[string]any{"type": "object"}}
}

func TestAzureOpenAIProviderRequestBodyKeys(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		want := []string{"max_completion_tokens", "messages", "model", "response_format"}
		got := make([]string, 0, len(body))
		for k := range body {
			got = append(got, k)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("request keys=%v want=%v", got, want)
		}
		if string(body["max_completion_tokens"]) != "4096" {
			t.Fatalf("max_completion_tokens=%s", body["max_completion_tokens"])
		}
		return okResponse(validCompletion), nil
	})}
	if _, err := NewAzureOpenAIProvider(client, staticCredential{}).Complete(context.Background(), testConfig(), testPrompt()); err != nil {
		t.Fatal(err)
	}
}

func TestAzureOpenAIProviderRefusesRedirects(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "demo.openai.azure.com" {
			t.Errorf("payload re-sent to %s", r.URL.Host)
		}
		h := make(http.Header)
		h.Set("Location", "https://collector.example.com/steal")
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Body: io.NopCloser(strings.NewReader("")), Header: h, Request: r}, nil
	})}
	_, err := NewAzureOpenAIProvider(client, staticCredential{}).Complete(context.Background(), testConfig(), testPrompt())
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("transport calls=%d, want 1 (redirect must not be followed)", calls)
	}
}

func TestNewAzureOpenAIProviderDefaultClientRefusesRedirects(t *testing.T) {
	p := NewAzureOpenAIProvider(nil, staticCredential{})
	if p.Client == nil || p.Client.CheckRedirect == nil {
		t.Fatal("default client must refuse redirects")
	}
	if err := p.Client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect err=%v", err)
	}
}

func TestAzureOpenAIProviderRejectsRefusalAndTruncation(t *testing.T) {
	cases := map[string]string{
		"refused":   `{"model":"m","choices":[{"finish_reason":"stop","message":{"content":"","refusal":"I can't help with that."}}]}`,
		"truncated": `{"model":"m","choices":[{"finish_reason":"length","message":{"content":"{\"summary\":\"Adv"}}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return okResponse(body), nil
			})}
			_, err := NewAzureOpenAIProvider(client, staticCredential{}).Complete(context.Background(), testConfig(), testPrompt())
			want := map[string]string{"refused": "refused", "truncated": "truncated"}[name]
			if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAzureOpenAIProviderRejectsPortAndScope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	})}
	p := NewAzureOpenAIProvider(client, staticCredential{})
	cfg := testConfig()
	cfg.Endpoint = "https://demo.openai.azure.com:8443"
	if _, err := p.Complete(context.Background(), cfg, testPrompt()); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("port err=%v", err)
	}
	cfg = testConfig()
	cfg.Scope = "https://management.azure.com/.default"
	if _, err := p.Complete(context.Background(), cfg, testPrompt()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("scope err=%v", err)
	}
}

// An ARM-audience AZURE_ACCESS_TOKEN must never reach the inference host: the
// production credential is the azd/az chain without the environment source.
func TestAzureOpenAIProviderNeverForwardsEnvironmentToken(t *testing.T) {
	armToken := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"https://management.azure.com"}`)) + ".sig"
	t.Setenv("AZURE_ACCESS_TOKEN", armToken)
	var gotScope string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		for i, a := range args {
			if a == "--scope" && i+1 < len(args) {
				gotScope = args[i+1]
			}
		}
		if name != "azd" {
			return nil, errors.New("unexpected source")
		}
		return []byte(`{"token":"tok-ai-audience","expiresOn":"2099-01-01T00:00:00Z"}`), nil
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		auth := r.Header.Get("Authorization")
		if strings.Contains(auth, armToken) || auth != "Bearer tok-ai-audience" {
			t.Fatalf("unexpected Authorization header forwarded")
		}
		return okResponse(validCompletion), nil
	})}
	p := NewAzureOpenAIProvider(client, azure.NewCLICredentialForTenant(run, ""))
	if _, err := p.Complete(context.Background(), testConfig(), testPrompt()); err != nil {
		t.Fatal(err)
	}
	if gotScope != DefaultScope {
		t.Fatalf("azd scope=%q", gotScope)
	}
}

func TestAzureOpenAIProviderRejectsLookalikeEndpoints(t *testing.T) {
	provider := NewAzureOpenAIProvider(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("request sent to %s", r.URL.Host)
		return nil, nil
	})}, staticCredential{})
	for _, endpoint := range []string{
		"https://evil.com",
		"https://x.openai.azure.com.evil.com",
		"https://evil.com/.openai.azure.com",
		"https://x.openai.azure.us",
		"https://user@x.openai.azure.com",
		"https://x.openai.azure.com:8443",
		"https://x.cognitiveservices.azure.com",
		"https://openai.azure.com.",
	} {
		cfg := testConfig()
		cfg.Endpoint = endpoint
		if _, err := provider.Complete(context.Background(), cfg, testPrompt()); err == nil {
			t.Errorf("endpoint %q was accepted", endpoint)
		}
	}
}

// The default provider (no injected credential) must mint its token with azd
// or az, never from AZURE_ACCESS_TOKEN.
func TestNewAzureOpenAIProviderDefaultCredentialIgnoresEnvironmentToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a fake azd")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\necho '{\"token\":\"tok-from-azd\",\"expiresOn\":\"2099-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, "azd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZURE_ACCESS_TOKEN", "arm-env-token")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok-from-azd" {
			t.Fatalf("Authorization did not come from azd")
		}
		return okResponse(validCompletion), nil
	})}
	if _, err := NewAzureOpenAIProvider(client, nil).Complete(context.Background(), testConfig(), testPrompt()); err != nil {
		t.Fatal(err)
	}
}

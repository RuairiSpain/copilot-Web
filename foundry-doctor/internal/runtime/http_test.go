package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type cred struct{}

func (cred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{Token: "token"}, nil
}

type errCred struct{}

func (errCred) Token(context.Context, string) (azure.AccessToken, error) {
	return azure.AccessToken{}, errors.New("token boom")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDoJSONRetryAndUnavailable(t *testing.T) {
	t.Run("retries 429 then succeeds", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()
		var got struct {
			OK bool `json:"ok"`
		}
		if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, nil, &got); err != nil {
			t.Fatal(err)
		}
		if !got.OK || calls.Load() != 2 {
			t.Fatalf("got=%+v calls=%d", got, calls.Load())
		}
	})

	t.Run("forbidden becomes unavailable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL+"?sig=x", nil, nil)
		var un *azure.UnavailableError
		if !errors.As(err, &un) || un == nil {
			t.Fatalf("err = %v", err)
		}
		if un.Capability == "" || un.Reason == "" {
			t.Fatalf("unavailable = %+v", un)
		}
	})

	t.Run("unauthorized becomes unavailable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, nil, nil)
		var un *azure.UnavailableError
		if !errors.As(err, &un) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRetryAfterAndSafeURL(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "1")
	if d := retryAfter(h, 0); d != time.Second {
		t.Fatalf("delay = %v", d)
	}
	if got := safeURL("https://example.com/path?sig=secret"); got != "https://example.com/path" {
		t.Fatalf("safeURL = %q", got)
	}
}

func TestDoJSONAdditionalBranches(t *testing.T) {
	t.Run("token failure", func(t *testing.T) {
		err := DoJSON(context.Background(), nil, errCred{}, "scope", http.MethodGet, "https://example.com", nil, nil)
		if err == nil || err.Error() != "token boom" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("transport retry succeeds", func(t *testing.T) {
		var calls atomic.Int32
		hc := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			if calls.Add(1) < 3 {
				return nil, errors.New("boom")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		})}
		var got struct {
			OK bool `json:"ok"`
		}
		if err := DoJSON(context.Background(), hc, cred{}, "scope", http.MethodGet, "https://example.com", nil, &got); err != nil {
			t.Fatal(err)
		}
		if !got.OK || calls.Load() != 3 {
			t.Fatalf("got=%+v calls=%d", got, calls.Load())
		}
	})

	t.Run("empty success with nil out", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()
		if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, map[string]string{"x": "y"}, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("decode failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{"))
		}))
		defer srv.Close()
		var got map[string]any
		if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, nil, &got); err == nil || !strings.Contains(err.Error(), "decode response") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()
		if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, nil, nil); err == nil || err.Error() != "http 404" {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("too large", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, maxResponseBytes+1))
		}))
		defer srv.Close()
		if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, nil, nil); err == nil || !strings.Contains(err.Error(), "response exceeds") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRetryAfterDateAndSleepRetryCancel(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", time.Now().Add(time.Second).UTC().Format(http.TimeFormat))
	if d := retryAfter(h, 0); d <= 0 || d > maxRetryAfter {
		t.Fatalf("date retry delay = %v", d)
	}
	if got := safeURL("://bad url"); got != "" {
		t.Fatalf("safeURL invalid = %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepRetry(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

func TestHTTPErrorsAreTypedNotTextMatched(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest, http.StatusTeapot, http.StatusBadGateway} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, "127.0.0.1", nil, nil)
		srv.Close()
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != status {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if IsNotFound(err) != (status == http.StatusNotFound) {
			t.Errorf("status %d: IsNotFound = %v", status, IsNotFound(err))
		}
	}
	// Text that merely contains 404 is not a not-found.
	if IsNotFound(errors.New("request 40412 failed")) || IsNotFound(nil) || IsNotFound(fmt.Errorf("wrapped: %w", &HTTPError{Status: 500})) {
		t.Error("IsNotFound matched a non-404")
	}
	if !IsNotFound(fmt.Errorf("wrapped: %w", &HTTPError{Status: 404})) {
		t.Error("wrapped 404 not recognised")
	}
}

func TestDecodeFailureIsAnInternalDecodeError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{")) }))
	defer srv.Close()
	var out map[string]any
	if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, "127.0.0.1", nil, &out); !errors.Is(err, azure.ErrDecode) {
		t.Fatalf("err = %v", err)
	}
}

func TestSameOrigin(t *testing.T) {
	base := "https://acct.services.ai.azure.com/api/projects/p/connections?api-version=v1"
	cases := map[string]bool{
		"https://acct.services.ai.azure.com/api/projects/p/connections?skiptoken=2": true,
		"https://ACCT.services.ai.azure.com/x":                                      true,
		"https://evil.example.com/api":                                              false,
		"https://acct.services.ai.azure.com.evil.com/x":                             false,
		"http://acct.services.ai.azure.com/x":                                       false,
		"//acct.services.ai.azure.com/x":                                            false,
		"/relative/path":                                                            false,
		"https://user@acct.services.ai.azure.com/x":                                 false,
		"://bad": false,
	}
	for next, want := range cases {
		if got := SameOrigin(base, next); got != want {
			t.Errorf("SameOrigin(%q) = %v, want %v", next, got, want)
		}
	}
}

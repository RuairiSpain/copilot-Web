package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDoJSONRejectsUnsafeRequestsBeforeSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer plain.Close()
	cases := []struct {
		name, method, url, suffix string
	}{
		{"empty host suffix", http.MethodGet, srv.URL, ""},
		{"blank host suffix", http.MethodGet, srv.URL, "  "},
		{"http scheme", http.MethodGet, plain.URL, "127.0.0.1"},
		{"credentials in URL", http.MethodGet, "https://user:pw@127.0.0.1/", "127.0.0.1"},
		{"POST", http.MethodPost, srv.URL, "127.0.0.1"},
		{"DELETE", http.MethodDelete, srv.URL, "127.0.0.1"},
		{"empty method", "", srv.URL, "127.0.0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", c.method, c.url, c.suffix, nil, nil)
			if !errors.Is(err, ErrRequestRejected) {
				t.Fatalf("err = %v, want ErrRequestRejected", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("a rejected request reached the network %d time(s)", calls.Load())
	}
	// The positive control: GET, https, matching suffix.
	if err := DoJSON(context.Background(), srv.Client(), cred{}, "scope", http.MethodGet, srv.URL, "127.0.0.1", nil, nil); err != nil {
		t.Fatalf("control request: %v", err)
	}
}

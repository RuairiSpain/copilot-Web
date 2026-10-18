package azure

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The 202 Location of a what-if is an opaque URL that carries extra query
// parameters. Only whatif.poll replays them; every other operation keeps the
// strict query allow-list.
func TestWhatIfPollReplaysOpaqueLocationQuery(t *testing.T) {
	ctx := context.Background()
	runPath := testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/whatIf"
	poll := testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/operationStatuses/op1"
	req := WhatIfRequest{OptIn: true, Scope: testRG, Template: []byte(`{"resources":[]}`)}
	loc := func(host, path, q string) reply {
		return reply{status: 202, header: http.Header{"Location": {host + path + q}}}
	}

	t.Run("extra query parameters are accepted and replayed verbatim", func(t *testing.T) {
		q := "?api-version=2020-01-01&t=638000000000000000&c=MIIHhzCC%2Bk%3D&s=sig%2Fx&h=ha_sh-1"
		f := newFake(t).
			on("POST", runPath, loc("https://management.test", poll, q)).
			on("GET", poll, reply{status: 200, body: fixture(t, "whatif_done.json")})
		a, _ := newAdapter(t, f)
		got, err := a.Run(ctx, req)
		if err != nil || got.Status != "Succeeded" {
			t.Fatalf("got %+v, %v", got, err)
		}
		last := f.calls[len(f.calls)-1]
		for _, w := range []string{"t=638000000000000000", "c=MIIHhzCC%2Bk%3D", "s=sig%2Fx", "h=ha_sh-1", "api-version=2026-06-01"} {
			if !strings.Contains(last, w) {
				t.Errorf("poll request %q lacks %q", last, w)
			}
		}
		if strings.Contains(last, "2020-01-01") || strings.Count(last, "api-version") != 1 {
			t.Errorf("the allow-listed api-version must replace the Location one: %q", last)
		}
	})
	t.Run("foreign host is refused", func(t *testing.T) {
		f := newFake(t).on("POST", runPath, loc("https://evil.example", poll, "?t=1"))
		a, _ := newAdapter(t, f)
		if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
			t.Errorf("err=%v calls=%d", err, f.callCount())
		}
	})
	t.Run("path traversal is refused", func(t *testing.T) {
		for _, p := range []string{
			testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/operationStatuses/..",
			testRG + "/providers/Microsoft.Resources/deployments/../deployments/x/operationStatuses/op1",
			testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/operationStatuses/op1/../../../locks",
		} {
			f := newFake(t).on("POST", runPath, loc("https://management.test", p, "?t=1"))
			a, _ := newAdapter(t, f)
			if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
				t.Errorf("%s: err=%v calls=%d", p, err, f.callCount())
			}
		}
	})
	t.Run("percent-encoded dot segments and query delimiters in the path are refused", func(t *testing.T) {
		base := testRG + "/providers/Microsoft.Resources/deployments/foundry-doctor-whatif/operationStatuses/"
		for _, p := range []string{base + "%2e%2e", base + "%2E%2E", base + "op1/%2e%2e/%2e%2e/locks", base + "op1%3fx=1", base + "op1%3Fx=1"} {
			f := newFake(t).on("POST", runPath, loc("https://management.test", p, "?t=1"))
			a, _ := newAdapter(t, f)
			if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
				t.Errorf("%s: err=%v calls=%d", p, err, f.callCount())
			}
		}
	})
	t.Run("a second API-Version in different case is stripped and replaced", func(t *testing.T) {
		q := "?API-Version=1999-01-01&t=1&api-VERSION=2000-01-01&Api%2DVersion=2001-01-01"
		f := newFake(t).
			on("POST", runPath, loc("https://management.test", poll, q)).
			on("GET", poll, reply{status: 200, body: fixture(t, "whatif_done.json")})
		a, _ := newAdapter(t, f)
		if _, err := a.Run(ctx, req); err != nil {
			t.Fatal(err)
		}
		last := strings.ToLower(f.calls[len(f.calls)-1])
		if strings.Count(last, "api-version") != 1 || strings.Contains(last, "1999") || strings.Contains(last, "2000-01-01") || strings.Contains(last, "2001-01-01") || !strings.Contains(last, "api-version=2026-06-01") {
			t.Errorf("api-version not replaced: %q", last)
		}
	})
	t.Run("cross-origin scheme or port is refused", func(t *testing.T) {
		for _, host := range []string{"http://management.test", "https://management.test:8443", "https://management.test.evil.example"} {
			f := newFake(t).on("POST", runPath, loc(host, poll, "?t=1"))
			a, _ := newAdapter(t, f)
			if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
				t.Errorf("%s: err=%v calls=%d", host, err, f.callCount())
			}
		}
	})
	t.Run("another operation with extra parameters is refused", func(t *testing.T) {
		other := testRG + "/providers/Microsoft.Authorization/locks"
		f := newFake(t).on("POST", runPath, loc("https://management.test", other, "?t=1&c=2"))
		a, _ := newAdapter(t, f)
		if _, err := a.Run(ctx, req); !errors.Is(err, ErrNotAllowed) || f.callCount() != 1 {
			t.Errorf("err=%v calls=%d", err, f.callCount())
		}
	})
}

func TestVerbatimQueryOnlyForWhatIfPoll(t *testing.T) {
	ctx := context.Background()
	a, _ := newAdapter(t, newFake(t))
	c := a.c
	// The strict path: extra keys are refused for every operation, poll included.
	for name, path := range map[string]string{
		"locks":  testRG + "/providers/Microsoft.Authorization/locks",
		"poll":   testRG + "/providers/Microsoft.Resources/deployments/d/operationStatuses/o",
		"vnet":   testRG + "/providers/Microsoft.Network/virtualNetworks/v",
		"policy": testScope + "/providers/Microsoft.Authorization/policyAssignments",
	} {
		if _, err := c.do(ctx, http.MethodGet, path, url.Values{"t": {"1"}}, nil); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: strict allow-list must refuse an extra key, got %v", name, err)
		}
	}
	// A verbatim query on a non-poll operation is refused before any network call.
	for _, path := range []string{
		testRG + "/providers/Microsoft.Authorization/locks",
		testRG + "/providers/Microsoft.Network/virtualNetworks/v",
	} {
		if _, err := c.pollWhatIf(ctx, path, "t=1"); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: pollWhatIf on another operation = %v", path, err)
		}
	}
	if q, err := verbatimQuery("api-version=1&t=2&API-VERSION=3"); err != nil || q != "t=2" {
		t.Errorf("verbatimQuery = %q, %v", q, err)
	}
	for _, bad := range []string{"t=a b", "t=1\n", "t=1#x"} {
		if _, err := verbatimQuery(bad); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("verbatimQuery(%q) = %v", bad, err)
		}
	}
}

// Microsoft.Resources/deployments/operationstatuses/read is listed in the
// management-and-governance permissions table of MicrosoftDocs/azure-docs
// ("Gets or lists deployment operation statuses.").
func TestWhatIfPollCapability(t *testing.T) {
	found := false
	for _, o := range AllowedOperations() {
		if o.Name == "whatif.poll" {
			found = true
			if o.Capability != "Microsoft.Resources/deployments/operationstatuses/read" {
				t.Errorf("whatif.poll capability = %q", o.Capability)
			}
		}
	}
	if !found {
		t.Error("whatif.poll missing")
	}
}

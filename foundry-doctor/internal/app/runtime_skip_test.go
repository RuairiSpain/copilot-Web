package app

import (
	"bytes"
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestRuntimeByDesignSkipDoesNotForceExit2(t *testing.T) {
	cases := []struct {
		name string
		skip sdk.Skip
		want int
	}{
		{"not-implemented placeholder (FND-RUN-008) hid nothing", sdk.Skip{RuleID: "FND-RUN-008", Reason: sdk.SkipNotImplemented}, ExitOK},
		{"no dependency proven private-endpoint backed hid nothing", sdk.Skip{RuleID: "FND-RUN-002", Reason: "input-unavailable: no dependency is proven private-endpoint backed; private-endpoint backing was not proven for any dependency, so no DNS name was probed"}, ExitOK},
		{"a permission gap hid a check", sdk.Skip{RuleID: "FND-RUN-001", Reason: "input-unavailable: capability roleAssignments; missing permission Microsoft.Authorization/roleAssignments/read"}, ExitUnavailable},
		{"an internal error hid a check", sdk.Skip{RuleID: "FND-RUN-004", Reason: "uncertain: internal error: a request was refused by the read-only allow-list"}, ExitUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newSvc(&fakeEngine{})
			svc.Runtime = &fakeRuntime{out: RunOutput{Skipped: []sdk.Skip{c.skip}}}
			code, err := Runtime(context.Background(), svc, RuntimeRequest{}, &bytes.Buffer{})
			if err != nil || code != c.want {
				t.Fatalf("exit = %d (err %v), want %d", code, err, c.want)
			}
		})
	}
}

func TestRuntimePassesApprovedScopesThrough(t *testing.T) {
	rt := &fakeRuntime{}
	svc, _ := newSvc(&fakeEngine{})
	svc.Runtime = rt
	scopes := []string{"/subscriptions/s/resourceGroups/other"}
	if _, err := Runtime(context.Background(), svc, RuntimeRequest{ApprovedScopes: scopes}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(rt.in.ApprovedScopes) != 1 || rt.in.ApprovedScopes[0] != scopes[0] {
		t.Errorf("approved scopes = %v", rt.in.ApprovedScopes)
	}
}

//go:build spike

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

type gwRun struct {
	Findings []struct {
		RuleID string `json:"ruleId"`
		Key    string `json:"key"`
	} `json:"findings"`
	Skipped []struct {
		RuleID string `json:"ruleId"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func gatewayRun(t *testing.T, dir string) gwRun {
	t.Helper()
	exe := os.Getenv("BICEP_PATH")
	if exe == "" {
		t.Skip("BICEP_PATH not set")
	}
	var out, errb bytes.Buffer
	svc := app.NewServices(&errb, app.Options{
		AzdVersion: func(context.Context) string { return "1.34.2" },
		Bicep:      bicep.DiscoverOptions{Path: exe},
	})
	run(context.Background(), []string{"doctor", "--dir", dir, "--local", "--rules", "FND-GW-*", "--format", "json"}, &out, &errb, svc)
	var r gwRun
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("%v\nstdout=%s\nstderr=%s", err, out.String(), errb.String())
	}
	return r
}

func (r gwRun) hasFinding(rule string) bool {
	return slices.ContainsFunc(r.Findings, func(f struct {
		RuleID string `json:"ruleId"`
		Key    string `json:"key"`
	}) bool {
		return f.RuleID == rule
	})
}

func (r gwRun) skipReason(rule string) string {
	for _, s := range r.Skipped {
		if s.RuleID == rule {
			return s.Reason
		}
	}
	return ""
}

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike -run TestIQGatewaySample ./cmd/foundry-doctor/
//
// The shipped sample is compiled with the real Bicep CLI: its policy and
// fragment come from loadTextContent, which compiles to a template variable,
// and its logger and diagnostic IDs compile through resourceId().
func TestIQGatewaySampleRealBicepPositive(t *testing.T) {
	r := gatewayRun(t, copyTree(t, sample("iq-gateway")))
	if len(r.Findings) != 0 {
		t.Fatalf("the shipped sample must raise no gateway finding: %+v", r.Findings)
	}
	for _, rule := range []string{"FND-GW-001", "FND-GW-002", "FND-GW-003", "FND-GW-004", "FND-GW-005"} {
		if reason := r.skipReason(rule); reason != "" {
			t.Errorf("%s was skipped (%s); a loadTextContent policy must be analysed", rule, reason)
		}
	}
	if reason := r.skipReason("FND-GW-006"); !strings.Contains(reason, "uncertain") || !strings.Contains(reason, "custom metrics") {
		t.Errorf("FND-GW-006 must be uncertain because custom metrics cannot be read, got %q", reason)
	}
}

// The same project with the JWT validation and the managed identity removed
// from the policy file must fail the rules that need them.
func TestIQGatewaySampleRealBicepNegative(t *testing.T) {
	dir := copyTree(t, sample("iq-gateway"))
	policy := filepath.Join(dir, "infra", "policies", "foundry-chat.xml")
	if err := os.WriteFile(policy, []byte("<policies><inbound><base /><include-fragment fragment-id=\"token-governance\" /></inbound><backend><base /></backend><outbound><base /></outbound><on-error><base /></on-error></policies>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := gatewayRun(t, dir)
	for _, rule := range []string{"FND-GW-001", "FND-GW-005"} {
		if !r.hasFinding(rule) {
			t.Errorf("%s must report a finding when validate-jwt and authentication-managed-identity are gone: %+v", rule, r)
		}
	}
}

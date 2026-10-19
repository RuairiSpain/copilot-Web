package ops

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	tProject = "Microsoft.CognitiveServices/accounts/projects"
	tPConn   = "Microsoft.CognitiveServices/accounts/projects/connections"
	tAConn   = "Microsoft.CognitiveServices/accounts/connections"
	tAppi    = "Microsoft.Insights/components"
)

func project(name string) sdk.ARMResource { return sdk.ARMResource{Type: tProject, Name: name} }

func conn(typ, name, category string) sdk.ARMResource {
	return sdk.ARMResource{Type: typ, Name: name, Properties: map[string]any{"category": category, "authType": "AAD"}}
}

func appi(name string, props map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: tAppi, Name: name, Properties: props}
}

func TestOPS002ApplicationInsightsConnection(t *testing.T) {
	ws := map[string]any{"WorkspaceResourceId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.OperationalInsights/workspaces/law"}
	cases := []struct {
		name         string
		arm          sdk.ARMModel
		wantFindings []string // finding keys
		wantSkip     string   // substring of whole-rule skip reason
		wantClean    bool
	}{
		{name: "no ARM model", arm: nil, wantSkip: sdk.SkipInputUnavailable},
		{name: "no project in template", arm: model{{Type: "Microsoft.CognitiveServices/accounts", Name: "acct", Kind: "AIServices"}}, wantSkip: sdk.SkipNotApplicable},
		{name: "missing connection is a finding", arm: model{project("acct/proj")}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "unrelated connection categories do not satisfy", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/search", "CognitiveSearch")}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "project scoped AppInsights connection passes", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "AppInsights")}, wantClean: true},
		{name: "category compared case-insensitively", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "appinsights")}, wantClean: true},
		{name: "account scoped AppInsights connection covers its project", arm: model{project("acct/proj"), conn(tAConn, "acct/appi", "AppInsights")}, wantClean: true},
		{name: "connection of another account does not cover", arm: model{project("acct/proj"), conn(tAConn, "other/appi", "AppInsights")}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "connection of another project does not cover", arm: model{project("acct/proj"), conn(tPConn, "acct/other/appi", "AppInsights")}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "two connections at the same scope", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/a", "AppInsights"), conn(tPConn, "acct/proj/b", "AppInsights")}, wantFindings: []string{"multiple-appinsights-connections"}},
		{name: "expression category is uncertain", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "[parameters('cat')]")}, wantSkip: "uncertain"},
		{name: "expression connection name is uncertain", arm: model{project("acct/proj"), conn(tPConn, "[format('{0}/{1}/appi', parameters('a'), parameters('p'))]", "AppInsights")}, wantSkip: "uncertain"},
		{name: "conditional connection is uncertain", arm: model{project("acct/proj"), {Type: tPConn, Name: "acct/proj/appi", Conditional: true, Condition: "[parameters('x')]", Properties: map[string]any{"category": "AppInsights"}}}, wantSkip: "uncertain"},
		{name: "literal false connection does not count", arm: model{project("acct/proj"), {Type: tPConn, Name: "acct/proj/appi", Conditional: true, Condition: "[false()]", Properties: map[string]any{"category": "AppInsights"}}}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "connected workspace based component passes", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "AppInsights"), appi("appi", ws)}, wantClean: true},
		{name: "connected classic component is a finding", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "AppInsights"), appi("appi", map[string]any{"Application_Type": "web"})}, wantFindings: []string{"component-not-workspace-based"}},
		{name: "expression workspace id is uncertain not a finding", arm: model{project("acct/proj"), conn(tPConn, "acct/proj/appi", "AppInsights"), appi("appi", map[string]any{"WorkspaceResourceId": "[parameters('ws')]"})}, wantSkip: "uncertain"},
		{name: "component alone does not hide the missing connection", arm: model{project("acct/proj"), appi("appi", ws)}, wantFindings: []string{"no-appinsights-connection"}},
		{name: "two projects, one connected", arm: model{project("acct/p1"), project("acct/p2"), conn(tPConn, "acct/p1/appi", "AppInsights")}, wantFindings: []string{"no-appinsights-connection"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &sdk.Input{Profile: "foundry-prod", ARM: tc.arm}
			got := eval(t, "FND-OPS-002", Providers{}, in)
			switch {
			case tc.wantSkip != "":
				if got.Skipped == nil || !strings.Contains(got.Skipped.Reason, tc.wantSkip) || len(got.Findings) != 0 {
					t.Fatalf("want skip %q, got %+v", tc.wantSkip, got)
				}
			case tc.wantClean:
				if got.Skipped != nil || len(got.Findings) != 0 || len(got.Skips) != 0 {
					t.Fatalf("want clean pass, got %+v", got)
				}
			default:
				var keys []string
				for _, f := range got.Findings {
					keys = append(keys, f.Key)
					if f.Evidence == "" {
						t.Fatal("empty evidence")
					}
				}
				if strings.Join(keys, ",") != strings.Join(tc.wantFindings, ",") {
					t.Fatalf("keys %v want %v (%+v)", keys, tc.wantFindings, got)
				}
			}
		})
	}
}

func TestOPS002NoLongerHardCodedSkip(t *testing.T) {
	got := eval(t, "FND-OPS-002", Providers{}, &sdk.Input{Profile: "foundry-prod", ARM: model{project("acct/proj")}})
	if got.Skipped != nil {
		t.Fatalf("rule must evaluate, got skip %+v", got.Skipped)
	}
}

func TestOPS002EvidenceHasNoConnectionTarget(t *testing.T) {
	c := conn(tPConn, "acct/proj/a", "AppInsights")
	c.Properties["target"] = "https://fake.invalid/target-marker"
	d := conn(tPConn, "acct/proj/b", "AppInsights")
	got := eval(t, "FND-OPS-002", Providers{}, &sdk.Input{ARM: model{project("acct/proj"), c, d}})
	if len(got.Findings) == 0 {
		t.Fatalf("expected finding, got %+v", got)
	}
	for _, f := range got.Findings {
		if strings.Contains(f.Evidence, "target-marker") {
			t.Fatalf("evidence leaks connection target: %q", f.Evidence)
		}
	}
}

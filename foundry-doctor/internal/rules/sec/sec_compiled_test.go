package sec

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// extended returns the golden model plus extra resources.
func extended(t *testing.T, extra ...sdk.ARMResource) sdk.ARMModel {
	t.Helper()
	m := rulestest.Model(t)
	return model(append(m.Resources(), extra...))
}

func names(fs []sdk.Finding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Resource.Name+"|"+f.Key)
	}
	return strings.Join(parts, "\n")
}

// TestCompiledBicepScope runs the SEC rules over the golden template compiled
// from main.bicep: expression names built with format() and uniqueString(),
// one linked and one unrelated Search service and Cosmos account, and Foundry
// connections.
func TestCompiledBicepScope(t *testing.T) {
	m := rulestest.Model(t)
	tests := []struct {
		rule     string
		findings int
		contains []string
		excludes []string
	}{
		{"FND-SEC-001", 1, []string{"format('ai-{0}'"}, []string{"speech"}},
		{"FND-SEC-002", 1, []string{"format('srch-{0}'"}, []string{"other-", "cond-"}},
		{"FND-SEC-003", 1, []string{"format('cosmos-{0}'"}, []string{"unrelated-"}},
		{"FND-SEC-006", 1, []string{"Azure datacenter"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.rule, func(t *testing.T) {
			out := run(t, tc.rule, &sdk.Input{ARM: m})
			if out.Skipped != nil {
				t.Fatalf("unexpected skip %+v", out.Skipped)
			}
			if len(out.Findings) != tc.findings {
				t.Fatalf("findings = %d want %d:\n%s", len(out.Findings), tc.findings, names(out.Findings))
			}
			all := names(out.Findings)
			for _, f := range out.Findings {
				all += "\n" + f.Evidence
			}
			for _, s := range tc.contains {
				if !strings.Contains(all, s) {
					t.Errorf("missing %q in:\n%s", s, all)
				}
			}
			for _, s := range tc.excludes {
				if strings.Contains(all, s) {
					t.Errorf("unrelated resource %q reported:\n%s", s, all)
				}
			}
		})
	}
}

// TestCompiledSEC010 keeps the scope assertions of the former table row: only the
// four Foundry-linked resources are evaluated, never the unrelated ones. FND-SEC-010
// v3 reports violations only, so without a declared CMK requirement the golden
// template yields no finding (platform-managed keys are notes), and with the
// requirement every platform-managed linked resource is one finding.
func TestCompiledSEC010(t *testing.T) {
	m := rulestest.Model(t)
	t.Run("no requirement declared", func(t *testing.T) {
		out := run(t, "FND-SEC-010", &sdk.Input{ARM: m})
		if out.Skipped != nil || len(out.Findings) != 0 {
			t.Fatalf("want no findings, got %+v", out)
		}
		if len(out.Skips) != 4 {
			t.Fatalf("want 4 platform-managed notes, got %d: %+v", len(out.Skips), out.Skips)
		}
	})
	t.Run("requirement declared", func(t *testing.T) {
		out := run(t, "FND-SEC-010", &sdk.Input{ARM: m, Policy: fakePolicy{PolicyKeyRequireCMK: true}})
		if out.Skipped != nil || len(out.Findings) != 4 {
			t.Fatalf("findings = %d want 4:\n%s", len(out.Findings), names(out.Findings))
		}
		all := names(out.Findings)
		for _, f := range out.Findings {
			all += "\n" + f.Evidence
		}
		if !strings.Contains(all, "CMK posture") {
			t.Errorf("missing CMK posture evidence:\n%s", all)
		}
		for _, s := range []string{"other-", "unrelated-", "'other{0}'"} {
			if strings.Contains(all, s) {
				t.Errorf("unrelated resource %q reported:\n%s", s, all)
			}
		}
	})
}

func TestCompiledSEC005UnrelatedVaultIsNotApplicable(t *testing.T) {
	out := run(t, "FND-SEC-005", &sdk.Input{ARM: rulestest.Model(t)})
	if out.Skipped == nil || out.Skipped.Reason != SkipOutOfScope || len(out.Findings) != 0 {
		t.Fatalf("want out-of-scope skip, got %+v", out)
	}
}

func TestCompiledSEC004KeyAuthWarning(t *testing.T) {
	out := run(t, "FND-SEC-004", &sdk.Input{ARM: rulestest.Model(t)})
	var linkedRec, otherRec string
	var seenLinked, seenOther bool
	for _, f := range out.Findings {
		if f.Key != "allowSharedKeyAccess" {
			continue
		}
		if strings.Contains(f.Resource.Name, "'other{0}'") {
			seenOther, otherRec = true, f.Recommendation
		} else {
			seenLinked, linkedRec = true, f.Recommendation
		}
	}
	if !seenLinked || !seenOther {
		t.Fatalf("want shared key findings on both accounts:\n%s", names(out.Findings))
	}
	if !strings.Contains(linkedRec, "AccountKey or SAS") || !strings.Contains(linkedRec, "Verify no client") {
		t.Errorf("linked account recommendation lacks the key-auth warning: %q", linkedRec)
	}
	if otherRec != "" {
		t.Errorf("unrelated account must keep the catalogue recommendation, got %q", otherRec)
	}
}

func connection(name string, props map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typeAccounts + "/connections", Name: "[format('{0}/{1}', format('ai-{0}', variables('token')), '" + name + "')]", Properties: props}
}

const condSearchRef = "[resourceId('Microsoft.Search/searchServices', format('cond-{0}', variables('token')))]"

func TestCompiledSEC002ConditionalAndUncertain(t *testing.T) {
	tests := []struct {
		name  string
		model sdk.ARMModel
		want  string // skip reason, "" for findings
	}{
		{
			name: "linked search is conditional on a parameter",
			model: rulestestWithout(t, func(r sdk.ARMResource) bool {
				return strings.Contains(r.Name, "'srch-{0}'") && r.Type == typeSearch
			}, connection("c2", props{"metadata": props{"ResourceId": condSearchRef}})),
			want: "conditional-resource",
		},
		{
			name:  "connection target is a parameter so scope is uncertain",
			model: extended(t, connection("c3", props{"target": "[parameters('endpoint')]"})),
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, "FND-SEC-002", &sdk.Input{ARM: tc.model})
			if tc.want == "" {
				// Findings on the provably linked search still win.
				if len(out.Findings) != 1 {
					t.Fatalf("want the linked search finding, got %+v", out)
				}
				return
			}
			if out.Skipped == nil || out.Skipped.Reason != tc.want {
				t.Fatalf("want skip %q, got %+v", tc.want, out)
			}
		})
	}
}

func rulestestWithout(t *testing.T, drop func(sdk.ARMResource) bool, extra ...sdk.ARMResource) sdk.ARMModel {
	t.Helper()
	m := rulestest.Without(rulestest.Model(t), drop)
	return model(append(m.Resources(), extra...))
}

func TestCompiledSEC002OpaqueOnlyIsScopeUncertain(t *testing.T) {
	// Only an opaque Foundry connection exists: no search can be proven linked
	// or unlinked.
	var searches []sdk.ARMResource
	for _, r := range rulestest.Model(t).Resources() {
		if r.Type == typeSearch {
			searches = append(searches, r)
		}
	}
	m := model(append(searches, connection("c", props{"target": "[parameters('endpoint')]"})))
	out := run(t, "FND-SEC-002", &sdk.Input{ARM: m})
	if out.Skipped == nil || out.Skipped.Reason != "scope-uncertain" {
		t.Fatalf("want scope-uncertain, got %+v", out)
	}
}

func TestLiteralFalseConditionIsNotDeployed(t *testing.T) {
	m := rulestest.Model(t)
	var rs []sdk.ARMResource
	for _, r := range m.Resources() {
		if r.Type == typeSearch && strings.Contains(r.Name, "'srch-{0}'") {
			r.Conditional, r.Condition = true, "false"
		}
		rs = append(rs, r)
	}
	out := run(t, "FND-SEC-002", &sdk.Input{ARM: model(rs)})
	// The only linked search is not deployed: nothing is left to evaluate.
	if out.Skipped == nil || len(out.Findings) != 0 {
		t.Fatalf("want skip, got %+v", out)
	}
}

func TestSubstringNamesDoNotLink(t *testing.T) {
	// Literal names: a connection to "srch" must not link "srch2" or "rch".
	conn := res(typeAccounts+"/connections", "acct/c", props{"target": "https://srch.search.windows.net"})
	rs := []sdk.ARMResource{res(typeSearch, "srch2", props{}), res(typeSearch, "rch", props{}), conn}
	out := run(t, "FND-SEC-002", &sdk.Input{ARM: model(rs)})
	if out.Skipped == nil || out.Skipped.Reason != SkipOutOfScope {
		t.Fatalf("want out-of-scope, got %+v", out)
	}
}

func TestFindingKeysSeparateSubChecks(t *testing.T) {
	out := run(t, "FND-SEC-004", &sdk.Input{ARM: model{res(typeStorage, "st", props{"allowBlobPublicAccess": true, "supportsHttpsTrafficOnly": false, "minimumTlsVersion": "TLS1_0"})}})
	seen := map[string]bool{}
	for _, f := range out.Findings {
		if f.Key == "" || seen[f.Key] {
			t.Errorf("missing or duplicate key %q", f.Key)
		}
		seen[f.Key] = true
	}
	if len(out.Findings) != 4 {
		t.Fatalf("findings = %d", len(out.Findings))
	}
}

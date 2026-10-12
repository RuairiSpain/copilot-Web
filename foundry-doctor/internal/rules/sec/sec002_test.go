package sec_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	id002     = "FND-SEC-002"
	typSearch = "Microsoft.Search/searchServices"
)

func withSearch(props obj, extra ...obj) []obj {
	return foundryWith("CognitiveSearch", typSearch, "srch", append([]obj{search("srch", props)}, extra...)...)
}

func cases002() []tcase {
	return []tcase{
		{"disableLocalAuth true passes", func(t *testing.T) {
			count(eval(t, id002, inputOf(t, withSearch(obj{"disableLocalAuth": true})))).want(t, 1, 0, 0, 0)
		}},
		{"service not referenced by Foundry is reported out of scope, not failed", func(t *testing.T) {
			c := count(eval(t, id002, inputOf(t, []obj{search("srch", obj{"disableLocalAuth": false})})))
			c.want(t, 0, 0, 0, 1)
			if c.skips[0].Reason != rules.ReasonNotApplicable || !strings.Contains(c.skips[0].Detail, "out-of-scope") {
				t.Fatalf("skip = %+v", c.skips[0])
			}
		}},
		{"disableLocalAuth absent and authOptions absent fails", func(t *testing.T) {
			c := count(eval(t, id002, inputOf(t, withSearch(obj{"sku": "x"}))))
			c.want(t, 0, 1, 0, 0)
			if f := c.findings[0]; f.Key != "disableLocalAuth:none" || f.Confidence != sdk.ConfidenceCertain ||
				!strings.Contains(f.Evidence, "connection conn1") {
				t.Fatalf("finding = %+v", f)
			}
		}},
		{"authOptions.aadOrApiKey fails with sub-state aadOrApiKey", func(t *testing.T) {
			p := obj{"disableLocalAuth": false, "authOptions": obj{"aadOrApiKey": obj{"aadAuthFailureMode": "http401WithBearerChallenge"}}}
			c := count(eval(t, id002, inputOf(t, withSearch(p))))
			c.want(t, 0, 1, 0, 0)
			if f := c.findings[0]; f.Key != "disableLocalAuth:aadOrApiKey(aadAuthFailureMode=http401WithBearerChallenge)" {
				t.Fatalf("key = %q", f.Key)
			}
			// A failure mode that is not an enum-shaped token is not echoed.
			p["authOptions"].(obj)["aadOrApiKey"].(obj)["aadAuthFailureMode"] = "free text here"
			c = count(eval(t, id002, inputOf(t, withSearch(p))))
			if c.findings[0].Key != "disableLocalAuth:aadOrApiKey" {
				t.Fatalf("key = %q", c.findings[0].Key)
			}
		}},
		{"authOptions.apiKeyOnly fails with sub-state apiKeyOnly", func(t *testing.T) {
			c := count(eval(t, id002, inputOf(t, withSearch(obj{"authOptions": obj{"apiKeyOnly": obj{}}}))))
			c.want(t, 0, 1, 0, 0)
			if c.findings[0].Key != "disableLocalAuth:apiKeyOnly" {
				t.Fatalf("key = %q", c.findings[0].Key)
			}
		}},
		{"disableLocalAuth from unresolved parameter is skipped", func(t *testing.T) {
			in := build(t, obj{"p": obj{"type": "bool"}}, nil, withSearch(obj{"disableLocalAuth": "[parameters('p')]"})...)
			c := count(eval(t, id002, in))
			c.want(t, 0, 0, 0, 1)
			if !c.skipReason("unresolved-value:") {
				t.Fatalf("skips = %+v", c.skips)
			}
		}},
		{"uncertain: service referenced only through a connection defined outside the scanned files is reported uncertain", func(t *testing.T) {
			// The capability host names a connection that is not in the template.
			in := inputOf(t, []obj{
				account("acct", nil), project("acct", "proj"),
				capHost("acct", "ch", obj{"vectorStoreConnections": []string{"external-conn"}}),
				search("srch", obj{"disableLocalAuth": false}),
			})
			c := count(eval(t, id002, in))
			c.want(t, 0, 0, 1, 0)
			if f := c.findings[0]; f.Confidence != sdk.ConfidenceUncertain || !strings.Contains(f.Evidence, "defined outside the scanned files") {
				t.Fatalf("finding = %+v", f)
			}
			// A compliant service stays a pass whatever the scope.
			in2 := inputOf(t, []obj{
				account("acct", nil), project("acct", "proj"),
				capHost("acct", "ch", obj{"vectorStoreConnections": []string{"external-conn"}}),
				search("srch", obj{"disableLocalAuth": true}),
			})
			count(eval(t, id002, in2)).want(t, 1, 0, 0, 0)
		}},
	}
}

func TestFND_SEC_002(t *testing.T) { runCases(t, cases002()) }

func TestSEC002_ScopeResolution(t *testing.T) {
	bad := obj{"disableLocalAuth": false}
	tests := []struct {
		name      string
		resources []obj
		want      [4]int
	}{
		{"connection ResourceId as literal ARM ID",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Search/searchServices/srch"),
				search("srch", bad)}, [4]int{0, 1, 0, 0}},
		{"connection ResourceId with subscription and group arguments",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", "[resourceId(subscriptionId(), 'rg', 'Microsoft.Search/searchServices', 'srch')]"),
				search("srch", bad)}, [4]int{0, 1, 0, 0}},
		{"connection to a different search service leaves this one out of scope",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", rid(typSearch, "other")),
				search("srch", bad)}, [4]int{0, 0, 0, 1}},
		{"connection of another category without ResourceId does not affect scope",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "AzureStorageAccount", ""),
				search("srch", bad)}, [4]int{0, 0, 0, 1}},
		{"search connection without ResourceId is uncertain",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "cognitivesearch", ""),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"category is compared case-insensitively",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "COGNITIVESEARCH", ""),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"capability host naming a known connection is not dangling",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", rid(typSearch, "other")),
				capHost("acct", "ch", obj{"vectorStoreConnections": []string{"c"}}),
				search("srch", bad)}, [4]int{0, 0, 0, 1}},
		{"capability host list unresolved is uncertain",
			[]obj{account("acct", nil), project("acct", "proj"),
				capHost("acct", "ch", obj{"vectorStoreConnections": "[parameters('x')]"}),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"capability host connection name from an expression is uncertain",
			[]obj{account("acct", nil), project("acct", "proj"),
				capHost("acct", "ch", obj{"vectorStoreConnections": []string{"[parameters('x')]"}}),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"capabilitySettings.vectorStore names the service (preview shape)",
			[]obj{account("acct", obj{"capabilitySettings": obj{"vectorStore": rid(typSearch, "srch")}}), project("acct", "proj"),
				search("srch", bad)}, [4]int{0, 1, 0, 0}},
		{"capabilitySettings.vectorStore unreadable is uncertain",
			[]obj{account("acct", obj{"capabilitySettings": obj{"vectorStore": "[parameters('id')]"}}), project("acct", "proj"),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"capabilitySettings.documentStore does not name a search service",
			[]obj{account("acct", obj{"capabilitySettings": obj{"documentStore": rid(typSearch, "srch")}}), project("acct", "proj"),
				search("srch", bad)}, [4]int{0, 0, 0, 1}},
		{"search name from an expression cannot be matched but the link is still read",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", "[resourceId('Microsoft.Search/searchServices', parameters('n'))]"),
				search("[parameters('n')]", bad)}, [4]int{0, 1, 0, 0}},
		{"project connection (three-segment name) is read",
			[]obj{account("acct", nil), project("acct", "proj"),
				res("Microsoft.CognitiveServices/accounts/projects/connections", "acct/proj/pc",
					obj{"category": "CognitiveSearch", "metadata": obj{"ResourceId": rid(typSearch, "srch")}}),
				search("srch", bad)}, [4]int{0, 1, 0, 0}},
		{"conditional connection makes the link uncertain",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", rid(typSearch, "srch")),
				search("srch", bad)}, [4]int{0, 0, 1, 0}},
		{"looped search service is uncertain",
			[]obj{account("acct", nil), project("acct", "proj"),
				connection("acct", "c", "CognitiveSearch", rid(typSearch, "srch")),
				search("srch", bad, obj{"copy": obj{"name": "x", "count": 2}})}, [4]int{0, 0, 1, 0}},
	}
	// The conditional case needs a condition on the connection.
	tests[14].resources[2]["condition"] = "[parameters('on')]"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := build(t, obj{"on": obj{"type": "bool"}, "n": obj{"type": "string"}}, nil, tt.resources...)
			count(eval(t, id002, in)).want(t, tt.want[0], tt.want[1], tt.want[2], tt.want[3])
		})
	}
}

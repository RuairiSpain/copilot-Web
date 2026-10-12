package sec_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	id003     = "FND-SEC-003"
	typCosmos = "Microsoft.DocumentDB/databaseAccounts"
)

// withCosmos wires the Cosmos account to the project the way the capability host flow does.
func withCosmos(props obj, extra ...obj) []obj {
	base := []obj{
		account("acct", nil), project("acct", "proj"),
		connection("acct", "cosmos-conn", "CosmosDB", rid(typCosmos, "cdb")),
		capHost("acct", "ch", obj{"threadStorageConnections": []string{"cosmos-conn"}}),
		cosmos("cdb", props),
	}
	return append(base, extra...)
}

func cases003() []tcase {
	return []tcase{
		{"Cosmos referenced by capability host with disableLocalAuth true passes", func(t *testing.T) {
			count(eval(t, id003, inputOf(t, withCosmos(obj{"disableLocalAuth": true})))).want(t, 1, 0, 0, 0)
		}},
		{"Cosmos referenced by capability host with disableLocalAuth false or absent fails", func(t *testing.T) {
			for name, props := range map[string]obj{"false": {"disableLocalAuth": false}, "absent": {"databaseAccountOfferType": "Standard"}} {
				t.Run(name, func(t *testing.T) {
					c := count(eval(t, id003, inputOf(t, withCosmos(props))))
					c.want(t, 0, 1, 0, 0)
					f := c.findings[0]
					if !strings.Contains(f.Evidence, "cosmos-conn") || !strings.Contains(f.Evidence, "ch.threadStorageConnections") ||
						!strings.Contains(f.Evidence, "authType (context only): AAD") || f.Key != "disableLocalAuth" {
						t.Fatalf("finding = %+v", f)
					}
				})
			}
		}},
		{"not-NoSQL API account is out of scope", func(t *testing.T) {
			// The catalogue marks the API-kind detection as unverified, so the rule does not exclude any
			// account by API: an account with a Mongo-style kind that Foundry uses is still checked.
			res := cosmos("cdb", obj{"disableLocalAuth": false}, obj{"kind": "MongoDB"})
			in := inputOf(t, append(withCosmos(nil)[:4:4], res))
			count(eval(t, id003, in)).want(t, 0, 1, 0, 0)
			// An account that nothing in Foundry names is out of scope whatever its API.
			count(eval(t, id003, inputOf(t, []obj{res}))).want(t, 0, 0, 0, 1)
		}},
		{"Cosmos account not referenced by any Foundry resource is skipped with reason out-of-scope", func(t *testing.T) {
			c := count(eval(t, id003, inputOf(t, []obj{cosmos("cdb", obj{"disableLocalAuth": false})})))
			c.want(t, 0, 0, 0, 1)
			if c.skips[0].Reason != rules.ReasonNotApplicable || !strings.Contains(c.skips[0].Detail, "out-of-scope") {
				t.Fatalf("skip = %+v", c.skips[0])
			}
		}},
		{"unresolved disableLocalAuth parameter is skipped", func(t *testing.T) {
			in := build(t, obj{"p": obj{"type": "bool"}}, nil, withCosmos(obj{"disableLocalAuth": "[parameters('p')]"})...)
			c := count(eval(t, id003, in))
			c.want(t, 0, 0, 0, 1)
			if !c.skipReason("unresolved-value:") {
				t.Fatalf("skips = %+v", c.skips)
			}
		}},
	}
}

func TestFND_SEC_003(t *testing.T) { runCases(t, cases003()) }

func TestSEC003_ScopeSources(t *testing.T) {
	bad := obj{"disableLocalAuth": false}
	t.Run("category variants are all recognised for an unreadable ResourceId", func(t *testing.T) {
		for _, cat := range []string{"CosmosDb", "CosmosDB", "AzureCosmosDb"} {
			in := inputOf(t, []obj{account("acct", nil), project("acct", "p"), connection("acct", "c", cat, ""), cosmos("cdb", bad)})
			count(eval(t, id003, in)).want(t, 0, 0, 1, 0)
		}
	})
	t.Run("capabilitySettings.documentStore (preview)", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", obj{"capabilitySettings": obj{"documentStore": rid(typCosmos, "cdb")}}), project("acct", "p"), cosmos("cdb", bad)})
		c := count(eval(t, id003, in))
		c.want(t, 0, 1, 0, 0)
		if !strings.Contains(c.findings[0].Evidence, "capabilitySettings.documentStore of acct") {
			t.Fatalf("evidence = %q", c.findings[0].Evidence)
		}
	})
	t.Run("connection to a Cosmos account declared under another name", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", nil), project("acct", "p"), connection("acct", "c", "CosmosDB", rid(typCosmos, "elsewhere")), cosmos("cdb", bad)})
		count(eval(t, id003, in)).want(t, 0, 0, 0, 1)
	})
	t.Run("storage list entries can name a Cosmos connection", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", nil), project("acct", "p"), capHost("acct", "ch", obj{"storageConnections": []string{"gone"}}), cosmos("cdb", bad)})
		count(eval(t, id003, in)).want(t, 0, 0, 1, 0)
	})
	t.Run("string instead of bool is skipped", func(t *testing.T) {
		count(eval(t, id003, inputOf(t, withCosmos(obj{"disableLocalAuth": "yes"})))).want(t, 0, 0, 0, 1)
	})
	t.Run("uncertain finding carries uncertain confidence", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", nil), project("acct", "p"), connection("acct", "c", "CosmosDB", ""), cosmos("cdb", bad)})
		c := count(eval(t, id003, in))
		if c.findings[0].Confidence != sdk.ConfidenceUncertain || !strings.Contains(c.findings[0].Evidence, "metadata.ResourceId") {
			t.Fatalf("finding = %+v", c.findings[0])
		}
	})
}

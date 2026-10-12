package sec_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	id004      = "FND-SEC-004"
	typStorage = "Microsoft.Storage/storageAccounts"
)

func goodStorage() obj {
	return obj{"allowSharedKeyAccess": false, "allowBlobPublicAccess": false, "supportsHttpsTrafficOnly": true, "minimumTlsVersion": "TLS1_2"}
}

func withStorage(props obj, extra ...obj) []obj {
	base := []obj{
		account("acct", nil), project("acct", "proj"),
		connection("acct", "st-conn", "AzureStorageAccount", rid(typStorage, "st")),
		storage("st", props),
	}
	return append(base, extra...)
}

// only4 applies a change to the compliant properties.
func mutate(f func(obj)) obj {
	p := goodStorage()
	f(p)
	return p
}

func failKeys(t *testing.T, props obj) []string {
	t.Helper()
	c := count(eval(t, id004, inputOf(t, withStorage(props))))
	var keys []string
	for _, f := range c.findings {
		keys = append(keys, f.Key)
	}
	return keys
}

func cases004() []tcase {
	return []tcase{
		{"all four properties compliant passes", func(t *testing.T) {
			count(eval(t, id004, inputOf(t, withStorage(goodStorage())))).want(t, 4, 0, 0, 0)
		}},
		{"allowSharedKeyAccess absent fails", func(t *testing.T) {
			k := failKeys(t, mutate(func(p obj) { delete(p, "allowSharedKeyAccess") }))
			if len(k) != 1 || k[0] != "allowSharedKeyAccess" {
				t.Fatalf("keys = %v", k)
			}
		}},
		{"allowBlobPublicAccess true fails", func(t *testing.T) {
			k := failKeys(t, mutate(func(p obj) { p["allowBlobPublicAccess"] = true }))
			if len(k) != 1 || k[0] != "allowBlobPublicAccess" {
				t.Fatalf("keys = %v", k)
			}
		}},
		{"minimumTlsVersion TLS1_0 fails", func(t *testing.T) {
			c := count(eval(t, id004, inputOf(t, withStorage(mutate(func(p obj) { p["minimumTlsVersion"] = "TLS1_0" })))))
			c.want(t, 3, 1, 0, 0)
			if f := c.findings[0]; f.Key != "minimumTlsVersion" || !strings.Contains(f.Evidence, "TLS1_0") {
				t.Fatalf("finding = %+v", f)
			}
		}},
		{"minimumTlsVersion absent fails", func(t *testing.T) {
			k := failKeys(t, mutate(func(p obj) { delete(p, "minimumTlsVersion") }))
			if len(k) != 1 || k[0] != "minimumTlsVersion" {
				t.Fatalf("keys = %v", k)
			}
		}},
		{"supportsHttpsTrafficOnly false fails", func(t *testing.T) {
			k := failKeys(t, mutate(func(p obj) { p["supportsHttpsTrafficOnly"] = false }))
			if len(k) != 1 || k[0] != "supportsHttpsTrafficOnly" {
				t.Fatalf("keys = %v", k)
			}
		}},
		{"properties driven by unresolved parameters are skipped per sub-check", func(t *testing.T) {
			p := obj{
				"allowSharedKeyAccess": "[parameters('p')]", "allowBlobPublicAccess": "[parameters('p')]",
				"supportsHttpsTrafficOnly": "[parameters('p')]", "minimumTlsVersion": "[parameters('s')]",
			}
			in := build(t, obj{"p": obj{"type": "bool"}, "s": obj{"type": "string"}}, nil, withStorage(p)...)
			c := count(eval(t, id004, in))
			c.want(t, 0, 0, 0, 4)
			for _, s := range c.skips {
				if !strings.HasPrefix(s.Reason, "unresolved-value:") {
					t.Fatalf("skip = %+v", s)
				}
			}
			// Only the unresolved sub-check is skipped; the rest are still evaluated.
			p2 := goodStorage()
			p2["minimumTlsVersion"] = "[parameters('s')]"
			in = build(t, obj{"s": obj{"type": "string"}}, nil, withStorage(p2)...)
			count(eval(t, id004, in)).want(t, 3, 0, 0, 1)
		}},
		{"uncertain: supportsHttpsTrafficOnly absent in a template using an API version older than 2019-04-01 is uncertain", func(t *testing.T) {
			p := goodStorage()
			delete(p, "supportsHttpsTrafficOnly")
			old := storage("st", p, obj{"apiVersion": "2018-07-01"})
			c := count(eval(t, id004, inputOf(t, append(withStorage(nil)[:3:3], old))))
			c.want(t, 3, 0, 1, 0)
			f := c.findings[0]
			if f.Confidence != sdk.ConfidenceUncertain || !strings.Contains(f.Evidence, "2018-07-01") {
				t.Fatalf("finding = %+v", f)
			}
			// From 2019-04-01 the default is true, so an absent value passes.
			cur := storage("st", p, obj{"apiVersion": "2019-04-01"})
			count(eval(t, id004, inputOf(t, append(withStorage(nil)[:3:3], cur)))).want(t, 4, 0, 0, 0)
		}},
	}
}

func TestFND_SEC_004(t *testing.T) { runCases(t, cases004()) }

func TestSEC004_Details(t *testing.T) {
	t.Run("storage not used by Foundry is out of scope", func(t *testing.T) {
		c := count(eval(t, id004, inputOf(t, []obj{storage("st", nil)})))
		c.want(t, 0, 0, 0, 1)
		if c.skips[0].Reason != rules.ReasonNotApplicable {
			t.Fatalf("skip = %+v", c.skips[0])
		}
	})
	t.Run("everything missing yields three findings and one default pass", func(t *testing.T) {
		c := count(eval(t, id004, inputOf(t, withStorage(obj{"sku": "x"}))))
		c.want(t, 2, 2, 0, 0) // blob public access and https default pass; shared key and TLS fail
	})
	t.Run("TLS1_3 passes and the enum is case-insensitive", func(t *testing.T) {
		count(eval(t, id004, inputOf(t, withStorage(mutate(func(p obj) { p["minimumTlsVersion"] = "tls1_3" }))))).want(t, 4, 0, 0, 0)
	})
	t.Run("unknown TLS value is skipped, not failed", func(t *testing.T) {
		count(eval(t, id004, inputOf(t, withStorage(mutate(func(p obj) { p["minimumTlsVersion"] = "TLS9" }))))).want(t, 3, 0, 0, 1)
	})
	t.Run("non-boolean literals are skipped", func(t *testing.T) {
		p := mutate(func(p obj) {
			p["allowSharedKeyAccess"], p["allowBlobPublicAccess"], p["supportsHttpsTrafficOnly"] = "no", "no", "no"
		})
		count(eval(t, id004, inputOf(t, withStorage(p)))).want(t, 1, 0, 0, 3)
	})
	t.Run("parameters with defaults are read", func(t *testing.T) {
		p := mutate(func(p obj) { p["allowSharedKeyAccess"] = "[parameters('shared')]" })
		params := obj{"shared": obj{"type": "bool", "defaultValue": true}}
		count(eval(t, id004, build(t, params, nil, withStorage(p)...))).want(t, 3, 1, 0, 0)
		count(eval(t, id004, build(t, params, obj{"shared": false}, withStorage(p)...))).want(t, 4, 0, 0, 0)
	})
	t.Run("conditional storage account is uncertain when failing", func(t *testing.T) {
		st := storage("st", mutate(func(p obj) { p["allowBlobPublicAccess"] = true }), obj{"condition": "[parameters('on')]"})
		in := build(t, obj{"on": obj{"type": "bool"}}, nil, append(withStorage(nil)[:3:3], st)...)
		count(eval(t, id004, in)).want(t, 3, 0, 1, 0)
	})
	t.Run("API version that is not a date is uncertain", func(t *testing.T) {
		p := goodStorage()
		delete(p, "supportsHttpsTrafficOnly")
		st := storage("st", p, obj{"apiVersion": "latest-" + canary()})
		c := count(eval(t, id004, inputOf(t, append(withStorage(nil)[:3:3], st))))
		c.want(t, 3, 0, 1, 0)
		if strings.Contains(c.findings[0].Evidence, canary()) {
			t.Fatal("api version text leaked")
		}
	})
}

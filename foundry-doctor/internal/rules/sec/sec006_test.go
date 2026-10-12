package sec_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

const id006 = "FND-SEC-006"

func ipRules(field string, values ...string) []obj {
	var out []obj
	for _, v := range values {
		out = append(out, obj{field: v})
	}
	return out
}

// storageWith returns a Foundry-used storage account with the given IP rules.
func storageWith(values ...string) []obj {
	return withStorage(obj{"networkAcls": obj{"defaultAction": "Deny", "ipRules": ipRules("value", values...)}})
}

func cosmosWith(values ...string) []obj {
	return withCosmos(obj{"ipRules": ipRules("ipAddressOrRange", values...)})
}

func cases006() []tcase {
	return []tcase{
		{"specific public /32 or /24 passes", func(t *testing.T) {
			count(eval(t, id006, inputOf(t, storageWith("203.0.113.10/32", "198.51.100.0/24")))).want(t, 1, 0, 0, 0)
			count(eval(t, id006, inputOf(t, cosmosWith("203.0.113.10", "198.51.100.0/24")))).want(t, 1, 0, 0, 0)
		}},
		{"Storage ipRules 10.0.0.0/24 fails as private range", func(t *testing.T) {
			c := count(eval(t, id006, inputOf(t, storageWith("10.0.0.0/24"))))
			c.want(t, 0, 1, 0, 0)
			f := c.findings[0]
			if f.Key != "private-range:properties.networkAcls.ipRules:10.0.0.0/24" ||
				f.Location.Pointer != "/resources/3/properties/networkAcls/ipRules/0/value" || !strings.Contains(f.Evidence, "private-network range") {
				t.Fatalf("finding = %+v", f)
			}
		}},
		{"Cosmos ipAddressOrRange 0.0.0.0 fails as all-Azure-datacenters", func(t *testing.T) {
			c := count(eval(t, id006, inputOf(t, cosmosWith("0.0.0.0"))))
			c.want(t, 0, 1, 0, 0)
			if f := c.findings[0]; !strings.HasPrefix(f.Key, "open-range:properties.ipRules:") || !strings.Contains(f.Evidence, "all Azure data centers") {
				t.Fatalf("finding = %+v", f)
			}
		}},
		{"any resource ipRules 0.0.0.0/0 fails as open range", func(t *testing.T) {
			kv := res("Microsoft.KeyVault/vaults", "kv", obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}})
			srch := search("srch", obj{"networkRuleSet": obj{"ipRules": ipRules("value", "0.0.0.0/0")}})
			acct := account("acct", obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}})
			in := inputOf(t, []obj{
				acct, project("acct", "proj"),
				connection("acct", "c1", "CognitiveSearch", rid(typSearch, "srch")),
				connection("acct", "c2", "AzureKeyVault", rid("Microsoft.KeyVault/vaults", "kv")),
				kv, srch,
			})
			// Storage and Cosmos too.
			c := count(eval(t, id006, in))
			c.want(t, 0, 3, 0, 0)
			in2 := inputOf(t, storageWith("0.0.0.0/0"))
			count(eval(t, id006, in2)).want(t, 0, 1, 0, 0)
			in3 := inputOf(t, cosmosWith("0.0.0.0/0"))
			count(eval(t, id006, in3)).want(t, 0, 1, 0, 0)
		}},
		{"ipRules built from an unresolved parameter is skipped", func(t *testing.T) {
			params := obj{"rules": obj{"type": "array"}, "one": obj{"type": "string"}}
			in := build(t, params, nil, withStorage(obj{"networkAcls": obj{"ipRules": "[parameters('rules')]"}})...)
			c := count(eval(t, id006, in))
			c.want(t, 0, 0, 0, 1)
			if !c.skipReason("unresolved-value:") {
				t.Fatalf("skips = %+v", c.skips)
			}
			in = build(t, params, nil, withStorage(obj{"networkAcls": obj{"ipRules": []obj{{"value": "[parameters('one')]"}}}})...)
			count(eval(t, id006, in)).want(t, 0, 0, 0, 1)
		}},
		{"uncertain: private range on Key Vault, Search or Cognitive Services is not evaluated (no verified source)", func(t *testing.T) {
			kv := res("Microsoft.KeyVault/vaults", "kv", obj{"networkAcls": obj{"ipRules": ipRules("value", "10.0.0.0/24")}})
			in := inputOf(t, []obj{
				account("acct", nil), project("acct", "proj"),
				connection("acct", "c2", "AzureKeyVault", rid("Microsoft.KeyVault/vaults", "kv")), kv,
			})
			c := count(eval(t, id006, in))
			c.want(t, 1, 0, 0, 1) // the range is not a finding; the check is reported as not evaluated
			if c.skips[0].Reason != rules.ReasonNotApplicable || !strings.Contains(c.skips[0].Detail, "not evaluated") {
				t.Fatalf("skip = %+v", c.skips[0])
			}
		}},
	}
}

func TestFND_SEC_006(t *testing.T) { runCases(t, cases006()) }

func TestSEC006_Ranges(t *testing.T) {
	tests := []struct {
		value string
		want  [4]int
	}{
		{"172.16.0.0/12", [4]int{0, 1, 0, 0}},
		{"172.20.1.1", [4]int{0, 1, 0, 0}},
		{"192.168.1.0/24", [4]int{0, 1, 0, 0}},
		{"100.64.0.0/10", [4]int{0, 1, 0, 0}},
		{"100.127.255.255/32", [4]int{0, 1, 0, 0}},
		{"172.32.0.1", [4]int{1, 0, 0, 0}},    // just outside 172.16.0.0/12
		{"8.0.0.0/5", [4]int{1, 0, 0, 0}},     // broader than a private range, not inside it
		{"0.0.0.0", [4]int{1, 0, 0, 0}},       // bare 0.0.0.0 is only special on Cosmos DB
		{"2001:db8::/32", [4]int{1, 0, 0, 0}}, // IPv6 is not matched against IPv4 private ranges
		{"not-an-ip", [4]int{0, 0, 0, 1}},
		{"10.0.0.0/33", [4]int{0, 0, 0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			count(eval(t, id006, inputOf(t, storageWith(tt.value)))).want(t, tt.want[0], tt.want[1], tt.want[2], tt.want[3])
		})
	}
	t.Run("several rules report each offending value separately", func(t *testing.T) {
		c := count(eval(t, id006, inputOf(t, storageWith("203.0.113.1", "10.1.0.0/16", "0.0.0.0/0"))))
		c.want(t, 0, 2, 0, 0)
		if c.findings[0].Key == c.findings[1].Key {
			t.Fatal("keys are not distinct")
		}
	})
	t.Run("non-string item is skipped", func(t *testing.T) {
		in := inputOf(t, withStorage(obj{"networkAcls": obj{"ipRules": []obj{{"value": 5}}}}))
		count(eval(t, id006, in)).want(t, 0, 0, 0, 1)
	})
	t.Run("absent or empty ipRules yields nothing", func(t *testing.T) {
		count(eval(t, id006, inputOf(t, withStorage(obj{"x": 1})))).want(t, 0, 0, 0, 0)
		count(eval(t, id006, inputOf(t, withStorage(obj{"networkAcls": obj{"ipRules": []obj{}}})))).want(t, 0, 0, 0, 0)
	})
	t.Run("storage not used by Foundry is out of scope", func(t *testing.T) {
		in := inputOf(t, []obj{storage("st", obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}})})
		count(eval(t, id006, in)).want(t, 0, 0, 0, 1)
	})
	t.Run("account without projects is out of scope", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}})})
		count(eval(t, id006, in)).want(t, 0, 0, 0, 1)
	})
	t.Run("account private range is not evaluated", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", obj{"networkAcls": obj{"ipRules": ipRules("value", "10.0.0.0/8")}}), project("acct", "p")})
		count(eval(t, id006, in)).want(t, 1, 0, 0, 1)
	})
	t.Run("account with an orphan project is uncertain", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}}), project("zzz", "p")})
		count(eval(t, id006, in)).want(t, 0, 0, 1, 0)
	})
	t.Run("conditional account is uncertain and a false condition is not deployed", func(t *testing.T) {
		params := obj{"on": obj{"type": "bool"}, "off": obj{"type": "bool", "defaultValue": false}}
		p := obj{"networkAcls": obj{"ipRules": ipRules("value", "0.0.0.0/0")}}
		in := build(t, params, nil, account("acct", p, obj{"condition": "[parameters('on')]"}), project("acct", "p"))
		count(eval(t, id006, in)).want(t, 0, 0, 1, 0)
		in = build(t, params, nil, account("acct", p, obj{"condition": "[parameters('off')]"}), project("acct", "p"))
		count(eval(t, id006, in)).want(t, 0, 0, 0, 1)
	})
	t.Run("cosmos private ranges fail", func(t *testing.T) {
		c := count(eval(t, id006, inputOf(t, cosmosWith("192.168.0.0/16"))))
		c.want(t, 0, 1, 0, 0)
	})
}

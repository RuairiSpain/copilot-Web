package bicep

import (
	"strings"
	"testing"
)

func TestSubnetsPlacesAgentThenPrivateEndpointSubnet(t *testing.T) {
	for _, c := range []struct {
		space     string
		agentLen  int
		withAgent bool
		agent, pe string
	}{
		{"10.20.0.0/16", 24, true, "10.20.0.0/24", "10.20.1.0/24"},
		{"10.20.0.0/16", 26, true, "10.20.0.0/26", "10.20.1.0/24"},
		{"192.168.0.0/16", 23, true, "192.168.0.0/23", "192.168.2.0/24"},
		{"10.0.0.0/22", 24, true, "10.0.0.0/24", "10.0.1.0/24"},
		{"10.0.0.0/24", 26, true, "10.0.0.0/26", "10.0.0.128/25"},
		{"10.20.0.0/16", 24, false, "", "10.20.0.0/24"},
		{"10.20.4.9/16", 24, false, "", "10.20.0.0/24"},
	} {
		agent, pe, problem := subnets(c.space, c.agentLen, c.withAgent)
		if problem != "" || agent != c.agent || pe != c.pe {
			t.Errorf("subnets(%s, %d, %v) = %q %q %q; want %q %q", c.space, c.agentLen, c.withAgent, agent, pe, problem, c.agent, c.pe)
		}
	}
	for _, c := range []struct {
		space, want string
		agentLen    int
	}{
		{"fd00::/48", "IPv4", 0},
		{"nonsense", "IPv4", 0},
		{"10.0.0.0/28", "no room", 29},
	} {
		if _, _, problem := subnets(c.space, c.agentLen, c.agentLen > 0); !strings.Contains(problem, c.want) {
			t.Errorf("subnets(%s, %d) problem = %q; want %q", c.space, c.agentLen, problem, c.want)
		}
	}
}

func TestLiterals(t *testing.T) {
	for in, want := range map[string]string{
		"plain":        `'plain'`,
		"it's":         `'it\'s'`,
		`back\slash`:   `'back\\slash'`,
		"${injection}": `'\${injection}'`,
		"two\nlines":   `'two\nlines'`,
		"tab\there\r":  `'tab\there\r'`,
	} {
		if got := str(in); got != want {
			t.Errorf("str(%q) = %s, want %s", in, got, want)
		}
	}
	if strs(nil) != "[]" || strs([]string{"a", "b"}) != "['a', 'b']" {
		t.Error("strs")
	}
	if propName("environment") != "environment" || propName("x-foundry-id") != "'x-foundry-id'" || propName("1abc") != "'1abc'" {
		t.Error("propName")
	}
	if tagsObject(nil) != "{}" || tagsObject(map[string]string{"b": "2", "a-1": "1"}) != "{\n  'a-1': '1'\n  b: '2'\n}" {
		t.Errorf("tagsObject = %q", tagsObject(map[string]string{"b": "2", "a-1": "1"}))
	}
	if zoneName("privatelink.blob.core.windows.net") != `'privatelink.blob.${environment().suffixes.storage}'` || zoneName("privatelink.search.windows.net") != `'privatelink.search.windows.net'` {
		t.Error("zoneName")
	}
	if zoneNames(nil) != "[]" || zoneNames([]string{"a.b", "privatelink.dfs.core.windows.net"}) != `['a.b', 'privatelink.dfs.${environment().suffixes.storage}']` {
		t.Error("zoneNames")
	}
	if boolean(true) != "true" || boolean(false) != "false" {
		t.Error("boolean")
	}
}

func TestSymbolsAreValidAndUnique(t *testing.T) {
	used := map[string]bool{}
	a, b := symbol("search:root", used), symbol("search-root", used)
	if a != "search_root" || b != "search_root_2" {
		t.Fatalf("%s %s", a, b)
	}
	if got := symbol("1st", used); got != "n_1st" {
		t.Fatal(got)
	}
	if got := symbol("", used); got != "n_" {
		t.Fatal(got)
	}
}

func TestParseARMID(t *testing.T) {
	a := parseARMID("/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/st1")
	if a.sub != "s1" || a.group != "rg1" || a.name != "st1" {
		t.Fatalf("%+v", a)
	}
}

func TestLongDeploymentNamesAreShortened(t *testing.T) {
	g := &gen{}
	short := g.deployName("storage")
	if short != "'xf-storage'" {
		t.Fatal(short)
	}
	long := g.deployName(strings.Repeat("x", 80))
	if len(long) > 66 || long != g.deployName(strings.Repeat("x", 80)) || long == g.deployName(strings.Repeat("x", 81)) {
		t.Fatalf("%s", long)
	}
}

func TestPrincipalTypes(t *testing.T) {
	for in, want := range map[string]string{"user": "User", "group": "Group", "servicePrincipal": "ServicePrincipal", "managedIdentity": "ServicePrincipal", "": "ServicePrincipal"} {
		if got := armPrincipalType(in); got != want {
			t.Errorf("%q -> %s", in, got)
		}
	}
}

func TestDeferredLabels(t *testing.T) {
	for kind, want := range map[string]string{
		"agent": "data plane", "evaluation": "data plane", "gateway": "Phase 5", "alerts": "Phase 5", "weird": "later phase",
		"governance (policy assignments, Defender plans, budgets)": "Phase 5",
	} {
		if got := deferredLabel(kind); !strings.Contains(got, want) {
			t.Errorf("%s -> %s", kind, got)
		}
	}
}

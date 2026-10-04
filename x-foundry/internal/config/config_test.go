package config_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
)

func decode(t *testing.T, doc string) *config.XFoundry {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const minimal = `{"topology":{"mode":"standalone"},"security":{"roles":{"admins":["Admins"]}},"projects":[{"name":"finance"}]}`

func TestDecodeFillsDefaultsAndTracksExplicitKeys(t *testing.T) {
	c := decode(t, `{"topology":{"mode":"standalone"},"security":{"roles":{"admins":[]},"purgeProtection":false},
		"projects":[{"name":"finance","inheritHub":false}],"search":{"enabled":false,"replicas":3}}`)
	if c.Security.PurgeProtection {
		t.Fatal("an explicit false must survive default filling")
	}
	if !c.Security.Network.PrivateDNS || c.Security.Network.AgentSubnetPrefixLength != 24 {
		t.Fatal("nested value structs get their defaults")
	}
	if c.Projects[0].InheritHub || !c.Projects[0].Has("inheritHub") {
		t.Fatal("explicit false inheritHub")
	}
	if c.Search == nil || c.Search.Enabled || c.Search.Replicas != 3 || c.Search.SKU != "standard" || !c.Search.SemanticRanking {
		t.Fatalf("search = %+v", c.Search)
	}
	if !c.Search.Has("replicas") || c.Search.Has("sku") {
		t.Fatal("explicit keys")
	}
	if c.Gateway != nil || c.Storage != nil {
		t.Fatal("absent optional sections stay nil")
	}
}

func TestDecodeListDefaults(t *testing.T) {
	c := decode(t, `{"topology":{"mode":"standalone"},"security":{"roles":{"admins":["a"]}},
		"projects":[{"name":"finance","agents":[{"name":"bot"},{"name":"two","protocols":["a2a"]}]}],
		"gateway":{"enabled":true}}`)
	agents := c.Projects[0].Agents
	if len(agents[0].Protocols) != 1 || agents[0].Protocols[0] != "responses" || agents[1].Protocols[0] != "a2a" {
		t.Fatalf("protocols = %v %v", agents[0].Protocols, agents[1].Protocols)
	}
	g := c.Gateway
	if len(g.Routing.RetryStatusCodes) != 5 || g.Routing.RetryStatusCodes[0] != 429 || g.TokenTracking.Dimensions[3] != "model" {
		t.Fatalf("gateway defaults = %+v", g.Routing)
	}
	if g.Security.MaxRequestBytes != 10485760 || g.Monitoring.Alerts.ErrorRatePercent != 5 {
		t.Fatalf("scalar defaults = %+v", g.Security)
	}
}

func TestPrincipalsDecodeFromStringsAndObjects(t *testing.T) {
	guid := "11111111-2222-3333-4444-555555555555"
	c := decode(t, `{"topology":{"mode":"standalone"},"projects":[{"name":"finance"}],
		"security":{"roles":{"admins":["Admins","`+guid+`",{"type":"user","name":"ann"}]}}}`)
	a := c.Security.Roles.Admins
	if a[0].Type != "group" || a[0].Name != "Admins" || a[1].ID != guid || a[1].Name != "" || a[2].Type != "user" {
		t.Fatalf("admins = %+v", a)
	}
	if a[1].Key() != guid || a[0].Key() != "admins" {
		t.Fatalf("keys: %q %q", a[1].Key(), a[0].Key())
	}
	if !config.IsGUID(guid) || config.IsGUID("not-a-guid") {
		t.Fatal("IsGUID")
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"topology":{"mode":"standalone"},"bogus":1}`), &raw)
	if _, err := config.Decode(raw); err == nil {
		t.Fatal("unknown keys are a backstop error")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	s := config.New[config.Storage]()
	if !s.Enabled || s.SKU != "Standard_ZRS" || s.RetentionDays != 30 || len(s.Purposes) != 1 || s.Has("sku") {
		t.Fatalf("storage = %+v", s)
	}
	d := config.New[config.ModelDeployment]()
	if d.VersionUpgradeOption != "NoAutoUpgrade" || d.Capacity != 10 {
		t.Fatalf("deployment = %+v", d)
	}
}

func TestCloneIsDeepAndKeepsExplicitKeys(t *testing.T) {
	c := decode(t, minimal)
	clone := config.Clone(c)
	clone.Projects[0].Name = "changed"
	clone.Security.Roles.Admins[0].Name = "changed"
	if c.Projects[0].Name != "finance" || c.Security.Roles.Admins[0].Name != "Admins" {
		t.Fatal("clone shares memory with the original")
	}
	if !clone.Projects[0].Has("name") {
		t.Fatal("explicit-key sets are copied")
	}
	c2 := decode(t, `{"topology":{"mode":"standalone"},"security":{"roles":{"admins":["a"]}},"projects":[{"name":"finance"}],
		"mcps":[{"name":"graph","endpoint":"https://a.example","headers":{"x":"y"}}],"agents":[{"name":"bot","environment":{"k":["v",1]}}]}`)
	cl := config.Clone(c2)
	cl.Mcps[0].Headers["x"] = "changed"
	if c2.Mcps[0].Headers["x"] != "y" {
		t.Fatal("maps are copied")
	}
	if !reflect.DeepEqual(config.Clone(c2.Agents), c2.Agents) {
		t.Fatal("clone must be equal to the original")
	}
}

func TestResolvedAgentSetup(t *testing.T) {
	for doc, want := range map[string]string{
		minimal: "standard",
		`{"topology":{"mode":"standalone"},"security":{"network":{"mode":"public"},"roles":{"admins":["a"]}},"projects":[{"name":"finance"}]}`:                            "basic",
		`{"topology":{"mode":"standalone"},"security":{"network":{"mode":"public"},"roles":{"admins":["a"]}},"projects":[{"name":"finance"}],"cosmos":{}}`:                "standard",
		`{"topology":{"mode":"standalone"},"security":{"roles":{"admins":["a"]}},"projects":[{"name":"finance"}],"agentService":{"setup":"basic"}}`:                       "basic",
		`{"topology":{"mode":"standalone"},"security":{"network":{"mode":"public"},"roles":{"admins":["a"]}},"projects":[{"name":"finance"}],"cosmos":{"enabled":false}}`: "basic",
	} {
		if got := decode(t, doc).ResolvedAgentSetup(); got != want {
			t.Errorf("%s -> %s, want %s", doc, got, want)
		}
	}
}

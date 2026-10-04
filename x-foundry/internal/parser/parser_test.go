package parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
)

const minimal = `
x-foundry:
  topology: {mode: standalone}
  security: {roles: {admins: [Admins]}}
  projects: [{name: finance}]
`

func failure(t *testing.T, err error) *diag.Failure {
	t.Helper()
	var f *diag.Failure
	if !errors.As(err, &f) {
		t.Fatalf("expected a *diag.Failure, got %v", err)
	}
	return f
}

func parseFail(t *testing.T, text string) []diag.Diagnostic {
	t.Helper()
	_, err := parser.ParseText(text, "<test>")
	return failure(t, err).Diagnostics
}

func has(ds []diag.Diagnostic, code, contains string) bool {
	for _, d := range ds {
		if d.Code == code && (strings.Contains(d.Message, contains) || strings.Contains(d.Path, contains)) {
			return true
		}
	}
	return false
}

func TestMinimalParsesWithDefaults(t *testing.T) {
	p, err := parser.ParseText(minimal, "<test>")
	if err != nil {
		t.Fatal(err)
	}
	c := p.Config
	if c.SchemaVersion != "1.0" || c.Security.Network.Mode != "private" || !c.Projects[0].InheritHub || c.Gateway != nil {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if !c.Projects[0].Has("name") || c.Projects[0].Has("inheritHub") {
		t.Fatal("explicit keys must be tracked")
	}
}

func TestParseFile(t *testing.T) {
	p, err := parser.ParseFile("../../examples/hub-spoke.yaml")
	if err != nil || p.Config.Hub.Name != "shared-ai" {
		t.Fatalf("%v %+v", err, p)
	}
	_, err = parser.ParseFile("/nonexistent/azure.yaml")
	if f := failure(t, err); f.Diagnostics[0].Code != "XF100" {
		t.Fatal(f)
	}
}

func TestInvalidYAMLAndDuplicateKeys(t *testing.T) {
	if ds := parseFail(t, "x-foundry: [unclosed"); ds[0].Code != "XF100" || !strings.Contains(ds[0].Message, "line") {
		t.Fatalf("%v", ds)
	}
	ds := parseFail(t, minimal+"  projects: [{name: other}]\n")
	if ds[0].Code != "XF100" || !strings.Contains(ds[0].Message, "already defined") {
		t.Fatalf("duplicate keys must be rejected: %v", ds)
	}
}

func TestDocumentShape(t *testing.T) {
	for _, text := range []string{"- a\n- b\n", "just text\n", ""} {
		if ds := parseFail(t, text); ds[0].Code != "XF101" {
			t.Fatalf("%q: %v", text, ds)
		}
	}
	if ds := parseFail(t, "name: app\n"); !has(ds, "XF101", "no 'x-foundry'") {
		t.Fatalf("%v", ds)
	}
	if ds := parseFail(t, "x-foundry: [a]\n"); ds[0].Code != "XF101" {
		t.Fatalf("%v", ds)
	}
}

func TestOtherAzdKeysAreIgnored(t *testing.T) {
	p, err := parser.ParseText(minimal+"services:\n  api: {project: ./api}\n", "<test>")
	if err != nil || p.Config.Projects[0].Name != "finance" {
		t.Fatalf("%v", err)
	}
}

func TestSchemaVersionMajorIsPinned(t *testing.T) {
	for _, v := range []string{"2.0", "0.9"} {
		ds := parseFail(t, minimal+"  schemaVersion: \""+v+"\"\n")
		if !has(ds, "XF103", "1.x") {
			t.Fatalf("%s: %v", v, ds)
		}
	}
	if p, err := parser.ParseText(minimal+"  schemaVersion: \"1.7\"\n", "<test>"); err != nil || p.Config.SchemaVersion != "1.7" {
		t.Fatalf("a compatible minor version is accepted: %v", err)
	}
	if ds := parseFail(t, minimal+"  schemaVersion: one\n"); ds[0].Code != "XF102" {
		t.Fatalf("a malformed version is a schema error: %v", ds)
	}
}

func TestSchemaErrorsHavePathsAndAreSorted(t *testing.T) {
	ds := parseFail(t, `
x-foundry:
  topology: {mode: mesh}
  security: {roles: {}}
  projects: [{name: x}]
  bogus: 1
`)
	var paths []string
	for _, d := range ds {
		paths = append(paths, d.Path)
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1] > paths[i] {
			t.Fatalf("paths are not sorted: %v", paths)
		}
	}
	for _, want := range []string{"x-foundry.topology.mode", "x-foundry.security.roles", "x-foundry.projects[0].name", "x-foundry"} {
		found := false
		for _, p := range paths {
			found = found || p == want
		}
		if !found {
			t.Fatalf("missing path %s in %v", want, paths)
		}
	}
}

func TestRequiredSectionsAndHubTopology(t *testing.T) {
	ds := parseFail(t, "x-foundry:\n  topology: {mode: standalone}\n")
	if !has(ds, "XF102", "security") || !has(ds, "XF102", "projects") {
		t.Fatalf("%v", ds)
	}
	if ds := parseFail(t, minimal+"  hub: {name: shared}\n"); ds[0].Code != "XF102" {
		t.Fatalf("hub in standalone: %v", ds)
	}
	hubSpoke := strings.Replace(minimal, "mode: standalone", "mode: hub-spoke", 1)
	if ds := parseFail(t, hubSpoke); ds[0].Code != "XF102" {
		t.Fatalf("hub-spoke without hub: %v", ds)
	}
}

func TestAnyOfErrorsAreCollapsed(t *testing.T) {
	ds := parseFail(t, strings.Replace(minimal, "admins: [Admins]", "admins: [{type: robot, name: x}]", 1))
	if !has(ds, "XF102", "does not match any allowed form") {
		t.Fatalf("%v", ds)
	}
}

func TestIPFormatsAreEnforced(t *testing.T) {
	good := strings.Replace(minimal, "security: {", "security: {network: {allowedIps: [10.0.0.0/8, 1.2.3.4, \"2001:db8::/32\"]}, ", 1)
	if _, err := parser.ParseText(good, "<test>"); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(minimal, "security: {", "security: {network: {allowedIps: [not-an-ip]}, ", 1)
	if ds := parseFail(t, bad); !has(ds, "XF102", "allowedIps") {
		t.Fatalf("%v", ds)
	}
}

func TestFormatsAreAsserted(t *testing.T) {
	cases := map[string]string{
		"uri":      "  mcps: [{name: graph, endpoint: \"not a uri\"}]\n",
		"hostname": "  security: {network: {allowedDomains: [\"bad host!\"]}, roles: {admins: [a]}}\n",
		"uuid":     "  gateway: {authentication: {audiences: [a], tenantId: not-a-uuid}}\n",
		"email":    "  governance: {budgets: {contacts: [not-an-email]}}\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			text := minimal
			if strings.HasPrefix(extra, "  security:") {
				text = strings.Replace(minimal, "  security: {roles: {admins: [Admins]}}\n", "", 1)
			}
			if ds := parseFail(t, text+extra); !has(ds, "XF102", "") {
				t.Fatalf("%v", ds)
			}
		})
	}
}

func TestSessionPoolAndRedisSessionKeys(t *testing.T) {
	cases := []struct{ name, extra, code, key string }{
		{"project", "  projects: [{name: ok, sessionPool: {size: 5}}]\n", "XF018", "sessionPool"},
		{"agent", "  projects: [{name: ok, agents: [{name: bot, sessionPooling: true}]}]\n", "XF018", "sessionPooling"},
		{"runtime", "  runtime: {enabled: true, image: x, sessionPools: []}\n", "XF018", "sessionPools"},
		{"agent pool", "  projects: [{name: ok, agentPool: {}}]\n", "XF018", "agentPool"},
		{"redis", "  redis: {enabled: true, sessionStore: true}\n", "XF019", "sessionStore"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := strings.Replace(minimal, "  projects: [{name: finance}]\n", "", 1) + c.extra
			ds := parseFail(t, text)
			if !has(ds, c.code, c.key) {
				t.Fatalf("%v", ds)
			}
			for _, d := range ds {
				if d.Code == "XF102" && strings.Contains(d.Message, "'"+c.key+"'") {
					t.Fatalf("the generic additionalProperties error must be suppressed: %v", d)
				}
			}
		})
	}
}

func TestFormatPath(t *testing.T) {
	for in, want := range map[string]string{
		"": "x-foundry", "/projects/0/name": "x-foundry.projects[0].name", "/a~1b/c~0d": "x-foundry.a/b.c~d",
	} {
		if got := parser.FormatPath(in); got != want {
			t.Errorf("FormatPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestYAMLWithNonStringKeys(t *testing.T) {
	v, err := parser.LoadYAML("1: a\n2: b\n")
	if err != nil {
		t.Fatal(err)
	}
	if m := v.(map[string]any); m["1"] != "a" {
		t.Fatalf("%v", m)
	}
}

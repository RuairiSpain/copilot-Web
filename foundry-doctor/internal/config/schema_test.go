package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"go.yaml.in/yaml/v3"
)

func readSchema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "schemas", "config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if s["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", s["$schema"])
	}
	return s
}

// ---- a small validator for the subset of JSON Schema the config schema uses ----

type validator struct{ root map[string]any }

func (v validator) resolve(s map[string]any) map[string]any {
	ref, ok := s["$ref"].(string)
	if !ok {
		return s
	}
	cur := any(v.root)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		cur = cur.(map[string]any)[part]
	}
	return cur.(map[string]any)
}

func jsonEqual(a, b any) bool {
	toNum := func(x any) (float64, bool) {
		switch n := x.(type) {
		case int:
			return float64(n), true
		case float64:
			return n, true
		}
		return 0, false
	}
	if x, ok := toNum(a); ok {
		y, ok := toNum(b)
		return ok && x == y
	}
	return reflect.DeepEqual(a, b)
}

// check returns the problems of doc against schema s at path p.
func (v validator) check(s map[string]any, doc any, p string) []string {
	s = v.resolve(s)
	var out []string
	bad := func(f string, a ...any) { out = append(out, p+": "+fmt.Sprintf(f, a...)) }
	if c, ok := s["const"]; ok && !jsonEqual(c, doc) {
		bad("want const %v", c)
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			found = found || jsonEqual(x, doc)
		}
		if !found {
			bad("%v not in enum", doc)
		}
	}
	if t, ok := s["type"].(string); ok {
		okType := false
		switch t {
		case "object":
			_, okType = doc.(map[string]any)
		case "array":
			_, okType = doc.([]any)
		case "string":
			_, okType = doc.(string)
		case "boolean":
			_, okType = doc.(bool)
		case "null":
			okType = doc == nil
		case "integer":
			_, okType = doc.(int)
		}
		if !okType {
			bad("want %s, got %T", t, doc)
			return out
		}
	}
	if n, ok := s["not"].(map[string]any); ok && len(v.check(n, doc, p)) == 0 {
		bad("matches a forbidden schema")
	}
	if one, ok := s["oneOf"].([]any); ok {
		n := 0
		for _, sub := range one {
			if len(v.check(sub.(map[string]any), doc, p)) == 0 {
				n++
			}
		}
		if n != 1 {
			bad("matches %d oneOf branches", n)
		}
	}
	switch d := doc.(type) {
	case string:
		if re, ok := s["pattern"].(string); ok && !regexp.MustCompile(re).MatchString(d) {
			bad("%q does not match %s", d, re)
		}
		if m, ok := s["minLength"].(float64); ok && float64(len(d)) < m {
			bad("shorter than %v", m)
		}
		if m, ok := s["maxLength"].(float64); ok && float64(len(d)) > m {
			bad("longer than %v", m)
		}
	case int:
		if m, ok := s["minimum"].(float64); ok && float64(d) < m {
			bad("%d below minimum %v", d, m)
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, x := range d {
				out = append(out, v.check(items, x, fmt.Sprintf("%s[%d]", p, i))...)
			}
		}
		if u, _ := s["uniqueItems"].(bool); u {
			for i := range d {
				for j := 0; j < i; j++ {
					if jsonEqual(d[i], d[j]) {
						bad("items %d and %d are equal", j, i)
					}
				}
			}
		}
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, have := d[r.(string)]; !have {
					bad("missing required %s", r)
				}
			}
		}
		if pn, ok := s["propertyNames"].(map[string]any); ok {
			for k := range d {
				out = append(out, v.check(pn, k, p+"{"+k+"}")...)
			}
		}
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if sub, ok := props[k].(map[string]any); ok {
				out = append(out, v.check(sub, d[k], p+"."+k)...)
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					bad("unknown key %q", k)
				}
			case map[string]any:
				out = append(out, v.check(ap, d[k], p+"."+k)...)
			}
		}
	}
	return out
}

func (v validator) validateYAML(t *testing.T, data string) []string {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
		t.Fatal(err)
	}
	return v.check(v.root, doc, "$")
}

// ---- tests ----

func TestExampleValidatesAgainstSchemaAndLoader(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "schemas", "examples", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	v := validator{readSchema(t)}
	if probs := v.validateYAML(t, string(b)); len(probs) > 0 {
		t.Errorf("example violates the schema:\n%s", strings.Join(probs, "\n"))
	}
	f, err := config.Parse("config.example.yaml", b)
	if err != nil {
		t.Fatalf("example rejected by the Go loader: %v", err)
	}
	if f.Environments["dev"].Policy.Network.PublicAccess == nil {
		t.Error("environments.dev.policy was not decoded")
	}
}

// TestSchemaAndLoaderAgree feeds the same documents to both and requires the same verdict.
func TestSchemaAndLoaderAgree(t *testing.T) {
	v := validator{readSchema(t)}
	cases := []struct {
		name, doc string
		ok        bool
	}{
		{"minimal", "version: 1\n", true},
		{"missing version", "profile: dev\n", false},
		{"wrong version", "version: 2\n", false},
		{"unknown top key", "version: 1\nfoo: 1\n", false},
		{"unknown policy key", "version: 1\npolicy:\n  nope: 1\n", false},
		{"unknown env key", "version: 1\nenvironments:\n  dev:\n    rules: {}\n", false},
		{"bad profile", "version: 1\nprofile: staging\n", false},
		{"short profile", "version: 1\nprofile: prod\n", true},
		{"bad scope", "version: 1\npolicy:\n  resourceScope: galaxy\n", false},
		{"bad publicAccess", "version: 1\npolicy:\n  network:\n    publicAccess: maybe\n", false},
		{"negative days", "version: 1\npolicy:\n  logRetention:\n    minimumDays: -1\n", false},
		{"negative ru", "version: 1\npolicy:\n  cost:\n    devMaxCosmosThroughput: -5\n", false},
		{"string days", "version: 1\npolicy:\n  logRetention:\n    minimumDays: ninety\n", false},
		{"dup models", "version: 1\npolicy:\n  models:\n    allow: [a/b, a/b]\n", false},
		{"model shape", "version: 1\npolicy:\n  models:\n    allow: [gpt4]\n", false},
		{"region upper", "version: 1\npolicy:\n  dataResidency:\n    regions: [WestEurope]\n", false},
		{"bad managedBy", "version: 1\npolicy:\n  managedByAzurePolicy: [nsg]\n", false},
		{"dup managedBy", "version: 1\npolicy:\n  managedByAzurePolicy: [diagnostic-settings, diagnostic-settings]\n", false},
		{"absolute path", "version: 1\ninputs:\n  azureYaml: /etc/azure.yaml\n", false},
		{"bad format", "version: 1\noutputs:\n  formats: [html]\n", false},
		{"bicep path", "version: 1\nadvanced:\n  bicep:\n    executable: /usr/bin/bicep\n", false},
		{"toggle bool", "version: 1\nadvanced:\n  checkov:\n    enabled: true\n", true},
		{"tag without name", "version: 1\npolicy:\n  tags:\n    required:\n      - format: x\n", false},
		{"bad mode", "version: 1\nvalidation:\n  runtime: maybe\n", false},
		{"scope id", "version: 1\npolicy:\n  allowedExternalScopes: [not-an-id]\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schemaOK := len(v.validateYAML(t, c.doc)) == 0
			_, err := config.Parse("t.yaml", []byte(c.doc))
			if schemaOK != c.ok {
				t.Errorf("schema verdict = %v, want %v: %v", schemaOK, c.ok, v.validateYAML(t, c.doc))
			}
			if (err == nil) != c.ok {
				t.Errorf("loader verdict = %v, want %v: %v", err == nil, c.ok, err)
			}
		})
	}
}

// goPaths lists key paths of the Go types by their yaml tags.
func goPaths(t reflect.Type, prefix string, out map[string]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			name, _, _ := strings.Cut(sf.Tag.Get("yaml"), ",")
			p := prefix + name
			out[p] = true
			goPaths(sf.Type, p+".", out)
		}
	case reflect.Map:
		out[prefix+"*"] = true
		goPaths(t.Elem(), prefix+"*.", out)
	case reflect.Slice:
		if e := t.Elem(); e.Kind() == reflect.Struct {
			goPaths(e, strings.TrimSuffix(prefix, ".")+"[].", out)
		}
	}
}

func schemaPaths(v validator, s map[string]any, prefix string, out map[string]bool) {
	s = v.resolve(s)
	if props, ok := s["properties"].(map[string]any); ok {
		for k, sub := range props {
			out[prefix+k] = true
			schemaPaths(v, sub.(map[string]any), prefix+k+".", out)
		}
	}
	if ap, ok := s["additionalProperties"].(map[string]any); ok {
		out[prefix+"*"] = true
		schemaPaths(v, ap, prefix+"*.", out)
	}
	if items, ok := s["items"].(map[string]any); ok {
		schemaPaths(v, items, strings.TrimSuffix(prefix, ".")+"[].", out)
	}
}

func TestSchemaKeysMatchGoStructTags(t *testing.T) {
	goSet, schSet := map[string]bool{}, map[string]bool{}
	goPaths(reflect.TypeFor[config.File](), "", goSet)
	s := readSchema(t)
	schemaPaths(validator{s}, s, "", schSet)
	for p := range goSet {
		if !schSet[p] {
			t.Errorf("key %s is in the Go types but not in the schema", p)
		}
	}
	for p := range schSet {
		if !goSet[p] {
			t.Errorf("key %s is in the schema but not in the Go types", p)
		}
	}
	if len(goSet) < 50 {
		t.Errorf("only %d Go key paths found; the reflection walk is broken", len(goSet))
	}
}

func TestSchemaClosedEverywhere(t *testing.T) {
	var walk func(path string, n any)
	walk = func(path string, n any) {
		m, ok := n.(map[string]any)
		if !ok {
			return
		}
		if m["type"] == "object" && m["properties"] != nil {
			if ap, ok := m["additionalProperties"].(bool); !ok || ap {
				t.Errorf("%s: object with properties must set additionalProperties false", path)
			}
		}
		for k, v := range m {
			walk(path+"/"+k, v)
		}
	}
	walk("", readSchema(t))
}

func schemaEnum(t *testing.T, path ...string) []string {
	t.Helper()
	cur := any(readSchema(t))
	for _, p := range path {
		cur = cur.(map[string]any)[p]
	}
	var out []string
	for _, e := range cur.(map[string]any)["enum"].([]any) {
		out = append(out, fmt.Sprint(e))
	}
	sort.Strings(out)
	return out
}

func TestSchemaEnumsMatchGoConstants(t *testing.T) {
	sorted := func(in ...string) []string { sort.Strings(in); return in }
	pol := []string{"properties", "policy"}
	_ = pol
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"resourceScope", schemaEnum(t, "$defs", "policy", "properties", "resourceScope"),
			sorted(string(model.ScopeSameResourceGroup), string(model.ScopeSameSubscription), string(model.ScopeAny))},
		{"publicAccess", schemaEnum(t, "$defs", "policy", "properties", "network", "properties", "publicAccess"),
			sorted(string(model.PublicForbidden), string(model.PublicEntraOnly), string(model.PublicAllowed))},
		{"dataResidency.scope", schemaEnum(t, "$defs", "policy", "properties", "dataResidency", "properties", "scope"),
			sorted(string(model.ResidencyGlobal), string(model.ResidencyDatazoneUS), string(model.ResidencyDatazoneEU), string(model.ResidencyDatazoneAPAC), string(model.ResidencyGeography))},
		{"outputs.formats", schemaEnum(t, "properties", "outputs", "properties", "formats", "items"), func() []string {
			var o []string
			for _, f := range config.Formats() {
				o = append(o, string(f))
			}
			return sorted(o...)
		}()},
		{"profile", schemaEnum(t, "properties", "profile"), sorted("dev", "test", "prod", "foundry-dev", "foundry-test", "foundry-prod")},
		{"managedBy", schemaEnum(t, "$defs", "policy", "properties", "managedByAzurePolicy", "items"),
			sorted(string(model.ManagedPrivateDNSZoneGroup), string(model.ManagedDiagnosticSettings))},
	}
	for _, c := range cases {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s: schema %v, Go %v", c.name, c.got, c.want)
		}
	}
}

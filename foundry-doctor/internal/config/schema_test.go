package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "config.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return m
}

func resolve(root, n map[string]any) map[string]any {
	for {
		ref, ok := n["$ref"].(string)
		if !ok {
			return n
		}
		cur := any(root)
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			cur = cur.(map[string]any)[part]
		}
		n = cur.(map[string]any)
	}
}

func schemaPaths(root, n map[string]any, prefix string, out map[string]bool) {
	n = resolve(root, n)
	if props, ok := n["properties"].(map[string]any); ok {
		if ap, ok := n["additionalProperties"].(map[string]any); ok {
			schemaPaths(root, ap, prefix+"{}", out)
		}
		for k, v := range props {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			schemaPaths(root, v.(map[string]any), p, out)
		}
		return
	}
	if ap, ok := n["additionalProperties"].(map[string]any); ok {
		schemaPaths(root, ap, prefix+"{}", out)
	}
	if items, ok := n["items"].(map[string]any); ok {
		schemaPaths(root, items, prefix+"[]", out)
	}
}

func goPaths(t reflect.Type, prefix string, out map[string]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice:
		goPaths(t.Elem(), prefix+"[]", out)
	case reflect.Map:
		goPaths(t.Elem(), prefix+"{}", out)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			p := name
			if prefix != "" {
				p = prefix + "." + name
			}
			out[p] = true
			goPaths(t.Field(i).Type, p, out)
		}
	}
}

func sorted(m map[string]bool) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	slices.Sort(s)
	return s
}

func TestSchemaKeysMatchGoStructs(t *testing.T) {
	root := loadSchema(t)
	sp, gp := map[string]bool{}, map[string]bool{}
	schemaPaths(root, root, "", sp)
	goPaths(reflect.TypeOf(Config{}), "", gp)
	if got, want := sorted(sp), sorted(gp); !reflect.DeepEqual(got, want) {
		var missing, extra []string
		for _, k := range want {
			if !sp[k] {
				missing = append(missing, k)
			}
		}
		for _, k := range got {
			if !gp[k] {
				extra = append(extra, k)
			}
		}
		t.Fatalf("schema and Go structs differ\nmissing from schema: %v\nnot in Go: %v", missing, extra)
	}
}

func enumAt(t *testing.T, root map[string]any, path ...string) []string {
	t.Helper()
	n := root
	for _, p := range path {
		n = resolve(root, n)
		n = n["properties"].(map[string]any)[p].(map[string]any)
	}
	n = resolve(root, n)
	if items, ok := n["items"].(map[string]any); ok {
		n = resolve(root, items)
	}
	raw, ok := n["enum"].([]any)
	if !ok {
		t.Fatalf("no enum at %v", path)
	}
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestSchemaEnumsMatchValidator(t *testing.T) {
	root := loadSchema(t)
	same := func(name string, got, want []string) {
		t.Helper()
		g, w := slices.Clone(got), slices.Clone(want)
		slices.Sort(g)
		slices.Sort(w)
		if !slices.Equal(g, w) {
			t.Errorf("%s: schema %v != Go %v", name, g, w)
		}
	}
	same("formats", enumAt(t, root, "outputs", "formats"), formatsSet)
	same("resourceScope", enumAt(t, root, "policy", "resourceScope"), resourceScopes)
	same("publicAccess", enumAt(t, root, "policy", "network", "publicAccess"), publicAccesses)
	same("dataResidency.scope", enumAt(t, root, "policy", "dataResidency", "scope"), residencyScope)
	same("managedByAzurePolicy", enumAt(t, root, "policy", "managedByAzurePolicy"), azurePolicyMgd)
	same("validation.bicep", enumAt(t, root, "validation", "bicep"), []string{"required", "optional", "disabled"})
	same("rules.include", enumAt(t, root, "rules", "include"), []string{"must-have", "nice-to-have"})
	for _, p := range enumAt(t, root, "profile") {
		if _, ok := LookupProfile(p); !ok {
			t.Errorf("schema profile %q not accepted by LookupProfile", p)
		}
	}
	for _, p := range ProfileNames() {
		if !slices.Contains(enumAt(t, root, "profile"), p) {
			t.Errorf("profile %q missing from schema", p)
		}
	}
}

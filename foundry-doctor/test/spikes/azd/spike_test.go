// Package azdspike is the executable half of the Phase 0 azure.yaml-only spike (ADR-004).
//
// It reads the fixtures in this directory with a lossless YAML node AST and checks the three defects that
// expected.json says a Foundry Doctor reader must find: duplicate keys, unresolved ${VAR} references, and
// `uses` entries that name no service or resource. It also shows why a decode-to-map reader is not enough.
//
// The analyser here is spike code that proves the approach. Production code will live in internal/azureyaml.
package azdspike

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type finding struct {
	Path        string
	Value       string // variable name or `uses` value
	Count       int    // occurrences, for duplicate keys
	Line        int
	Column      int
	Occurrences []position
}

type position struct {
	Line   int
	Column int
}

type result struct {
	Duplicates []finding
	Unresolved []finding // ${VAR} with no value and no default
	BadUses    []finding
}

// analyse reads azure.yaml bytes. env supplies azd environment values (.azure/<env>/.env).
func analyse(src []byte, env map[string]string) (result, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(src))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		if err == io.EOF {
			return result{}, fmt.Errorf("empty document")
		}
		return result{}, err
	}
	var res result
	if len(root.Content) == 0 {
		return res, fmt.Errorf("empty document")
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return res, fmt.Errorf("azure.yaml root must be a mapping, got %s", nodeKind(top))
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err == nil {
		return res, fmt.Errorf("azure.yaml must contain exactly one document")
	} else if err != io.EOF {
		return res, fmt.Errorf("invalid trailing YAML document: %w", err)
	}
	walk(top, "", env, &res)
	sort.SliceStable(res.Duplicates, func(i, j int) bool {
		a, b := res.Duplicates[i], res.Duplicates[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Path < b.Path
	})
	names := map[string]bool{}
	for _, section := range []string{"services", "resources"} {
		if m := child(top, section); m != nil && m.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(m.Content); i += 2 {
				names[m.Content[i].Value] = true
			}
		}
	}
	if svc := child(top, "services"); svc != nil && svc.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(svc.Content); i += 2 {
			uses := child(svc.Content[i+1], "uses")
			if uses == nil || uses.Kind != yaml.SequenceNode {
				continue
			}
			for j, u := range uses.Content {
				if !names[u.Value] {
					res.BadUses = append(res.BadUses, finding{Path: fmt.Sprintf("services.%s.uses[%d]", svc.Content[i].Value, j), Value: u.Value, Line: u.Line})
				}
			}
		}
	}
	return res, nil
}

func nodeKind(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return "null"
		}
		return "scalar"
	default:
		return fmt.Sprintf("kind %d", n.Kind)
	}
}

func child(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func walk(n *yaml.Node, path string, env map[string]string, res *result) {
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string][]position{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			k := key.Value
			seen[k] = append(seen[k], position{Line: key.Line, Column: key.Column})
			p := k
			if path != "" {
				p = path + "." + k
			}
			walk(n.Content[i+1], p, env, res)
		}
		for _, k := range slices.Sorted(mapsKeys(seen)) {
			if len(seen[k]) > 1 {
				p := k
				if path != "" {
					p = path + "." + k
				}
				occurrences := slices.Clone(seen[k])
				res.Duplicates = append(res.Duplicates, finding{
					Path: p, Count: len(occurrences), Line: occurrences[0].Line,
					Column: occurrences[0].Column, Occurrences: occurrences,
				})
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			walk(c, fmt.Sprintf("%s[%d]", path, i), env, res)
		}
	case yaml.ScalarNode:
		for _, name := range unresolved(n.Value, env) {
			res.Unresolved = append(res.Unresolved, finding{Path: path, Value: name, Line: n.Line})
		}
	}
}

func mapsKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// refRe matches ${NAME}, ${NAME:-default} and ${NAME-default}. A reference is skipped when it is
// escaped ($${...}) or is a Foundry expression (${{ ... }}). ADR-004 records that the real expander
// supports more of the shell grammar; this spike covers the forms the fixtures use.
var refRe = regexp.MustCompile(`(\$+)\{([A-Za-z_][A-Za-z0-9_]*)(:?-([^}]*))?\}`)

func unresolved(s string, env map[string]string) []string {
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		if len(m[1])%2 == 0 { // $${VAR} is an escaped literal
			continue
		}
		if _, ok := env[m[2]]; ok {
			continue
		}
		if m[3] != "" { // has a default
			continue
		}
		out = append(out, m[2])
	}
	return out
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			env[k] = strings.Trim(v, `"`)
		}
	}
	return env
}

type expected struct {
	Fixtures map[string]struct {
		DuplicateKeys []struct {
			Path        string
			Occurrences int
		} `json:"duplicateKeys"`
		UnresolvedEnvRefs []struct{ Path, Name string }  `json:"unresolvedEnvRefs"`
		UnresolvedUses    []struct{ Path, Value string } `json:"unresolvedUses"`
	} `json:"fixtures"`
}

func TestFixturesMatchExpected(t *testing.T) {
	raw, err := os.ReadFile("expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var exp expected
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatal(err)
	}
	if len(exp.Fixtures) != 2 {
		t.Fatalf("expected.json lists %d fixtures, want 2", len(exp.Fixtures))
	}
	for name, want := range exp.Fixtures {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(name, "azure.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			env := readEnv(t, filepath.Join(name, ".azure", "dev", "env.fixture"))
			got, err := analyse(src, env)
			if err != nil {
				t.Fatal(err)
			}
			var gotDup, wantDup []string
			for _, d := range got.Duplicates {
				gotDup = append(gotDup, fmt.Sprintf("%s x%d", d.Path, d.Count))
			}
			for _, d := range want.DuplicateKeys {
				wantDup = append(wantDup, fmt.Sprintf("%s x%d", d.Path, d.Occurrences))
			}
			if !slices.Equal(gotDup, wantDup) {
				t.Errorf("duplicate keys = %v, want %v", gotDup, wantDup)
			}
			var gotRef, wantRef []string
			for _, d := range got.Unresolved {
				gotRef = append(gotRef, d.Path+" "+d.Value)
			}
			for _, d := range want.UnresolvedEnvRefs {
				wantRef = append(wantRef, d.Path+" "+d.Name)
			}
			if !slices.Equal(gotRef, wantRef) {
				t.Errorf("unresolved env refs = %v, want %v", gotRef, wantRef)
			}
			var gotUses, wantUses []string
			for _, d := range got.BadUses {
				gotUses = append(gotUses, d.Path+" "+d.Value)
			}
			for _, d := range want.UnresolvedUses {
				wantUses = append(wantUses, d.Path+" "+d.Value)
			}
			if !slices.Equal(gotUses, wantUses) {
				t.Errorf("unresolved uses = %v, want %v", gotUses, wantUses)
			}
		})
	}
}

// Records how go.yaml.in/yaml/v3 treats duplicate keys (verified here, not assumed): decoding into a map rejects
// them with an error that names no useful location, while the node AST parses and lets Foundry Doctor report every
// occurrence with its line. Some other YAML libraries keep the last value silently, so the AST route is the portable one.
func TestDuplicateKeyBehaviourOfTheLibrary(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("invalid-duplicate-and-unresolved", "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	err = yaml.Unmarshal(src, &m)
	if err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("decode into a map should reject the duplicate key, got err = %v", err)
	}
	// The node AST still parses, which is what lets Foundry Doctor report the location and every occurrence.
	var n yaml.Node
	if err := yaml.Unmarshal(src, &n); err != nil {
		t.Fatalf("node AST should parse a file with duplicate keys: %v", err)
	}
}

func TestReferenceForms(t *testing.T) {
	env := map[string]string{"SET": "x"}
	tests := []struct {
		name string
		yaml string
		want []string // unresolved variable names
	}{
		{"plain set", "services:\n  a:\n    env:\n      K: ${SET}\n", nil},
		{"plain unset", "services:\n  a:\n    env:\n      K: ${MISSING}\n", []string{"MISSING"}},
		{"default form", "services:\n  a:\n    env:\n      K: ${MISSING:-fallback}\n", nil},
		{"escaped literal", "services:\n  a:\n    env:\n      K: $${MISSING}\n", nil},
		{"foundry expression passthrough", "services:\n  a:\n    env:\n      K: ${{project.endpoint}}\n", nil},
		{"escaped foundry expression", "services:\n  a:\n    env:\n      K: $${{project.endpoint}}\n", nil},
		{"two refs one unset", "services:\n  a:\n    env:\n      K: ${SET}-${MISSING}\n", []string{"MISSING"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := analyse([]byte(tc.yaml), env)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range res.Unresolved {
				got = append(got, f.Value)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("unresolved = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUsesMayNameAResource(t *testing.T) {
	src := "resources:\n  db:\n    type: db.postgres\nservices:\n  a:\n    uses:\n      - db\n      - nope\n"
	res, err := analyse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.BadUses) != 1 || res.BadUses[0].Value != "nope" {
		t.Fatalf("bad uses = %+v, want only \"nope\" (a resources: entry is a valid target)", res.BadUses)
	}
}

func TestUnparseableYAMLIsAnError(t *testing.T) {
	if _, err := analyse([]byte("services:\n  a: [unclosed\n"), nil); err == nil {
		t.Fatal("expected a parse error")
	}
	if _, err := analyse([]byte(""), nil); err == nil {
		t.Fatal("expected an error for an empty document")
	}
}

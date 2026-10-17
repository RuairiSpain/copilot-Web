package azureyaml

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestResolvedAliasesAndMergeKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		path    []string
		want    string
		missing bool
	}{
		{"merge key supplies value", "base: &b\n  host: azure.ai.agent\nservices:\n  a:\n    <<: *b\n    kind: prompt\n", []string{"services", "a", "host"}, "azure.ai.agent", false},
		{"explicit key beats merged", "base: &b\n  host: old\nservices:\n  a:\n    <<: *b\n    host: new\n", []string{"services", "a", "host"}, "new", false},
		{"explicit key after merge still wins", "base: &b\n  host: old\nservices:\n  a:\n    host: new\n    <<: *b\n", []string{"services", "a", "host"}, "new", false},
		{"earlier merge source wins", "x: &x\n  k: first\ny: &y\n  k: second\nservices:\n  a:\n    <<: [*x, *y]\n", []string{"services", "a", "k"}, "first", false},
		{"alias of scalar", "v: &v hello\nservices:\n  a:\n    host: *v\n", []string{"services", "a", "host"}, "hello", false},
		{"alias of mapping", "m: &m\n  host: h\nservices:\n  a: *m\n", []string{"services", "a", "host"}, "h", false},
		{"key not in merge source is absent", "base: &b\n  host: h\nservices:\n  a:\n    <<: *b\n", []string{"services", "a", "nope"}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Parse([]byte(tc.yaml), "azure.yaml")
			if err != nil {
				t.Fatal(err)
			}
			got, _, ok := d.Lookup(tc.path...)
			if tc.missing {
				if ok {
					t.Fatalf("found %q", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("got %q ok=%v want %q", got, ok, tc.want)
			}
		})
	}
}

func TestResolvedHasNoAliasNodes(t *testing.T) {
	d, err := Parse([]byte("b: &b\n  k: v\nservices:\n  a:\n    <<: *b\n  c: *b\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var check func(n *yaml.Node)
	check = func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode || n.Tag == "!!merge" {
			t.Fatalf("unresolved node %v %q", n.Kind, n.Value)
		}
		for _, c := range n.Content {
			check(c)
		}
	}
	check(d.Resolved())
	if got := strings.Join(d.ServiceNames(), ","); got != "a,c" {
		t.Errorf("services = %s", got)
	}
}

func TestResolvedBombIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 12; i++ {
		refs := strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d, ", i-1), 10), ", ")
		fmt.Fprintf(&b, "a%d: &a%d [%s]\n", i, i, refs)
	}
	d, err := Parse([]byte(b.String()), "azure.yaml")
	if err != nil {
		t.Skipf("parser rejected hostile document: %v", err)
	}
	found := false
	for _, dg := range d.Diagnostics() {
		found = found || strings.Contains(dg.Message, "alias expansion limit")
	}
	if !found {
		t.Error("expected alias expansion limit diagnostic")
	}
}

package azureyaml

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzAzureYAMLParse checks that Parse never panics or hangs and keeps its invariants for any input:
// an error means a nil result, and every issue, duplicate and interpolation carries a real position.
// Without -fuzz it runs the seed corpus as an ordinary test.
func FuzzAzureYAMLParse(f *testing.F) {
	seeds := []string{
		"", "~", "- a", "a: b: c", "name: x\n---\nname: y\n", "name: &a x\nb: *a\n", "<<: *x\n",
		"name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: p\n    uses: [a, b]\n",
		"services:\n\tapi: 1\n", "\xef\xbb\xbfname: x\r\nservices: {}\r\n", "name: ${A:-${B}}\n", "v: $${{ x }}\n",
		"name: x\nname: y\n", "? [a]\n: b\n", "a: !!binary AAAA\n", "a: [[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]\n",
		"ключ: значение\n", "a: '${'\n", "a: ${{\n", "a: \"\\u0000\"\n", "name: \x00\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	if files, err := filepath.Glob(filepath.Join(fixtureDir, "*.yaml")); err == nil {
		for _, name := range files {
			if b, err := os.ReadFile(name); err == nil {
				f.Add(b)
			}
		}
	}
	lim := Limits{MaxBytes: 1 << 16, MaxDepth: 32, MaxNodes: 5000, MaxScalarBytes: 4096}
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Parse(data, Options{Limits: lim})
		if err != nil {
			if r != nil {
				t.Fatal("non-nil result with error")
			}
			return
		}
		if r == nil || r.YAML == nil || r.YAML.Root == nil {
			t.Fatal("nil result without error")
		}
		for _, i := range r.Issues {
			if i.Pos.Line < 0 || i.Pos.Column < 0 || i.Code == "" || i.Level == "" {
				t.Fatalf("bad issue %+v", i)
			}
		}
		for _, d := range r.Duplicates {
			if len(d.Positions) < 2 {
				t.Fatalf("duplicate with one position: %+v", d)
			}
		}
		for _, i := range r.Interpolations {
			if i.Pos.Line < 1 || i.Form == "" {
				t.Fatalf("bad interpolation %+v", i)
			}
		}
		_ = r.UndefinedUses()
		_ = r.UnresolvedRefs(func(string) bool { return false })
	})
}

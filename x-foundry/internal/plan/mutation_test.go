package plan_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
)

type step struct {
	key string
	idx int
}

func clone(v any) any {
	b, _ := json.Marshal(v)
	dec := json.NewDecoder(jsonReader(b))
	dec.UseNumber()
	var out any
	_ = dec.Decode(&out)
	return out
}

// walk calls fn for every node below v with the path to it.
func walk(v any, path []step, fn func([]step, any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			p := append(append([]step{}, path...), step{key: k, idx: -1})
			fn(p, x)
			walk(x, p, fn)
		}
	case []any:
		for i, x := range t {
			p := append(append([]step{}, path...), step{idx: i})
			fn(p, x)
			walk(x, p, fn)
		}
	}
}

// parent returns the container holding the node at path, and the last step.
func parent(root any, path []step) (any, step) {
	cur := root
	for _, s := range path[:len(path)-1] {
		if s.idx >= 0 {
			cur = cur.([]any)[s.idx]
		} else {
			cur = cur.(map[string]any)[s.key]
		}
	}
	return cur, path[len(path)-1]
}

func set(root any, path []step, value any, remove bool) {
	holder, last := parent(root, path)
	switch h := holder.(type) {
	case map[string]any:
		if remove {
			delete(h, last.key)
		} else {
			h[last.key] = value
		}
	case []any:
		if !remove {
			h[last.idx] = value
		}
	}
}

// TestMutationsNeverPanic feeds single-field mutations of every example through the whole
// pipeline. Invalid input must produce diagnostics, never a panic or an internal error.
func TestMutationsNeverPanic(t *testing.T) {
	runs := 0
	for _, f := range exampleFiles(t) {
		text, err := readFile(f)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := parser.LoadYAML(text)
		if err != nil {
			t.Fatal(err)
		}
		section := doc.(map[string]any)["x-foundry"]
		var paths [][]step
		var values []any
		walk(section, nil, func(p []step, v any) { paths = append(paths, p); values = append(values, v) })
		for i, p := range paths {
			variants := []func(any){func(root any) { set(root, p, nil, true) }}
			switch v := values[i].(type) {
			case string:
				for _, s := range []string{"ghost-x", "gpt-5", "finance", "policies", "shared-ai"} {
					s := s
					variants = append(variants, func(root any) { set(root, p, s, false) })
				}
			case bool:
				variants = append(variants, func(root any) { set(root, p, !v, false) })
			case []any:
				if len(v) > 0 {
					variants = append(variants, func(root any) { set(root, p, append(append([]any{}, v...), v...), false) })
				}
			}
			for n, mutate := range variants {
				copyDoc := clone(doc)
				mutate(copyDoc.(map[string]any)["x-foundry"])
				name := fmt.Sprintf("%s/%v/%d", f, p, n)
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s panicked: %v", name, r)
						}
					}()
					if _, err := plan.AnalyseMapping(copyDoc, name); err != nil {
						t.Fatalf("%s: internal error: %v", name, err)
					}
				}()
				runs++
			}
		}
	}
	if runs < 1000 {
		t.Fatalf("only %d mutations ran", runs)
	}
	t.Logf("%d mutations", runs)
}

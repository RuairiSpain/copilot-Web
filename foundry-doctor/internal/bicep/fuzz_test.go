package bicep

import "testing"

// FuzzNormalise checks that arbitrary input never panics and that a successful result is
// internally consistent.
func FuzzNormalise(f *testing.F) {
	for _, p := range [][]string{{"spike", "main.arm.json"}, {"foundry-v2", "foundry.arm.json"}, {"foundry", "foundry.arm.json"}} {
		f.Add(readFixture(f, p...))
	}
	f.Add([]byte(`{"resources":{"a":{"type":1,"name":[],"copy":3,"condition":{},"properties":"[parameters('x')]"}}}`))
	f.Add([]byte(`{"parameters":{"x":null},"resources":[null,1,{"type":"Microsoft.Resources/deployments","properties":{"template":5,"parameters":[]}}]}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		n, err := Normalise(data, Options{MaxDepth: 4, MaxResources: 200})
		if err != nil {
			return
		}
		if len(n.Info) != len(n.Template.Resources) {
			t.Fatalf("Info %d != Resources %d", len(n.Info), len(n.Template.Resources))
		}
		for _, r := range n.Template.Resources {
			_ = r.Get("properties.a[0].b")
		}
	})
}

func FuzzParseDiagnostics(f *testing.F) {
	f.Add("/a/b.bicep(1,2) : Error BCP001: boom [https://aka.ms/bicep/core-diagnostics#BCP001]\nnoise")
	f.Add(" : Error x: ")
	f.Fuzz(func(t *testing.T, s string) {
		for _, d := range ParseDiagnostics(s) {
			_ = toModel(d, "/a")
		}
	})
}

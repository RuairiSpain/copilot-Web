package bicep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const armDefault = `{
 "languageVersion": "",
 "parameters": {"pw": {"type": "securestring"}, "n": {"type": "string"}},
 "resources": [
  {"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"s1","condition":"[parameters('n')]"},
  {"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"[format('s{0}', copyIndex())]","copy":{"name":"loop","count":"[length(x)]"}},
  {"type":"Microsoft.Resources/deployments","apiVersion":"2022-09-01","name":"storage","properties":{"template":{
     "resources":[{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"inner","dependsOn":["b","a"]}],
     "outputs":{"id":{"type":"string","value":"[resourceId('x')]"}}}}}
 ],
 "outputs": {"z":{"type":"string","value":"lit"},"a":{"type":"string","value":"[1]"}}
}`

const armSymbolic = `{
 "languageVersion": "2.0",
 "resources": {
  "zeta": {"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"z"},
  "alpha": {"type":"Microsoft.Foo/bar","apiVersion":"1","name":"a","dependsOn":["zeta"]}
 }
}`

func TestParseARM(t *testing.T) {
	t.Run("array form", func(t *testing.T) {
		tpl, err := ParseARM([]byte(armDefault))
		if err != nil {
			t.Fatal(err)
		}
		if len(tpl.Resources) != 3 || !tpl.Resources[0].Conditional || tpl.Resources[1].Copy == nil || tpl.Resources[1].SymbolicName != "loop" {
			t.Fatalf("%+v", tpl.Resources)
		}
		if !tpl.Parameters[1].Secure || tpl.Parameters[0].Secure {
			t.Errorf("params sorted n,pw: %+v", tpl.Parameters)
		}
		if tpl.Outputs[0].Name != "a" || tpl.Outputs[1].Value != "lit" {
			t.Errorf("outputs: %+v", tpl.Outputs)
		}
		m := tpl.Resources[2]
		if !m.IsModule() || m.Pointer != "/resources/2" || m.Nested.Resources[0].Pointer != "/resources/2/properties/template/resources/0" {
			t.Fatalf("module: %+v", m)
		}
		if d := m.Nested.Resources[0].DependsOn; d[0] != "a" || d[1] != "b" {
			t.Errorf("dependsOn unsorted: %v", d)
		}
		var n int
		tpl.Walk(func(Resource, int) { n++ })
		if n != 4 {
			t.Errorf("walk=%d", n)
		}
		if g := tpl.Resources[0].Guards(); len(g) != 1 || !strings.HasPrefix(g[0], "condition") {
			t.Errorf("guards %v", g)
		}
	})
	t.Run("symbolic form", func(t *testing.T) {
		tpl, err := ParseARM([]byte(armSymbolic))
		if err != nil {
			t.Fatal(err)
		}
		if tpl.Resources[0].SymbolicName != "alpha" || tpl.Resources[1].SymbolicName != "zeta" || tpl.Resources[0].Pointer != "/resources/alpha" {
			t.Fatalf("%+v", tpl.Resources)
		}
	})
	bad := []struct{ name, in string }{
		{"malformed", `{`},
		{"resources string", `{"resources":"x"}`},
		{"bad resource", `{"resources":[1]}`},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			if _, err := ParseARM([]byte(b.in)); err == nil {
				t.Fatal("want error")
			}
		})
	}
	t.Run("missing resources", func(t *testing.T) {
		tpl, err := ParseARM([]byte(`{}`))
		if err != nil || len(tpl.Resources) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("depth bound", func(t *testing.T) {
		s := `{"resources":[]}`
		for i := 0; i < maxModuleDepth+3; i++ {
			s = `{"resources":[{"type":"Microsoft.Resources/deployments","name":"m","properties":{"template":` + s + `}}]}`
		}
		if _, err := ParseARM([]byte(s)); err == nil {
			t.Fatal("want depth error")
		}
	})
}

func TestMapper(t *testing.T) {
	tpl, _ := ParseARM([]byte(armDefault))
	m := Mapper{Entry: "main.bicep", Modules: ModuleFiles{"storage": "modules/storage.bicep"}}
	locs := m.LocateAll(tpl)
	if len(locs) != 4 {
		t.Fatalf("%d", len(locs))
	}
	if l := locs[0].Location; l.File != "main.bicep" || l.Confidence != ConfidenceLikely || l.Line != 0 || len(l.Guards) != 1 {
		t.Errorf("%+v", l)
	}
	if l := locs[3].Location; l.File != "modules/storage.bicep" || l.Confidence != ConfidenceLikely || locs[3].Depth != 1 {
		t.Errorf("%+v", l)
	}
	unresolved := Mapper{Entry: "main.bicep"}.LocateAll(tpl)[3].Location
	if unresolved.Confidence != ConfidenceUncertain || unresolved.File != "main.bicep" {
		t.Errorf("%+v", unresolved)
	}
	if l := (Mapper{}).LocateAll(tpl)[0].Location; l.Confidence != ConfidenceUncertain {
		t.Errorf("no entry: %+v", l)
	}
	expr := Mapper{Entry: "m", Modules: ModuleFiles{"storage": "s.bicep"}}
	if f, ok := expr.lookup("[format('{0}', 'storage')]"); !ok || f != "s.bicep" {
		t.Error("expression match")
	}
	d := FromDiagnostic(Diagnostic{File: "a", Line: 2, Column: 3})
	if d.Confidence != ConfidenceCertain || d.Line != 2 {
		t.Errorf("%+v", d)
	}
}

func TestEnvScrub(t *testing.T) {
	r := ExecRunner{Getenv: func(k string) string {
		return map[string]string{"PATH": "/bin", "AZURE_CLIENT_SECRET": "s3cret", "HOME": "/h"}[k]
	}}
	for _, e := range r.env() {
		if strings.Contains(e, "s3cret") || strings.HasPrefix(e, "AZURE_") {
			t.Fatalf("leaked %s", e)
		}
	}
}

func TestExecRunnerFaults(t *testing.T) {
	_, err := ExecRunner{}.Run(t.Context(), filepath.Join(t.TempDir(), "missing-exe"), nil, "")
	if err == nil {
		t.Fatal("want start error")
	}
}

func TestFixturesPresent(t *testing.T) {
	for _, f := range []string{"main.bicep", "error.bicep", "bicepconfig.json", "modules/storage.bicep"} {
		if _, err := os.Stat(filepath.Join("testdata", f)); err != nil {
			t.Errorf("fixture %s: %v", f, err)
		}
	}
}

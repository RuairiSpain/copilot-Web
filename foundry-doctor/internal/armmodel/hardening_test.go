package armmodel_test

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// blowup builds a template of `depth` nested modules. Each module passes its
// parameter a to the next one as format('{0}{1}...{7}', a x8), so unbounded
// substitution grows 8^depth.
func blowup(depth int) []byte {
	args := make([]string, 8)
	fmts := make([]string, 8)
	for i := range args {
		args[i] = "parameters('a')"
		fmts[i] = fmt.Sprintf("{%d}", i)
	}
	grow := "[format('" + strings.Join(fmts, "") + "', " + strings.Join(args, ", ") + ")]"
	leaf := map[string]any{
		"type": "Microsoft.Search/searchServices", "apiVersion": "2025-05-01",
		"name": "[parameters('a')]", "location": "[parameters('a')]", "properties": map[string]any{"tag": "[parameters('a')]"},
	}
	tpl := map[string]any{"parameters": map[string]any{"a": map[string]any{"type": "string"}}, "resources": []any{leaf}}
	for i := 0; i < depth; i++ {
		dep := map[string]any{
			"type": "Microsoft.Resources/deployments", "apiVersion": "2025-04-01", "name": fmt.Sprintf("m%d", depth-i),
			"properties": map[string]any{
				"mode": "Incremental", "expressionEvaluationOptions": map[string]any{"scope": "inner"},
				"parameters": map[string]any{"a": map[string]any{"value": grow}}, "template": tpl,
			},
		}
		tpl = map[string]any{"parameters": map[string]any{"a": map[string]any{"type": "string"}}, "resources": []any{dep}}
	}
	// The entry template also has its own resource named after its parameter.
	tpl["resources"] = append(tpl["resources"].([]any), map[string]any{
		"type": "Microsoft.Search/searchServices", "apiVersion": "2025-05-01", "name": "[parameters('a')]", "properties": map[string]any{},
	})
	b, _ := json.Marshal(tpl)
	return b
}

// R1: exponential parameter substitution is capped. The template is small, the
// model completes quickly within a memory bound, over-cap values are unknown.
func TestSubstitutionBlowupIsBounded(t *testing.T) {
	raw := blowup(10)
	if len(raw) > 32<<10 {
		t.Fatalf("test template is %d bytes; it should stay a small hostile input", len(raw))
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	m, err := armmodel.FromARM(raw, armmodel.Options{Entry: "main.bicep"})
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > time.Second {
		t.Errorf("model took %v, want well under a second", elapsed)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 256<<20 {
		t.Errorf("model allocated %d MiB", alloc>>20)
	}
	rs := m.Resources()
	if len(rs) != 2 {
		t.Fatalf("want the leaf and the entry resource, got %d", len(rs))
	}
	total := 0
	for _, r := range rs {
		total += len(r.Name) + len(r.Region)
		if len(r.Name) > armmodel.MaxExprBytes {
			t.Errorf("name of %d bytes exceeds the expression cap", len(r.Name))
		}
	}
	if total > 4*armmodel.MaxExprBytes {
		t.Errorf("retained %d bytes of names", total)
	}
	// Whatever the leaf's name is, it is the value of a module-local parameter
	// the caller could not inline, so it must not compare equal to anything.
	leaf, entry := rs[0], rs[1]
	if got := armexpr.Compare(armexpr.ParseName(leaf.Type, leaf.Name), armexpr.ParseName(entry.Type, entry.Name)); got != armexpr.Unknown {
		t.Errorf("Compare = %v, want Unknown (leaf name %.80q)", got, leaf.Name)
	}
	if got := armexpr.CompareValues(leaf.Region, entry.Name); got != armexpr.Unknown {
		t.Errorf("CompareValues = %v, want Unknown", got)
	}
	if !strings.Contains(leaf.Name, armexpr.ScopeSep) {
		t.Errorf("the over-cap binding should keep a scope-qualified reference: %.120q", leaf.Name)
	}
}

func TestBindingAtCapIsKeptAndOverCapIsNot(t *testing.T) {
	mk := func(n int) []byte {
		val := strings.Repeat("x", n)
		return []byte(`{"resources":[{"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"m","properties":{"mode":"Incremental",
		 "expressionEvaluationOptions":{"scope":"inner"},"parameters":{"n":{"value":"` + val + `"}},
		 "template":{"parameters":{"n":{"type":"string"}},"resources":[
		  {"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('n')]","properties":{}}]}}}]}`)
	}
	small, err := armmodel.FromARM(mk(100), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := small.Resources()[0].Name; got != strings.Repeat("x", 100) {
		t.Errorf("small binding not substituted: %.60q", got)
	}
	big, err := armmodel.FromARM(mk(armmodel.MaxBindingBytes+10), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := big.Resources()[0].Name; got != "[parameters('n@m')]" {
		t.Errorf("over-cap binding must stay scope-qualified, got %.60q", got)
	}
}

func TestModelBoundsTruncate(t *testing.T) {
	var rs []string
	for i := 0; i < armmodel.MaxResources+5; i++ {
		rs = append(rs, fmt.Sprintf(`{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-05-01","name":"s%d"}`, i))
	}
	m, err := armmodel.FromARM([]byte(`{"resources":[`+strings.Join(rs, ",")+`]}`), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Resources()) != armmodel.MaxResources || !m.Truncated() {
		t.Errorf("resources %d truncated %v", len(m.Resources()), m.Truncated())
	}
	var nilModel *armmodel.Model
	if nilModel.Truncated() {
		t.Error("nil model reports truncated")
	}
}

// R2: module paths come from deployment names, so inserting a module earlier
// does not change the path of an existing one.
func TestModulePathsAreIndependentOfDeclarationOrder(t *testing.T) {
	paths := func(raw []byte) map[string]string {
		m, err := armmodel.FromARM(raw, armmodel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for i, r := range m.Resources() {
			out[strings.ToLower(r.Type)+"|"+r.Name] = m.Placements()[i].Module
		}
		return out
	}
	base := rulestest.ScenarioJSON(t, "twice")
	var doc map[string]any
	if err := json.Unmarshal(base, &doc); err != nil {
		t.Fatal(err)
	}
	rs := doc["resources"].([]any)
	extra := map[string]any{
		"type": "Microsoft.Resources/deployments", "apiVersion": "2025-04-01", "name": "inserted",
		"properties": map[string]any{"mode": "Incremental", "expressionEvaluationOptions": map[string]any{"scope": "inner"},
			"template": map[string]any{"resources": []any{map[string]any{"type": "Microsoft.Storage/storageAccounts", "apiVersion": "2023-05-01", "name": "ins"}}}},
	}
	doc["resources"] = append([]any{extra}, rs...)
	changed, _ := json.Marshal(doc)
	before, after := paths(base), paths(changed)
	for k, p := range before {
		if after[k] != p {
			t.Errorf("%s: module path %q became %q after inserting an unrelated module", k, p, after[k])
		}
		if p != "" && (strings.Contains(p, "m1") || strings.Contains(p, "m2")) {
			t.Errorf("module path %q looks index-derived", p)
		}
	}
	if len(before) == 0 {
		t.Fatal("no resources")
	}
}

func TestModulePathUsesDeploymentName(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	var got []string
	for _, p := range m.Placements() {
		got = append(got, p.Module)
	}
	want := []string{"", "search-f", "search-x", "conn"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("module paths %v, want %v", got, want)
	}
}

// R3: a module output read resolves to the module's output expression, in the
// caller's terms, when the module is neither conditional nor looped.
func TestModuleOutputIsResolved(t *testing.T) {
	m := rulestest.Scenario(t, "avm")
	var conn sdk.ARMResource
	for _, r := range m.Resources() {
		if strings.HasSuffix(strings.ToLower(r.Type), "/connections") {
			conn = r
		}
	}
	meta, _ := conn.Properties["metadata"].(map[string]any)
	id, _ := meta["ResourceId"].(string)
	want := "[resourceId('Microsoft.Search/searchServices', format('srch-{0}', variables('uniq')))]"
	if id != want {
		t.Errorf("ResourceId = %q, want %q", id, want)
	}
	if strings.Contains(id, "reference(") || strings.Contains(id, armexpr.ScopeSep) {
		t.Errorf("unresolved output read left in %q", id)
	}
}

func outputReader(cond string) []byte {
	return []byte(`{"resources":[
	 {"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"s1",` + cond + `"properties":{"mode":"Incremental",
	  "expressionEvaluationOptions":{"scope":"inner"},
	  "template":{"resources":[{"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"srch","properties":{}}],
	   "outputs":{"id":{"type":"string","value":"[resourceId('Microsoft.Search/searchServices','srch')]"},
	              "key":{"type":"securestring","value":"[listKeys(resourceId('Microsoft.Search/searchServices','srch'),'2025-05-01').primaryKey]"}}}}},
	 {"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-05-01","name":"st","properties":{
	   "a":"[reference(resourceId('Microsoft.Resources/deployments','s1'),'2025-04-01').outputs.id.value]",
	   "b":"[reference(resourceId('Microsoft.Resources/deployments','s1'),'2025-04-01').outputs.key.value]",
	   "c":"[reference(resourceId('Microsoft.Resources/deployments','s1'),'2025-04-01').outputs.missing.value]",
	   "d":"[reference(resourceId('Microsoft.Resources/deployments','other'),'2025-04-01').outputs.id.value]"}}
	]}`)
}

func TestModuleOutputResolutionCases(t *testing.T) {
	read := func(cond string) map[string]any {
		m, err := armmodel.FromARM(outputReader(cond), armmodel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		rs := m.Resources()
		return rs[len(rs)-1].Properties
	}
	p := read("")
	// All-literal resourceId text is folded to the bare name like every other
	// property string, so a literal output arrives as the name.
	if p["a"] != "srch" {
		t.Errorf("a = %v, want the module output expression (folded literal)", p["a"])
	}
	for _, k := range []string{"b", "c", "d"} {
		if s, _ := p[k].(string); !strings.Contains(s, "reference(") {
			t.Errorf("%s = %v: secure, missing and unknown-module outputs must stay unresolved", k, p[k])
		}
	}
	for name, cond := range map[string]string{
		"conditional": `"condition":"[parameters('on')]",`,
		"looped":      `"copy":{"name":"loop","count":2},`,
	} {
		if s, _ := read(cond)["a"].(string); !strings.Contains(s, "reference(") {
			t.Errorf("%s module: output must stay unresolved, got %q", name, s)
		}
	}
}

// R3: two modules reading each other's outputs do not loop forever.
func TestModuleOutputCycleTerminates(t *testing.T) {
	mod := func(name, other string) string {
		return `{"type":"Microsoft.Resources/deployments","apiVersion":"2025-04-01","name":"` + name + `","properties":{"mode":"Incremental",
		 "expressionEvaluationOptions":{"scope":"inner"},
		 "parameters":{"p":{"value":"[reference(resourceId('Microsoft.Resources/deployments','` + other + `'),'2025-04-01').outputs.o.value]"}},
		 "template":{"parameters":{"p":{"type":"string"}},"resources":[{"type":"Microsoft.Search/searchServices","apiVersion":"2025-05-01","name":"[parameters('p')]","properties":{}}],
		  "outputs":{"o":{"type":"string","value":"[parameters('p')]"}}}}}`
	}
	m, err := armmodel.FromARM([]byte(`{"resources":[`+mod("a", "b")+`,`+mod("b", "a")+`]}`), armmodel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Resources()) != 2 {
		t.Fatalf("resources = %d", len(m.Resources()))
	}
}

// R7: a module's dependsOn applies to its flattened child resources, and a
// dependsOn on another module names that module's path.
func TestModuleDependsOnPropagatesToChildren(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	rs, ps := m.Resources(), m.Placements()
	found := false
	for i, r := range rs {
		if !strings.HasSuffix(strings.ToLower(r.Type), "/connections") {
			continue
		}
		found = true
		if len(ps[i].DependsOn) != 1 || !strings.Contains(ps[i].DependsOn[0], "Microsoft.CognitiveServices/accounts") {
			t.Errorf("connection should inherit the module's dependsOn on the account: %v", ps[i].DependsOn)
		}
	}
	if !found {
		t.Fatal("no connection resource")
	}
	am := rulestest.Scenario(t, "avm")
	for i, p := range am.Placements() {
		if strings.HasSuffix(strings.ToLower(am.Resources()[i].Type), "/connections") {
			if len(p.ModuleDeps) != 1 || p.ModuleDeps[0] != "s1" {
				t.Errorf("connection module depends on module s1: %v", p.ModuleDeps)
			}
		}
	}
}

// R5: the file of a resource inside a module comes from the deployment-name
// map, then from the template hash of the nested template.
func TestModuleFileMapping(t *testing.T) {
	raw := rulestest.ScenarioJSON(t, "twice")
	byName, err := armmodel.FromARM(raw, armmodel.Options{Entry: "main.bicep", Modules: map[string]string{"search-f": "modules/search.bicep", "conn": "modules/connection.bicep"}})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, r := range byName.Resources() {
		files = append(files, r.Location.File)
	}
	if got, want := strings.Join(files, ","), "main.bicep,modules/search.bicep,main.bicep,modules/connection.bicep"; got != want {
		t.Errorf("by name: %s, want %s", got, want)
	}
	// Both search modules have the same template hash, so one map entry names
	// the file of every instance.
	var hash string
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, r := range doc["resources"].([]any) {
		if mod, ok := r.(map[string]any); ok && mod["name"] == "search-f" {
			hash = bicepHash(mod)
		}
	}
	if hash == "" {
		t.Fatal("compiled module carries no templateHash")
	}
	byHash, err := armmodel.FromARM(raw, armmodel.Options{Entry: "main.bicep", ModuleHashes: map[string]string{hash: "modules/search.bicep"}})
	if err != nil {
		t.Fatal(err)
	}
	files = files[:0]
	for _, r := range byHash.Resources() {
		files = append(files, r.Location.File)
	}
	if got, want := strings.Join(files, ","), "main.bicep,modules/search.bicep,modules/search.bicep,main.bicep"; got != want {
		t.Errorf("by hash: %s, want %s", got, want)
	}
}

func bicepHash(mod map[string]any) string {
	props, _ := mod["properties"].(map[string]any)
	tpl, _ := props["template"].(map[string]any)
	meta, _ := tpl["metadata"].(map[string]any)
	gen, _ := meta["_generator"].(map[string]any)
	h, _ := gen["templateHash"].(string)
	return h
}

// R4: a resource scope is kept as the unfolded resourceId expression.
func TestResourceScopeIsNotFolded(t *testing.T) {
	m := rulestest.Golden(t, "idnscope")
	n := 0
	for _, r := range m.Resources() {
		if r.Scope == "" {
			continue
		}
		n++
		if r.Scope != "[resourceId('Microsoft.CognitiveServices/accounts', 'aiservices')]" {
			t.Errorf("scope = %q", r.Scope)
		}
	}
	if n != 3 {
		t.Errorf("want 3 scoped role assignments, got %d", n)
	}
}

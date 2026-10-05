package bicep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "test", "fixtures", "bicep"}, parts...)...)
}

func readFixture(t testing.TB, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func normaliseFixture(t *testing.T, opts Options, parts ...string) *Normalised {
	t.Helper()
	opts.File = "main.bicep"
	n, err := Normalise(readFixture(t, parts...), opts)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// byName returns the resource and info with the given ARM name (as read, after substitution).
func byName(t *testing.T, n *Normalised, name string) (model.Resource, ResourceInfo) {
	t.Helper()
	for i, r := range n.Template.Resources {
		if r.Name == name {
			return r, n.Info[i]
		}
	}
	t.Fatalf("resource %q not found", name)
	return model.Resource{}, ResourceInfo{}
}

func TestNormaliseARM_ArrayForm(t *testing.T) {
	n := normaliseFixture(t, Options{}, "spike", "main.arm.json")
	if got := len(n.Template.Resources); got != 7 {
		t.Fatalf("resources = %d, want 7 (5 root + 2 module bodies)", got)
	}
	if len(n.Info) != len(n.Template.Resources) {
		t.Fatal("Info is not parallel to Resources")
	}
	r0, i0 := n.Template.Resources[0], n.Info[0]
	if r0.Symbolic != "" || i0.Pointer != "/resources/0" || !i0.Conditional || i0.Looped {
		t.Errorf("resource 0: %+v %+v", r0, i0)
	}
	if i0.ConditionValue.State != model.Literal {
		t.Errorf("deployExtra defaults to true, condition = %+v", i0.ConditionValue)
	}
	if v, _ := i0.ConditionValue.Bool(); !v {
		t.Errorf("condition literal = %v", i0.ConditionValue.Lit)
	}
	if i1 := n.Info[1]; !i1.Looped || i1.CopyName != "accts" {
		t.Errorf("looped resource: %+v", i1)
	}
	if !n.Info[3].Module || n.Template.Resources[3].Name != "single" {
		t.Errorf("module single: %+v", n.Info[3])
	}
	nested := n.Info[6] // body of the looped, conditional module
	if len(nested.ModuleChain) != 1 || !nested.MayNotDeploy() || !nested.MultiInstance() {
		t.Errorf("module body flags: %+v", nested)
	}
	if nested.Confidence != sdk.ConfidenceUncertain || n.Info[0].Confidence != sdk.ConfidenceLikely {
		t.Errorf("confidence: nested %q root %q", nested.Confidence, n.Info[0].Confidence)
	}
	if n.Template.Resources[3].Body["properties"].(map[string]any)["template"] != nil {
		t.Error("inline template must not be duplicated in the module body")
	}
	if len(n.Template.Outputs) != 2 || n.Template.Outputs[0].Name != "extraId" {
		t.Errorf("outputs = %+v", n.Template.Outputs)
	}
	if got := strings.Join(n.Template.Parameters, ","); got != "accountNames,adminPassword,deployExtra,location" {
		t.Errorf("parameters = %s", got)
	}
	if i, ok := n.Find("/resources/3"); !ok || i != 3 {
		t.Error("Find failed")
	}
}

func TestNormaliseARM_Language2Form(t *testing.T) {
	n := normaliseFixture(t, Options{}, "foundry-v2", "foundry.arm.json")
	r, in := byName(t, n, "fdacctliteral")
	if r.Symbolic != "acctLiteral" || in.Pointer != "/resources/acctLiteral" {
		t.Errorf("symbolic form: %+v", in)
	}
	var dep, project ResourceInfo
	for i, x := range n.Template.Resources {
		switch x.Symbolic {
		case "acctLiteral::modelDeployments":
			dep = n.Info[i]
		case "acctLiteral::project":
			project = n.Info[i]
			if len(x.DependsOn) != 2 {
				t.Errorf("project dependsOn = %v", x.DependsOn)
			}
		}
	}
	if !dep.Looped || dep.Parent != "acctLiteral" || dep.CopyName != "acctLiteral::modelDeployments" {
		t.Errorf("deployments: %+v", dep)
	}
	if project.Parent != "acctLiteral" || project.Looped {
		t.Errorf("project: %+v", project)
	}
	if _, in := byName(t, n, "privatelink.cognitiveservices.azure.com"); len(in.ModuleChain) != 1 ||
		in.ModuleChain[0].Label != "peDns" || !in.ModuleChain[0].Conditional ||
		in.Pointer != "/resources/peDns/properties/template/resources/zone" {
		t.Errorf("zone in module: %+v", in)
	}
}

func TestNormaliseARM_DisableLocalAuth(t *testing.T) {
	for _, form := range []string{"foundry", "foundry-v2"} {
		t.Run(form, func(t *testing.T) {
			path := []string{form, "foundry.arm.json"}
			tests := []struct {
				name     string
				supplied map[string]any
				want     model.ValueState
				wantBool bool
				source   string
				reason   string
			}{
				{"fdacctliteral", nil, model.Literal, true, "", ""},
				{"fdacctdefault", nil, model.Literal, true, SourceDefault, ""},
				{"fdacctdefault", map[string]any{"disableAuthWithDefault": false}, model.Literal, false, SourceSupplied, ""},
				{"fdacctnodefault", nil, model.Unresolved, false, SourceUnresolved, "no-default"},
				{"fdacctnodefault", map[string]any{"DisableAuthNoDefault": true}, model.Literal, true, SourceSupplied, ""},
				{"fdacctabsent", nil, model.Absent, false, "", ""},
			}
			for _, tt := range tests {
				n := normaliseFixture(t, Options{ParameterValues: tt.supplied}, path...)
				r, in := byName(t, n, tt.name)
				v := r.Get("properties.disableLocalAuth")
				if v.State != tt.want {
					t.Fatalf("%s %v: state = %v, want %v", tt.name, tt.supplied, v.State, tt.want)
				}
				if b, _ := v.Bool(); tt.want == model.Literal && b != tt.wantBool {
					t.Errorf("%s %v: value = %v", tt.name, tt.supplied, v.Lit)
				}
				var ref *ParamRef
				for i := range in.ParamRefs {
					if in.ParamRefs[i].Path == "properties.disableLocalAuth" {
						ref = &in.ParamRefs[i]
					}
				}
				switch {
				case tt.source == "" && ref != nil:
					t.Errorf("%s: unexpected ParamRef %+v", tt.name, ref)
				case tt.source != "" && (ref == nil || ref.Source != tt.source || ref.Reason != tt.reason):
					t.Errorf("%s: ParamRef = %+v, want source %s reason %q", tt.name, ref, tt.source, tt.reason)
				}
			}
		})
	}
}

func TestNormaliseARM_LoopedDeploymentsAndConditionalEndpoint(t *testing.T) {
	n := normaliseFixture(t, Options{}, "foundry", "foundry.arm.json")
	deps := n.Template.ResourcesOfType("microsoft.cognitiveservices/accounts/deployments")
	if len(deps) != 1 {
		t.Fatalf("deployments = %d", len(deps))
	}
	if i, _ := n.Find("/resources/0"); !n.Info[i].Looped || n.Info[i].CopyName != "acctLiteral::modelDeployments" {
		t.Errorf("array form copy name: %+v", n.Info[i])
	}
	pe := n.Template.ResourcesOfType("Microsoft.Network/privateEndpoints")
	if len(pe) != 1 {
		t.Fatalf("private endpoints = %d", len(pe))
	}
	_, in := byName(t, n, "fdacctliteral-private-endpoint")
	if !in.Conditional || in.Condition != "[parameters('createPrivateEndpoint')]" {
		t.Errorf("endpoint: %+v", in)
	}
	if b, ok := in.ConditionValue.Bool(); !ok || b {
		t.Errorf("createPrivateEndpoint defaults to false: %+v", in.ConditionValue)
	}
	n = normaliseFixture(t, Options{ParameterValues: map[string]any{"createPrivateEndpoint": true}}, "foundry", "foundry.arm.json")
	if _, in = byName(t, n, "fdacctliteral-private-endpoint"); !in.ConditionValue.IsLiteral() || in.ConditionValue.Lit != true {
		t.Errorf("supplied condition: %+v", in.ConditionValue)
	}
	// Properties that depend on a parameter without a value stay unresolved, not guessed.
	if v := pe[0].Get("properties.subnet.id"); v.State != model.Literal {
		// peSubnetId has the literal default ''
		t.Errorf("subnet id = %+v", v)
	}
}

func TestNormaliseARM_SyntheticMixedForms(t *testing.T) {
	n := normaliseFixture(t, Options{}, "synthetic", "main.arm.json")
	if got := len(n.Template.Resources); got != 25 {
		t.Errorf("resources = %d, want 25", got)
	}
	maxChain, conditional := 0, 0
	for _, in := range n.Info {
		maxChain = max(maxChain, len(in.ModuleChain))
		if in.MayNotDeploy() {
			conditional++
		}
	}
	if maxChain != 3 {
		t.Errorf("deepest module chain = %d, want 3", maxChain)
	}
	if conditional == 0 {
		t.Error("expected conditional resources")
	}
	acct := n.Template.ResourcesOfType("Microsoft.CognitiveServices/accounts")
	if len(acct) != 1 {
		t.Fatalf("accounts = %d", len(acct))
	}
	if v := acct[0].Get("properties.disableLocalAuth"); !v.IsLiteral() || v.Lit != true {
		t.Errorf("disableLocalAuth = %+v", v)
	}
	// A ternary over a parameter is an ARM expression: never evaluated.
	if v := acct[0].Get("properties.publicNetworkAccess"); v.State != model.Unresolved {
		t.Errorf("publicNetworkAccess = %+v", v)
	}
	if v := acct[0].Get("properties.networkAcls.defaultAction"); v.State != model.Unresolved {
		t.Errorf("defaultAction = %+v", v)
	}
	if len(n.Warnings) != 0 || n.Truncated {
		t.Errorf("warnings = %v truncated = %v", n.Warnings, n.Truncated)
	}
}

func TestNormaliseARM_Synthetic_CopyIsLooped(t *testing.T) {
	n := normaliseFixture(t, Options{}, "synthetic", "main.arm.json")
	deps := n.Template.ResourcesOfType("Microsoft.CognitiveServices/accounts/deployments")
	if len(deps) != 1 {
		t.Fatalf("deployments = %d", len(deps))
	}
	i, ok := n.Find("/resources/resources/properties/template/resources/foundryAccount::modelDeployments")
	if !ok || !n.Info[i].Looped || n.Info[i].Parent != "foundryAccount" {
		t.Errorf("modelDeployments: %+v", n.Info[i])
	}
}

func TestNormaliseARM_Inline(t *testing.T) {
	const doc = `{
	  "parameters": {
	    "secureP": {"type": "securestring", "defaultValue": "x"},
	    "exprP": {"type": "string", "defaultValue": "[newGuid()]"},
	    "objP": {"type": "object", "defaultValue": {"a": 1}},
	    "refP": {"$ref": "#/definitions/sec", "defaultValue": "y"},
	    "kvP": {"type": "string"},
	    "flag": {"type": "bool", "defaultValue": false}
	  },
	  "definitions": {"sec": {"type": "securestring"}},
	  "outputs": {"k": {"type": "securestring", "value": "[x]"}, "o": {"type": "string", "value": "v"}},
	  "resources": [
	    {"type": "T/a", "apiVersion": "1", "name": "n", "existing": true},
	    {"type": "T/b", "apiVersion": "1", "name": "[parameters('exprP')]",
	     "condition": "[parameters('flag')]",
	     "properties": {"s": "[parameters('secureP')]", "e": "[parameters('exprP')]", "o": "[parameters('OBJP')]",
	       "r": "[parameters('refP')]", "u": "[parameters('missing')]", "mix": "[concat(parameters('flag'))]",
	       "list": ["[parameters('flag')]"]},
	    "resources": [{"type": "T/b/c", "name": "child"}]},
	    {"type": "microsoft.resources/deployments", "name": "tl", "properties": {"templateLink": {"uri": "x"}}},
	    {"type": "Microsoft.Resources/deployments", "name": "outer", "properties": {
	      "parameters": {"p": {"value": "[parameters('kvP')]"}, "q": {"reference": {"keyVault": {}}}},
	      "template": {"parameters": {"p": {"type":"string"}, "q": {"type":"string"}},
	        "resources": [{"type": "T/d", "name": "d", "properties": {"x": "[parameters('flag')]"}}]}}}
	  ],
	  "ignored": 1
	}`
	n, err := Normalise([]byte(doc), Options{File: "m.bicep", ParameterValues: map[string]any{"kvP": "[bad"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Existing) != 1 || n.Existing[0] != "/resources/0" {
		t.Errorf("existing = %v", n.Existing)
	}
	var b model.Resource
	var bi ResourceInfo
	for i, r := range n.Template.Resources {
		if r.Type == "T/b" {
			b, bi = r, n.Info[i]
		}
	}
	reasons := map[string]string{}
	for _, r := range bi.ParamRefs {
		reasons[r.Path] = r.Source + ":" + r.Reason
	}
	want := map[string]string{
		"properties.s":       "unresolved:secure-parameter",
		"properties.e":       "unresolved:default-is-expression",
		"properties.o":       "default:",
		"properties.r":       "unresolved:secure-parameter",
		"properties.u":       "unresolved:unknown-parameter",
		"properties.list[0]": "default:",
		"name":               "unresolved:default-is-expression",
	}
	for k, v := range want {
		if reasons[k] != v {
			t.Errorf("ParamRef %s = %q, want %q", k, reasons[k], v)
		}
	}
	if v := b.Get("properties.o.a"); v.Lit != float64(1) {
		t.Errorf("object default: %+v", v)
	}
	if v := b.Get("properties.mix"); v.State != model.Unresolved {
		t.Errorf("mixed expression: %+v", v)
	}
	if v := b.Get("properties.s"); v.State != model.Unresolved {
		t.Errorf("secure default must not be exposed: %+v", v)
	}
	if bi.ConditionValue.Lit != false || !bi.ConditionValue.IsLiteral() {
		t.Errorf("condition: %+v", bi.ConditionValue)
	}
	if _, ok := b.Body["resources"]; ok {
		t.Error("legacy child array must not stay in the body")
	}
	var sawChild, sawTL bool
	for _, r := range n.Template.Resources {
		sawChild = sawChild || r.Name == "child"
	}
	for _, w := range n.Warnings {
		sawTL = sawTL || strings.Contains(w, "templateLink")
	}
	if !sawChild || !sawTL {
		t.Errorf("child %v templateLink warning %v (%v)", sawChild, sawTL, n.Warnings)
	}
	// Outer evaluation scope (absent options): the nested template reads the caller's scope.
	d, di := byName(t, n, "d")
	if v := d.Get("properties.x"); v.State != model.Literal || v.Lit != false || len(di.ModuleChain) != 1 {
		t.Errorf("outer scope nested resource: %+v %+v", v, di)
	}
	if o := n.Template.Outputs; len(o) != 2 || !o[0].Secure || o[1].Secure {
		t.Errorf("outputs = %+v", o)
	}
	for _, p := range n.Parameters {
		if p.Name == "secureP" && (!p.Secure || p.Value.State != model.Unresolved) {
			t.Errorf("secure parameter info = %+v", p)
		}
		if p.Name == "refP" && !p.Secure {
			t.Error("$ref to a secure type must be secure")
		}
		if p.Name == "kvP" && p.Source != SourceSupplied {
			t.Errorf("bad supplied value kvP = %+v", p)
		}
	}
}

func TestNormaliseARM_InnerScopeMapping(t *testing.T) {
	const doc = `{
	  "parameters": {"top": {"type":"string","defaultValue":"T"}, "noval": {"type":"string"}},
	  "resources": {"m": {"type":"Microsoft.Resources/deployments","name":"m","properties":{
	    "expressionEvaluationOptions":{"scope":"Inner"},
	    "parameters":{"a":{"value":"[parameters('top')]"},"b":{"value":"[parameters('noval')]"},
	                  "c":{"value":"[concat('x')]"},"d":{"value":"lit"},"e":{"value":"[parameters('ghost')]"},"skip":{}},
	    "template":{"parameters":{"a":{"type":"string"},"b":{"type":"string"},"c":{"type":"string"},
	                              "d":{"type":"string"},"e":{"type":"string"},"f":{"type":"string","defaultValue":"F"},"g":{"type":"string"}},
	      "resources":{"r":{"type":"T/r","name":"r","properties":{"a":"[parameters('a')]","b":"[parameters('b')]",
	        "c":"[parameters('c')]","d":"[parameters('d')]","e":"[parameters('e')]","f":"[parameters('f')]","g":"[parameters('g')]"}}}}}}}
	}`
	n, err := Normalise([]byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, in := byName(t, n, "r")
	got := map[string]string{}
	for _, ref := range in.ParamRefs {
		got[ref.Path] = ref.Source + ":" + ref.Reason
	}
	want := map[string]string{
		"properties.a": "supplied:", "properties.b": "unresolved:no-default", "properties.c": "unresolved:arm-expression",
		"properties.d": "supplied:", "properties.e": "unresolved:arm-expression", "properties.f": "default:",
		"properties.g": "unresolved:no-default",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if s, _ := r.Get("properties.a").String(); s != "T" {
		t.Errorf("a = %v", r.Get("properties.a"))
	}
}

func TestNormaliseARM_Limits(t *testing.T) {
	const doc = `{"resources":[{"type":"A","name":"1"},{"type":"A","name":"2"},{"type":"A","name":"3"}]}`
	n, err := Normalise([]byte(doc), Options{MaxResources: 2})
	if err != nil || !n.Truncated || len(n.Template.Resources) != 2 || len(n.Warnings) != 1 {
		t.Errorf("limit: %v %+v", err, n)
	}
	const deep = `{"resources":[{"type":"Microsoft.Resources/deployments","name":"a","properties":{"template":{"resources":[
	  {"type":"Microsoft.Resources/deployments","name":"b","properties":{"template":{"resources":[{"type":"X","name":"x"}]}}}]}}}]}`
	n, err = Normalise([]byte(deep), Options{MaxDepth: 1})
	if err != nil || len(n.Template.Resources) != 2 || len(n.Warnings) != 1 {
		t.Errorf("depth: %v %+v", err, n)
	}
}

func TestNormaliseARM_Errors(t *testing.T) {
	for _, in := range []string{``, `[]`, `"x"`, `null`, `{`} {
		if _, err := Normalise([]byte(in), Options{}); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
	n, err := Normalise([]byte(`{}`), Options{})
	if err != nil || len(n.Template.Resources) != 0 {
		t.Errorf("empty object: %v %+v", err, n)
	}
	var nilN *Normalised
	if _, ok := nilN.Find("/x"); ok {
		t.Error("nil Find")
	}
}

func TestEscapePointer(t *testing.T) {
	if got := escapePointer("a/b~c"); got != "a~1b~0c" {
		t.Errorf("got %s", got)
	}
}

// TestNormaliseARM_ResourceMetaMatchesInfo checks that the model resources carry the same
// identity and flags as the parallel Info slice, so rules do not need the bicep package.
func TestNormaliseARM_ResourceMetaMatchesInfo(t *testing.T) {
	for _, form := range []string{"foundry", "foundry-v2"} {
		n := normaliseFixture(t, Options{}, form, "foundry.arm.json")
		if len(n.Template.Resources) != len(n.Info) || len(n.Info) == 0 {
			t.Fatalf("%s: resources=%d info=%d", form, len(n.Template.Resources), len(n.Info))
		}
		for i, in := range n.Info {
			m := n.Template.Resources[i].Meta
			if m.Pointer != in.Pointer || m.Looped != in.Looped || m.Conditional != in.Conditional ||
				m.MayNotDeploy != in.MayNotDeploy() || m.MultiInstance != in.MultiInstance() ||
				len(m.ModuleChain) != len(in.ModuleChain) || m.Confidence != string(in.Confidence) {
				t.Errorf("%s resource %d: meta %+v does not match info %+v", form, i, m, in)
			}
		}
	}
}

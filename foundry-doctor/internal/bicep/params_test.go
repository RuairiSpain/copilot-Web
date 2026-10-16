package bicep

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandAzd(t *testing.T) {
	env := map[string]string{"AZURE_LOCATION": "swedencentral", "AZURE_ENV_NAME": "dev", "EMPTY": ""}
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"plain", "plain", true},
		{"${AZURE_LOCATION}", "swedencentral", true},
		{"rg-${AZURE_ENV_NAME}-${AZURE_LOCATION}", "rg-dev-swedencentral", true},
		{"${MISSING}", "", false},
		{"${EMPTY}", "", false},
		{"${MISSING=eastus}", "eastus", true},
		{"${MISSING:=eastus}", "eastus", true},
		{"${MISSING-eastus}", "eastus", true},
		{"${MISSING:-eastus}", "eastus", true},
		{"${AZURE_LOCATION=eastus}", "swedencentral", true},
		{"${EMPTY=eastus}", "", true},
		{"${EMPTY:-eastus}", "eastus", true},
		{"${EMPTY:=eastus}", "eastus", true},
		{"${unterminated", "", false},
		{"${bad name}", "", false},
		{"${}", "", false},
		{"${1BAD}", "", false},
		{"a${MISSING}b", "", false},
	}
	f := ExpandAzd(env, true)
	for _, c := range cases {
		got, ok := f(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Expand(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	// Without a readable environment nothing that substitutes is a fact,
	// defaults included.
	g := ExpandAzd(nil, false)
	for _, in := range []string{"${AZURE_LOCATION}", "${X=eastus}"} {
		if got, ok := g(in); ok {
			t.Errorf("unknown environment: %q resolved to %q", in, got)
		}
	}
	if got, ok := g("literal"); !ok || got != "literal" {
		t.Errorf("literal without environment: %q %v", got, ok)
	}
}

func TestParseParametersJSON(t *testing.T) {
	data := `{
	  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#",
	  "contentVersion": "1.0.0.0",
	  "parameters": {
	    "location": {"value": "${AZURE_LOCATION}"},
	    "unset": {"value": "${NOT_IN_ENV}"},
	    "withDefault": {"value": "${NOT_IN_ENV=eastus}"},
	    "flag": {"value": true},
	    "count": {"value": 3},
	    "obj": {"value": {"a": 1}},
	    "arr": {"value": [1]},
	    "nul": {"value": null},
	    "kv": {"reference": {"keyVault": {"id": "x"}, "secretName": "s"}},
	    "bad": 7
	  }
	}`
	set, err := ParseParametersJSON([]byte(data), ExpandAzd(map[string]string{"AZURE_LOCATION": "swedencentral"}, true))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"location": "swedencentral", "withDefault": "eastus", "flag": true, "count": json.Number("3"),
		"unset": nil, "obj": nil, "arr": nil, "nul": nil, "kv": nil, "bad": nil,
	}
	if len(set.Values) != len(want) {
		t.Fatalf("values %v", set.Values)
	}
	for k, w := range want {
		if got, ok := set.Values[k]; !ok || got != w {
			t.Errorf("%s = %#v (set %v), want %#v", k, got, ok, w)
		}
	}
	if len(set.Unresolved) != 1 || set.Unresolved[0] != "unset" {
		t.Errorf("Unresolved %v", set.Unresolved)
	}
}

func TestParseParametersJSONLiteralWithoutExpand(t *testing.T) {
	set, err := ParseParametersJSON([]byte(`{"parameters":{"x":{"value":"${A}"}}}`), nil)
	if err != nil || set.Values["x"] != "${A}" {
		t.Fatalf("%v %v", set, err)
	}
}

func TestParseParametersJSONNamedFailures(t *testing.T) {
	big := `{"parameters":{"x":{"value":"` + strings.Repeat("a", MaxParameterFileBytes) + `"}}}`
	var many strings.Builder
	many.WriteString(`{"parameters":{`)
	for i := 0; i <= MaxParameters; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`"p` + strings.Repeat("x", 3) + itoa(i) + `":{"value":1}`)
	}
	many.WriteString(`}}`)
	cases := []struct {
		name, in, reason string
	}{
		{"empty", ``, ParamMalformed},
		{"not json", `{not json`, ParamMalformed},
		{"truncated", `{"parameters":{"x":`, ParamMalformed},
		{"array", `[1,2]`, ParamMalformed},
		{"trailing", `{"parameters":{}} {"x":1}`, ParamMalformed},
		{"no parameters", `{"contentVersion":"1.0.0.0"}`, ParamMalformed},
		{"oversize", big, ParamTooLarge},
		{"too many", many.String(), ParamTooMany},
	}
	for _, c := range cases {
		_, err := ParseParametersJSON([]byte(c.in), nil)
		if ReasonOf(err) != c.reason {
			t.Errorf("%s: reason %q (%v), want %q", c.name, ReasonOf(err), err, c.reason)
		}
	}
	if ReasonOf(errors.New("x")) != "" || ReasonOf(nil) != "" {
		t.Error("ReasonOf on a foreign error")
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestBuildParams(t *testing.T) {
	ok := `{"parametersJson":"{\"parameters\":{\"deploySearch\":{\"value\":true},\"prefix\":{\"value\":\"dev\"}}}","templateJson":"{}"}`
	run := func(res RunResult, err error) (ParamSet, *fakeRunner, error) {
		fr := &fakeRunner{res: res, err: err}
		c := Compiler{Tool: Tool{Path: "bicep"}, Runner: fr}
		set, e := c.BuildParams(context.Background(), "/p/infra/main.bicepparam")
		return set, fr, e
	}
	set, fr, err := run(RunResult{Stdout: []byte(ok)}, nil)
	if err != nil || set.Values["deploySearch"] != true || set.Values["prefix"] != "dev" {
		t.Fatalf("%v %v", set, err)
	}
	if got := strings.Join(fr.calls[0], " "); got != "build-params main.bicepparam --stdout --no-restore" || fr.dir != filepath.FromSlash("/p/infra") {
		t.Errorf("args %q dir %q", got, fr.dir)
	}
	for name, c := range map[string]struct {
		res    RunResult
		err    error
		reason string
	}{
		"exit":      {RunResult{ExitCode: 1, Stdout: []byte(ok)}, nil, ParamBicepparamFailed},
		"garbage":   {RunResult{Stdout: []byte("not json")}, nil, ParamBicepparamFailed},
		"no params": {RunResult{Stdout: []byte(`{"templateJson":"{}"}`)}, nil, ParamBicepparamFailed},
		"truncated": {RunResult{Stdout: []byte(ok), Truncated: true}, nil, ParamTooLarge},
		"start":     {RunResult{}, errors.New("exec: not found"), ParamBicepparamFailed},
		"bad json":  {RunResult{Stdout: []byte(`{"parametersJson":"{nope"}`)}, nil, ParamMalformed},
	} {
		_, _, err := run(c.res, c.err)
		if ReasonOf(err) != c.reason {
			t.Errorf("%s: reason %q (%v), want %q", name, ReasonOf(err), err, c.reason)
		}
	}
	if _, err := (Compiler{}).BuildParams(context.Background(), "x.bicepparam"); err == nil {
		t.Error("no tool must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fr2 := &fakeRunner{err: ctx.Err()}
	if _, err := (Compiler{Tool: Tool{Path: "bicep"}, Runner: fr2}).BuildParams(ctx, "x.bicepparam"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

func TestTemplateRetainsVariablesAndLiteralDefaults(t *testing.T) {
	tpl := `{
	  "parameters": {
	    "a": {"type":"string","defaultValue":"x"},
	    "b": {"type":"bool","defaultValue":false},
	    "c": {"type":"int","defaultValue":-2},
	    "d": {"type":"object","defaultValue":{}},
	    "e": {"type":"securestring","defaultValue":"hidden"},
	    "f": {"type":"string"}
	  },
	  "variables": {"v1":"lit","v2":"[concat('a','b')]","v3":{"k":1}},
	  "resources": []
	}`
	got, err := ParseARM([]byte(tpl))
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]string{}
	for _, p := range got.Parameters {
		defs[p.Name] = string(p.Default)
	}
	for k, w := range map[string]string{"a": `"x"`, "b": "false", "c": "-2", "d": "", "e": "", "f": ""} {
		if defs[k] != w {
			t.Errorf("default of %s = %q, want %q", k, defs[k], w)
		}
	}
	if len(got.Variables) != 3 || got.Variables[0].Name != "v1" || string(got.Variables[1].Value) != `"[concat('a','b')]"` {
		t.Errorf("variables %+v", got.Variables)
	}
}

func TestVariablesBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"variables":{`)
	for i := 0; i < MaxVariables+50; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"v` + itoa(1000+i) + `":1`)
	}
	b.WriteString(`,"huge":"` + strings.Repeat("z", MaxVariableBytes+1) + `"},"resources":[]}`)
	got, err := ParseARM([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Variables) != MaxVariables {
		t.Errorf("kept %d variables, want %d", len(got.Variables), MaxVariables)
	}
	for _, v := range got.Variables {
		if v.Name == "huge" {
			t.Error("oversize variable kept")
		}
	}
}

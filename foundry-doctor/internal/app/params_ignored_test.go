package app

import (
	"fmt"
	"strings"
	"testing"
)

// P7: values the model ignores from a parameter file are named once, without
// their values.
func TestIgnoredParameterValuesAreWarnedByName(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{
	  "prefix": {"value": "demo"},
	  "deploySearch": {"value": "not-a-bool-SENTINELVALUE"},
	  "publicNetworkAccess": {"value": "[concat('a','b')]"},
	  "capacity": {"value": "many"}
	}}`)
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if n := strings.Count(got.log, "were ignored"); n != 1 {
		t.Fatalf("want exactly one ignored-values warning, got %d: %q", n, got.log)
	}
	for _, name := range []string{"capacity", "deploySearch", "publicNetworkAccess"} {
		if !strings.Contains(got.log, name) {
			t.Errorf("warning lacks %s: %q", name, got.log)
		}
	}
	if strings.Contains(got.log, "SENTINELVALUE") || strings.Contains(got.log, "concat(") {
		t.Errorf("values must not be logged: %q", got.log)
	}
	if strings.Contains(got.log, "prefix") {
		t.Errorf("a used parameter must not be named: %q", got.log)
	}
}

func TestNoIgnoredParametersNoWarning(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.parameters.json", `{"parameters":{"prefix":{"value":"demo"}}}`)
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if strings.Contains(got.log, "ignored") {
		t.Errorf("unexpected warning: %q", got.log)
	}
}

func TestIgnoredNoteCapsNamesAndSkipsUnresolvedVars(t *testing.T) {
	var names []string
	for i := 0; i < 25; i++ {
		names = append(names, fmt.Sprintf("p%02d", i))
	}
	p := paramResult{file: "infra/main.parameters.json", unresolved: []string{"p00"}}
	note := ignoredNote(p, names)
	if !strings.Contains(note, "and 4 more") || strings.Contains(note, "p00") || strings.Contains(note, "p24") {
		t.Errorf("cap and unresolved handling: %q", note)
	}
	if ignoredNote(p, []string{"p00"}) != "" {
		t.Error("a parameter already reported as unresolved is not repeated")
	}
}

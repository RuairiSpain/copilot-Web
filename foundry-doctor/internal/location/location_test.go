package location

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestSanitizePath(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"a/b.yaml":               "a/b.yaml",
		"./a//b":                 "a/b",
		"/etc/passwd":            "passwd",
		"C:\\x\\y.bicep":         "y.bicep",
		"..\\..\\z":              "z",
		"a/../../b":              "b",
		"\\\\srv\\share\\f.json": "f.json",
		"~/.ssh/id":              "id",
		"a\x00b/c\n":             "ab/c",
		"..":                     "",
	}
	for in, want := range cases {
		got := SanitizePath(in)
		if got != want {
			t.Errorf("SanitizePath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRegionPrecision(t *testing.T) {
	if (Region{}).Precise() {
		t.Fatal("empty region should not be precise")
	}
	if !(Region{File: "azure.yaml", Line: 2}).Precise() {
		t.Fatal("line-based region should be precise")
	}
}

func TestFromSources(t *testing.T) {
	if got := FromFinding(sdk.Finding{Location: sdk.Location{File: "C:\\repo\\azure.yaml", Line: 4, Column: 2}}); got.File != "azure.yaml" || got.Line != 4 || got.Column != 2 {
		t.Fatalf("finding region = %+v", got)
	}
	if got := FromDiagnostic(bicep.Diagnostic{File: "infra\\main.bicep", Line: 9, Column: 1}); got.File != "infra/main.bicep" || got.Line != 9 || got.Column != 1 {
		t.Fatalf("diagnostic region = %+v", got)
	}
}

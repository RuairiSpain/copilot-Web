package bicep

import (
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	src := []byte("param location string = 'westeurope'\nresource st 'Microsoft.Storage/storageAccounts@2023-05-01' = {\n  name: 'storagedemo'\n}\n")
	got, err := Apply(src, []Comment{{Line: 2, Text: "BCP057 error ref abc"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{Banner, "// Foundry Doctor: BCP057 error ref abc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("annotated bicep missing %q:\n%s", want, text)
		}
	}
}

func TestApplyRejectsOutOfRange(t *testing.T) {
	if _, err := Apply([]byte("param x string\n"), []Comment{{Line: 8, Text: "x"}}); err == nil {
		t.Fatal("want error")
	}
}

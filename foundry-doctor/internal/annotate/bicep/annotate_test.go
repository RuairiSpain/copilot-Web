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

func TestApplyBannerWithoutComments(t *testing.T) {
	got, err := Apply([]byte("\uFEFFparam p string\r\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\uFEFF" + Banner + "\nparam p string\n"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApplyKeepsDisableNextLineAdjacent(t *testing.T) {
	src := []byte("param a string\n#disable-next-line no-unused-params\nparam b string\n")
	got, err := Apply(src, []Comment{{Line: 3, Text: "BCP000 warning ref x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Banner + "\nparam a string\n// Foundry Doctor: BCP000 warning ref x\n#disable-next-line no-unused-params\nparam b string\n"
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

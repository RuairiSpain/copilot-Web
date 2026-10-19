package yaml

import (
	"strings"
	"testing"

	goyaml "go.yaml.in/yaml/v3"
)

func TestApply(t *testing.T) {
	src := []byte("name: demo\nservices:\n  api:\n    host: containerapp\n")
	got, err := Apply(src, []Comment{{Line: 1, Text: "FND-CFG-001 error ref abc"}, {Line: 4, Text: "FND-CFG-005 warning ref def"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{Banner, "# Foundry Doctor: FND-CFG-001 error ref abc", "    # Foundry Doctor: FND-CFG-005 warning ref def"} {
		if !strings.Contains(text, want) {
			t.Fatalf("annotated yaml missing %q:\n%s", want, text)
		}
	}
	var v any
	if err := goyaml.Unmarshal(got, &v); err != nil {
		t.Fatalf("annotated yaml must still parse: %v\n%s", err, text)
	}
}

func TestApplyRejectsOutOfRange(t *testing.T) {
	if _, err := Apply([]byte("a: b\n"), []Comment{{Line: 9, Text: "x"}}); err == nil {
		t.Fatal("want error")
	}
}

func TestApplyBannerWithoutCommentsKeepsBOMFirst(t *testing.T) {
	got, err := Apply([]byte("\uFEFFname: demo\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\uFEFF" + Banner + "\nname: demo\n"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
	var v map[string]any
	if err := goyaml.Unmarshal(got, &v); err != nil || v["name"] != "demo" {
		t.Fatalf("annotated yaml must keep its meaning: %v %v", v, err)
	}
}

// Phase 6 gate (lead B1): the YAML scanner counts NEL, LS, PS and lone CR as
// line breaks, so a finding's line number must be counted the same way.
func TestApplyCountsUnicodeAndLoneCRBreaks(t *testing.T) {
	for name, brk := range map[string]string{"NEL": "\u0085", "LS": "\u2028", "PS": "\u2029", "CR": "\r"} {
		src := "a: 1" + brk + "b: 2\n"
		got, err := Apply([]byte(src), []Comment{{Line: 2, Text: "x"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		eol := "\n"
		if brk == "\r" {
			eol = "\r" // inserted lines use the file's own first line ending
		}
		want := Banner + eol + "a: 1" + brk + "# Foundry Doctor: x" + eol + "b: 2\n"
		if string(got) != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
}

func TestApplyKeepsCRLF(t *testing.T) {
	got, err := Apply([]byte("a: 1\r\nb: 2\r\n"), []Comment{{Line: 2, Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Banner + "\r\na: 1\r\n# Foundry Doctor: x\r\nb: 2\r\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

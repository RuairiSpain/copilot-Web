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

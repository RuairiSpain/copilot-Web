package diff

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	if got := Unified("azure.yaml", []byte("a\n"), []byte("a\n")); got != nil {
		t.Fatalf("expected nil diff, got %q", got)
	}
	got := string(Unified("azure.yaml", []byte("a\n"), []byte("a\nb\n")))
	for _, want := range []string{"--- a/azure.yaml", "+++ b/azure.yaml", "@@ -1,1 +1,2 @@", "-a", "+a", "+b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "No newline") {
		t.Fatalf("unexpected no-newline marker:\n%s", got)
	}
}

func TestUnifiedEmptyBeforeAndMissingNewline(t *testing.T) {
	got := string(Unified("x.bicep", nil, []byte("// banner\n")))
	if !strings.Contains(got, "@@ -0,0 +1,1 @@") {
		t.Fatalf("empty before side must be -0,0:\n%s", got)
	}
	got = string(Unified("x.bicep", []byte("a"), []byte("// banner\na\n")))
	if !strings.Contains(got, "-a\n\\ No newline at end of file\n+// banner\n") {
		t.Fatalf("missing no-newline marker:\n%s", got)
	}
}

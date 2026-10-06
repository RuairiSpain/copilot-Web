package diff

import "testing"

func TestUnified(t *testing.T) {
	if got := Unified("azure.yaml", []byte("a\n"), []byte("a\n")); got != nil {
		t.Fatalf("expected nil diff, got %q", got)
	}
	got := string(Unified("azure.yaml", []byte("a\n"), []byte("a\nb\n")))
	for _, want := range []string{"--- a/azure.yaml", "+++ b/azure.yaml", "@@ -1,1 +1,2 @@", "-a", "+a", "+b"} {
		if !contains(got, want) {
			t.Fatalf("diff missing %q:\n%s", want, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (func() bool { return len(s) > 0 && (index(s, sub) >= 0) })()))
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

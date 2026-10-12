package runtime

import "testing"

func TestValidateDataPlaneName(t *testing.T) {
	for _, name := range []string{"acct", "svc-1", "a123", "abc-def"} {
		if err := ValidateDataPlaneName("test", name); err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"x.evil.com#", "..", "@", "svc:443", "Upper", "münchen", "-bad", ""} {
		if err := ValidateDataPlaneName("test", name); err == nil {
			t.Fatalf("hostile name %q accepted", name)
		}
	}
}

func TestHostMatchesSuffix(t *testing.T) {
	if !hostMatchesSuffix("svc.search.windows.net", ".search.windows.net") {
		t.Fatal("expected suffix match")
	}
	if hostMatchesSuffix("svc.search.windows.net.evil.com", ".search.windows.net") {
		t.Fatal("unexpected suffix match")
	}
	if hostMatchesSuffix("example.com", ".search.windows.net") {
		t.Fatal("unexpected host match")
	}
}

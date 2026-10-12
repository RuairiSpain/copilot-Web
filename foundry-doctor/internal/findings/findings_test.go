package findings

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestRedact(t *testing.T) {
	fake := strings.Repeat("x", 12) + "Zq9"
	tests := []struct {
		name, in string
		gone     string
	}{
		{"password kv", "password=" + fake, fake},
		{"json secret", `{"clientSecret": "` + fake + `"}`, fake},
		{"bearer", "Authorization: Bearer " + fake + fake, fake},
		{"sas sig", "https://a.blob.example/x?sv=1&sig=" + fake + "&se=2", fake},
		{"userinfo", "https://user:" + fake + "@host/path", fake},
		{"jwt", "eyJhbGciOiJI.eyJzdWIiOiIx.c2lnbmF0dXJl", "eyJhbGciOiJI"},
		{"github", "ghp_" + strings.Repeat("a", 20), strings.Repeat("a", 20)},
		{"storage key", strings.Repeat("A", 86) + "==", strings.Repeat("A", 86)},
		{"pem", "-----BEGIN PRIV" + "ATE KEY-----\n" + fake + "\n-----END PRIVATE KEY-----", fake},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)
			if strings.Contains(got, tt.gone) {
				t.Fatalf("secret leaked: %q", got)
			}
			if !strings.Contains(got, Placeholder) {
				t.Fatalf("no placeholder: %q", got)
			}
			if Redact(got) != got {
				t.Fatalf("not idempotent: %q", got)
			}
		})
	}
}

func TestRedactLeavesBenignText(t *testing.T) {
	for _, in := range []string{"", "publicNetworkAccess is Enabled", "services.web.host: containerapp"} {
		if got := Redact(in); got != in {
			t.Errorf("Redact(%q) = %q", in, got)
		}
	}
}

func TestFingerprintStable(t *testing.T) {
	base := sdk.Finding{RuleID: "FND-CFG-001", RuleVersion: 1, Resource: sdk.ResourceRef{Name: "web"}, Location: sdk.Location{File: "azure.yaml", Line: 3}}
	moved := base
	moved.Location.Line = 99
	moved.Evidence = "different"
	if Fingerprint(base) != Fingerprint(moved) {
		t.Error("fingerprint must ignore line and evidence")
	}
	other := base
	other.Resource.Name = "api"
	if Fingerprint(base) == Fingerprint(other) {
		t.Error("fingerprint must depend on resource")
	}
	if len(Fingerprint(base)) != 32 {
		t.Error("unexpected length")
	}
}

func TestSortDeterministic(t *testing.T) {
	fs := []sdk.Finding{
		{RuleID: "B", Location: sdk.Location{File: "b", Line: 1}},
		{RuleID: "A", Location: sdk.Location{File: "a", Line: 2}},
		{RuleID: "A", Location: sdk.Location{File: "a", Line: 1}},
	}
	Sort(fs)
	if fs[0].Location.Line != 1 || fs[1].Location.Line != 2 || fs[2].Location.File != "b" {
		t.Errorf("bad order: %+v", fs)
	}
}

func TestRedactFinding(t *testing.T) {
	f := RedactFinding(sdk.Finding{Evidence: "token=abcdefghijk", Fix: "pass" + "word: hunter2hunter2"})
	if strings.Contains(f.Evidence, "abcdefghijk") || strings.Contains(f.Fix, "hunter2") {
		t.Errorf("leak: %+v", f)
	}
}

func FuzzRedact(f *testing.F) {
	for _, s := range []string{"", "password=abc", "Bearer abcdefgh12345", "-----BEGIN PRIV" + "ATE KEY-----", "a://b:c@d", "eyJaaaaaaaa." + "eyJbbbbbbbb.cccc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		once := Redact(s)
		if Redact(once) != once {
			t.Fatalf("not idempotent for %q", s)
		}
	})
}

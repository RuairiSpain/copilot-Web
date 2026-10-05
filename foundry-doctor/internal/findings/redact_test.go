package findings

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// canary is the planted secret body. Secret-shaped strings are assembled at run time from
// fragments so no secret-looking literal sits in the source or in a fixture.
const canary = "CANARYb64ABCDEFGH1234567890abcdef"

// shapes returns planted secrets that embed canary. Every one must be fully masked.
func shapes() map[string]string {
	return map[string]string{
		"openai":           "s" + "k-" + canary,
		"openai-proj":      "s" + "k-proj-" + canary + "_x-y",
		"github-pat":       "gh" + "p_" + canary,
		"github-fine":      "github" + "_pat_" + canary,
		"aws":              "AK" + "IA" + "CANARY1234567890",
		"account-key":      "Account" + "Key=" + canary + "==",
		"sas-key":          "SharedAccess" + "Key=" + canary,
		"password":         "Pass" + "word=" + canary,
		"password-quoted":  `Pass` + `word="` + canary + ` with spaces"`,
		"bearer":           "Authorization: Bear" + "er " + canary,
		"bearer-bare":      "Bear" + "er " + canary,
		"authz-basic":      "authorization=Ba" + "sic " + canary,
		"url-userinfo":     "https://admin:" + canary + "@example.invalid/path",
		"url-token-user":   "https://" + canary + "@example.invalid/",
		"sas-sig":          "https://x.invalid/c?sv=2024&sig=" + canary + "&se=2030",
		"json-key":         `{"api` + `Key": "` + canary + `"}`,
		"env-key":          "AZURE_OPENAI_API_" + "KEY=" + canary,
		"yaml-secret":      "client_" + "secret: " + canary,
		"pem":              "-----BEGIN PRIVATE " + "KEY-----\n" + canary + "\n-----END PRIVATE " + "KEY-----",
		"pem-rsa":          "-----BEGIN RSA PRIVATE " + "KEY-----\r\n" + canary + "\r\n-----END RSA PRIVATE " + "KEY-----",
		"pem-unterminated": "-----BEGIN PRIVATE " + "KEY-----\n" + canary + "\n" + canary,
		"jwt":              "ey" + "Jhbc12." + canary + "." + canary,
		"aad-secret":       "abc8" + "Q~" + canary,
		"storage-key":      strings.Repeat("A", 40) + canary[:20] + strings.Repeat("b", 26) + "==",
	}
}

func TestRedactShapes(t *testing.T) {
	for name, secret := range shapes() {
		t.Run(name, func(t *testing.T) {
			in := "before " + secret + " after"
			got := Redact(in)
			if strings.Contains(got, "CANARY") || strings.Contains(got, "ABCDEFGH") {
				t.Fatalf("secret leaked: %q", got)
			}
			if len(got) > len(in) {
				t.Fatalf("output expanded: %d > %d", len(got), len(in))
			}
			if again := Redact(got); again != got {
				t.Fatalf("not idempotent: %q then %q", got, again)
			}
			if !strings.HasPrefix(got, "before ") {
				t.Fatalf("surrounding text damaged: %q", got)
			}
		})
	}
}

func TestRedactExact(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"plain text unchanged", "disableLocalAuth is false on account contoso", "disableLocalAuth is false on account contoso"},
		{"key name kept", "Account" + "Key=" + canary, "Account" + "Key=***"},
		{"short password masked without growing", "Pass" + "word=ab", "Pass" + "word=**"},
		{"url keeps host", "https://u:" + canary + "@h.invalid/x", "https://***@h.invalid/x"},
		{"sas keeps other params", "?sv=1&sig=" + canary + "&se=2", "?sv=1&sig=***&se=2"},
		{"crlf preserved", "a\r\nPass" + "word=" + canary + "\r\nb", "a\r\nPass" + "word=***\r\nb"},
		{"unicode preserved", "ключ 密钥 🔑 Pass" + "word=" + canary + " ✓", "ключ 密钥 🔑 Pass" + "word=*** ✓"},
		{"short key value not secret-shaped", "apiKey: abc", "apiKey: abc"},
		{"secret on next line", "Pass" + "word=\n" + canary, "Pass" + "word=\n***"},
		{"two secrets", "Pass" + "word=" + canary + ";Account" + "Key=" + canary, "Pass" + "word=***;Account" + "Key=***"},
		{"resource id kept", "/subscriptions/0000/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct", "/subscriptions/0000/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct"},
		{"prose bearer short", "bearer of bad news", "bearer of bad news"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Fatalf("Redact(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactSecretSplitAcrossLines(t *testing.T) {
	in := "Account" + "Key=\r\n   " + canary + "\nnext line"
	got := Redact(in)
	if strings.Contains(got, "CANARY") {
		t.Fatalf("leaked: %q", got)
	}
	if !strings.HasSuffix(got, "next line") {
		t.Fatalf("lost trailing line: %q", got)
	}
}

func TestRedactHugeInputs(t *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 6000) // about 160 KB
	in := filler + "Pass" + "word=" + canary + "\n" + filler
	got := Redact(in)
	if strings.Contains(got, "CANARY") {
		t.Fatal("secret leaked in huge input")
	}
	if len(got) > len(in) {
		t.Fatal("expanded")
	}
	// Unterminated PEM swallows the rest, linear time.
	pem := "-----BEGIN PRIVATE " + "KEY-----\n" + strings.Repeat("A", 1<<18)
	if got := Redact("x " + pem); got != "x ***" {
		t.Fatalf("unterminated PEM: got %d bytes", len(got))
	}
	// Pathological repetition must not blow up.
	_ = Redact(strings.Repeat("Pass"+"word=", 4000))
	_ = Redact(strings.Repeat("-----BEGIN PRIVATE "+"KEY-----", 2000))
}

func TestRedactInvalidUTF8(t *testing.T) {
	in := "\xff\xfe bad \xc3 Pass" + "word=" + canary + " \xe2\x82"
	got := Redact(in)
	if strings.Contains(got, "CANARY") {
		t.Fatalf("leaked: %q", got)
	}
	if !strings.HasPrefix(got, "\xff\xfe bad \xc3 ") || !strings.HasSuffix(got, " \xe2\x82") {
		t.Fatalf("invalid bytes not preserved: %q", got)
	}
	if utf8.ValidString(in) {
		t.Fatal("test input should be invalid UTF-8")
	}
}

func TestRedactorConcurrent(t *testing.T) {
	r := NewRedactor()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if got := r.Redact("Pass" + "word=" + canary); strings.Contains(got, "CANARY") {
					t.Error("leaked")
				}
			}
		}()
	}
	wg.Wait()
}

func TestSanitize(t *testing.T) {
	secret := "Pass" + "word=" + canary
	sup := &sdk.Suppression{Reason: "accepted " + secret, Expires: "2030-01-01"}
	f := sdk.Finding{
		RuleID:         "FD-TEST-001",
		Evidence:       "found " + secret,
		Recommendation: "rotate " + secret,
		Fix:            "set " + secret,
		Key:            "k " + secret,
		Resource:       sdk.ResourceRef{Name: secret, ID: secret, Pointer: "/p/" + secret},
		Location:       sdk.Location{File: "a/" + secret, Pointer: secret, Line: 7},
		Suppressed:     sup,
	}
	got := Sanitize(f)
	fields := map[string]string{
		"Evidence": got.Evidence, "Recommendation": got.Recommendation, "Fix": got.Fix, "Key": got.Key,
		"Resource.Name": got.Resource.Name, "Resource.ID": got.Resource.ID, "Resource.Pointer": got.Resource.Pointer,
		"Location.File": got.Location.File, "Location.Pointer": got.Location.Pointer, "Suppressed.Reason": got.Suppressed.Reason,
	}
	for name, v := range fields {
		if strings.Contains(v, "CANARY") {
			t.Errorf("%s leaked: %q", name, v)
		}
	}
	if got.RuleID != f.RuleID || got.Location.Line != 7 || got.Suppressed.Expires != "2030-01-01" {
		t.Error("non-text fields changed")
	}
	if !strings.Contains(sup.Reason, "CANARY") || !strings.Contains(f.Evidence, "CANARY") {
		t.Error("Sanitize mutated its input")
	}
	// Nil suppression and zero value are fine.
	if z := Sanitize(sdk.Finding{}); z.Suppressed != nil {
		t.Error("zero finding gained a suppression")
	}
}

func FuzzRedact(f *testing.F) {
	for i := range uint8(len(fuzzShapes())) {
		f.Add("", "", i)
	}
	f.Add("Pass"+"word=\"", "\"", uint8(2))
	f.Add("-----BEGIN PRIVATE "+"KEY-----", "", uint8(0))
	f.Add("\xff\xfe", "\r\n\x00", uint8(6))
	f.Add(strings.Repeat("é🔑", 50), "bearer ", uint8(3))
	f.Fuzz(func(t *testing.T, prefix, suffix string, shape uint8) {
		if strings.Contains(prefix+suffix, "CANARY") {
			t.Skip()
		}
		shapesList := fuzzShapes()
		secret := shapesList[int(shape)%len(shapesList)]
		in := prefix + "\n" + secret + "\n" + suffix
		got := Redact(in)
		if strings.Contains(got, "CANARY") || strings.Contains(got, "ABCDEFGH") {
			t.Fatalf("canary leaked\n in: %q\nout: %q", in, got)
		}
		if len(got) > len(in) {
			t.Fatalf("output expanded: %d > %d", len(got), len(in))
		}
		if again := Redact(got); again != got {
			t.Fatalf("not idempotent\n 1: %q\n 2: %q", got, again)
		}
		if Redact(in) != got {
			t.Fatal("not deterministic")
		}
	})
}

// fuzzShapes is shapes() as an ordered slice so the fuzz index is stable.
func fuzzShapes() []string {
	m := shapes()
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sortStrings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = m[n]
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

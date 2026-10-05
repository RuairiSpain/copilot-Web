package suppress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	fpA = "fp1:0123456789abcdef0123456789abcdef"
	fpB = "fp1:fedcba9876543210fedcba9876543210"
)

func finding(rule, fp, name string) sdk.Finding {
	return sdk.Finding{
		RuleID: rule, RuleVersion: 1, Severity: sdk.SeverityError, Fingerprint: fp,
		Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.Example/Widgets", Name: name},
	}
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func byFP(rule, fp, expires string) Suppression {
	return Suppression{RuleID: rule, Fingerprint: fp, Reason: "because", Owner: "me", Expires: expires}
}

func file(s ...Suppression) File {
	return File{Version: 1, Suppressions: s, Source: ".foundry-doctor/suppressions.yaml"}
}

const good = "version: 1\nsuppressions:\n  - ruleId: FND-A-001\n    fingerprint: " + fpA +
	"\n    reason: why\n    owner: me\n    expires: 2026-12-31\n"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"valid unquoted date", good, ""},
		{"valid quoted date", strings.Replace(good, "2026-12-31", "\"2026-12-31\"", 1), ""},
		{"valid CRLF", strings.ReplaceAll(good, "\n", "\r\n"), ""},
		{"valid selector", "version: 1\nsuppressions:\n  - ruleId: FND-A-001\n    resource: {type: Microsoft.X/y, name: n}\n    reason: r\n    owner: o\n    expires: 2026-12-31\n", ""},
		{"empty suppressions list", "version: 1\nsuppressions: []\n", ""},
		{"empty file", "", "empty"},
		{"unknown key", good + "    note: x\n", "note"},
		{"unknown top key", good + "extra: 1\n", "extra"},
		{"unknown version", strings.Replace(good, "version: 1", "version: 2", 1), "unsupported version 2"},
		{"multi document", good + "---\n" + good, "more than one"},
		{"missing reason", strings.Replace(good, "    reason: why\n", "", 1), "suppressions[0]: missing required field \"reason\""},
		{"blank reason", strings.Replace(good, "reason: why", "reason: '   '", 1), "\"reason\""},
		{"missing owner", strings.Replace(good, "    owner: me\n", "", 1), "suppressions[0]: missing required field \"owner\""},
		{"missing expires", strings.Replace(good, "    expires: 2026-12-31\n", "", 1), "suppressions[0]: missing required field \"expires\""},
		{"missing ruleId", strings.Replace(good, "  - ruleId: FND-A-001\n    fingerprint", "  - fingerprint", 1), "\"ruleId\""},
		{"no target", strings.Replace(good, "    fingerprint: "+fpA+"\n", "", 1), "missing target"},
		{"both targets", strings.Replace(good, "    reason", "    resource: {name: n}\n    reason", 1), "not both"},
		{"bad fingerprint", strings.Replace(good, fpA, "fp1:zz", 1), "fp1:<32 hex>"},
		{"impossible date", strings.Replace(good, "2026-12-31", "2026-02-30", 1), "expires \"2026-02-30\""},
		{"month 13", strings.Replace(good, "2026-12-31", "2026-13-01", 1), "expires"},
		{"short date", strings.Replace(good, "2026-12-31", "2026-1-5", 1), "expires"},
		{"datetime", strings.Replace(good, "2026-12-31", "\"2026-12-31T00:00:00Z\"", 1), "expires"},
		{"free text date", strings.Replace(good, "2026-12-31", "never", 1), "expires"},
		{"duplicate", good + "  - ruleId: FND-A-001\n    fingerprint: " + fpA + "\n    reason: r\n    owner: o\n    expires: 2027-01-01\n", "duplicate of suppressions[0]"},
		{"suppressing suppression diagnostics", strings.Replace(good, "FND-A-001", "FND-SYS-SUPPRESSION-EXPIRED", 1), "cannot be suppressed"},
		{"wrong type", strings.Replace(good, "reason: why", "reason: [a]", 1), "invalid suppressions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse([]byte(tc.in), "s.yaml")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if f.Source != "s.yaml" {
					t.Errorf("source = %q", f.Source)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %v does not contain %q", err, tc.wantErr)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("error does not wrap ErrInvalid: %v", err)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	err := file(Suppression{RuleID: "FND-A-001", Fingerprint: fpA}).Validate()
	if err == nil {
		t.Fatal("want error")
	}
	for _, w := range []string{"\"reason\"", "\"owner\"", "\"expires\""} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q lacks %s", err, w)
		}
	}
}

func TestParseTooLarge(t *testing.T) {
	if _, err := Parse(make([]byte, MaxFileBytes+1), "s"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestExpiryBoundary(t *testing.T) {
	plus14 := time.FixedZone("UTC+14", 14*3600)
	minus12 := time.FixedZone("UTC-12", -12*3600)
	tests := []struct {
		name        string
		now         time.Time
		wantExpired bool
	}{
		{"day before expiry", utc("2026-12-30T12:00:00Z"), false},
		{"start of expiry day", utc("2026-12-31T00:00:00Z"), false},
		{"last second of expiry day", utc("2026-12-31T23:59:59Z"), false},
		{"last nanosecond of expiry day", utc("2026-12-31T23:59:59Z").Add(999999999 * time.Nanosecond), false},
		{"first instant after expiry day", utc("2027-01-01T00:00:00Z"), true},
		{"well after", utc("2028-01-01T00:00:00Z"), true},
		// 2027-01-01T01:00 at UTC+14 is still 2026-12-31T11:00Z: not expired, although the local calendar day has rolled over.
		{"zone ahead, UTC day not over", time.Date(2027, 1, 1, 1, 0, 0, 0, plus14), false},
		// 2026-12-31T20:00 at UTC-12 is 2027-01-01T08:00Z: expired although the local date is still the expiry day.
		{"zone behind, UTC day over", time.Date(2026, 12, 31, 20, 0, 0, 0, minus12), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Apply([]sdk.Finding{finding("FND-A-001", fpA, "w")}, file(byFP("FND-A-001", fpA, "2026-12-31")), tc.now)
			hidden := res.Findings[0].Suppressed != nil
			if hidden == tc.wantExpired {
				t.Errorf("suppressed=%v but expired=%v", hidden, tc.wantExpired)
			}
			if got := len(res.Diagnostics) == 1; got != tc.wantExpired {
				t.Errorf("diagnostic present=%v want %v", got, tc.wantExpired)
			}
		})
	}
}

func TestApplyMatching(t *testing.T) {
	now := utc("2026-10-05T00:00:00Z")
	sel := func(rule string, r Selector) Suppression {
		return Suppression{RuleID: rule, Resource: r, Reason: "r", Owner: "o", Expires: "2026-12-31"}
	}
	tests := []struct {
		name string
		s    Suppression
		f    sdk.Finding
		want bool
	}{
		{"fingerprint match", byFP("FND-A-001", fpA, "2026-12-31"), finding("FND-A-001", fpA, "w"), true},
		{"fingerprint differs", byFP("FND-A-001", fpA, "2026-12-31"), finding("FND-A-001", fpB, "w"), false},
		{"rule differs", byFP("FND-A-001", fpA, "2026-12-31"), finding("FND-B-001", fpA, "w"), false},
		{"selector name", sel("FND-A-001", Selector{Name: "w"}), finding("FND-A-001", fpA, "w"), true},
		{"selector name differs", sel("FND-A-001", Selector{Name: "x"}), finding("FND-A-001", fpA, "w"), false},
		{"selector type case-insensitive", sel("FND-A-001", Selector{Type: "microsoft.example/widgets"}), finding("FND-A-001", fpA, "w"), true},
		{"selector all fields", sel("FND-A-001", Selector{Kind: "arm-resource", Type: "Microsoft.Example/Widgets", Name: "w"}), finding("FND-A-001", fpA, "w"), true},
		{"selector one field wrong", sel("FND-A-001", Selector{Kind: "service", Name: "w"}), finding("FND-A-001", fpA, "w"), false},
		{"selector rule differs", sel("FND-B-001", Selector{Name: "w"}), finding("FND-A-001", fpA, "w"), false},
		{"selector pointer", sel("FND-A-001", Selector{Pointer: "/p"}), func() sdk.Finding {
			f := finding("FND-A-001", fpA, "w")
			f.Resource.Pointer = "/p"
			return f
		}(), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Apply([]sdk.Finding{tc.f}, file(tc.s), now)
			if got := res.Findings[0].Suppressed != nil; got != tc.want {
				t.Fatalf("suppressed=%v want %v", got, tc.want)
			}
			if tc.want {
				s := res.Findings[0].Suppressed
				if s.Reason != "r" && s.Reason != "because" || s.Expires != "2026-12-31" || s.Source != ".foundry-doctor/suppressions.yaml" {
					t.Errorf("suppression record = %+v", s)
				}
				if len(res.Unused) != 0 {
					t.Errorf("used entry reported unused")
				}
			} else if len(res.Unused) != 1 {
				t.Errorf("unmatched entry should be unused, got %d", len(res.Unused))
			}
		})
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	in := []sdk.Finding{finding("FND-A-001", fpA, "w")}
	Apply(in, file(byFP("FND-A-001", fpA, "2026-12-31")), utc("2026-10-05T00:00:00Z"))
	if in[0].Suppressed != nil {
		t.Error("input mutated")
	}
}

func TestApplyFirstValidWinsAndExpiredSkipped(t *testing.T) {
	a := byFP("FND-A-001", fpA, "2026-01-01") // expired
	a.Reason = "old"
	b := Suppression{RuleID: "FND-A-001", Resource: Selector{Name: "w"}, Reason: "new", Owner: "o", Expires: "2026-12-31"}
	c := Suppression{RuleID: "FND-A-001", Resource: Selector{Kind: "arm-resource"}, Reason: "later", Owner: "o", Expires: "2027-12-31"}
	res := Apply([]sdk.Finding{finding("FND-A-001", fpA, "w")}, file(a, b, c), utc("2026-10-05T00:00:00Z"))
	if got := res.Findings[0].Suppressed; got == nil || got.Reason != "new" {
		t.Fatalf("want the first valid entry (new), got %+v", got)
	}
	if len(res.Diagnostics) != 1 {
		t.Errorf("diagnostics = %d", len(res.Diagnostics))
	}
	if len(res.Unused) != 1 || res.Unused[0].Reason != "later" {
		t.Errorf("unused = %+v", res.Unused)
	}
}

func TestExpiredDiagnosticShape(t *testing.T) {
	s := byFP("FND-A-001", fpA, "2026-01-01")
	s.Reason = "SECRET-LOOKING-REASON"
	res := Apply(nil, file(s), utc("2026-10-05T00:00:00Z"))
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.RuleID != "FND-SYS-SUPPRESSION-EXPIRED" || d.Severity != sdk.SeverityError || d.Category != "" || len(d.Basis) != 0 {
		t.Errorf("diagnostic = %+v", d)
	}
	if d.Fingerprint != "" || d.Suppressed != nil || d.Baselined {
		t.Errorf("diagnostic must be unstamped and unhidden: %+v", d)
	}
	if d.Location.File != ".foundry-doctor/suppressions.yaml" || d.Resource.Kind != "file" {
		t.Errorf("location = %+v %+v", d.Location, d.Resource)
	}
	if d.Key == "" || !strings.Contains(d.Evidence, "2026-01-01") || !strings.Contains(d.Evidence, "FND-A-001") {
		t.Errorf("key/evidence = %q / %q", d.Key, d.Evidence)
	}
	if strings.Contains(d.Evidence, "SECRET") {
		t.Error("reason text leaked into evidence")
	}
	if !d.Severity.Valid() || d.Recommendation == "" || d.Confidence != sdk.ConfidenceCertain {
		t.Errorf("incomplete diagnostic: %+v", d)
	}
}

func TestExpiredWithoutMatchingFindingStillReported(t *testing.T) {
	res := Apply(nil, file(byFP("FND-A-001", fpA, "2026-01-01")), utc("2026-10-05T00:00:00Z"))
	if len(res.Diagnostics) != 1 || len(res.Unused) != 0 {
		t.Errorf("diagnostics=%d unused=%d", len(res.Diagnostics), len(res.Unused))
	}
}

func TestSelectorDiagnosticKeysDistinct(t *testing.T) {
	a := Suppression{RuleID: "FND-A-001", Resource: Selector{Name: "a"}, Reason: "r", Owner: "o", Expires: "2026-01-01"}
	b := a
	b.Resource.Name = "b"
	res := Apply(nil, file(a, b), utc("2026-10-05T00:00:00Z"))
	if len(res.Diagnostics) != 2 || res.Diagnostics[0].Key == res.Diagnostics[1].Key {
		t.Errorf("keys must differ: %+v", res.Diagnostics)
	}
}

func TestSuppressionDiagnosticsNeverSuppressed(t *testing.T) {
	// Bypass Validate on purpose: Apply must be safe on its own.
	s := Suppression{RuleID: "FND-SYS-SUPPRESSION-EXPIRED", Resource: Selector{Kind: "file"}, Reason: "r", Owner: "o", Expires: "2026-12-31"}
	d := finding("FND-SYS-SUPPRESSION-EXPIRED", "", "x")
	d.Resource.Kind = "file"
	res := Apply([]sdk.Finding{d}, file(s), utc("2026-10-05T00:00:00Z"))
	if res.Findings[0].Suppressed != nil {
		t.Error("suppression diagnostics must not be suppressible")
	}
}

func TestAlreadySuppressedKept(t *testing.T) {
	f := finding("FND-A-001", fpA, "w")
	f.Suppressed = &sdk.Suppression{Reason: "x", Expires: "2030-01-01"}
	res := Apply([]sdk.Finding{f}, file(byFP("FND-A-001", fpA, "2026-12-31")), utc("2026-10-05T00:00:00Z"))
	if res.Findings[0].Suppressed.Reason != "x" || len(res.Unused) != 1 {
		t.Errorf("existing suppression overwritten: %+v", res.Findings[0].Suppressed)
	}
}

func TestMalformedExpiryNeverHides(t *testing.T) {
	// Apply on an unvalidated file fails closed: a bad date counts as expired.
	res := Apply([]sdk.Finding{finding("FND-A-001", fpA, "w")}, file(byFP("FND-A-001", fpA, "2026-02-30")), utc("2026-01-01T00:00:00Z"))
	if res.Findings[0].Suppressed != nil || len(res.Diagnostics) != 1 {
		t.Error("malformed expiry must not hide the finding")
	}
}

func TestReadWrite(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, ".foundry-doctor"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".foundry-doctor", "suppressions.yaml"), []byte(good))
	f, err := Read(ctx, root, ".foundry-doctor/suppressions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if f.Source != ".foundry-doctor/suppressions.yaml" || len(f.Suppressions) != 1 {
		t.Errorf("file = %+v", f)
	}
	f, err = Read(ctx, root, filepath.Join(root, ".foundry-doctor", "suppressions.yaml"))
	if err != nil || f.Source != ".foundry-doctor/suppressions.yaml" {
		t.Errorf("absolute path: %v %q", err, f.Source)
	}
}

func TestReadUnsafe(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := context.Background()
	mustWrite(t, filepath.Join(outside, "s.yaml"), []byte(good))
	mustWrite(t, filepath.Join(root, "real.yaml"), []byte(good))
	mustSymlink(t, filepath.Join(outside, "s.yaml"), filepath.Join(root, "link.yaml"))
	mustSymlink(t, "real.yaml", filepath.Join(root, "inroot-link.yaml"))
	mustSymlink(t, outside, filepath.Join(root, "linkdir"))
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"link.yaml", "inroot-link.yaml", "linkdir/s.yaml", "../" + filepath.Base(outside) + "/s.yaml",
		filepath.Join(outside, "s.yaml"), "adir", "",
	} {
		if _, err := Read(ctx, root, p); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Read(%q): want ErrUnsafePath, got %v", p, err)
		}
	}
	if _, err := Read(ctx, "", "real.yaml"); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("empty root: %v", err)
	}
}

func TestReadMissingOversizeCancelled(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(context.Background(), root, "nope.yaml"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want ErrNotExist, got %v", err)
	}
	if _, err := Read(context.Background(), filepath.Join(root, "missing"), "x.yaml"); err == nil {
		t.Error("missing root should fail")
	}
	mustWrite(t, filepath.Join(root, "big.yaml"), bytes.Repeat([]byte("#"), MaxFileBytes+1))
	if _, err := Read(context.Background(), root, "big.yaml"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("want ErrTooLarge, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, root, "x.yaml"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestReadInvalidContent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "s.yaml"), []byte(strings.Replace(good, "owner: me", "owner: ''", 1)))
	if _, err := Read(context.Background(), root, "s.yaml"); err == nil || !strings.Contains(err.Error(), "\"owner\"") {
		t.Errorf("want owner error, got %v", err)
	}
}

func TestExampleLoadsAndApplies(t *testing.T) {
	f, err := Read(context.Background(), filepath.Join("..", "..", "schemas", "examples"), "suppressions.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Suppressions) != 2 || f.Suppressions[0].Expires != "2026-12-31" {
		t.Errorf("example = %+v", f)
	}
}

func TestExampleRejectsUnknownKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "examples", "suppressions.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"top":   string(data) + "bogus: 1\n",
		"entry": strings.Replace(string(data), "owner: platform-team\n", "owner: platform-team\n    bogus: 1\n", 1),
	} {
		if _, err := Parse([]byte(mutated), "x"); err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Errorf("%s: unknown key not rejected: %v", name, err)
		}
	}
}

type schema struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Defs       map[string]*schema         `json:"$defs"`
	Additional *bool                      `json:"additionalProperties"`
}

func yamlFields(t *testing.T, typ reflect.Type) (all, required []string) {
	t.Helper()
	for i := range typ.NumField() {
		parts := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")
		if parts[0] == "-" {
			continue
		}
		if parts[0] == "" {
			t.Fatalf("%s.%s has no yaml name", typ.Name(), typ.Field(i).Name)
		}
		all = append(all, parts[0])
		omit := false
		for _, p := range parts[1:] {
			omit = omit || p == "omitempty"
		}
		if !omit {
			required = append(required, parts[0])
		}
	}
	sort.Strings(all)
	sort.Strings(required)
	return all, required
}

func checkAgainst(t *testing.T, name string, s *schema, typ reflect.Type) {
	t.Helper()
	all, req := yamlFields(t, typ)
	var props []string
	for k := range s.Properties {
		props = append(props, k)
	}
	sort.Strings(props)
	gotReq := append([]string{}, s.Required...)
	sort.Strings(gotReq)
	if !reflect.DeepEqual(props, all) {
		t.Errorf("%s: schema properties %v != struct fields %v", name, props, all)
	}
	// The schema marks exactly one of fingerprint/resource through oneOf, so the Go
	// struct's omitempty pair is the only legitimate difference in 'required'.
	if !slices.Equal(gotReq, req) {
		t.Errorf("%s: schema required %v != struct required %v", name, gotReq, req)
	}
	if s.Additional == nil || *s.Additional {
		t.Errorf("%s: additionalProperties must be false", name)
	}
}

func TestSchemaMatchesStructs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "suppressions.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	checkAgainst(t, "File", &s, reflect.TypeOf(File{}))
	checkAgainst(t, "Suppression", s.Defs["suppression"], reflect.TypeOf(Suppression{}))
	checkAgainst(t, "Selector", s.Defs["selector"], reflect.TypeOf(Selector{}))
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

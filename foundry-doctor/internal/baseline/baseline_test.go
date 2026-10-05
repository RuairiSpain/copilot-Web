package baseline

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

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	fpA = "fp1:0123456789abcdef0123456789abcdef"
	fpB = "fp1:fedcba9876543210fedcba9876543210"
	fpC = "fp1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func finding(rule, fp string) sdk.Finding {
	return sdk.Finding{
		RuleID: rule, RuleVersion: 1, Severity: sdk.SeverityError, Fingerprint: fp,
		Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.Example/Widgets", Name: "w", ID: "/subscriptions/secret-sub/x"},
		Evidence: "evidence text",
	}
}

func valid(entries ...Entry) File { return File{Version: 1, GeneratedBy: "t 1", Entries: entries} }

func entry(fp, rule string) Entry { return Entry{Fingerprint: fp, RuleID: rule, RuleVersion: 1} }

func TestGenerate(t *testing.T) {
	fs := []sdk.Finding{
		finding("FND-B-001", fpB),
		finding("FND-A-001", fpA),
		finding("FND-A-001", fpA), // duplicate fingerprint collapses
		finding("FND-SYS-SUPPRESSION-EXPIRED", ""),
		finding("bicep/BCP035", ""),
	}
	f, err := Generate(fs, " foundry-doctor 1.0 ", "2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || f.GeneratedBy != "foundry-doctor 1.0" || len(f.Entries) != 2 {
		t.Fatalf("unexpected file: %+v", f)
	}
	if f.Entries[0].RuleID != "FND-A-001" || f.Entries[1].RuleID != "FND-B-001" {
		t.Errorf("entries not sorted: %+v", f.Entries)
	}
	if f.Entries[0].Resource.Type != "microsoft.example/widgets" || f.Entries[0].FirstSeen != "2026-10-05" {
		t.Errorf("entry fields wrong: %+v", f.Entries[0])
	}
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name      string
		findings  []sdk.Finding
		firstSeen string
	}{
		{"missing fingerprint", []sdk.Finding{finding("FND-A-001", "")}, ""},
		{"foreign scheme", []sdk.Finding{finding("FND-A-001", "fp2:0123456789abcdef0123456789abcdef")}, ""},
		{"bad firstSeen", nil, "2026-02-30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Generate(tc.findings, "t", tc.firstSeen); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestGenerateEmptyIsValid(t *testing.T) {
	f, err := Generate(nil, "t", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "entries: []") {
		t.Errorf("empty baseline should render entries: [], got %q", data)
	}
	if _, err := Parse(data); err != nil {
		t.Errorf("round trip: %v", err)
	}
}

func TestGenerateOmitsSensitiveFields(t *testing.T) {
	f, _ := Generate([]sdk.Finding{finding("FND-A-001", fpA)}, "t", "")
	data, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"secret-sub", "evidence text", "firstSeen"} {
		if strings.Contains(string(data), s) {
			t.Errorf("output contains %q:\n%s", s, data)
		}
	}
}

func TestApply(t *testing.T) {
	b := valid(entry(fpA, "FND-A-001"), entry(fpC, "FND-C-001"))
	in := []sdk.Finding{
		finding("FND-A-001", fpA),                   // exact match
		finding("FND-A-001", fpB),                   // same rule, changed fingerprint: stays visible
		finding("FND-NEW-001", fpB),                 // new
		finding("FND-A-001", ""),                    // unstamped
		finding("FND-SYS-SUPPRESSION-EXPIRED", fpA), // diagnostic, even with a matching fp
	}
	out, stale := Apply(in, b)
	want := []bool{true, false, false, false, false}
	for i, w := range want {
		if out[i].Baselined != w {
			t.Errorf("finding %d Baselined=%v want %v", i, out[i].Baselined, w)
		}
	}
	for i := range in {
		if in[i].Baselined {
			t.Errorf("input %d was mutated", i)
		}
	}
	if len(stale) != 1 || stale[0].Fingerprint != fpC {
		t.Errorf("stale = %+v, want the %s entry", stale, fpC)
	}
}

func TestApplyEmpty(t *testing.T) {
	out, stale := Apply(nil, File{})
	if len(out) != 0 || len(stale) != 0 {
		t.Errorf("got %v %v", out, stale)
	}
	out, stale = Apply([]sdk.Finding{finding("FND-A-001", fpA)}, valid())
	if out[0].Baselined || len(stale) != 0 {
		t.Error("empty baseline must hide nothing")
	}
}

func TestApplyStaleSorted(t *testing.T) {
	b := valid(entry(fpB, "FND-B-001"), entry(fpA, "FND-A-001"))
	_, stale := Apply(nil, b)
	if len(stale) != 2 || stale[0].RuleID != "FND-A-001" {
		t.Errorf("stale not sorted: %+v", stale)
	}
}

func TestMarshalDeterministic(t *testing.T) {
	a := valid(entry(fpB, "FND-B-001"), entry(fpA, "FND-A-001"))
	b := valid(entry(fpA, "FND-A-001"), entry(fpB, "FND-B-001"))
	da, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := Marshal(b)
	if !bytes.Equal(da, db) {
		t.Errorf("order of input changed output:\n%s\n%s", da, db)
	}
	if a.Entries[0].RuleID != "FND-B-001" {
		t.Error("Marshal modified its input")
	}
	if bytes.Contains(da, []byte("\r")) {
		t.Error("output must use LF")
	}
	got, err := Parse(da)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Marshal(got)
	if !bytes.Equal(da, again) {
		t.Error("round trip not stable")
	}
}

func TestMarshalRejectsInvalid(t *testing.T) {
	if _, err := Marshal(File{Version: 2}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

const head = "version: 1\ngeneratedBy: t\nentries:\n"

func TestParse(t *testing.T) {
	good := head + "  - fingerprint: " + fpA + "\n    ruleId: FND-A-001\n    ruleVersion: 1\n    resource: {}\n"
	tests := []struct {
		name    string
		in      string
		wantErr string // substring; empty means success
	}{
		{"valid", good, ""},
		{"valid CRLF", strings.ReplaceAll(good, "\n", "\r\n"), ""},
		{"valid with firstSeen", good + "    firstSeen: 2026-10-05\n", ""},
		{"empty file", "", "empty"},
		{"unknown top-level key", good + "extra: 1\n", "extra"},
		{"unknown entry key", good + "    owner: x\n", "owner"},
		{"unknown version", strings.Replace(good, "version: 1", "version: 2", 1), "unsupported version 2"},
		{"missing version", strings.Replace(good, "version: 1\n", "", 1), "unsupported version 0"},
		{"multi document", good + "---\n" + good, "more than one"},
		{"trailing empty document", good + "---\n", "more than one"},
		{"duplicate fingerprint", good + "  - fingerprint: " + fpA + "\n    ruleId: FND-B-001\n    ruleVersion: 1\n    resource: {}\n", "duplicate fingerprint"},
		{"foreign fingerprint scheme", strings.Replace(good, "fp1:", "fp2:", 1), "unknown scheme"},
		{"uppercase hex", strings.Replace(good, "0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF", 1), "unknown scheme"},
		{"short fingerprint", strings.Replace(good, fpA, "fp1:abcd", 1), "unknown scheme"},
		{"missing fingerprint", head + "  - ruleId: FND-A-001\n    ruleVersion: 1\n    resource: {}\n", "\"fingerprint\""},
		{"missing ruleId", head + "  - fingerprint: " + fpA + "\n    ruleVersion: 1\n    resource: {}\n", "\"ruleId\""},
		{"zero ruleVersion", strings.Replace(good, "ruleVersion: 1", "ruleVersion: 0", 1), "ruleVersion"},
		{"diagnostic rule", strings.Replace(good, "FND-A-001", "FND-SYS-INVALID-AZURE-YAML", 1), "cannot be baselined"},
		{"bicep diagnostic", strings.Replace(good, "FND-A-001", "bicep/BCP035", 1), "cannot be baselined"},
		{"bad date 30 Feb", good + "    firstSeen: \"2026-02-30\"\n", "firstSeen"},
		{"wrong type", strings.Replace(good, "ruleVersion: 1", "ruleVersion: abc", 1), "cannot unmarshal"},
		{"not yaml", "version: [1\n", "invalid baseline"},
		{"alias bomb shape rejected by size/shape", head + "  - *missing\n", "invalid baseline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.in))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
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

func TestParseTooLarge(t *testing.T) {
	if _, err := Parse(make([]byte, MaxFileBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	f := valid(entry(fpA, "FND-A-001"))
	if err := Write(ctx, root, ".foundry-doctor/baseline.yaml", f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx, root, ".foundry-doctor/baseline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f) {
		t.Errorf("got %+v want %+v", got, f)
	}
	// absolute path inside the root is accepted
	if _, err := Read(ctx, root, filepath.Join(root, ".foundry-doctor", "baseline.yaml")); err != nil {
		t.Errorf("absolute inside root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".foundry-doctor", "baseline.yaml.tmp")); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
	// overwrite
	if err := Write(ctx, root, ".foundry-doctor/baseline.yaml", valid()); err != nil {
		t.Errorf("overwrite: %v", err)
	}
}

func TestReadUnsafe(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := context.Background()
	data, _ := Marshal(valid(entry(fpA, "FND-A-001")))
	mustWrite(t, filepath.Join(outside, "b.yaml"), data)
	mustWrite(t, filepath.Join(root, "real.yaml"), data)
	mustSymlink(t, filepath.Join(outside, "b.yaml"), filepath.Join(root, "link.yaml"))
	mustSymlink(t, "real.yaml", filepath.Join(root, "inroot-link.yaml"))
	mustSymlink(t, outside, filepath.Join(root, "linkdir"))
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, path string
	}{
		{"symlink outside root", "link.yaml"},
		{"symlink inside root", "inroot-link.yaml"},
		{"symlinked directory", "linkdir/b.yaml"},
		{"dotdot", "../" + filepath.Base(outside) + "/b.yaml"},
		{"absolute outside", filepath.Join(outside, "b.yaml")},
		{"directory", "adir"},
		{"empty path", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Read(ctx, root, tc.path); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("want ErrUnsafePath, got %v", err)
			}
		})
	}
	if _, err := Read(ctx, "", "real.yaml"); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("empty root: %v", err)
	}
}

func TestReadMissingAndCancelled(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(context.Background(), root, "nope.yaml"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want ErrNotExist, got %v", err)
	}
	if _, err := Read(context.Background(), filepath.Join(root, "missing-root"), "x.yaml"); err == nil {
		t.Error("missing root should fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, root, "x.yaml"); !errors.Is(err, context.Canceled) {
		t.Errorf("read: %v", err)
	}
	if err := Write(ctx, root, "x.yaml", valid()); !errors.Is(err, context.Canceled) {
		t.Errorf("write: %v", err)
	}
}

func TestReadOversize(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "big.yaml"), bytes.Repeat([]byte("#"), MaxFileBytes+1))
	if _, err := Read(context.Background(), root, "big.yaml"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestWriteUnsafe(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := context.Background()
	mustSymlink(t, outside, filepath.Join(root, "linkdir"))
	mustSymlink(t, filepath.Join(outside, "t.yaml"), filepath.Join(root, "link.yaml"))
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"linkdir/b.yaml", "link.yaml", "../escape.yaml", "adir", ""} {
		if err := Write(ctx, root, p, valid()); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Write(%q): want ErrUnsafePath, got %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "b.yaml")); !os.IsNotExist(err) {
		t.Error("write escaped the root")
	}
	if _, err := os.Stat(filepath.Join(outside, "t.yaml")); !os.IsNotExist(err) {
		t.Error("write followed a symlink")
	}
}

func TestWriteRejectsInvalidAndLeavesNoFile(t *testing.T) {
	root := t.TempDir()
	if err := Write(context.Background(), root, "b.yaml", File{Version: 9}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.yaml")); !os.IsNotExist(err) {
		t.Error("file created for invalid baseline")
	}
}

func TestWriteStaleTempFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "b.yaml.tmp"), []byte("x"))
	if err := Write(context.Background(), root, "b.yaml", valid()); err == nil {
		t.Error("expected failure when the temp name is taken")
	}
}

func TestIsDiagnostic(t *testing.T) {
	for id, want := range map[string]bool{"FND-SYS-X": true, "bicep/BCP035": true, "FND-NET-001": false, "": false} {
		if IsDiagnostic(id) != want {
			t.Errorf("IsDiagnostic(%q) != %v", id, want)
		}
	}
}

func TestExampleLoads(t *testing.T) {
	f, err := Read(context.Background(), filepath.Join("..", "..", "schemas", "examples"), "baseline.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 2 {
		t.Errorf("entries = %d", len(f.Entries))
	}
}

func TestExampleRejectsUnknownKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "examples", "baseline.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"top":   string(data) + "bogus: 1\n",
		"entry": strings.Replace(string(data), "ruleVersion: 1\n", "ruleVersion: 1\n    bogus: 1\n", 1),
	} {
		if _, err := Parse([]byte(mutated)); err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Errorf("%s: unknown key not rejected: %v", name, err)
		}
	}
}

// schema mirrors the parts of a JSON schema object that the structural check reads.
type schema struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Ref        string                     `json:"$ref"`
	Items      *schema                    `json:"items"`
	Defs       map[string]*schema         `json:"$defs"`
	Additional *bool                      `json:"additionalProperties"`
}

func loadSchema(t *testing.T) *schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "baseline.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func yamlFields(t *testing.T, typ reflect.Type) (all, required []string) {
	t.Helper()
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("yaml")
		parts := strings.Split(tag, ",")
		if parts[0] == "" || parts[0] == "-" {
			t.Fatalf("%s.%s has no yaml name", typ.Name(), typ.Field(i).Name)
		}
		all = append(all, parts[0])
		if !contains(parts[1:], "omitempty") {
			required = append(required, parts[0])
		}
	}
	sort.Strings(all)
	sort.Strings(required)
	return all, required
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func keys(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func checkAgainst(t *testing.T, name string, s *schema, typ reflect.Type) {
	t.Helper()
	all, req := yamlFields(t, typ)
	gotReq := append([]string{}, s.Required...)
	sort.Strings(gotReq)
	if !reflect.DeepEqual(keys(s.Properties), all) {
		t.Errorf("%s: schema properties %v != struct fields %v", name, keys(s.Properties), all)
	}
	if !slices.Equal(gotReq, req) {
		t.Errorf("%s: schema required %v != struct required %v", name, gotReq, req)
	}
	if s.Additional == nil || *s.Additional {
		t.Errorf("%s: additionalProperties must be false", name)
	}
}

func TestSchemaMatchesStructs(t *testing.T) {
	s := loadSchema(t)
	checkAgainst(t, "File", s, reflect.TypeOf(File{}))
	checkAgainst(t, "Entry", s.Defs["entry"], reflect.TypeOf(Entry{}))
	checkAgainst(t, "Resource", s.Defs["resource"], reflect.TypeOf(Resource{}))
	var version struct {
		Const int `json:"const"`
	}
	if err := json.Unmarshal(s.Properties["version"], &version); err != nil || version.Const != Version {
		t.Errorf("schema version const = %d, want %d (%v)", version.Const, Version, err)
	}
}

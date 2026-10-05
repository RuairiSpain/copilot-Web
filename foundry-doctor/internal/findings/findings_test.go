package findings

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func base() sdk.Finding {
	return sdk.Finding{
		RuleID: "FD-NET-001", RuleVersion: 1, Severity: sdk.SeverityWarning, Profile: "baseline",
		Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.Storage/storageAccounts", Name: "acct", Pointer: "/properties/x"},
		Location: sdk.Location{File: "infra/main.bicep", Line: 10, Column: 3},
		Evidence: "evidence", Key: "defaultAction",
	}
}

func TestFingerprintFormat(t *testing.T) {
	fp := Fingerprint(base())
	if !strings.HasPrefix(fp, "fp1:") || len(fp) != len("fp1:")+32 {
		t.Fatalf("bad format %q", fp)
	}
	if fp != Fingerprint(base()) {
		t.Fatal("not deterministic")
	}
}

func TestFingerprintStability(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*sdk.Finding)
		same   bool
	}{
		{"line moved", func(f *sdk.Finding) { f.Location.Line, f.Location.Column = 99, 40 }, true},
		{"profile changed", func(f *sdk.Finding) { f.Profile = "strict" }, true},
		{"severity changed", func(f *sdk.Finding) { f.Severity = sdk.SeverityError }, true},
		{"confidence changed", func(f *sdk.Finding) { f.Confidence = sdk.ConfidenceLikely }, true},
		{"evidence prose changed", func(f *sdk.Finding) { f.Evidence = "other words" }, true},
		{"recommendation changed", func(f *sdk.Finding) { f.Recommendation = "x" }, true},
		{"adapter changed", func(f *sdk.Finding) { f.Adapter = "psrule" }, true},
		{"file moved, named resource", func(f *sdk.Finding) { f.Location.File = "other/path.bicep" }, true},
		{"type case", func(f *sdk.Finding) { f.Resource.Type = "microsoft.storage/STORAGEACCOUNTS" }, true},
		{"key whitespace", func(f *sdk.Finding) { f.Key = "  defaultAction\n" }, true},
		{"pointer ignored when key set", func(f *sdk.Finding) { f.Resource.Pointer = "/other" }, true},
		{"rule id", func(f *sdk.Finding) { f.RuleID = "FD-NET-002" }, false},
		{"rule version bump", func(f *sdk.Finding) { f.RuleVersion = 2 }, false},
		{"resource name", func(f *sdk.Finding) { f.Resource.Name = "acct2" }, false},
		{"resource kind", func(f *sdk.Finding) { f.Resource.Kind = "service" }, false},
		{"resource type", func(f *sdk.Finding) { f.Resource.Type = "Microsoft.Search/searchServices" }, false},
		{"key", func(f *sdk.Finding) { f.Key = "publicNetworkAccess" }, false},
		{"name case preserved", func(f *sdk.Finding) { f.Resource.Name = "ACCT" }, false},
		{"pointer used when key empty", func(f *sdk.Finding) { f.Key = ""; f.Resource.Pointer = "/a" }, false},
	}
	want := Fingerprint(base())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := base()
			tt.mutate(&f)
			if got := Fingerprint(f); (got == want) != tt.same {
				t.Fatalf("same=%v want %v (%s vs %s)", got == want, tt.same, got, want)
			}
		})
	}
}

func TestFingerprintKeyEmptyPointerDistinguishes(t *testing.T) {
	a, b := base(), base()
	a.Key, b.Key = "", ""
	a.Resource.Pointer, b.Resource.Pointer = "/a", "/b"
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("pointer should distinguish when key is empty")
	}
}

func TestFingerprintPathSeparators(t *testing.T) {
	mk := func(name, file string) sdk.Finding {
		f := base()
		f.Resource = sdk.ResourceRef{Kind: "file", Name: name}
		f.Location.File = file
		f.Key = ""
		return f
	}
	if Fingerprint(mk(`infra\main.bicep`, "")) != Fingerprint(mk("infra/main.bicep", "")) {
		t.Error("file-kind name differs by separator")
	}
	if Fingerprint(mk(`./infra/main.bicep`, "")) != Fingerprint(mk("infra/main.bicep", "")) {
		t.Error("leading ./ matters")
	}
	// No logical name: location file stands in, separator-insensitive.
	noName := func(file string) sdk.Finding {
		f := base()
		f.Resource = sdk.ResourceRef{Kind: "project"}
		f.Location.File = file
		return f
	}
	if Fingerprint(noName(`a\b\azure.yaml`)) != Fingerprint(noName("a/b/azure.yaml")) {
		t.Error("fallback file path differs by separator")
	}
	if Fingerprint(noName("a/azure.yaml")) == Fingerprint(noName("b/azure.yaml")) {
		t.Error("different files must differ when no logical name")
	}
}

func TestFingerprintSeparatorAmbiguity(t *testing.T) {
	a, b := base(), base()
	a.Resource.Name, a.Key = "ab", "c"
	b.Resource.Name, b.Key = "a", "bc"
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("fields run together")
	}
	c := base()
	c.Key = "x\x00y"
	d := base()
	d.Key = "x�y"
	_ = Fingerprint(c) // must not panic; NUL is neutralised
}

func TestFingerprintUnicodeAndEmpty(t *testing.T) {
	if Fingerprint(sdk.Finding{}) == "" {
		t.Fatal("empty finding must still fingerprint")
	}
	f := base()
	f.Key = "日本語🔑"
	if Fingerprint(f) == Fingerprint(base()) {
		t.Fatal("unicode key ignored")
	}
}

func TestStamp(t *testing.T) {
	sev := map[string]sdk.Severity{"baseline": sdk.SeverityInfo, "strict": sdk.SeverityError, "bad": "loud"}
	tests := []struct {
		name     string
		profile  string
		sev      map[string]sdk.Severity
		platform bool
		start    sdk.Severity
		want     sdk.Severity
	}{
		{"profile maps", "baseline", sev, false, sdk.SeverityWarning, sdk.SeverityInfo},
		{"strict maps", "strict", sev, false, sdk.SeverityInfo, sdk.SeverityError},
		{"platform basis always error", "baseline", sev, true, sdk.SeverityInfo, sdk.SeverityError},
		{"platform basis even if map says info", "baseline", map[string]sdk.Severity{"baseline": sdk.SeverityInfo}, true, "", sdk.SeverityError},
		{"platform basis with nil map", "x", nil, true, "", sdk.SeverityError},
		{"missing profile keeps rule severity", "other", sev, false, sdk.SeverityWarning, sdk.SeverityWarning},
		{"invalid mapped severity ignored", "bad", sev, false, sdk.SeverityWarning, sdk.SeverityWarning},
		{"nil map keeps severity", "baseline", nil, false, sdk.SeverityWarning, sdk.SeverityWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := sdk.Finding{Severity: tt.start}
			got := Stamp(f, tt.profile, tt.sev, tt.platform)
			if got.Severity != tt.want || got.Profile != tt.profile {
				t.Fatalf("got %s/%s want %s/%s", got.Severity, got.Profile, tt.want, tt.profile)
			}
			if f.Profile != "" || f.Severity != tt.start {
				t.Fatal("input modified")
			}
		})
	}
}

func TestSort(t *testing.T) {
	mk := func(file string, line, col int, rule, key, fp string) sdk.Finding {
		return sdk.Finding{RuleID: rule, Key: key, Fingerprint: fp, Location: sdk.Location{File: file, Line: line, Column: col}}
	}
	want := []sdk.Finding{
		mk("", 0, 0, "A", "", "fp1:a"),
		mk("a.bicep", 1, 1, "A", "", "fp1:a"),
		mk("a.bicep", 1, 2, "A", "", "fp1:a"),
		mk("a.bicep", 2, 1, "A", "", "fp1:a"),
		mk("a.bicep", 2, 1, "B", "", "fp1:a"),
		mk("a.bicep", 2, 1, "B", "k1", "fp1:a"),
		mk("a.bicep", 2, 1, "B", "k2", "fp1:a"),
		mk("a.bicep", 2, 1, "B", "k2", "fp1:b"),
		mk("b.bicep", 1, 1, "A", "", "fp1:a"),
	}
	last := mk("b.bicep", 1, 1, "A", "", "fp1:a")
	last.Evidence = "tie broken by evidence"
	want = append(want, last)

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		got := append([]sdk.Finding(nil), want...)
		rng.Shuffle(len(got), func(a, b int) { got[a], got[b] = got[b], got[a] })
		Sort(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d: order not deterministic:\n got %+v\nwant %+v", i, got, want)
		}
	}
	Sort(nil)
	Sort([]sdk.Finding{})
}

func TestSortFileBeforeLine(t *testing.T) {
	fs := []sdk.Finding{
		{RuleID: "A", Location: sdk.Location{File: "z", Line: 1}},
		{RuleID: "A", Location: sdk.Location{File: "a", Line: 9}},
	}
	Sort(fs)
	if fs[0].Location.File != "a" {
		t.Fatalf("file must dominate line: %+v", fs)
	}
}

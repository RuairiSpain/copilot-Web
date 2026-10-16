package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestSemverPrecedence(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0-beta.9", "1.0.0-beta.10", -1},
		{"1.0.0-beta.10", "1.0.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"1.0.0+build1", "1.0.0+build2", 0},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-rc.1", "1.0.0-beta.11", 1},
		{"1.2.3", "1.10.0", -1},
	}
	for _, tc := range tests {
		a, ok1 := parseStrictSemver(tc.a)
		b, ok2 := parseStrictSemver(tc.b)
		if !ok1 || !ok2 {
			t.Fatalf("parse %q %q", tc.a, tc.b)
		}
		if got := a.cmp(b); got != tc.want {
			t.Errorf("cmp(%s,%s)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "v1.2.3", "01.2.3", "1.2.3-", "1.2.3-01", "1.2.3.4", "a.b.c"} {
		if _, ok := parseStrictSemver(bad); ok {
			t.Errorf("parseStrictSemver(%q) accepted", bad)
		}
	}
}

func TestParseAzdRange(t *testing.T) {
	tests := []struct {
		in      string
		st      parseStatus
		holds   map[string]bool // version -> contained
		comment string
	}{
		{">=1.34.2", parseValid, map[string]bool{"1.34.2": true, "1.34.1": false, "2.0.0": true}, ""},
		{">= 0.6.0-beta.3", parseValid, map[string]bool{"0.6.0-beta.3": true, "0.6.0-beta.2": false}, "schema example"},
		{">=1.0.0 <2.0.0", parseValid, map[string]bool{"1.5.0": true, "2.0.0": false, "1.0.0-beta.1": false}, "AND"},
		{">=1.0.0 || <0.5.0", parseValid, map[string]bool{"0.4.0": true, "0.5.0": false, "1.0.0": true}, "OR"},
		{">=1.0.0 <2.0.0 || >=3.0.0", parseValid, map[string]bool{"1.5.0": true, "2.5.0": false, "3.1.0": true}, "AND binds tighter"},
		{"1.2.3", parseValid, map[string]bool{"1.2.3": true, "1.2.4": false}, "no operator is equality"},
		{"==1.2.3", parseValid, map[string]bool{"1.2.3": true, "1.2.4": false}, ""},
		{"=1.2.3", parseValid, map[string]bool{"1.2.3": true}, ""},
		{"!=1.2.3", parseValid, map[string]bool{"1.2.3": false, "1.2.4": true}, ""},
		{"!1.2.3", parseValid, map[string]bool{"1.2.3": false, "1.2.4": true}, ""},
		{">1.0.0", parseValid, map[string]bool{"1.0.0": false, "1.0.1": true}, ""},
		{"<=1.0.0", parseValid, map[string]bool{"1.0.0": true, "1.0.1": false}, ""},
		{"1.x", parseValid, map[string]bool{"1.9.0": true, "2.0.0": false, "0.9.0": false}, "wildcard"},
		{"1.2.x", parseValid, map[string]bool{"1.2.9": true, "1.3.0": false}, "wildcard"},
		{"  >=1.0.0   <2.0.0  ", parseValid, map[string]bool{"1.1.0": true}, "extra spaces"},
		{"", parseInvalid, nil, "empty"},
		{"   ", parseInvalid, nil, "blank"},
		{"latest", parseInvalid, nil, ""},
		{">=1.0.0, <2.0.0", parseInvalid, nil, "comma"},
		{">=1.0.0,<2.0.0", parseInvalid, nil, "comma"},
		{"~1.0.0", parseInvalid, nil, ""},
		{"~>1.0.0", parseInvalid, nil, ""},
		{"^1.0.0", parseInvalid, nil, ""},
		{"=>1.0.0", parseInvalid, nil, ""},
		{"=<1.0.0", parseInvalid, nil, ""},
		{"*", parseUnverified, nil, "one-character token: blang drops it, not verified"},
		{"1.X", parseInvalid, nil, "uppercase X"},
		{"1.0.0 - 2.0.0", parseUnverified, nil, "blang accepts it (drops the one-character - token); not verified"},
		{"|| >=1.0.0", parseInvalid, nil, "leading or"},
		{">=1.0.0 ||", parseInvalid, nil, "trailing or"},
		{">=1.0.0 || || <2.0.0", parseInvalid, nil, "empty alternative"},
		{">=1.0", parseInvalid, nil, "partial version"},
		{"v1.0.0", parseInvalid, nil, "v prefix"},
		{">=", parseInvalid, nil, "operator only"},
		{">= ||", parseInvalid, nil, "operator then or"},
		{">=1.0.0||<2.0.0", parseUnverified, nil, "glued or is not verified"},
		{">=1.x", parseUnverified, nil, "operator with wildcard is not verified"},
		{"x", parseUnverified, nil, "bare wildcard is not verified"},
	}
	for _, tc := range tests {
		t.Run(tc.in+"/"+tc.comment, func(t *testing.T) {
			r, st := parseAzdRange(tc.in)
			if st != tc.st {
				t.Fatalf("status=%v want %v", st, tc.st)
			}
			for vs, want := range tc.holds {
				v, ok := parseStrictSemver(vs)
				if !ok {
					t.Fatal(vs)
				}
				if got := r.contains(v); got != want {
					t.Errorf("contains(%s)=%v want %v", vs, got, want)
				}
			}
		})
	}
}

func TestParseMMConstraint(t *testing.T) {
	tests := []struct {
		in     string
		any    bool
		st     parseStatus
		groups int
	}{
		{"", true, parseValid, 0},
		{"latest", true, parseValid, 0},
		{">=1.0.0", false, parseValid, 1},
		{">= 1.0.0", false, parseValid, 1},
		{"=>1.0.0", false, parseValid, 1},
		{"=<1.0.0", false, parseValid, 1},
		{"~1.2", false, parseValid, 1},
		{"~>1.2.3", false, parseValid, 1},
		{"^1.2.3", false, parseValid, 1},
		{"!=1.2.3", false, parseValid, 1},
		{"1.2.3", false, parseValid, 1},
		{">=1.0.0, <2.0.0", false, parseValid, 1},
		{">=1.0.0 <2.0.0", false, parseValid, 1},
		{">=1.0.0 || <0.5.0", false, parseValid, 2},
		{"1.0.0 - 2.0.0", false, parseValid, 1},
		{"1.x", false, parseValid, 1},
		{"1.X", false, parseValid, 1},
		{"*", false, parseValid, 1},
		{"v1.2.3", false, parseValid, 1},
		{"not-a-version", false, parseInvalid, 0},
		{">=1.0.0,,<2.0.0", false, parseInvalid, 0},
		{">=1.0.0 ||", false, parseInvalid, 0},
		{">=", false, parseInvalid, 0},
		{"1.2.3.4", false, parseInvalid, 0},
		{"Latest", false, parseInvalid, 0},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			g, anyV, st := parseMMConstraint(tc.in)
			if st != tc.st || anyV != tc.any || len(g) != tc.groups {
				t.Fatalf("any=%v st=%v groups=%d", anyV, st, len(g))
			}
		})
	}
}

func TestMMIntervals(t *testing.T) {
	tests := []struct {
		in     string
		lo, hi string // "" unset; "[x" inclusive, "(x" exclusive
	}{
		{">=1.0.0", "[1.0.0", ""},
		{">1.0.0", "(1.0.0", ""},
		{"<1.0.0-beta.12", "", "(1.0.0-beta.12"},
		{"<=1.0.0", "", "[1.0.0"},
		{"~1.2.3", "[1.2.3", "(1.3.0"},
		{"~1", "[1.0.0", "(2.0.0"},
		{"^1.2.3", "[1.2.3", "(2.0.0"},
		{"^0.2.3", "[0.2.3", "(0.3.0"},
		{"^0.0.3", "[0.0.3", "(0.0.4"},
		{"1.2", "[1.2.0", "(1.3.0"},
		{"1.2.3", "[1.2.3", "[1.2.3"},
		{"1.x", "[1.0.0", "(2.0.0"},
		{">1.2", "[1.3.0", ""},
		{"<=1.2", "", "(1.3.0"},
		{"1.0.0 - 2.0.0", "[1.0.0", "[2.0.0"},
		{">=1.0.0 <2.0.0", "[1.0.0", "(2.0.0"},
		{"!=1.0.0", "", ""},
		{"*", "", ""},
	}
	fmtB := func(b bound) string {
		if !b.set {
			return ""
		}
		if b.incl {
			return "[" + b.v.String()
		}
		return "(" + b.v.String()
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			g, _, st := parseMMConstraint(tc.in)
			if st != parseValid {
				t.Fatal("invalid")
			}
			if lo, hi := fmtB(g[0].lo), fmtB(g[0].hi); lo != tc.lo || hi != tc.hi {
				t.Fatalf("lo=%q hi=%q", lo, hi)
			}
		})
	}
}

func TestAzdDevBuild(t *testing.T) {
	for in, want := range map[string]bool{"0.0.0-dev.0": true, "0.0.0-dev.1": false, "0.0.0": false, "1.0.0-dev.0": false} {
		v, _ := parseStrictSemver(in)
		if got := isAzdDevBuild(v); got != want {
			t.Errorf("%s: %v", in, got)
		}
	}
}

func TestCFG001AzdRequiredVersion(t *testing.T) {
	y := func(r string) string { return "name: app\nrequiredVersions:\n  azd: \"" + r + "\"\n" }
	tests := []struct {
		name       string
		yaml       string
		azd        string
		wantKey    string // finding key, "" for none
		skip       string // uncertain skip substring
		evidenceIn string
	}{
		{name: "in range", yaml: y(">=1.34.2"), azd: "1.35.0"},
		{name: "in range with v prefix", yaml: y(">=1.34.2"), azd: "v1.35.0"},
		{name: "in range OR second alternative", yaml: y("<1.0.0 || >=1.34.2"), azd: "1.35.0"},
		{name: "out of range", yaml: y(">=1.34.2"), azd: "1.30.0", wantKey: "required-versions-azd-out-of-range", evidenceIn: "azd 1.30.0 is outside"},
		{name: "prerelease below release floor", yaml: y(">=1.0.0"), azd: "1.0.0-beta.1", wantKey: "required-versions-azd-out-of-range"},
		{name: "prerelease satisfies prerelease floor", yaml: y(">=1.0.0-beta.1"), azd: "1.0.0-beta.2"},
		{name: "dev build is exempt", yaml: y(">=99.0.0"), azd: "0.0.0-dev.0"},
		{name: "unknown version is uncertain", yaml: y(">=1.34.2"), azd: "", skip: "azd-version-unknown"},
		{name: "unparseable azd version is uncertain", yaml: y(">=1.34.2"), azd: "not-a-version", skip: "not a valid semantic version"},
		{name: "invalid range comma", yaml: y(">=1.0.0, <2.0.0"), azd: "1.5.0", wantKey: "required-versions-azd-invalid", evidenceIn: "not a valid semver range"},
		{name: "invalid range caret", yaml: y("^1.0.0"), azd: "", wantKey: "required-versions-azd-invalid", evidenceIn: "not a valid semver range"},
		{name: "invalid range latest", yaml: y("latest"), azd: "1.5.0", wantKey: "required-versions-azd-invalid", evidenceIn: "latest"},
		{name: "invalid range empty", yaml: y(""), azd: "1.5.0", wantKey: "required-versions-azd-invalid", evidenceIn: "not a valid semver range"},
		{name: "invalid range decided even for a dev build", yaml: y("~1.0.0"), azd: "0.0.0-dev.0", wantKey: "required-versions-azd-invalid", evidenceIn: "not a valid semver range"},
		{name: "unverified form is uncertain", yaml: y(">=1.x"), azd: "1.5.0", skip: "not verified"},
		{name: "absent", yaml: "name: app\n", azd: "1.5.0"},
		{name: "null value is no constraint", yaml: "name: app\nrequiredVersions:\n  azd:\n", azd: "1.5.0"},
		{name: "non-string is uncertain", yaml: "name: app\nrequiredVersions:\n  azd:\n    a: b\n", azd: "1.5.0", skip: "not a string"},
	}
	r := find(RegisterWith(Providers{}), "FND-CFG-001")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, r, &sdk.Input{AzureYAML: doc(t, tc.yaml), AzdVersion: tc.azd})
			switch {
			case tc.wantKey != "":
				if res.Skipped != nil || len(res.Findings) != 1 || res.Findings[0].Key != tc.wantKey {
					t.Fatalf("want finding %s, got %+v skip=%+v", tc.wantKey, res.Findings, res.Skipped)
				}
				if res.Findings[0].Location.Line != 3 || !strings.Contains(res.Findings[0].Evidence, tc.evidenceIn) {
					t.Fatalf("finding %+v", res.Findings[0])
				}
			case tc.skip != "":
				if res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, uncertainPrefix) || !strings.Contains(res.Skipped.Reason, tc.skip) {
					t.Fatalf("want uncertain skip %q, got %+v findings=%+v", tc.skip, res.Skipped, res.Findings)
				}
			default:
				if res.Skipped != nil || len(res.Findings) != 0 {
					t.Fatalf("want clean, got %+v %+v", res.Skipped, res.Findings)
				}
			}
		})
	}
}

func TestCFG001ExtensionInvalidIsNeverAnError(t *testing.T) {
	for _, c := range []string{"not-a-version", ">=1.0.0 ||", "1.2.3.4", ">=", ">=1.0.0,,<2"} {
		y := "name: app\nrequiredVersions:\n  extensions:\n    azure.ai.agents: \"" + c + "\"\n    other.ext: \"" + c + "\"\n"
		res := run(t, find(RegisterWith(Providers{}), "FND-CFG-001"), &sdk.Input{AzureYAML: doc(t, y), AzdVersion: "1.35.0"})
		if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "exact tag match") {
			t.Errorf("%q: findings=%+v skip=%+v", c, res.Findings, res.Skipped)
		}
	}
}

func TestVersionNotesPrerelease(t *testing.T) {
	notes := func(c string) []versionNote {
		d := doc(t, "name: app\nrequiredVersions:\n  extensions:\n    azure.ai.agents: \""+c+"\"\n")
		return requiredVersionNotes(d.(document).Resolved(), cfg001MinVersions)
	}
	tests := []struct {
		in   string
		want string // substring of the only note, "" for none
		info bool
	}{
		{">=0.5.0", "no pre-release", false},
		{">=0.5.0 || >=1.0.0-beta.1", "allows versions below", true},
		{">=1.0.0-beta.1", "allows versions below", true},
		{"~1.0.0-beta.18", "", false},
		{"<1.0.0-beta.2 || >=2.0.0", "allows versions below", true},
		{"latest", "", false},
		{"", "", false},
		{"<1.0.0-beta.2 || <1.0.0-beta.3", "excludes the tested range", false},
		{">1.0.0-beta.18", "requires versions above", false},
		{">=1.0.0 || >=2.0.0", "requires versions above", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			ns := notes(tc.in)
			if tc.want == "" {
				if len(ns) != 0 {
					t.Fatalf("notes=%+v", ns)
				}
				return
			}
			if len(ns) != 1 || !strings.Contains(ns[0].Text, tc.want) || ns[0].Info != tc.info {
				t.Fatalf("notes=%+v", ns)
			}
		})
	}
}

package cfg

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The fixture testdata/semver_diff.json was generated once from the real
// libraries azd uses, in a scratch module that is not part of this module:
//
//	github.com/blang/semver/v4 v4.0.0         ParseRange, then the range func
//	github.com/Masterminds/semver/v3 v3.4.0   NewConstraint, then Check
//
// Each entry records whether the library accepts the input and which of the
// listed versions the accepted range matches. The test holds the hand-written
// parsers to it: a string the library rejects must never be called valid, a
// string the library accepts must never be called invalid (the parser may say
// unverified), and where the parser decides, it must agree on every version.

type diffEntry struct {
	Input   string   `json:"input"`
	Valid   bool     `json:"valid"`
	Matches []string `json:"matches"`
}

type diffFixture struct {
	Versions []string    `json:"versions"`
	Blang    []diffEntry `json:"blangSemverV4_0_0"`
	Masters  []diffEntry `json:"mastermindsSemverV3_4_0"`
}

func loadDiffFixture(t *testing.T) diffFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/semver_diff.json")
	if err != nil {
		t.Fatal(err)
	}
	var f diffFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Versions) == 0 || len(f.Blang) < 1000 || len(f.Masters) < 1000 {
		t.Fatalf("fixture too small: %d versions, %d blang, %d masterminds", len(f.Versions), len(f.Blang), len(f.Masters))
	}
	return f
}

func TestParseAzdRangeAgreesWithBlangSemver(t *testing.T) {
	f := loadDiffFixture(t)
	valid, decided := 0, 0
	for _, e := range f.Blang {
		rng, st := parseAzdRange(e.Input)
		switch st {
		case parseUnverified:
			continue
		case parseInvalid:
			if e.Valid {
				t.Errorf("parseAzdRange(%q) = invalid, but blang/semver accepts it", e.Input)
			}
			decided++
			continue
		}
		decided++
		if !e.Valid {
			t.Errorf("parseAzdRange(%q) = valid, but blang/semver rejects it", e.Input)
			continue
		}
		valid++
		for _, vs := range f.Versions {
			v, ok := parseStrictSemver(vs)
			if !ok {
				t.Fatalf("fixture version %q does not parse", vs)
			}
			if got, want := rng.contains(v), slices.Contains(e.Matches, vs); got != want {
				t.Errorf("parseAzdRange(%q).contains(%s) = %v, blang/semver says %v", e.Input, vs, got, want)
			}
		}
	}
	if valid < 200 || decided < 1000 {
		t.Errorf("only %d valid and %d decided inputs were compared; the parser is too eager to say unverified", valid, decided)
	}
}

func TestParseMMConstraintAgreesWithMasterminds(t *testing.T) {
	f := loadDiffFixture(t)
	valid, decided := 0, 0
	for _, e := range f.Masters {
		groups, anyV, st := parseMMConstraint(e.Input)
		if st == parseUnverified {
			continue
		}
		decided++
		// The empty string and "latest" are handled by azd before the library
		// (pkg/extensions/manager.go), so the parser accepts them although
		// NewConstraint does not.
		if e.Input == "" || e.Input == "latest" {
			if st != parseValid || !anyV {
				t.Errorf("parseMMConstraint(%q) must mean any version", e.Input)
			}
			continue
		}
		if (st == parseValid) != e.Valid {
			t.Errorf("parseMMConstraint(%q) valid=%v, Masterminds says %v", e.Input, st == parseValid, e.Valid)
			continue
		}
		if st != parseValid {
			continue
		}
		valid++
		for _, vs := range f.Versions {
			v, ok := parseStrictSemver(vs)
			if !ok {
				t.Fatalf("fixture version %q does not parse", vs)
			}
			if !mmComparable(e.Input) {
				continue
			}
			if got, want := mmContains(groups, v), slices.Contains(e.Matches, vs); got != want {
				t.Errorf("parseMMConstraint(%q) contains(%s) = %v, Masterminds says %v", e.Input, vs, got, want)
			}
		}
	}
	if valid < 100 || decided < 1000 {
		t.Errorf("only %d valid and %d decided inputs were compared", valid, decided)
	}
}

// mmComparable reports whether the interval model of a constraint is exact:
// "!=" holes and "||" free of pre-release subtleties are not tracked, so only
// plain bound-based constraints are compared version by version.
func mmComparable(in string) bool {
	for i := 0; i < len(in); i++ {
		if in[i] == '!' {
			return false
		}
	}
	return true
}

// mmContains evaluates the interval model with Masterminds' pre-release rule:
// a pre-release version matches only a constraint that itself has one.
func mmContains(groups []mmGroup, v semver) bool {
	for _, g := range groups {
		if len(v.pre) > 0 && !g.hasPre {
			continue
		}
		if g.lo.set {
			c := v.cmp(g.lo.v)
			if c < 0 || c == 0 && !g.lo.incl {
				continue
			}
		}
		if g.hi.set {
			c := v.cmp(g.hi.v)
			if c > 0 || c == 0 && !g.hi.incl {
				continue
			}
		}
		return true
	}
	return false
}

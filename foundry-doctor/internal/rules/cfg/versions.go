package cfg

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// semver is a version with an optional alpha, beta or rc pre-release number.
type semver struct {
	major, minor, patch int
	pre                 int // 0 release, 1 alpha, 2 beta, 3 rc
	preNum              int
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(alpha|beta|rc)\.(\d+))?$`)

func parseSemver(s string) (semver, bool) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return semver{}, false
	}
	var v semver
	var err error
	for i, dst := range []*int{&v.major, &v.minor, &v.patch} {
		if *dst, err = strconv.Atoi(m[i+1]); err != nil {
			return semver{}, false
		}
	}
	if m[4] != "" {
		v.pre = map[string]int{"alpha": 1, "beta": 2, "rc": 3}[m[4]]
		if v.preNum, err = strconv.Atoi(m[5]); err != nil {
			return semver{}, false
		}
	}
	return v, true
}

// cmp orders versions; a release sorts after every pre-release of the same
// major.minor.patch.
func (a semver) cmp(b semver) int {
	rank := func(v semver) int {
		if v.pre == 0 {
			return 1 << 20
		}
		return v.pre
	}
	pairs := [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}, {rank(a), rank(b)}, {a.preNum, b.preNum}}
	for _, p := range pairs {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

type bound struct {
	v    semver
	incl bool
	set  bool
}

// versionRange is the interval a constraint allows.
type versionRange struct{ lo, hi bound }

func (r *versionRange) raiseLo(b bound) {
	if !r.lo.set || b.v.cmp(r.lo.v) > 0 || (b.v.cmp(r.lo.v) == 0 && !b.incl) {
		r.lo = b
	}
}

func (r *versionRange) lowerHi(b bound) {
	if !r.hi.set || b.v.cmp(r.hi.v) < 0 || (b.v.cmp(r.hi.v) == 0 && !b.incl) {
		r.hi = b
	}
}

var comparatorRe = regexp.MustCompile(`^\s*(>=|<=|>|<|=|~)?\s*([^\s<>=~]+)`)

// parseConstraint parses the space-separated comparator form shown in the
// azure.yaml schema examples: ">=1.0.0", "~2.0.0", "=3.1.2", ">= 1.0.0 < 2.0.0".
// A bare version means equality. "latest" and the empty string allow any
// version (any=true). Anything else, including || and ^, is not parsed.
func parseConstraint(s string) (r versionRange, any, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "latest") {
		return r, true, true
	}
	for s != "" {
		m := comparatorRe.FindStringSubmatch(s)
		if m == nil {
			return r, false, false
		}
		v, vok := parseSemver(m[2])
		if !vok {
			return r, false, false
		}
		switch m[1] {
		case ">=":
			r.raiseLo(bound{v, true, true})
		case ">":
			r.raiseLo(bound{v, false, true})
		case "<=":
			r.lowerHi(bound{v, true, true})
		case "<":
			r.lowerHi(bound{v, false, true})
		case "~":
			r.raiseLo(bound{v, true, true})
			r.lowerHi(bound{semver{major: v.major, minor: v.minor + 1}, false, true})
		default: // "=" or bare
			r.raiseLo(bound{v, true, true})
			r.lowerHi(bound{v, true, true})
		}
		s = strings.TrimSpace(s[len(m[0]):])
	}
	return r, false, true
}

// versionNote describes a requiredVersions.extensions entry the rule cannot
// judge as an error.
type versionNote struct {
	Name string
	Node *yaml.Node
	Text string
}

// requiredVersionNotes compares each declared extension constraint with the
// tested minimum. A constraint that allows versions below the tested range, or
// that excludes the tested range, is a note and never an error: the tested
// range is a verification window, not a platform requirement. Constraints that
// cannot be parsed are notes too.
func requiredVersionNotes(root *yaml.Node, mins map[string]string) []versionNote {
	ext := child(child(root, "requiredVersions"), "extensions")
	var out []versionNote
	for _, p := range pairs(ext) {
		minText, known := mins[p.Key.Value]
		if !known {
			continue
		}
		minV, minOK := parseSemver(minText)
		if !minOK {
			continue
		}
		raw := scalar(p.Val)
		r, anyVersion, parsed := parseConstraint(raw)
		var text string
		switch {
		case p.Val == nil || p.Val.Kind != yaml.ScalarNode, !parsed:
			text = fmt.Sprintf("requiredVersions.extensions.%s constraint cannot be parsed", p.Key.Value)
		case anyVersion:
		case r.hi.set && (r.hi.v.cmp(minV) < 0 || (r.hi.v.cmp(minV) == 0 && !r.hi.incl)):
			text = fmt.Sprintf("requiredVersions.extensions.%s excludes the tested range starting at %s", p.Key.Value, minText)
		case !r.lo.set || r.lo.v.cmp(minV) < 0:
			text = fmt.Sprintf("requiredVersions.extensions.%s allows versions below the tested %s", p.Key.Value, minText)
		}
		if text != "" {
			out = append(out, versionNote{p.Key.Value, p.Val, text})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func noteTexts(ns []versionNote) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Text)
	}
	return out
}

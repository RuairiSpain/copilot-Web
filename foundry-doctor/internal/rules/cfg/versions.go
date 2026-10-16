package cfg

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file holds two small, hand-written constraint parsers. azd parses the
// two requiredVersions keys with different libraries and the grammars differ
// (docs/spikes/azd-required-versions.md, verified 2026-10-16):
//
//	requiredVersions.azd              blang/semver/v4 v4.0.0 ParseRange
//	requiredVersions.extensions.<id>  Masterminds/semver/v3 v3.4.0 NewConstraint
//
// No dependency is added; each parser implements only the forms the note
// verified. A form the note does not cover is reported as parseUnverified,
// never as invalid, so an unverified guess cannot produce a false error.

// semver is a full semantic version with dot-separated pre-release
// identifiers. Build metadata is parsed and ignored (it never takes part in
// precedence).
type semver struct {
	major, minor, patch uint64
	pre                 []string
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
			return false
		}
	}
	return true
}

// splitPreBuild splits "core-pre+build" into its parts. ok is false when a
// present pre-release or build part has an empty or malformed identifier.
func splitPreBuild(s string) (core string, pre []string, ok bool) {
	if i := strings.IndexByte(s, '+'); i >= 0 {
		for _, id := range strings.Split(s[i+1:], ".") {
			if !isIdent(id) {
				return "", nil, false
			}
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		for _, id := range strings.Split(s[i+1:], ".") {
			if !isIdent(id) {
				return "", nil, false
			}
			pre = append(pre, id)
		}
		s = s[:i]
	}
	return s, pre, true
}

func hasLeadingZero(s string) bool { return len(s) > 1 && s[0] == '0' }

// parseStrictSemver parses major.minor.patch with an optional pre-release and
// build part, the way blang/semver Parse does: three numeric parts without
// leading zeros, numeric pre-release identifiers without leading zeros, and no
// "v" prefix.
func parseStrictSemver(s string) (semver, bool) {
	core, pre, ok := splitPreBuild(s)
	if !ok {
		return semver{}, false
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var nums [3]uint64
	for i, p := range parts {
		if !isDigits(p) || hasLeadingZero(p) {
			return semver{}, false
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}
	for _, id := range pre {
		if isDigits(id) && hasLeadingZero(id) {
			return semver{}, false
		}
	}
	return semver{nums[0], nums[1], nums[2], pre}, true
}

// parseGivenAzdVersion parses an azd version the tool was given. A leading "v"
// is tolerated because users type it.
func parseGivenAzdVersion(s string) (semver, bool) {
	return parseStrictSemver(strings.TrimPrefix(strings.TrimSpace(s), "v"))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cmp orders versions by semantic-version precedence: a release sorts after
// every pre-release of the same major.minor.patch; numeric identifiers sort
// below alphanumeric ones; a longer identifier list sorts above its prefix.
func (a semver) cmp(b semver) int {
	for _, c := range []int{cmpUint(a.major, b.major), cmpUint(a.minor, b.minor), cmpUint(a.patch, b.patch)} {
		if c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, yn := isDigits(x), isDigits(y)
		switch {
		case xn && yn:
			xv, _ := strconv.ParseUint(x, 10, 64)
			yv, _ := strconv.ParseUint(y, 10, 64)
			if c := cmpUint(xv, yv); c != 0 {
				return c
			}
		case xn:
			return -1
		case yn:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return cmpUint(uint64(len(a.pre)), uint64(len(b.pre)))
}

func (a semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", a.major, a.minor, a.patch)
	if len(a.pre) > 0 {
		s += "-" + strings.Join(a.pre, ".")
	}
	return s
}

// isAzdDevBuild reports azd's local dev build version. azd skips the
// requiredVersions.azd check for it (internal.IsDevVersion), so the doctor
// treats the range as satisfied.
func isAzdDevBuild(v semver) bool {
	return v.major == 0 && v.minor == 0 && v.patch == 0 && len(v.pre) == 2 && v.pre[0] == "dev" && v.pre[1] == "0"
}

// parseStatus is the outcome of parsing a constraint string.
type parseStatus int

const (
	// parseValid: the library accepts the string.
	parseValid parseStatus = iota
	// parseInvalid: the library rejects the string (verified grammar).
	parseInvalid
	// parseUnverified: the verified grammar does not say; do not decide.
	parseUnverified
)

// ---------------------------------------------------------------------------
// requiredVersions.azd: blang/semver/v4 ParseRange

type azdCmp struct {
	op string // "<", "<=", ">", ">=", "=", "!="
	v  semver
}

// azdRange is an OR of AND groups.
type azdRange [][]azdCmp

var azdOps = []string{"<=", ">=", "==", "!=", "<", ">", "=", "!"}

func splitAzdOp(tok string) (op, rest string) {
	for _, o := range azdOps {
		if strings.HasPrefix(tok, o) {
			switch o {
			case "==":
				return "=", tok[2:]
			case "!":
				return "!=", tok[1:]
			}
			return o, tok[len(o):]
		}
	}
	return "", tok
}

// azdWildcard expands the lowercase wildcard forms "N.x" and "N.M.x" into the
// half-open span [lo, hi). isWild reports whether the token uses a wildcard;
// ok is false for a wildcard shape the research note does not cover.
func azdWildcard(tok string) (lo, hi semver, isWild, ok bool) {
	core := tok
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	if !strings.Contains(core, "x") {
		return lo, hi, false, true
	}
	if core != tok {
		return lo, hi, true, false // pre-release on a wildcard: not covered
	}
	parts := strings.Split(core, ".")
	num := func(s string) (uint64, bool) {
		if !isDigits(s) || hasLeadingZero(s) {
			return 0, false
		}
		n, err := strconv.ParseUint(s, 10, 64)
		return n, err == nil
	}
	switch {
	case len(parts) == 2 && parts[1] == "x":
		if maj, k := num(parts[0]); k {
			return semver{major: maj}, semver{major: maj + 1}, true, true
		}
	case len(parts) == 3 && parts[2] == "x":
		maj, k1 := num(parts[0])
		mnr, k2 := num(parts[1])
		if k1 && k2 {
			return semver{major: maj, minor: mnr}, semver{major: maj, minor: mnr + 1}, true, true
		}
	}
	return lo, hi, true, false
}

// parseAzdRange parses a requiredVersions.azd value with the blang/semver/v4
// ParseRange grammar: operators < <= > >= = == ! !=, no operator meaning
// equality, an optional space between operator and version, space-separated
// constraints ANDed, "||" ORed (AND binds tighter, no parentheses), lowercase
// x wildcards. Comma, ~, ~>, ^, =>, =<, *, X, hyphen ranges, "latest", the
// empty string and a leading or trailing "||" are rejected.
func parseAzdRange(s string) (azdRange, parseStatus) {
	toks := strings.Fields(s)
	if len(toks) == 0 {
		return nil, parseInvalid
	}
	var groups azdRange
	var cur []azdCmp
	sawTerm := false // a comparator was added since the last "||"
	flush := func() bool {
		if !sawTerm {
			return false
		}
		groups = append(groups, cur)
		cur, sawTerm = nil, false
		return true
	}
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok == "||" {
			if !flush() {
				return nil, parseInvalid
			}
			continue
		}
		if strings.Contains(tok, "||") {
			return nil, parseUnverified // "||" glued to a version: tokenisation not verified
		}
		op, rest := splitAzdOp(tok)
		if rest == "" {
			if i+1 >= len(toks) || toks[i+1] == "||" {
				return nil, parseInvalid
			}
			i++
			rest = toks[i]
		}
		lo, hi, wild, wok := azdWildcard(rest)
		switch {
		case wild && (!wok || op != ""):
			return nil, parseUnverified
		case wild:
			cur = append(cur, azdCmp{">=", lo}, azdCmp{"<", hi})
		default:
			v, vok := parseStrictSemver(rest)
			if !vok {
				return nil, parseInvalid
			}
			if op == "" {
				op = "="
			}
			cur = append(cur, azdCmp{op, v})
		}
		sawTerm = true
	}
	if !flush() {
		return nil, parseInvalid // trailing "||"
	}
	return groups, parseValid
}

// contains reports whether v satisfies the range (plain semver precedence, so
// ">=1.0.0" rejects 1.0.0-beta.1).
func (r azdRange) contains(v semver) bool {
	for _, g := range r {
		all := true
		for _, c := range g {
			k := v.cmp(c.v)
			var ok bool
			switch c.op {
			case "<":
				ok = k < 0
			case "<=":
				ok = k <= 0
			case ">":
				ok = k > 0
			case ">=":
				ok = k >= 0
			case "=":
				ok = k == 0
			case "!=":
				ok = k != 0
			}
			if !ok {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// requiredVersions.extensions.<id>: Masterminds/semver/v3 NewConstraint

type bound struct {
	v    semver
	incl bool
	set  bool
}

// interval is the span a comparator or group allows. Holes made by "!=" are
// not tracked; only bounds matter for the tested-window notes.
type interval struct{ lo, hi bound }

func (r *interval) raiseLo(b bound) {
	if !r.lo.set || b.v.cmp(r.lo.v) > 0 || (b.v.cmp(r.lo.v) == 0 && !b.incl) {
		r.lo = b
	}
}

func (r *interval) lowerHi(b bound) {
	if !r.hi.set || b.v.cmp(r.hi.v) < 0 || (b.v.cmp(r.hi.v) == 0 && !b.incl) {
		r.hi = b
	}
}

// mmGroup is one "||" alternative: the intersection of its comparators.
type mmGroup struct {
	interval
	// hasPre: some comparator carries a pre-release. Masterminds rejects
	// pre-release candidates unless the constraint has one.
	hasPre bool
}

// partial is a Masterminds version with 0 to 3 specified components; a
// wildcard (x, X, *) ends the specified prefix.
type partial struct {
	n   int // specified components, 0 means fully wild
	v   semver
	pre bool
}

func parseMMVersion(s string) (partial, bool) {
	s = strings.TrimPrefix(s, "v")
	core, pre, ok := splitPreBuild(s)
	if !ok || core == "" {
		return partial{}, false
	}
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return partial{}, false
	}
	var nums [3]uint64
	p := partial{n: len(parts), v: semver{pre: pre}, pre: len(pre) > 0}
	wild := false
	for i, part := range parts {
		switch {
		case part == "x" || part == "X" || part == "*":
			if !wild {
				p.n = i
			}
			wild = true
		case isDigits(part):
			if wild {
				continue
			}
			n, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return partial{}, false
			}
			nums[i] = n
		default:
			return partial{}, false
		}
	}
	if wild && p.pre {
		return partial{}, false
	}
	p.v.major, p.v.minor, p.v.patch = nums[0], nums[1], nums[2]
	return p, true
}

// bump returns the first version above every version sharing the first n
// components of p.
func (p partial) bump(n int) semver {
	switch n {
	case 0, 1:
		return semver{major: p.v.major + 1}
	case 2:
		return semver{major: p.v.major, minor: p.v.minor + 1}
	}
	return semver{major: p.v.major, minor: p.v.minor, patch: p.v.patch + 1}
}

var mmOps = []string{">=", "=>", "<=", "=<", "!=", "~>", "<", ">", "=", "~", "^"}

func splitMMOp(tok string) (op, rest string) {
	for _, o := range mmOps {
		if strings.HasPrefix(tok, o) {
			switch o {
			case "=>":
				return ">=", tok[2:]
			case "=<":
				return "<=", tok[2:]
			case "~>":
				return "~", tok[2:]
			}
			return o, tok[len(o):]
		}
	}
	return "", tok
}

// mmInterval maps one comparator to the span it allows.
func mmInterval(op string, p partial) interval {
	var iv interval
	lo := bound{p.v, true, true}
	if p.n == 0 || op == "!=" {
		return iv
	}
	switch op {
	case "", "=":
		iv.raiseLo(lo)
		if p.n == 3 {
			iv.lowerHi(bound{p.v, true, true})
		} else {
			iv.lowerHi(bound{p.bump(p.n), false, true})
		}
	case "~":
		iv.raiseLo(lo)
		if p.n == 1 {
			iv.lowerHi(bound{p.bump(1), false, true})
		} else {
			iv.lowerHi(bound{p.bump(2), false, true})
		}
	case "^":
		iv.raiseLo(lo)
		switch {
		case p.v.major > 0 || p.n == 1:
			iv.lowerHi(bound{p.bump(1), false, true})
		case p.v.minor > 0 || p.n == 2:
			iv.lowerHi(bound{p.bump(2), false, true})
		default:
			iv.lowerHi(bound{p.bump(3), false, true})
		}
	case ">":
		if p.n == 3 {
			iv.raiseLo(bound{p.v, false, true})
		} else {
			iv.raiseLo(bound{p.bump(p.n), true, true})
		}
	case ">=":
		iv.raiseLo(lo)
	case "<":
		iv.lowerHi(bound{p.v, false, true})
	case "<=":
		if p.n == 3 {
			iv.lowerHi(bound{p.v, true, true})
		} else {
			iv.lowerHi(bound{p.bump(p.n), false, true})
		}
	}
	return iv
}

func (g *mmGroup) apply(iv interval, p partial) {
	if iv.lo.set {
		g.raiseLo(iv.lo)
	}
	if iv.hi.set {
		g.lowerHi(iv.hi)
	}
	if p.pre {
		g.hasPre = true
	}
}

// parseMMGroup parses one "||" alternative: comma- or space-separated
// comparators, with "a - b" hyphen ranges.
func parseMMGroup(s string) (mmGroup, parseStatus) {
	var g mmGroup
	count := 0
	for _, seg := range strings.Split(s, ",") {
		toks := strings.Fields(seg)
		if len(toks) == 0 {
			return g, parseInvalid // empty alternative or separator
		}
		for i := 0; i < len(toks); i++ {
			if i+2 < len(toks) && toks[i+1] == "-" {
				a, aok := parseMMVersion(toks[i])
				b, bok := parseMMVersion(toks[i+2])
				if !aok || !bok {
					return g, parseInvalid
				}
				g.apply(mmInterval(">=", a), a)
				g.apply(mmInterval("<=", b), b)
				count++
				i += 2
				continue
			}
			op, rest := splitMMOp(toks[i])
			if rest == "" {
				if i+1 >= len(toks) {
					return g, parseInvalid
				}
				i++
				rest = toks[i]
			}
			p, ok := parseMMVersion(rest)
			if !ok {
				return g, parseInvalid
			}
			g.apply(mmInterval(op, p), p)
			count++
		}
	}
	if count == 0 {
		return g, parseInvalid
	}
	return g, parseValid
}

// parseMMConstraint parses a requiredVersions.extensions value with the
// Masterminds/semver/v3 NewConstraint grammar. "latest" and the empty string
// mean any version (any=true); azd handles them before the library
// (pkg/extensions/manager.go:297-300). A string the library rejects is
// parseInvalid: azd then falls back to an exact tag match, so the caller must
// not report an error.
func parseMMConstraint(s string) (groups []mmGroup, anyVersion bool, st parseStatus) {
	if s == "" || s == "latest" {
		return nil, true, parseValid
	}
	for _, alt := range strings.Split(s, "||") {
		g, gst := parseMMGroup(alt)
		if gst != parseValid {
			return nil, false, gst
		}
		groups = append(groups, g)
	}
	return groups, false, parseValid
}

// ---------------------------------------------------------------------------
// notes for requiredVersions.extensions

// versionNote describes a requiredVersions.extensions entry the rule cannot
// judge as an error.
type versionNote struct {
	Name string
	Node *yaml.Node
	Text string
	// Info marks a note that never makes a rule inconclusive: a floor below
	// the tested minimum says nothing about the checks the rule ran.
	Info bool
}

// testedMax is the highest extension version the catalogue verified
// (compatibility.extensions upper bounds of FND-CFG-001 and FND-CFG-011).
var testedMax = map[string]string{
	"azure.ai.agents":   "1.0.0-beta.18",
	"azure.ai.projects": "1.0.0-beta.13",
}

func parseTested(s string) (semver, bool) {
	p, ok := parseMMVersion(s)
	return p.v, ok && p.n == 3
}

// requiredVersionNotes checks each declared extension constraint. A value that
// is not a valid Masterminds constraint is a note (azd falls back to an exact
// tag match, so it is never an error). For the extensions with a tested
// version, a constraint that allows versions below the tested range, that
// excludes it, or that rejects pre-release candidates is a note too: the
// tested range is a verification window, not a platform requirement. The
// installed extension versions live in azd's user config and cannot be read,
// so the constraints are never decided.
func requiredVersionNotes(root *yaml.Node, mins map[string]string) []versionNote {
	ext := child(child(root, "requiredVersions"), "extensions")
	var out []versionNote
	for _, p := range pairs(ext) {
		if p.Val == nil || p.Val.Tag == "!!null" {
			continue // nil pointer in azd: no constraint
		}
		name := p.Key.Value
		if p.Val.Kind != yaml.ScalarNode {
			out = append(out, versionNote{name, p.Val, fmt.Sprintf("requiredVersions.extensions.%s constraint cannot be parsed", name), false})
			continue
		}
		groups, anyVersion, st := parseMMConstraint(p.Val.Value)
		if st != parseValid {
			out = append(out, versionNote{name, p.Val, fmt.Sprintf("requiredVersions.extensions.%s constraint cannot be parsed; azd falls back to an exact tag match", name), false})
			continue
		}
		minText, known := mins[name]
		if !known || anyVersion {
			continue
		}
		minV, minOK := parseTested(minText)
		if !minOK {
			continue
		}
		maxV, maxOK := parseTested(testedMax[name])
		allBelow, allAbove, preOK, floorBelow := true, maxOK, false, false
		for _, g := range groups {
			if !(g.hi.set && (g.hi.v.cmp(minV) < 0 || (g.hi.v.cmp(minV) == 0 && !g.hi.incl))) {
				allBelow = false
			}
			if !(maxOK && g.lo.set && (g.lo.v.cmp(maxV) > 0 || (g.lo.v.cmp(maxV) == 0 && !g.lo.incl))) {
				allAbove = false
			}
			if g.hasPre {
				preOK = true
			}
			if !g.lo.set || g.lo.v.cmp(minV) < 0 {
				floorBelow = true
			}
		}
		var text string
		info := false
		switch {
		case allBelow:
			text = fmt.Sprintf("requiredVersions.extensions.%s excludes the tested range starting at %s", name, minText)
		case allAbove:
			text = fmt.Sprintf("requiredVersions.extensions.%s requires versions above the highest tested %s; those versions were not verified", name, testedMax[name])
		case !preOK:
			text = fmt.Sprintf("requiredVersions.extensions.%s has no pre-release in its constraint, so azd rejects pre-release versions such as the tested %s", name, minText)
		case floorBelow:
			text = fmt.Sprintf("requiredVersions.extensions.%s allows versions below the tested %s", name, minText)
			info = true
		}
		if text != "" {
			out = append(out, versionNote{name, p.Val, text, info})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// noteTexts returns the texts of the notes that make a rule inconclusive.
// Info notes are left out: they do not turn other passed checks into skips.
func noteTexts(ns []versionNote) []string {
	var out []string
	for _, n := range ns {
		if n.Info {
			continue
		}
		out = append(out, n.Text)
	}
	return out
}

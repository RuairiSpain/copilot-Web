package rules

import (
	"fmt"
	"strconv"
	"strings"
)

// semver is a parsed semantic version (semver.org precedence rules). Build metadata is ignored.
type semver struct {
	major, minor, patch int
	pre                 []string
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v semver
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("version %q is not major.minor.patch", s)
	}
	nums := [3]*int{&v.major, &v.minor, &v.patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, fmt.Errorf("version %q has an invalid number %q", s, p)
		}
		*nums[i] = n
	}
	if hasPre {
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return v, fmt.Errorf("version %q has an empty prerelease identifier", s)
			}
		}
	}
	return v, nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compare returns -1, 0 or 1 by semver precedence: a prerelease sorts before its release.
func (a semver) compare(b semver) int {
	for _, c := range []int{cmpInt(a.major, b.major), cmpInt(a.minor, b.minor), cmpInt(a.patch, b.patch)} {
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
		xi, xerr := strconv.ParseUint(x, 10, 64)
		yi, yerr := strconv.ParseUint(y, 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if xi != yi {
				if xi < yi {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

// InRange reports whether version satisfies rng, a space-separated list of comparators such as
// ">=1.34.2 <=1.36.0-beta.1" (the catalogue format). All comparators must hold. A comparator
// without an operator means equality. It returns an error when either argument does not parse.
func InRange(version, rng string) (bool, error) {
	v, err := parseSemver(version)
	if err != nil {
		return false, err
	}
	fields := strings.Fields(rng)
	if len(fields) == 0 {
		return false, fmt.Errorf("empty version range")
	}
	for _, f := range fields {
		op := ""
		for _, cand := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, cand) {
				op = cand
				break
			}
		}
		bound, err := parseSemver(strings.TrimPrefix(f, op))
		if err != nil {
			return false, fmt.Errorf("range %q: %w", rng, err)
		}
		c := v.compare(bound)
		ok := false
		switch op {
		case ">=":
			ok = c >= 0
		case "<=":
			ok = c <= 0
		case ">":
			ok = c > 0
		case "<":
			ok = c < 0
		default:
			ok = c == 0
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

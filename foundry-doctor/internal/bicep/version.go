// Package bicep wraps the Bicep CLI (ADR-002). It never parses Bicep: it discovers and runs the
// compiler, turns the compiler's diagnostics into model.Diagnostic and sdk.Finding values, and
// normalises the compiled ARM JSON into model.ARMTemplate.
//
// Source mapping from ARM resources back to Bicep lines is not provided by the compiler, so
// nothing here invents a line number: derived resources carry the entry file, a JSON pointer
// into the ARM document and a confidence of likely or uncertain.
package bicep

import (
	"fmt"
	"regexp"
	"strconv"
)

// MinVersion returns the oldest Bicep CLI the adapter accepts. It is the earliest version at which
// `build --stdout`, `--diagnostics-format` and `lint --diagnostics-format sarif` to stdout all
// exist (docs/development/phase-1-tooling-facts.md).
func MinVersion() Version { return Version{Major: 0, Minor: 24, Patch: 24} }

// Version is a parsed Bicep CLI version.
type Version struct {
	Major, Minor, Patch int
	Hash                string // short commit hash printed by the CLI, may be empty
}

var versionRe = regexp.MustCompile(`Bicep CLI version (\d+)\.(\d+)\.(\d+)(?: \(([0-9a-fA-F]+)\))?`)

// ParseVersion reads the output of `bicep --version`, for example
// "Bicep CLI version 0.47.16 (3f73e1a234)".
func ParseVersion(out string) (Version, error) {
	m := versionRe.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("parse bicep version: unrecognised output")
	}
	var v Version
	for i, dst := range []*int{&v.Major, &v.Minor, &v.Patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, fmt.Errorf("parse bicep version %q: %w", m[0], err)
		}
		*dst = n
	}
	v.Hash = m[4]
	return v, nil
}

// String returns "major.minor.patch".
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Compare returns -1, 0 or 1 when v is older than, equal to, or newer than o. The hash is ignored.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		switch {
		case p[0] < p[1]:
			return -1
		case p[0] > p[1]:
			return 1
		}
	}
	return 0
}

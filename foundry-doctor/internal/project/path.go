package project

import (
	"fmt"
	"slices"
	"strings"
)

const maxRelPath = 4096

// windowsReserved holds the Windows device names. A name is reserved with any extension
// ("CON.txt") and with trailing spaces. They are refused on every platform so a project that is
// safe on Linux is also safe when the same repository is checked out on Windows.
var windowsReserved = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}, "CONIN$": {}, "CONOUT$": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
	"COM¹": {}, "COM²": {}, "COM³": {}, "LPT¹": {}, "LPT²": {}, "LPT³": {},
}

func isReserved(seg string) bool {
	base := seg
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, " ")
	_, ok := windowsReserved[strings.ToUpper(base)]
	return ok
}

// ValidateRel checks a project-relative path and returns it normalised: backslashes become
// slashes, "." segments and duplicate separators are dropped, and the result uses forward slashes
// only. It does not touch the file system. A path that normalises to nothing returns ".".
//
// It rejects: empty input, NUL and control characters, absolute paths (leading separator, UNC,
// drive letter), ".." segments, ':' in a segment (drive or NTFS stream), Windows reserved device
// names, and segments ending in a dot or space (Windows strips them, which can alias another name).
func ValidateRel(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafePath)
	}
	if len(rel) > maxRelPath {
		return "", fmt.Errorf("%w: path longer than %d bytes", ErrUnsafePath, maxRelPath)
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] < 0x20 || rel[i] == 0x7f {
			return "", fmt.Errorf("%w: control character in path", ErrUnsafePath)
		}
	}
	p := strings.ReplaceAll(rel, `\`, "/")
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: absolute path", ErrUnsafePath)
	}
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z') {
		return "", fmt.Errorf("%w: drive-qualified path", ErrUnsafePath)
	}
	var out []string
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "" || seg == ".":
			continue
		case seg == "..":
			return "", fmt.Errorf("%w: parent segment", ErrUnsafePath)
		case strings.Contains(seg, ":"):
			return "", fmt.Errorf("%w: colon in a path segment", ErrUnsafePath)
		case isReserved(seg):
			return "", fmt.Errorf("%w: reserved device name", ErrUnsafePath)
		case strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " "):
			return "", fmt.Errorf("%w: segment ends in dot or space", ErrUnsafePath)
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return ".", nil
	}
	return strings.Join(out, "/"), nil
}

// ValidateName checks a single path segment such as an azd environment name. It must survive
// ValidateRel unchanged and contain no separator.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%w: invalid name", ErrUnsafePath)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: name contains a separator", ErrUnsafePath)
	}
	clean, err := ValidateRel(name)
	if err != nil {
		return err
	}
	if clean != name {
		return fmt.Errorf("%w: name is not canonical", ErrUnsafePath)
	}
	return nil
}

// CaseCollisions returns groups of names that differ only in case, each group sorted, groups
// sorted by first member. On a case-insensitive file system (Windows, default macOS) such names
// address the same entry, so callers surface them as a warning.
func CaseCollisions(names []string) [][]string {
	groups := map[string][]string{}
	for _, n := range names {
		k := strings.ToLower(n)
		groups[k] = append(groups[k], n)
	}
	var out [][]string
	for _, g := range groups {
		if len(g) > 1 {
			slices.Sort(g)
			out = append(out, g)
		}
	}
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return out
}

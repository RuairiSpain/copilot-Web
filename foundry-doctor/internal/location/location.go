package location

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Region is a canonical source location shared by annotation and SARIF
// renderers. File is repo-relative and slash-separated; line and column are
// 1-based when known.
type Region struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// FromFinding maps an sdk location into a canonical region.
func FromFinding(f sdk.Finding) Region {
	return Region{
		File:   SanitizePath(f.Location.File),
		Line:   f.Location.Line,
		Column: f.Location.Column,
	}
}

// FromDiagnostic maps an exact Bicep compiler diagnostic into a canonical
// region.
func FromDiagnostic(d bicep.Diagnostic) Region {
	return Region{
		File:   SanitizePath(d.File),
		Line:   d.Line,
		Column: d.Column,
	}
}

// Precise reports whether the region is exact enough for inline annotations.
func (r Region) Precise() bool {
	return r.File != "" && r.Line > 0
}

// SanitizePath turns an untrusted path into a safe repo-relative,
// slash-separated path. Absolute paths, drive letters, UNC paths and
// traversal are reduced to their final element; control characters are
// removed. The result is never absolute and never contains "..".
func SanitizePath(p string) string {
	return strings.ReplaceAll(sanitizePath(p), ":", "_")
}

func sanitizePath(p string) string {
	p = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, p)
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return ""
	}
	unsafe := strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') || strings.HasPrefix(p, "~")
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		unsafe = true
	}
	if unsafe {
		cleaned = path.Base(cleaned)
		if cleaned == "." || cleaned == ".." || cleaned == "/" || cleaned == "" {
			return ""
		}
		if len(cleaned) >= 2 && cleaned[1] == ':' {
			cleaned = cleaned[2:]
		}
		return cleaned
	}
	if cleaned == "." {
		return ""
	}
	return cleaned
}

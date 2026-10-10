package bicep

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Severity of a compiler diagnostic.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic is a normalised Bicep compiler diagnostic. Line and Column are
// 1-based; 0 means unknown. Diagnostics are exact (confidence "certain").
type Diagnostic struct {
	File     string   `json:"file"` // slash-separated, relative to the project root when possible
	Line     int      `json:"line,omitempty"`
	Column   int      `json:"column,omitempty"`
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	DocsURL  string   `json:"docsUrl,omitempty"`
}

const (
	maxMessage     = 1024
	maxDiagnostics = 4096
)

var (
	textDiagRE = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\) : (Error|Warning|Info) ([A-Za-z0-9_\-]+): (.*)$`)
	docsRE     = regexp.MustCompile(`\s*\[(https?://[^\s\]]+)\]\s*$`)
	ruleIDRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]{0,63}$`)
)

func severityOf(s string) Severity {
	switch strings.ToLower(s) {
	case "error":
		return SeverityError
	case "info", "note", "none":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// safePath returns a slash path relative to root when file is inside root,
// otherwise only the base name (never leaks absolute paths).
func safePath(root, file string) string {
	file = strings.TrimPrefix(file, "file:///")
	file = strings.TrimPrefix(file, "file://")
	if i := strings.IndexAny(file, "\x00\r\n"); i >= 0 {
		file = file[:i]
	}
	if file == "" {
		return ""
	}
	if root != "" && filepath.IsAbs(file) {
		if rel, err := filepath.Rel(root, file); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
	}
	if filepath.IsAbs(file) || strings.HasPrefix(file, "/") || (len(file) > 1 && file[1] == ':') {
		file = strings.ReplaceAll(file, "\\", "/")
		return file[strings.LastIndex(file, "/")+1:]
	}
	return filepath.ToSlash(filepath.Clean(file))
}

// ParseTextDiagnostics parses `file(line,col) : Level code: message` lines.
// Non-matching lines (banners, experimental-feature notices) are ignored.
func ParseTextDiagnostics(stderr, root string) []Diagnostic {
	var out []Diagnostic
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimRight(ansiRE.ReplaceAllString(line, ""), "\r")
		m := textDiagRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		l, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		msg := m[6]
		docs := ""
		if d := docsRE.FindStringSubmatch(msg); d != nil {
			docs = d[1]
			msg = docsRE.ReplaceAllString(msg, "")
		}
		out = append(out, Diagnostic{
			File: safePath(root, m[1]), Line: l, Column: c,
			Severity: severityOf(m[4]), Code: m[5],
			Message: sanitize(msg, maxMessage), DocsURL: docs,
		})
		if len(out) >= maxDiagnostics {
			break
		}
	}
	sortDiagnostics(out)
	return out
}

type sarifLog struct {
	Runs []struct {
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine   int `json:"startLine"`
						StartColumn int `json:"startColumn"`
						CharOffset  int `json:"charOffset"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// ParseSARIF parses the SARIF 2.1.0 log written by --diagnostics-format sarif.
// An absent level is treated as a warning. Bounded and strict: malformed JSON,
// too many runs/results, negative positions or invalid rule ids are errors.
func ParseSARIF(data []byte, root string) ([]Diagnostic, error) {
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, toolErr(KindAdapter, "malformed SARIF diagnostics", err)
	}
	if len(log.Runs) > 8 {
		return nil, toolErr(KindAdapter, "too many SARIF runs", nil)
	}
	var out []Diagnostic
	for _, run := range log.Runs {
		if len(run.Results) > maxDiagnostics {
			return nil, toolErr(KindAdapter, "too many SARIF results", nil)
		}
		for _, r := range run.Results {
			if !ruleIDRE.MatchString(r.RuleID) {
				return nil, toolErr(KindAdapter, "invalid SARIF rule id", nil)
			}
			if len(r.Locations) > 8 {
				return nil, toolErr(KindAdapter, "too many SARIF locations", nil)
			}
			d := Diagnostic{
				Severity: severityOf(r.Level), Code: r.RuleID,
				Message: sanitize(r.Message.Text, maxMessage),
			}
			if len(r.Locations) > 0 {
				pl := r.Locations[0].PhysicalLocation
				if pl.Region.StartLine < 0 || pl.Region.StartColumn < 0 || pl.Region.CharOffset < 0 {
					return nil, toolErr(KindAdapter, "negative SARIF position", nil)
				}
				d.File = safePath(root, pl.ArtifactLocation.URI)
				d.Line = pl.Region.StartLine
				d.Column = pl.Region.StartColumn
				if d.Column == 0 && pl.Region.CharOffset > 0 {
					d.Column = pl.Region.CharOffset
				}
			}
			out = append(out, d)
		}
	}
	sortDiagnostics(out)
	return out, nil
}

func sortDiagnostics(d []Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		a, b := d[i], d[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(d []Diagnostic) bool {
	for _, x := range d {
		if x.Severity == SeverityError {
			return true
		}
	}
	return false
}

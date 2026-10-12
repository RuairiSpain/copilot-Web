package annotate

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/location"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	FormatReview = "review"
	FormatGitHub = "github"
	FormatSARIF  = "sarif"

	ManifestSchemaVersion = "1"
	maxMessageBytes       = 240
)

// Entry is one annotation candidate.
type Entry struct {
	ID             string       `json:"id"`
	Kind           string       `json:"kind"`
	RuleID         string       `json:"ruleId"`
	Severity       sdk.Severity `json:"severity,omitempty"`
	Category       string       `json:"category,omitempty"`
	File           string       `json:"file,omitempty"`
	Line           int          `json:"line,omitempty"`
	Column         int          `json:"column,omitempty"`
	Message        string       `json:"message"`
	Recommendation string       `json:"recommendation,omitempty"`
	Fingerprint    string       `json:"fingerprint,omitempty"`
	Adapter        string       `json:"adapter,omitempty"`
	Inline         bool         `json:"inline"`
	InlineReason   string       `json:"inlineReason,omitempty"`
}

// Manifest describes generated review-copy annotations.
type Manifest struct {
	SchemaVersion string         `json:"schemaVersion"`
	Tool          string         `json:"tool"`
	ToolVersion   string         `json:"toolVersion"`
	Profile       string         `json:"profile"`
	ReviewDir     string         `json:"reviewDir"`
	DiffPath      string         `json:"diffPath,omitempty"`
	Summary       ManifestCounts `json:"summary"`
	Files         []ManifestFile `json:"files"`
	Annotations   []Entry        `json:"annotations"`
}

type ManifestCounts struct {
	Total        int `json:"total"`
	Inline       int `json:"inline"`
	ManifestOnly int `json:"manifestOnly"`
}

type ManifestFile struct {
	Path            string `json:"path"`
	ReviewPath      string `json:"reviewPath"`
	CommentStyle    string `json:"commentStyle"`
	AnnotationCount int    `json:"annotationCount"`
}

// Collect normalises findings and Bicep compiler diagnostics into one ordered
// stream of annotation entries.
func Collect(findingsIn []sdk.Finding, diagnostics []bicep.Diagnostic) []Entry {
	out := make([]Entry, 0, len(findingsIn)+len(diagnostics))
	for _, f := range findingsIn {
		rawFile := f.Location.File
		r := location.FromFinding(f)
		if !isProjectRelativePath(rawFile) {
			r.File = ""
		}
		id := f.Fingerprint
		if id == "" {
			id = findings.Fingerprint(f)
		}
		e := Entry{
			ID:             safeID(id),
			Kind:           "finding",
			RuleID:         findings.Redact(f.RuleID),
			Severity:       f.Severity,
			Category:       string(f.Category),
			File:           r.File,
			Line:           r.Line,
			Column:         r.Column,
			Message:        summarise(f.Evidence),
			Recommendation: summarise(f.Recommendation),
			Fingerprint:    findings.Redact(id),
			Adapter:        findings.Redact(f.Adapter),
		}
		if e.Message == "" {
			e.Message = summarise(f.Fix)
		}
		if e.Message == "" {
			e.Message = findings.Redact(f.RuleID)
		}
		e.Inline, e.InlineReason = inlineEligibility(rawFile, e)
		out = append(out, e)
	}
	for _, d := range diagnostics {
		rawFile := d.File
		r := location.FromDiagnostic(d)
		if !isProjectRelativePath(rawFile) {
			r.File = ""
		}
		e := Entry{
			ID:           safeID(fmt.Sprintf("bicep-%s-%s-%d-%d", d.Code, r.File, r.Line, r.Column)),
			Kind:         "bicep-diagnostic",
			RuleID:       findings.Redact(d.Code),
			Severity:     sdk.Severity(strings.ToLower(string(d.Severity))),
			File:         r.File,
			Line:         r.Line,
			Column:       r.Column,
			Message:      summarise(d.Message),
			Adapter:      "bicep",
			InlineReason: "",
		}
		e.Inline, e.InlineReason = inlineEligibility(rawFile, e)
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		return a.Message < b.Message
	})
	return out
}

func inlineEligibility(rawFile string, e Entry) (bool, string) {
	if e.File == "" || e.Line <= 0 {
		return false, "imprecise-location"
	}
	if !isProjectRelativePath(rawFile) {
		return false, "outside-project"
	}
	switch strings.ToLower(path.Ext(e.File)) {
	case ".yaml", ".yml":
		return true, ""
	case ".bicep":
		if e.Kind == "bicep-diagnostic" {
			return true, ""
		}
		return false, "untrusted-bicep-location"
	default:
		return false, "unsupported-file-type"
	}
}

func isProjectRelativePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "~") {
		return false
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "/") {
		return false
	}
	if len(p) >= 2 && p[1] == ':' {
		return false
	}
	cleaned := path.Clean(p)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "/../")
}

func summarise(s string) string {
	s = findings.Redact(s)
	s = strings.ToValidUTF8(s, "\uFFFD")
	var b strings.Builder
	space := false
	prevColon := false
	for _, r := range s {
		switch {
		case r == '%':
			b.WriteString("%25")
			space = false
			prevColon = false
		case r == ':':
			if prevColon {
				b.WriteRune('∷')
				prevColon = false
			} else {
				b.WriteRune(':')
				prevColon = true
			}
			space = false
		case r == '\r' || r == '\n' || unicode.IsControl(r):
			if !space {
				b.WriteByte(' ')
			}
			space = true
			prevColon = false
		default:
			b.WriteRune(r)
			space = false
			prevColon = false
		}
		if b.Len() >= maxMessageBytes {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return ""
	}
	if len(out) > maxMessageBytes {
		return trimToMaxBytes(out, maxMessageBytes-len("…")) + "…"
	}
	return out
}

func trimToMaxBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			s = s[:len(s)-1]
		} else {
			s = s[:len(s)-size]
		}
		if len(s) <= max {
			return s
		}
	}
	return ""
}

func safeID(id string) string {
	id = summarise(id)
	id = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, id)
	id = strings.Trim(id, "._-")
	if id == "" {
		return "annotation"
	}
	return id
}

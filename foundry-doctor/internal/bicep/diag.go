package bicep

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Diagnostic source and the code of the informational directive diagnostic.
const (
	DiagSource           = "bicep"
	CodeDisableDirective = "disable-next-line"
	maxMessage           = 1000
)

// Text format, verified on Bicep 0.47.16 and 0.24.24 (docs/development/bicep-adapter.md):
//
//	<path>(<line>,<col>) : <Error|Warning|Info> <code>: <message> [<docs url>]
//
// The text form is used instead of SARIF because the compiler's SARIF has no start column and
// no level for warnings. Lines that do not match (experimental-feature notices, banners) are
// ignored.
var (
	diagRe = regexp.MustCompile(`^(.+?)(?:\((\d+),(\d+)\))? : (Error|Warning|Info) ([A-Za-z0-9_.-]+): (.*)$`)
	urlRe  = regexp.MustCompile(`\s*\[(https://aka\.ms/bicep/[^\]\s]+)\]\s*$`)
)

// ParsedDiagnostic is one compiler diagnostic with its absolute-path file, before relativising.
type ParsedDiagnostic struct {
	File     string
	Line     int
	Column   int
	Severity sdk.Severity
	Code     string
	Message  string
	DocsURL  string
}

// ParseDiagnostics extracts diagnostics from compiler stderr. Order is preserved.
func ParseDiagnostics(stderr string) []ParsedDiagnostic {
	var out []ParsedDiagnostic
	sc := bufio.NewScanner(strings.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		m := diagRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		d := ParsedDiagnostic{File: m[1], Code: m[5], Message: m[6]}
		d.Line, _ = strconv.Atoi(m[2])
		d.Column, _ = strconv.Atoi(m[3])
		switch m[4] {
		case "Error":
			d.Severity = sdk.SeverityError
		case "Warning":
			d.Severity = sdk.SeverityWarning
		default:
			d.Severity = sdk.SeverityInfo
		}
		if u := urlRe.FindStringSubmatch(d.Message); u != nil {
			d.DocsURL = u[1]
			d.Message = urlRe.ReplaceAllString(d.Message, "")
		}
		out = append(out, d)
	}
	return out
}

// DocsURL returns the documentation URL the compiler prints for a code. BCP codes use the
// core-diagnostics page, linter rule ids the linter page.
func DocsURL(code string) string {
	if code == "" || code == CodeDisableDirective {
		return ""
	}
	if strings.HasPrefix(code, "BCP") {
		return "https://aka.ms/bicep/core-diagnostics#" + code
	}
	return "https://aka.ms/bicep/linter-diagnostics#" + code
}

// toModel converts a parsed diagnostic into the model type: path made relative to root with
// forward slashes (a path outside root keeps only its base name), message cleaned and bounded,
// absolute root prefixes removed.
func toModel(p ParsedDiagnostic, root string) model.Diagnostic {
	return model.Diagnostic{
		Source:   DiagSource,
		Code:     p.Code,
		Severity: p.Severity,
		Message:  cleanMessage(p.Message, root),
		File:     relPath(root, p.File),
		Pos:      model.Pos{Line: p.Line, Column: p.Column},
	}
}

func relPath(root, file string) string {
	if file == "" {
		return ""
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	if root != "" {
		if rel, err := filepath.Rel(root, file); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(file)
}

func cleanMessage(msg, root string) string {
	if root != "" {
		msg = strings.ReplaceAll(msg, filepath.ToSlash(root)+"/", "")
		msg = strings.ReplaceAll(msg, root+string(filepath.Separator), "")
	}
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, msg)
	if len(msg) > maxMessage {
		msg = strings.ToValidUTF8(msg[:maxMessage], "") + "..."
	}
	return strings.TrimSpace(msg)
}

// HasError reports whether any diagnostic is an error.
func HasError(ds []model.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == sdk.SeverityError {
			return true
		}
	}
	return false
}

// directiveRe matches the compiler directive on a line of its own. This is a line scan for one
// documented comment directive, not a Bicep parser.
var directiveRe = regexp.MustCompile(`^\s*#disable-next-line\b(.*)$`)

// ScanDisableDirectives lists the `#disable-next-line` directives of one Bicep file as
// informational diagnostics (code "disable-next-line"). The compiler drops the suppressed
// diagnostics, so without this a suppression would be invisible. root is used to relativise.
func ScanDisableDirectives(root, file string) ([]model.Diagnostic, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("scan directives in %s: %w", filepath.Base(file), err)
	}
	defer func() { _ = f.Close() }()
	rel := relPath(root, file)
	var out []model.Diagnostic
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for n := 1; sc.Scan(); n++ {
		m := directiveRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		codes := strings.Fields(m[1])
		for i, c := range codes { // a trailing comment is not a code
			if strings.HasPrefix(c, "//") {
				codes = codes[:i]
				break
			}
		}
		msg := "A #disable-next-line directive suppresses compiler diagnostics on the next line"
		if len(codes) > 0 {
			msg += ": " + cleanMessage(strings.Join(codes, ", "), root)
		}
		out = append(out, model.Diagnostic{
			Source: DiagSource, Code: CodeDisableDirective, Severity: sdk.SeverityInfo,
			Message: msg, File: rel, Pos: model.Pos{Line: n, Column: 1},
		})
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("scan directives in %s: %w", filepath.Base(file), err)
	}
	return out, nil
}

// ToFinding normalises one compiler diagnostic into a Finding (ADR-009): ID bicep/<code>,
// severity as the compiler reported it, confidence certain for the diagnostic's own location.
// The engine stamps profile, category and fingerprint. Rule logic is never re-implemented here.
func ToFinding(d model.Diagnostic, profile string) sdk.Finding {
	f := sdk.Finding{
		RuleID:      "bicep/" + d.Code,
		RuleVersion: 1,
		Severity:    d.Severity,
		Profile:     profile,
		Evidence:    d.Message,
		DocsURL:     DocsURL(d.Code),
		Confidence:  sdk.ConfidenceCertain,
		Adapter:     ToolName,
		Key:         d.Code,
		Resource:    sdk.ResourceRef{Kind: "file", Name: d.File},
		Location:    sdk.Location{File: d.File, Line: d.Pos.Line, Column: d.Pos.Column},
	}
	f.Recommendation = "Fix the compiler diagnostic; see the linked documentation."
	if d.Code == CodeDisableDirective {
		f.Recommendation = "Review that the suppression is still justified; the doctor does not hide it."
	}
	return f
}

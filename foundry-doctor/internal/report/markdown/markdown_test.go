package markdown_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/markdown"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/reporttest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func render(t *testing.T, r *sdk.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := markdown.New().Write(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	reporttest.Golden(t, "report.golden.md", []byte(render(t, reporttest.FullReport())))
}

func TestFormat(t *testing.T) {
	if markdown.New().Format() != "markdown" {
		t.Fatal("format")
	}
}

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a|b", `a\|b`},
		{"line1\nline2", "line1 line2"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert\\(1\\)&lt;/script&gt;"},
		{"[x](http://evil)", `\[x\]\(http://evil\)`},
		{"@octocat", "&#64;octocat"},
		{"a&b", "a&amp;b"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"`code`", "\\`code\\`"},
		{"日本語 \U0001F600", "日本語 \U0001F600"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := markdown.Escape(tc.in); got != tc.want {
			t.Errorf("Escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTablesStayRectangular(t *testing.T) {
	out := render(t, reporttest.FullReport())
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		// Unescaped pipes only: remove escaped ones and count.
		n := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|")
		if n < 3 {
			t.Errorf("odd row: %q", line)
		}
	}
	// The finding with a pipe, HTML and a link in its evidence stays inside one cell.
	if strings.Contains(out, "<b>") || strings.Contains(out, "](http://evil") && !strings.Contains(out, `\]\(http://evil`) {
		t.Errorf("markup injected:\n%s", out)
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, reporttest.Canary) {
		t.Error("escape or canary leaked")
	}
}

func TestDocsLink(t *testing.T) {
	out := render(t, reporttest.FullReport())
	if !strings.Contains(out, "[docs](https://learn.microsoft.com/azure/ai-services/)") {
		t.Errorf("https docs link missing:\n%s", out)
	}
	for _, bad := range []string{"javascript:alert(1)", "http://insecure.example", "https://a b.example"} {
		r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "X", Severity: sdk.SeverityInfo, DocsURL: bad, Evidence: "e"}}}
		o := render(t, r)
		if strings.Contains(o, "[docs](") {
			t.Errorf("link rendered for %q:\n%s", bad, o)
		}
	}
}

func TestDocsLinkEncodesBreakers(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "X", Severity: sdk.SeverityInfo, DocsURL: "https://x.example/)![x](y", Evidence: "e"}}}
	if o := render(t, r); strings.Contains(o, ")![x](") {
		t.Errorf("link broken out:\n%s", o)
	}
}

func TestSections(t *testing.T) {
	out := render(t, reporttest.FullReport())
	for _, want := range []string{"## Effective policy", "## Findings (5)", "## Skipped checks (3)", "## Owner summary",
		"profile-key-missing:policy.allowedExternalScopes", "Skipped is not passed", "compiled ARM template", "(no file)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestEmptyAndNil(t *testing.T) {
	for _, r := range []*sdk.Report{nil, {}} {
		out := render(t, r)
		if !strings.Contains(out, "## Findings (0)") || !strings.Contains(out, "None.") || !strings.Contains(out, "Skipped is not passed") {
			t.Errorf("empty report:\n%s", out)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteError(t *testing.T) {
	if markdown.New().Write(failWriter{}, &sdk.Report{}) == nil {
		t.Fatal("want error")
	}
}

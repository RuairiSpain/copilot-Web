package console_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/console"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/reporttest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func render(t *testing.T, opts console.Options, r *sdk.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := console.New(opts).Write(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	reporttest.Golden(t, "console.golden", []byte(render(t, console.Options{}, reporttest.FullReport())))
}

func TestFormat(t *testing.T) {
	if got := console.New(console.Options{}).Format(); got != "console" {
		t.Fatalf("Format = %q", got)
	}
}

func TestContent(t *testing.T) {
	out := render(t, console.Options{}, reporttest.FullReport())
	for _, want := range []string{
		"Profile: foundry-prod",
		"validation.bicep = required",
		"missing capability: compiled ARM template",
		"profile-key-missing:policy.allowedExternalScopes: FND-IDN-004, FND-IDN-005",
		"Hidden by --min-severity: 4",
		"Skipped is not passed",
		"(no file)",
		"[baselined]",
		"[suppressed until 2027-01-01]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, reporttest.Canary) {
		t.Errorf("escape or canary leaked:\n%s", out)
	}
	// Errors are listed before warnings within a file.
	if strings.Index(out, "FND-SEC-002 @12:5") > strings.Index(out, "FND-NET-001 @40:3") {
		t.Errorf("severity order wrong:\n%s", out)
	}
}

func TestColor(t *testing.T) {
	out := render(t, console.Options{Color: true}, reporttest.FullReport())
	if !strings.Contains(out, "\x1b[31merror\x1b[0m") {
		t.Errorf("colour missing:\n%s", out)
	}
	plain := render(t, console.Options{}, reporttest.FullReport())
	if strings.Contains(plain, "\x1b") {
		t.Error("colour on by default")
	}
}

func TestEmptyAndNil(t *testing.T) {
	for _, r := range []*sdk.Report{nil, {}} {
		out := render(t, console.Options{}, r)
		for _, want := range []string{"Findings (0)", "Skipped checks (0)", "Owner summary", "Skipped is not passed", "(none)"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in\n%s", want, out)
			}
		}
	}
}

func TestUnknownSeverity(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "X", Evidence: "e"}}}
	if out := render(t, console.Options{Color: true}, r); !strings.Contains(out, "unknown") {
		t.Errorf("unknown severity not labelled:\n%s", out)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteError(t *testing.T) {
	if err := console.New(console.Options{}).Write(failWriter{}, reporttest.FullReport()); err == nil {
		t.Fatal("want error")
	}
}

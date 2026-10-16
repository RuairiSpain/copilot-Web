package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/baseline"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type captureReporter struct{ got Report }

func (c *captureReporter) Render(w io.Writer, _ string, r Report) error {
	c.got = r
	_, err := io.WriteString(w, "ok\n")
	return err
}

func fp(c string) string { return strings.Repeat(c, 32) }

func TestDoctorCountsFindingsHiddenByMinSeverity(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{
		{RuleID: "A", Severity: sdk.SeverityWarning, Fingerprint: fp("a")},
		{RuleID: "B", Severity: sdk.SeverityInfo, Fingerprint: fp("b")},
		{RuleID: "C", Severity: sdk.SeverityError, Fingerprint: fp("c")},
	}}}
	svc, _ := newSvc(eng)
	rep := &captureReporter{}
	svc.Reporter = rep
	code, err := Doctor(context.Background(), svc, DoctorRequest{MinSeverity: sdk.SeverityError}, io.Discard)
	if err != nil || code != ExitFindings {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if rep.got.Hidden != 2 || len(rep.got.Findings) != 1 {
		t.Fatalf("hidden=%d shown=%d, want 2 and 1", rep.got.Hidden, len(rep.got.Findings))
	}
	// A warning hidden by the filter must not change the exit code or vanish.
	eng.out.Findings = eng.out.Findings[:1]
	code, _ = Doctor(context.Background(), svc, DoctorRequest{MinSeverity: sdk.SeverityError}, io.Discard)
	if code != ExitOK || rep.got.Hidden != 1 || len(rep.got.Findings) != 0 {
		t.Fatalf("code=%d hidden=%d shown=%d", code, rep.got.Hidden, len(rep.got.Findings))
	}
}

func TestValidateWriteBaselineFlags(t *testing.T) {
	for name, r := range map[string]DoctorRequest{
		"force alone":         {Force: true},
		"write with baseline": {WriteBaseline: "b.yaml", Baseline: "x.yaml"},
	} {
		if err := r.Validate(); err == nil || ExitCodeForError(err) != ExitUnavailable {
			t.Errorf("%s: err = %v, want usage error", name, err)
		}
	}
	r := DoctorRequest{WriteBaseline: "b.yaml", Force: true}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorWriteBaseline(t *testing.T) {
	dir := t.TempDir()
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{
		{RuleID: "B", Severity: sdk.SeverityError, Fingerprint: fp("b"), Evidence: "evidence text at /abs/path"},
		{RuleID: "A", Severity: sdk.SeverityWarning, Fingerprint: fp("a")},
		{RuleID: "NOFP", Severity: sdk.SeverityWarning},
	}}}
	svc, _ := newSvc(eng)
	rep := &captureReporter{}
	svc.Reporter = rep
	req := DoctorRequest{Dir: dir, WriteBaseline: baseline.DefaultPath}

	var out bytes.Buffer
	code, err := Doctor(context.Background(), svc, req, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if rep.got.Profile != "" || !strings.Contains(out.String(), "2 entries") {
		t.Fatalf("report must not be rendered; out=%q", out.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(baseline.DefaultPath)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := baseline.Parse(data)
	if err != nil || len(f.Entries) != 2 {
		t.Fatalf("entries=%v err=%v", f, err)
	}
	if strings.Contains(string(data), "evidence") || strings.Contains(string(data), "/abs") {
		t.Fatalf("baseline leaks evidence:\n%s", data)
	}

	// Second run without --force is a usage error (2) and leaves the file alone.
	code, err = Doctor(context.Background(), svc, req, io.Discard)
	if code != ExitUnavailable || err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if again, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(baseline.DefaultPath))); !bytes.Equal(again, data) {
		t.Fatal("existing baseline was modified")
	}
	req.Force = true
	if code, err = Doctor(context.Background(), svc, req, io.Discard); code != ExitOK || err != nil {
		t.Fatalf("force: code=%d err=%v", code, err)
	}

	// An escaping path is a usage error too.
	req.WriteBaseline = "../x.yaml"
	if code, _ = Doctor(context.Background(), svc, req, io.Discard); code != ExitUnavailable {
		t.Fatalf("escape: code=%d", code)
	}
}

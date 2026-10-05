package sdk_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeAdapter struct{}

func (fakeAdapter) Name() string { return "fake" }
func (fakeAdapter) Detect(context.Context) sdk.ToolStatus {
	return sdk.ToolStatus{Name: "fake", State: sdk.ToolAvailable}
}
func (fakeAdapter) Run(context.Context, sdk.AdapterRequest) ([]sdk.Finding, error) {
	return nil, nil
}

type fakeReporter struct{}

func (fakeReporter) Format() string                     { return "fake" }
func (fakeReporter) Write(io.Writer, *sdk.Report) error { return nil }

var (
	_ sdk.Adapter  = fakeAdapter{}
	_ sdk.Reporter = fakeReporter{}
)

func TestZeroFindingJSON(t *testing.T) {
	b, err := json.Marshal(sdk.Finding{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"ruleId":"","ruleVersion":0,"severity":"","category":"","profile":"","evidence":"","recommendation":"","confidence":"","baselined":false,"fingerprint":""}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestZeroReportJSON(t *testing.T) {
	b, err := json.Marshal(sdk.Report{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schemaVersion":"","tool":{"name":"","version":""},"profile":"","effectivePolicy":null,"tools":null,"findings":null,"skipped":null,"summary":{"info":0,"warning":0,"error":0,"suppressed":0,"baselined":0,"skipped":0,"passed":0,"hidden":0,"skippedByReason":null},"exitCode":0}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestSeverityOrdering(t *testing.T) {
	if !sdk.SeverityError.AtLeast(sdk.SeverityWarning) || sdk.SeverityInfo.AtLeast(sdk.SeverityWarning) {
		t.Fatal("ordering wrong")
	}
	if sdk.Severity("").AtLeast(sdk.SeverityInfo) {
		t.Fatal("invalid severity must not meet a threshold")
	}
	if sdk.CompareSeverity(sdk.SeverityInfo, sdk.SeverityError) != -1 {
		t.Fatal("compare wrong")
	}
	if _, err := sdk.ParseSeverity("fatal"); err == nil {
		t.Fatal("want error")
	}
}

func TestExitCodes(t *testing.T) {
	got := []int{sdk.ExitOK, sdk.ExitFindings, sdk.ExitCannotRun, sdk.ExitSkippedStrict, sdk.ExitInternal}
	for i, v := range got {
		if v != i {
			t.Fatalf("exit code %d = %d", i, v)
		}
	}
}

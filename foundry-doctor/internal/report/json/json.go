// Package json renders the sdk.Report as the versioned JSON report (schemas/report.schema.json).
package json

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"io"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/norm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Reporter is the JSON sdk.Reporter.
type Reporter struct{}

var _ sdk.Reporter = Reporter{}

// New returns a JSON reporter.
func New() Reporter { return Reporter{} }

// Format returns "json".
func (Reporter) Format() string { return "json" }

// Write renders r as indented JSON with a trailing newline. The report is the sdk.Report itself
// with three normalisations: slices and maps are non-nil, findings and skipped checks are in
// canonical order, and policy values under credential-like keys are redacted.
func (Reporter) Write(w io.Writer, r *sdk.Report) error {
	rep := norm.Normalised(r)
	var buf bytes.Buffer
	enc := stdjson.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&rep); err != nil {
		return fmt.Errorf("encode json report: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write json report: %w", err)
	}
	return nil
}

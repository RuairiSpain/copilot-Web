package report

import (
	"encoding/json"
	"io"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// JSONSchemaVersion is the version of the JSON report layout.
const JSONSchemaVersion = "1"

type jsonSummary struct {
	Error      int `json:"error"`
	Warning    int `json:"warning"`
	Info       int `json:"info"`
	Suppressed int `json:"suppressed"`
	Baselined  int `json:"baselined"`
	Skipped    int `json:"skipped"`
}

type jsonReport struct {
	SchemaVersion string        `json:"schemaVersion"`
	Tool          string        `json:"tool"`
	ToolVersion   string        `json:"toolVersion"`
	Profile       string        `json:"profile"`
	GeneratedAt   string        `json:"generatedAt,omitempty"`
	ExitCode      int           `json:"exitCode"`
	Summary       jsonSummary   `json:"summary"`
	Findings      []sdk.Finding `json:"findings"`
	Skipped       []sdk.Skip    `json:"skipped"`
}

// JSON writes the full machine-readable report, including suppressed and
// baselined findings (flagged) and skipped checks.
func JSON(w io.Writer, fs []sdk.Finding, run Run) error {
	p := prepared(fs)
	skips := preparedSkips(run.Skipped)
	c := count(p)
	r := jsonReport{
		SchemaVersion: JSONSchemaVersion,
		Tool:          ToolName,
		ToolVersion:   run.ToolVersion,
		Profile:       run.Profile,
		GeneratedAt:   run.GeneratedAt,
		ExitCode:      run.ExitCode,
		Summary:       jsonSummary{c.Error, c.Warning, c.Info, c.Suppressed, c.Baselined, len(skips)},
		Findings:      p,
		Skipped:       skips,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

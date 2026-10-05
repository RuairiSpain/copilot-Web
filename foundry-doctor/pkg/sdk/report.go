package sdk

import "io"

// ReportSchemaVersion is the version of the JSON report layout. Bump it on any breaking change.
const ReportSchemaVersion = "1"

// Exit codes (PRD section 7).
const (
	ExitOK            = 0 // no unsuppressed finding at or above --fail-on
	ExitFindings      = 1 // at least one finding meets or exceeds the threshold
	ExitCannotRun     = 2 // input, dependency, authentication or permission unavailable
	ExitSkippedStrict = 3 // a check was skipped and --strict was given
	ExitInternal      = 4 // internal error or adapter protocol failure
)

// ToolInfo identifies the producer of a report.
type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Summary counts findings. Counts cover unsuppressed, non-baselined findings unless the name says otherwise.
type Summary struct {
	Info       int `json:"info"`
	Warning    int `json:"warning"`
	Error      int `json:"error"`
	Suppressed int `json:"suppressed"`
	Baselined  int `json:"baselined"`
	Skipped    int `json:"skipped"`
	Passed     int `json:"passed"`
	// Hidden counts findings removed from display by --min-severity; they still count towards the exit code.
	Hidden int `json:"hidden"`
	// SkippedByReason counts skipped checks by reason (ADR-012). Map keys are sorted by encoding/json.
	SkippedByReason map[string]int `json:"skippedByReason"`
}

// Report is the complete, deterministic result of one run. It has no wall-clock field and no
// absolute path; the same inputs produce byte-identical output. Producers set slices and maps to
// non-nil empty values so that the JSON shows [] and {} rather than null.
type Report struct {
	SchemaVersion   string         `json:"schemaVersion"`
	Tool            ToolInfo       `json:"tool"`
	Profile         string         `json:"profile"`
	EffectivePolicy map[string]any `json:"effectivePolicy"`
	Tools           []ToolStatus   `json:"tools"`
	Findings        []Finding      `json:"findings"`
	Skipped         []SkippedCheck `json:"skipped"`
	Summary         Summary        `json:"summary"`
	ExitCode        int            `json:"exitCode"`
}

// Reporter renders a Report in one format. Output must be deterministic.
type Reporter interface {
	// Format is the --format value, for example "json" or "sarif".
	Format() string
	Write(w io.Writer, r *Report) error
}

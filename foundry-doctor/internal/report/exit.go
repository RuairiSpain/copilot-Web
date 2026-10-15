package report

import "github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"

// Exit codes (PRD section 7).
const (
	ExitOK            = 0
	ExitFindings      = 1
	ExitCannotRun     = 2
	ExitStrictSkipped = 3
	ExitInternal      = 4
)

// Outcome is the run outcome used to classify the exit code. It is based on
// what happened, never on an external tool's numeric exit code.
type Outcome struct {
	// Findings are the final findings after baseline/suppression resolution.
	// Suppressed and baselined findings never count.
	Findings []sdk.Finding
	// FailOn is the --fail-on threshold. Empty or invalid means error.
	FailOn sdk.Severity
	// Strict is --strict: any skipped check yields exit 3.
	Strict bool
	// Skipped lists skipped checks. A Required skip (input, dependency,
	// authentication or permission unavailable) means validation could not run.
	Skipped []sdk.Skip
	// CannotRun is set when the requested validation could not run for a
	// reason that is not a per-rule skip (missing project, Bicep CLI, auth).
	CannotRun bool
	// InternalError is set for internal errors and adapter protocol failures.
	InternalError bool
}

// ClassifyExit maps an outcome to an exit code. Precedence: internal error (4),
// validation could not run (2, over findings), strict skip (3), findings (1),
// otherwise 0. Optional skips without --strict do not change the code.
func ClassifyExit(o Outcome) int {
	if o.InternalError {
		return ExitInternal
	}
	if o.CannotRun {
		return ExitCannotRun
	}
	for _, s := range o.Skipped {
		if s.Required {
			return ExitCannotRun
		}
	}
	if o.Strict && len(o.Skipped) > 0 {
		return ExitStrictSkipped
	}
	threshold := o.FailOn
	if !threshold.Valid() {
		threshold = sdk.SeverityError
	}
	for _, f := range o.Findings {
		if f.Suppressed == nil && !f.Baselined && f.Severity.Rank() >= threshold.Rank() {
			return ExitFindings
		}
	}
	return ExitOK
}

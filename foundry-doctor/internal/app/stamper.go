package app

import (
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// stamper adapts internal/findings to rules.Stamper. Fingerprint sanitises the finding first, so the
// fingerprint is computed over the same Key, resource and location text that the report shows
// (ADR-008; Sanitize before Fingerprint).
type stamper struct{}

var _ rules.Stamper = stamper{}

// NewStamper returns the rules.Stamper backed by internal/findings.
func NewStamper() rules.Stamper { return stamper{} }

// Fingerprint implements rules.Stamper.
func (stamper) Fingerprint(f sdk.Finding) string {
	return findings.Fingerprint(findings.Sanitize(f))
}

// Redact implements rules.Stamper.
func (stamper) Redact(s string) string { return findings.Redact(s) }

// seal sanitises a finding and then fingerprints it. It is idempotent.
func seal(f sdk.Finding) sdk.Finding {
	f = findings.Sanitize(f)
	f.Fingerprint = findings.Fingerprint(f)
	return f
}

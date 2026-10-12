package net_test

import (
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// findingsStamper adapts the real fingerprint and redaction code to rules.Stamper.
type findingsStamper struct{}

func (findingsStamper) Fingerprint(f sdk.Finding) string { return findings.Fingerprint(f) }
func (findingsStamper) Redact(s string) string           { return findings.Redact(s) }

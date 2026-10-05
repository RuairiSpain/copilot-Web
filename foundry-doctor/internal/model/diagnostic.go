package model

import "github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"

// Diagnostic is a message from a parser or the Bicep compiler, mapped to a finding by the engine (ADR-009).
type Diagnostic struct {
	Source   string // "azureyaml", "bicep", "engine"
	Code     string // for example "BCP035" or "invalid-azure-yaml"
	Severity sdk.Severity
	Message  string // already redacted
	File     string
	Pos      Pos
}

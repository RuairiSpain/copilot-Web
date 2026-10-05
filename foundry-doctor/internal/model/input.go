package model

import "github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"

// Input is the immutable bundle every rule reads. Rules must not modify it.
type Input struct {
	Project     Project
	Profile     string        // foundry-dev, foundry-test or foundry-prod
	AzureYAML   *AzureYAML    // nil if azure.yaml could not be parsed (an engine diagnostic exists)
	YAMLReport  *YAMLAnalysis // duplicates, interpolations, extension blocks and structural issues; nil with AzureYAML
	ARM         *ARMTemplate  // nil unless Project.HasARM(); rules that need it skip with synthetic-infrastructure
	Graph       Graph
	Environment *Environment // the selected azd environment; nil if none or unavailable
	Policy      Policy       // the effective, merged policy (ADR-007)
	Versions    Versions
	Tools       []sdk.ToolStatus
	Diagnostics []Diagnostic
}

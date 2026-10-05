package engine

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// AssembleInput is everything Assemble needs. Findings are stamped, include engine diagnostics and
// adapter findings, and carry Suppressed and Baselined as set by the suppression and baseline stages.
type AssembleInput struct {
	Tool            sdk.ToolInfo
	Profile         string // long form
	EffectivePolicy map[string]any
	Tools           []sdk.ToolStatus
	Findings        []sdk.Finding
	Skipped         []sdk.SkippedCheck
	Passed          int
	MinSeverity     sdk.Severity // display filter; default info
	FailOn          sdk.Severity // exit-1 threshold; default error
	Strict          bool         // any skipped check gives exit 3
	// CannotRun means input, dependency, authentication or permission was unavailable (exit 2).
	// A required tool that is not available also gives exit 2.
	CannotRun bool
	// Internal means an internal error or adapter protocol failure (exit 4), for example any rules.RuleError.
	Internal bool
}

// ExitCode applies ADR-010 decision 2: 4, then 2, then 1, then 3, then 0. Exit 1 looks at every
// unsuppressed, non-baselined finding, whatever --min-severity displays.
func ExitCode(findings []sdk.Finding, skipped int, failOn sdk.Severity, strict, cannotRun, internal bool) int {
	if !failOn.Valid() {
		failOn = sdk.SeverityError
	}
	switch {
	case internal:
		return sdk.ExitInternal
	case cannotRun:
		return sdk.ExitCannotRun
	}
	for _, f := range findings {
		if f.Suppressed == nil && !f.Baselined && f.Severity.AtLeast(failOn) {
			return sdk.ExitFindings
		}
	}
	if strict && skipped > 0 {
		return sdk.ExitSkippedStrict
	}
	return sdk.ExitOK
}

// Assemble builds the deterministic report. Slices and maps are never nil; findings, skipped checks and
// tools are sorted; tool paths are reduced to a base name. Summary counting:
//   - Suppressed and Baselined count every such finding, before the display filter.
//   - Info, Warning and Error count displayed, live findings; Hidden counts live findings below MinSeverity.
//   - Findings below MinSeverity are removed from the list (suppressed and baselined ones included).
func Assemble(in AssembleInput) sdk.Report {
	minSev := in.MinSeverity
	if !minSev.Valid() {
		minSev = sdk.SeverityInfo
	}
	all := slices.Clone(in.Findings)
	rules.SortFindings(all)
	skipped := append([]sdk.SkippedCheck{}, in.Skipped...)
	rules.SortSkipped(skipped)

	sum := sdk.Summary{Skipped: len(skipped), Passed: in.Passed, SkippedByReason: rules.SkipSummary(skipped)}
	shown := make([]sdk.Finding, 0, len(all))
	for _, f := range all {
		live := f.Suppressed == nil && !f.Baselined
		switch {
		case f.Suppressed != nil:
			sum.Suppressed++
		case f.Baselined:
			sum.Baselined++
		}
		if !f.Severity.AtLeast(minSev) {
			if live {
				sum.Hidden++
			}
			continue
		}
		shown = append(shown, f)
		if live {
			switch f.Severity {
			case sdk.SeverityInfo:
				sum.Info++
			case sdk.SeverityWarning:
				sum.Warning++
			case sdk.SeverityError:
				sum.Error++
			}
		}
	}

	tools := redactTools(in.Tools)
	cannotRun := in.CannotRun
	for _, t := range tools {
		if t.Required && t.State != sdk.ToolAvailable && t.State != sdk.ToolFailed {
			cannotRun = true
		}
	}
	policy := in.EffectivePolicy
	if policy == nil {
		policy = map[string]any{}
	}
	return sdk.Report{
		SchemaVersion: sdk.ReportSchemaVersion, Tool: in.Tool, Profile: in.Profile,
		EffectivePolicy: policy, Tools: tools, Findings: shown, Skipped: skipped, Summary: sum,
		ExitCode: ExitCode(all, len(skipped), in.FailOn, in.Strict, cannotRun, in.Internal),
	}
}

func redactTools(ts []sdk.ToolStatus) []sdk.ToolStatus {
	out := make([]sdk.ToolStatus, len(ts))
	for i, t := range ts {
		if t.Path != "" {
			p := strings.ReplaceAll(t.Path, `\`, "/")
			t.Path = p[strings.LastIndexByte(p, '/')+1:]
		}
		out[i] = t
	}
	slices.SortStableFunc(out, func(a, b sdk.ToolStatus) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// EffectivePolicy lists the value of every ADR-007 key, baselines applied, for the report header.
// A key with no value and no baseline is nil (JSON null). Keys are the model.Key* names.
func EffectivePolicy(p model.Policy) map[string]any {
	deref := func(b *bool) any {
		if b == nil {
			return nil
		}
		return *b
	}
	list := func(l []string) any {
		if l == nil {
			return nil
		}
		return slices.Clone(l)
	}
	baseList := func(l []string) any {
		if l == nil {
			return []string{}
		}
		return slices.Clone(l)
	}
	m := map[string]any{
		model.KeyResourceScope:             string(p.EffectiveResourceScope()),
		model.KeyAllowedExternalScopes:     baseList(p.AllowedExternalScopes),
		model.KeyEnvironmentsProduction:    list(p.Environments.Production),
		model.KeyEnvironmentsNonProduction: list(p.Environments.NonProduction),
		model.KeyEnvironmentsDevelopment:   list(p.Environments.Development),
		model.KeyTagsResourceTypes:         "rule-default",
		model.KeyLogRetentionMinimumDays:   nil,
		model.KeyModelsAllow:               list(p.Models.Allow),
		model.KeyModelsDeny:                list(p.Models.Deny),
		model.KeyDataResidencyScope:        nil,
		model.KeyDataResidencyRegions:      list(p.DataResidency.Regions),
		// Absent deployment SKUs mean "the SKUs whose documented scope satisfies the scope" (ADR-007).
		model.KeyDataResidencyDeploymentSkus:    "rule-default",
		model.KeyDisasterRecoveryDeclared:       deref(p.DisasterRecovery.Declared),
		model.KeyNetworkPublicAccess:            string(p.EffectivePublicAccess()),
		model.KeyMonitoringPublicTelemetry:      p.EffectivePublicTelemetry(),
		model.KeyManagedByAzurePolicy:           managedList(p.ManagedByAzurePolicy),
		model.KeyKnowledgeRequireDocLevelAccess: deref(p.Knowledge.RequireDocumentLevelAccess),
		model.KeyCostDevMaxCosmosThroughput:     nil,
		model.KeyCostProductionSizedSkuExempt:   baseList(p.Cost.ProductionSizedSkuExemptions),
		model.KeyTagsRequired:                   nil,
	}
	if p.LogRetention.MinimumDays != nil {
		m[model.KeyLogRetentionMinimumDays] = *p.LogRetention.MinimumDays
	}
	if p.DataResidency.Scope != nil {
		m[model.KeyDataResidencyScope] = string(*p.DataResidency.Scope)
	}
	if p.Cost.DevMaxCosmosThroughput != nil {
		m[model.KeyCostDevMaxCosmosThroughput] = *p.Cost.DevMaxCosmosThroughput
	}
	if p.Tags.ResourceTypes != nil {
		m[model.KeyTagsResourceTypes] = slices.Clone(p.Tags.ResourceTypes)
	}
	if p.DataResidency.DeploymentSkus != nil {
		m[model.KeyDataResidencyDeploymentSkus] = slices.Clone(p.DataResidency.DeploymentSkus)
	}
	if p.Tags.Required != nil {
		req := make([]any, len(p.Tags.Required))
		for i, t := range p.Tags.Required {
			e := map[string]any{"name": t.Name}
			if t.Format != "" {
				e["format"] = t.Format
			}
			req[i] = e
		}
		m[model.KeyTagsRequired] = req
	}
	return m
}

func managedList(l []model.ManagedBy) []string {
	out := make([]string, len(l))
	for i, v := range l {
		out[i] = string(v)
	}
	return out
}

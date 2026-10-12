package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/preflight"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// PreflightTarget identifies the deployment target. Empty fields fall back to
// the AZURE_* environment variables (azd convention) in the production wiring.
type PreflightTarget struct {
	TenantID       string
	SubscriptionID string
	ResourceGroup  string
	Location       string
}

// PreflightInput is the rule engine input for the preflight command.
type PreflightInput struct {
	RunInput
	Target PreflightTarget
	// WhatIf records the explicit --what-if / --preflight opt-in.
	WhatIf         bool
	ApprovedScopes []string
	AllowedDeletes []string
}

// PreflightEngine evaluates the FND-DEP rules against live Azure state.
type PreflightEngine interface {
	Run(ctx context.Context, in PreflightInput) (RunOutput, error)
}

// PreflightRequest mirrors the preflight flags.
type PreflightRequest struct {
	DoctorRequest
	Target         PreflightTarget
	WhatIf         bool
	ApprovedScopes []string
	AllowedDeletes []string
}

// Preflight runs the Azure deployment preflight, writes the report with a
// readiness summary and returns the exit code. Reads only (what-if and name
// checks are the sanctioned non-GET read operations).
func Preflight(ctx context.Context, svc Services, req PreflightRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	code, err := preflightRun(ctx, svc, req, stdout)
	if err != nil {
		return ExitCodeForError(err), err
	}
	return code, nil
}

func preflightRun(ctx context.Context, svc Services, req PreflightRequest, stdout io.Writer) (int, error) {
	if svc.Preflight == nil {
		return 0, fmt.Errorf("%w: preflight is not available in this build", ErrUnavailable)
	}
	rulesSel := req.Rules
	if len(rulesSel) == 0 {
		rulesSel = []string{"FND-DEP-*"}
	}
	set, err := svc.Config.Resolve(ctx, ConfigRequest{
		Dir: req.Dir, Profile: req.Profile,
		Rules: rulesSel, Baseline: req.Baseline, Suppress: req.Suppress,
	})
	if err != nil {
		return 0, fmt.Errorf("resolve config: %w", err)
	}
	src, err := svc.Project.Load(ctx, req.Dir, set.Inputs)
	if err != nil {
		return 0, fmt.Errorf("discover project: %w", err)
	}
	if svc.Stderr != nil {
		for _, w := range src.Warnings {
			_, _ = fmt.Fprintf(svc.Stderr, "warning: %s\n", findings.Redact(w))
		}
	}
	doc, err := azureyaml.Parse(src.AzureYAML, src.AzureYAMLAt)
	if err != nil {
		return 0, fmt.Errorf("%w: parse azure.yaml: %w", ErrUnavailable, err)
	}
	var arm sdk.ARMModel
	if svc.ARM != nil {
		arm, err = svc.ARM.Load(ctx, src)
		switch {
		case err == nil:
		case errors.Is(err, ErrARMUnavailable) && ctx.Err() == nil:
			arm = nil
			if svc.Stderr != nil {
				_, _ = fmt.Fprintf(svc.Stderr, "warning: %s; template-dependent preflight checks are skipped\n", findings.Redact(err.Error()))
			}
		default:
			return 0, fmt.Errorf("load bicep/arm model: %w", err)
		}
	}
	out, err := svc.Preflight.Run(ctx, PreflightInput{
		RunInput: RunInput{
			Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
			Selectors: set.Rules, Exclude: set.Exclude, Source: src,
			AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
		},
		Target: req.Target, WhatIf: req.WhatIf,
		ApprovedScopes: req.ApprovedScopes, AllowedDeletes: req.AllowedDeletes,
	})
	if err != nil {
		return 0, fmt.Errorf("run preflight rules: %w", err)
	}
	fs := out.Findings
	if set.Baseline != "" && svc.Baseline != nil {
		if fs, err = svc.Baseline.Apply(ctx, set.Baseline, fs, svc.now()); err != nil {
			return 0, fmt.Errorf("apply baseline: %w", err)
		}
	}
	if set.Suppressions != "" && svc.Suppress != nil {
		if fs, err = svc.Suppress.Apply(ctx, set.Suppressions, fs, svc.now()); err != nil {
			return 0, fmt.Errorf("apply suppressions: %w", err)
		}
	}
	for i := range fs {
		fs[i] = findings.RedactFinding(fs[i])
	}
	shown := make([]sdk.Finding, 0, len(fs))
	for _, f := range fs {
		if f.Severity.Rank() >= req.MinSeverity.Rank() {
			shown = append(shown, f)
		}
	}
	findings.Sort(shown)
	skips := slices.Clone(out.Skipped)
	slices.SortFunc(skips, func(a, b sdk.Skip) int {
		if c := strings.Compare(a.RuleID, b.RuleID); c != 0 {
			return c
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	for i := range skips {
		skips[i].Required = true
	}
	code := ExitCode(Outcome{Findings: shown, Skips: skips, FailOn: req.FailOn, Strict: req.Strict})
	var buf bytes.Buffer
	// Readiness is computed from the raw rule outcomes, before baseline,
	// suppression and --min-severity filtering, so a hidden or low-severity
	// failure can never read as ready.
	readyIDs := out.Evaluated
	if len(readyIDs) == 0 {
		readyIDs = preflight.RuleIDs()
	}
	rep := Report{
		Profile: set.Profile, Environment: set.Environment, Policy: set.Policy,
		Findings: shown, Skipped: skips, ExitCode: code,
		Readiness: report.BuildReadiness(readyIDs, out.Findings, skips),
	}
	if err := svc.Reporter.Render(&buf, req.Format, rep); err != nil {
		return 0, fmt.Errorf("render %s report: %w", req.Format, err)
	}
	if err := svc.emit(req.Out, buf.Bytes(), stdout); err != nil {
		return 0, err
	}
	return code, nil
}

// preflightEngine wires the real read-only Azure adapter. The adapter is built
// lazily and performs no I/O until a rule needs it; with no usable credential
// every Azure-dependent rule skips as input-unavailable.
type preflightEngine struct {
	cat    *lazyCatalog
	azd    func(context.Context) string
	getenv func(string) string
	// newClient is replaced in tests.
	newClient func() (azure.Client, error)
}

func defaultAzureClient() (azure.Client, error) {
	a, err := azure.New(azure.Options{})
	if err != nil {
		return azure.Client{}, err
	}
	return a.Client(), nil
}

func (e preflightEngine) Run(ctx context.Context, in PreflightInput) (RunOutput, error) {
	cat, err := e.cat.get(ctx)
	if err != nil {
		return RunOutput{}, err
	}
	mk := e.newClient
	if mk == nil {
		mk = defaultAzureClient
	}
	client, err := mk()
	if err != nil {
		return RunOutput{}, fmt.Errorf("%w: azure adapter: %w", ErrUnavailable, err)
	}
	genv := e.getenv
	if genv == nil {
		genv = func(string) string { return "" }
	}
	pick := func(v, key string) string {
		if v != "" {
			return v
		}
		return genv(key)
	}
	d := preflight.Deps{
		Context: client.Context, Inventory: client.Inventory, Permissions: client.Permissions,
		Models: client.Models, Policy: client.Policy, WhatIf: client.WhatIf, Names: client.Names,
		Regions: client.Regions, Deployments: client.Deployments, SubnetLinks: client.SubnetLinks, Evidence: client.Evidence,
		Target: preflight.Target{
			TenantID:       pick(in.Target.TenantID, "AZURE_TENANT_ID"),
			SubscriptionID: pick(in.Target.SubscriptionID, "AZURE_SUBSCRIPTION_ID"),
			ResourceGroup:  pick(in.Target.ResourceGroup, "AZURE_RESOURCE_GROUP"),
			Location:       pick(in.Target.Location, "AZURE_LOCATION"),
		},
		WhatIfOptIn: in.WhatIf, ApprovedScopes: in.ApprovedScopes, AllowedDeletes: in.AllowedDeletes,
	}
	reg := rules.NewRegistry()
	if err := preflight.Register(reg, d); err != nil {
		return RunOutput{}, err
	}
	sel, required, err := BuildSelector(cat, in.Selectors, in.Exclude)
	if err != nil {
		return RunOutput{}, err
	}
	eng := &rules.Engine{Catalog: cat, Registry: reg, Selector: sel, Required: required}
	ae := engineAdapter{azd: e.azd}
	rep, err := eng.Run(ctx, &sdk.Input{
		Profile: in.Profile, Environment: in.Environment, AzdVersion: ae.azdVersion(ctx, in.RunInput),
		AzureYAML: in.AzureYAML, ARM: in.ARM, Policy: in.Policy,
	})
	if err != nil {
		return RunOutput{}, err
	}
	return RunOutput{Findings: rep.Findings, Skipped: rep.Skipped, Evaluated: rep.Evaluated}, nil
}

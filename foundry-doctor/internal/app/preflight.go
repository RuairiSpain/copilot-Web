package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
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
	// Parameters are the deployment parameter values of the root template from
	// the parameter file (ADR-018); a nil value is set but unresolved. Values of
	// secret-looking keys are already withheld (nil). They are used only for the
	// opt-in what-if, and the rules still drop secure parameters.
	Parameters map[string]any
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
	var params map[string]any
	if req.WhatIf {
		params = whatIfParameters(ctx, svc, src, arm != nil)
	}
	out, err := svc.Preflight.Run(ctx, PreflightInput{
		RunInput: RunInput{
			Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
			Selectors: set.Rules, Exclude: set.Exclude, Source: src,
			AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
		},
		Target: req.Target, WhatIf: req.WhatIf,
		ApprovedScopes: req.ApprovedScopes, AllowedDeletes: req.AllowedDeletes,
		Parameters: params,
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
	// A check that could not run (missing permission, unresolved input) is a
	// required skip: exit 2. An "uncertain" outcome ran and could not reach a
	// verdict; it is reported, counts against readiness, and exits 3 only under
	// --strict, like a doctor skip that hides a check. FND-DEP-002, -008 and
	// -009 are uncertain by design, so exit 2 for them would make every run fail.
	for i := range skips {
		skips[i].Required = rules.ReasonHead(skips[i].Reason) != "uncertain"
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

// deploymentParameterSource is implemented by the production ARM loader. It
// reads the same parameter files as the model (ADR-018) so the what-if request
// describes the deployment the model describes.
type deploymentParameterSource interface {
	DeploymentParameters(ctx context.Context, src Source) map[string]any
}

// whatIfParameters returns the root-template parameter values for the what-if
// request, or nil when no parameter file applies or the loader cannot supply
// them (the rules then report the required parameters as unresolved).
func whatIfParameters(ctx context.Context, svc Services, src Source, haveModel bool) map[string]any {
	if !haveModel || svc.ARM == nil {
		return nil
	}
	ps, ok := svc.ARM.(deploymentParameterSource)
	if !ok {
		return nil
	}
	return ps.DeploymentParameters(ctx, src)
}

// DeploymentParameters implements deploymentParameterSource for the production
// loader. It repeats the entry resolution and parameter-file loading of Load
// (ADR-018: .bicepparam before .parameters.json, azd substitution from the
// selected environment, no secrets) and returns the values by parameter name. A
// value the loader could not resolve is nil, and the value of a key that looks
// like a secret holder is withheld (nil) so that it can never be sent. Warnings
// were already printed by Load and are not repeated.
func (l armLoader) DeploymentParameters(ctx context.Context, src Source) map[string]any {
	if src.Dir == "" {
		return nil
	}
	entry := path.Join(defaultInfraPath(src.InfraPath), "main.bicep")
	if doc, err := azureyaml.Parse(src.AzureYAML, src.AzureYAMLAt); err == nil {
		if resolved, ok := resolveBicepEntry(src, doc); ok {
			entry = resolved
		}
	}
	root, err := os.OpenRoot(src.Dir)
	if err != nil {
		return nil
	}
	defer root.Close()
	var pr paramResult
	if fi, err := root.Stat(entry); err == nil && fi.Mode().IsRegular() {
		var comp *bicep.Compiler
		if tool, err := bicep.Discover(ctx, l.disc); err == nil {
			comp = &bicep.Compiler{Tool: tool, Runner: l.runner, Root: src.Dir}
		}
		pr = l.loadParameters(ctx, root, entry, comp, src)
	} else {
		pr = l.loadParameters(ctx, root, strings.TrimSuffix(entry, ".bicep")+".json", nil, src)
	}
	if !pr.provided {
		return nil
	}
	out := make(map[string]any, len(pr.values))
	for k, v := range pr.values {
		if secretishKey(k) {
			v = nil
		}
		out[k] = v
	}
	return out
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

func defaultAzureClient() (azure.Client, error) { return azureClientForTenant("") }

// azureClientForTenant builds the read-only adapter. A non-empty tenant is
// passed to the azd and az token calls so a multi-tenant sign-in yields a token
// for the tenant that owns the target subscription.
func azureClientForTenant(tenant string) (azure.Client, error) {
	a, err := azure.New(azure.Options{TenantID: tenant})
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
	tenant := pick(in.Target.TenantID, "AZURE_TENANT_ID")
	mk := e.newClient
	if mk == nil {
		mk = func() (azure.Client, error) { return azureClientForTenant(tenant) }
	}
	client, err := mk()
	if err != nil {
		return RunOutput{}, fmt.Errorf("%w: azure adapter: %w", ErrUnavailable, err)
	}
	d := preflight.Deps{
		Context: client.Context, Inventory: client.Inventory, Permissions: client.Permissions,
		Models: client.Models, Policy: client.Policy, WhatIf: client.WhatIf, Names: client.Names,
		Regions: client.Regions, Deployments: client.Deployments, SubnetLinks: client.SubnetLinks, Evidence: client.Evidence,
		Extras: client.Extras, Parameters: in.Parameters,
		Target: preflight.Target{
			TenantID:       tenant,
			SubscriptionID: pick(in.Target.SubscriptionID, "AZURE_SUBSCRIPTION_ID"),
			ResourceGroup:  pick(in.Target.ResourceGroup, "AZURE_RESOURCE_GROUP"),
			Location:       pick(in.Target.Location, "AZURE_LOCATION"),
		},
		WhatIfOptIn: in.WhatIf, ApprovedScopes: in.ApprovedScopes, AllowedDeletes: in.AllowedDeletes,
	}
	// A nil RuntimeFoundry interface must stay a nil AccountDeployments.
	if client.Foundry != nil {
		d.Foundry = client.Foundry
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

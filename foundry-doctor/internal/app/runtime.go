package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	runrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/run"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	foundrydp "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/foundry"
	netprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/network"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// RuntimeTarget identifies the deployed runtime target. Empty subscription and
// resource group values fall back to AZURE_* environment variables.
type RuntimeTarget struct {
	TenantID       string
	SubscriptionID string
	ResourceGroup  string
	Account        string
	Project        string
}

// RuntimeInput is the rule engine input for the runtime command.
type RuntimeInput struct {
	RunInput
	Target  RuntimeTarget
	Vantage runtime.Vantage
	Timeout time.Duration
	// ApprovedScopes are extra scopes (resource IDs) that connection targets may
	// lie in besides the project resource group (FND-RUN-003, ADR-022).
	ApprovedScopes []string
}

// RuntimeEngine evaluates the FND-RUN rules against live Azure state.
type RuntimeEngine interface {
	Run(ctx context.Context, in RuntimeInput) (RunOutput, error)
}

// RuntimeRequest mirrors the runtime flags.
type RuntimeRequest struct {
	DoctorRequest
	Target         RuntimeTarget
	Vantage        runtime.Vantage
	Timeout        time.Duration
	ApprovedScopes []string
}

// Runtime runs the Azure runtime diagnosis, writes the report with a readiness
// summary and returns the exit code.
func Runtime(ctx context.Context, svc Services, req RuntimeRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	code, err := runtimeRun(ctx, svc, req, stdout)
	if err != nil {
		return ExitCodeForError(err), err
	}
	return code, nil
}

func runtimeRun(ctx context.Context, svc Services, req RuntimeRequest, stdout io.Writer) (int, error) {
	if svc.Runtime == nil {
		return 0, fmt.Errorf("%w: runtime is not available in this build", ErrUnavailable)
	}
	rulesSel := req.Rules
	if len(rulesSel) == 0 {
		rulesSel = []string{"FND-RUN-*"}
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
		if err != nil && svc.Stderr != nil {
			_, _ = fmt.Fprintf(svc.Stderr, "warning: %s; template correlation may be incomplete\n", findings.Redact(err.Error()))
		}
	}
	out, err := svc.Runtime.Run(ctx, RuntimeInput{
		RunInput: RunInput{
			Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
			Selectors: set.Rules, Exclude: set.Exclude, Source: src,
			AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
		},
		Target: req.Target, Vantage: req.Vantage, Timeout: req.Timeout, ApprovedScopes: req.ApprovedScopes,
	})
	if err != nil {
		return 0, fmt.Errorf("run runtime rules: %w", err)
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
		skips[i].Required = runtimeSkipRequired(skips[i])
	}
	code := ExitCode(Outcome{Findings: shown, Skips: skips, FailOn: req.FailOn, Strict: req.Strict})
	var buf bytes.Buffer
	readyIDs := out.Evaluated
	if len(readyIDs) == 0 {
		readyIDs = runrules.RuleIDs()
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

// runtimeSkipRequired applies the same rule as preflight: a skip is Required
// (exit 2) only when a check could not run. An "uncertain" outcome ran and could
// not reach a verdict: it is reported, counts against readiness, exits 0 and
// exits 3 under --strict. A by-design skip (for example the not-implemented
// FND-RUN-008 placeholder that FND-RUN-* selects) hid nothing and never
// changes the exit code. An uncertain outcome that reports a defect of this
// tool ("uncertain: internal error ...") did not run the check, so it is still
// Required.
func runtimeSkipRequired(s sdk.Skip) bool {
	if rules.ClassifySkip(s) == rules.SkipByDesign {
		return false
	}
	if rules.ReasonHead(s.Reason) != "uncertain" {
		return true
	}
	_, detail, _ := strings.Cut(s.Reason, ":")
	return strings.HasPrefix(strings.TrimSpace(detail), "internal error")
}

type runtimeEngine struct {
	cat       *lazyCatalog
	azd       func(context.Context) string
	getenv    func(string) string
	newClient func() (azure.Client, error)
	// httpClient, cred and resolver override the data-plane defaults (tests).
	httpClient *http.Client
	cred       azure.TokenCredential
	resolver   netprobe.Resolver
	// netResolver replaces the system resolver inside the production resolver
	// (tests use a loopback DNS stub to get real *net.DNSError values).
	netResolver *net.Resolver
}

func (e runtimeEngine) Run(ctx context.Context, in RuntimeInput) (RunOutput, error) {
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
	var cred azure.TokenCredential = azure.NewDefaultCredential(nil)
	if e.cred != nil {
		cred = e.cred
	}
	httpClient := &http.Client{Timeout: runtime.Timeout(in.Timeout, 30*time.Second)}
	if e.httpClient != nil {
		httpClient = e.httpClient
	}
	var resolver netprobe.Resolver = productionResolver(e.netResolver)
	if e.resolver != nil {
		resolver = e.resolver
	}
	deps := runrules.Deps{
		FoundryMgmt:    client.Foundry,
		RBAC:           client.RBAC,
		Monitor:        client.Monitor,
		ProjectData:    foundrydp.HTTPClient{HTTP: httpClient, Credential: cred},
		SearchData:     searchprobe.HTTPClient{HTTP: httpClient, Credential: cred},
		Resolver:       resolver,
		ApprovedScopes: in.ApprovedScopes,
		Target: runrules.Target{
			SubscriptionID: pick(in.Target.SubscriptionID, "AZURE_SUBSCRIPTION_ID"),
			ResourceGroup:  pick(in.Target.ResourceGroup, "AZURE_RESOURCE_GROUP"),
			Account:        in.Target.Account,
			Project:        in.Target.Project,
		},
		Options: runtime.Options{
			Vantage:     in.Vantage,
			Timeout:     runtime.Timeout(in.Timeout, 30*time.Second),
			Parallelism: 4,
		},
	}
	sel, required, err := BuildSelector(cat, in.Selectors, in.Exclude)
	if err != nil {
		return RunOutput{}, err
	}
	ae := engineAdapter{azd: e.azd}
	input := &sdk.Input{
		Profile: in.Profile, Environment: in.Environment, AzdVersion: ae.azdVersion(ctx, in.RunInput),
		AzureYAML: in.AzureYAML, ARM: in.ARM, Policy: in.Policy,
	}
	// The selected runtime rules run through runtime.Scheduler: bounded
	// parallelism and a per-probe timeout (--timeout). The engine then replays
	// the stored outcomes in rule order.
	var selected []string
	for _, cr := range cat {
		if sel.Matches(cr) && slices.Contains(runrules.RuleIDs(), cr.ID) {
			selected = append(selected, cr.ID)
		}
	}
	reg := rules.NewRegistry()
	for _, r := range runrules.Schedule(ctx, deps, input, selected) {
		if err := reg.Register(r); err != nil {
			return RunOutput{}, err
		}
	}
	eng := &rules.Engine{Catalog: cat, Registry: reg, Selector: sel, Required: required}
	rep, err := eng.Run(ctx, input)
	if err != nil {
		return RunOutput{}, err
	}
	return RunOutput{Findings: rep.Findings, Skipped: rep.Skipped, Evaluated: rep.Evaluated}, nil
}

// productionResolver is the resolver the runtime rules use. It must be
// netprobe.NetResolver: that maps "no such host" to ErrNXDomain and a DNS
// timeout to context.DeadlineExceeded. A bare net.Resolver would hand the rules
// raw *net.DNSError values they do not recognise, so an NXDOMAIN would be
// skipped as "resolver error" and a timeout would never read as a timeout.
func productionResolver(r *net.Resolver) netprobe.Resolver {
	return netprobe.NetResolver{Resolver: r}
}

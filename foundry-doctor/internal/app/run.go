package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/baseline"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/suppress"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// ReasonNotImplemented is the skip reason for a catalogue rule of the selected pack that this build has no Go
// implementation for. It is reported, never silently dropped (skipped is not passed).
const ReasonNotImplemented = "not-implemented"

func lookPath(name string) (string, error) { return exec.LookPath(name) }

// Run executes the doctor command: it writes the report to stdout (or --out) and diagnostics to stderr.
// It returns nil for exit 0 and an *ExitError otherwise; see sdk.Exit*.
func Run(ctx context.Context, opts Options, stdout, stderr io.Writer) error {
	ee := run(ctx, opts, stdout, stderr)
	if ee == nil {
		return nil
	}
	if ee.Err != nil {
		fmt.Fprintf(stderr, "error: %s\n", findings.Redact(ee.Err.Error()))
	}
	return ee
}

func configExit(err error) *ExitError {
	var c config.ExitCoder
	if errors.As(err, &c) {
		return &ExitError{Code: c.ExitCode(), Err: err}
	}
	return &ExitError{Code: sdk.ExitCannotRun, Err: err}
}

func parseThreshold(name, v string, def sdk.Severity) (sdk.Severity, *ExitError) {
	if v == "" {
		return def, nil
	}
	s, err := sdk.ParseSeverity(v)
	if err != nil {
		return "", exitErr(sdk.ExitCannotRun, "--%s: %w", name, err)
	}
	return s, nil
}

func run(ctx context.Context, opts Options, stdout, stderr io.Writer) *ExitError {
	if len(opts.Unavailable) > 0 {
		names := slices.Clone(opts.Unavailable)
		slices.Sort(names)
		return exitErr(sdk.ExitCannotRun, "%s: %s", strings.Join(names, ", "), UnavailableMessage)
	}
	if opts.Now == nil {
		return exitErr(sdk.ExitInternal, "no clock was supplied")
	}
	minSev, ee := parseThreshold("min-severity", opts.MinSeverity, sdk.SeverityInfo)
	if ee != nil {
		return ee
	}
	failOn, ee := parseThreshold("fail-on", opts.FailOn, sdk.SeverityError)
	if ee != nil {
		return ee
	}
	var flagFormats []config.Format
	for _, f := range opts.Formats {
		flagFormats = append(flagFormats, config.Format(f))
	}

	cat := opts.Catalogue
	if cat == nil {
		var err error
		if cat, err = engine.DefaultCatalogue(ctx); err != nil {
			return exitErr(sdk.ExitInternal, "load the rule catalogue: %w", err)
		}
	}
	cli, err := rules.ParseSelector(opts.Rules...)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "--rules: %w", err)
	}

	// Project and configuration.
	start := opts.Dir
	if start == "" {
		start = "."
	}
	disc, err := project.Discover(ctx, start)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "find the project: %w", err)
	}
	p := disc.Project
	azd, err := project.NewStandaloneAzdContext(p.Root)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "open the project: %w", err)
	}
	defer azd.Close()

	var cfgFile *config.File
	if opts.ConfigPath != "" {
		cp := opts.ConfigPath
		if !filepath.IsAbs(cp) {
			cp = filepath.Join(start, cp)
		}
		cfgFile, err = config.Load(ctx, cp)
	} else {
		cfgFile, err = config.LoadOptional(ctx, filepath.Join(p.Root, filepath.FromSlash(config.DefaultPath)))
	}
	if err != nil {
		return configExit(err)
	}
	profiles, err := config.DefaultProfiles()
	if err != nil {
		return exitErr(sdk.ExitInternal, "load profiles: %w", err)
	}
	envArg := opts.Environment
	if envArg == "" && (cfgFile == nil || cfgFile.Inputs.Environment == "") {
		if cur, cerr := azd.CurrentEnvironment(ctx); cerr == nil {
			envArg = cur
		}
	}
	eff, err := config.Resolve(config.Flags{
		Profile: opts.Profile, Environment: opts.Environment, Rules: nil,
		Baseline: opts.Baseline, Suppressions: opts.Suppressions, Formats: flagFormats,
	}, cfgFile, envArg, profiles)
	if err != nil {
		return configExit(err)
	}
	profile := eff.Profile.Name

	formats, ee := planFormats(eff, opts)
	if ee != nil {
		return ee
	}

	// Inputs: azure.yaml.
	in := model.Input{Project: p, Profile: profile, Policy: eff.Policy}
	st := NewStamper()
	var modelDiags []model.Diagnostic
	raw, err := azd.ReadFile(ctx, p.AzureYAMLPath)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "read %s: %w", p.AzureYAMLPath, err)
	}
	yres, perr := azureyaml.Parse(raw, azureyaml.Options{File: p.AzureYAMLPath})
	if perr != nil {
		d := model.Diagnostic{Source: "azureyaml", Code: "invalid-azure-yaml", Severity: sdk.SeverityError, Message: perr.Error(), File: p.AzureYAMLPath}
		var se interface{ Pos() model.Pos }
		if errors.As(perr, &se) {
			d.Pos = se.Pos()
		}
		modelDiags = append(modelDiags, d)
	} else {
		in.AzureYAML, in.YAMLReport = yres.YAML, yres
	}

	// Environment.
	var envNote string
	if eff.Environment != "" {
		env, eerr := azd.EnvValues(ctx, eff.Environment)
		switch {
		case eerr == nil:
			in.Environment = &env
		case opts.Environment != "":
			return exitErr(sdk.ExitCannotRun, "environment %q: %w", eff.Environment, eerr)
		default:
			envNote = fmt.Sprintf("environment %q is not readable; environment-dependent rules are skipped", eff.Environment)
		}
	}
	if v, verr := azd.Versions(ctx); verr == nil {
		in.Versions = v
	}

	// Infrastructure.
	iac, ee := loadIaC(ctx, opts, eff, disc, azd)
	if ee != nil {
		return ee
	}
	in.ARM = iac.arm
	in.Versions.Bicep = iac.bicepVersion
	in.Tools = iac.tools
	modelDiags = append(modelDiags, iac.diags...)
	in.Diagnostics = modelDiags
	in.Graph = graph.Build(in).Graph

	// Rules.
	entries, notImpl, err := selectRules(cat, eff, cli, opts.Rules)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "select rules: %w", err)
	}
	out, err := rules.Execute(ctx, &in, entries, rules.ExecConfig{Profile: profile, Stamper: st})
	if err != nil {
		return exitErr(sdk.ExitInternal, "execute rules: %w", err)
	}

	// Findings: native, engine diagnostics, compiler diagnostics.
	var diagFindings, adapterFindings []sdk.Finding
	for _, d := range modelDiags {
		f, derr := engine.FromModelDiagnostic(st, profile, d)
		if derr != nil {
			return exitErr(sdk.ExitInternal, "map diagnostic: %w", derr)
		}
		if d.Source == "bicep" {
			f.Adapter = bicep.ToolName
			adapterFindings = append(adapterFindings, f)
			continue
		}
		diagFindings = append(diagFindings, f)
	}
	kept, _ := engine.DedupAdapter(out.Findings, adapterFindings, engine.NewOverlapIndex(cat.Registry.Catalog()))
	all := slices.Concat(out.Findings, diagFindings, kept)
	all, dups, err := engine.MergeFingerprints(st, profile, all)
	if err != nil {
		return exitErr(sdk.ExitInternal, "merge fingerprints: %w", err)
	}
	all = append(all, dups...)
	for i := range all {
		all[i] = seal(all[i])
	}

	// Baseline, then suppressions.
	all, ee = applyBaseline(ctx, p.Root, eff, all)
	if ee != nil {
		return ee
	}
	all, ee = applySuppressions(ctx, p.Root, eff, all, st, profile, opts.Now())
	if ee != nil {
		return ee
	}
	if opts.WriteBaseline != "" {
		if all, ee = writeBaseline(ctx, p.Root, opts, all); ee != nil {
			return ee
		}
	}

	// Skipped checks.
	skipped := slices.Clone(out.Skipped)
	skipped = append(skipped, notImpl...)
	if iac.toolMissing {
		for i := range skipped {
			if skipped[i].Reason == rules.ReasonUnresolvedValue("compiled-arm-unavailable") {
				skipped[i].Reason = rules.ReasonToolMissing(bicep.ToolName)
				skipped[i].MissingCapability = "Bicep CLI"
			}
		}
	}

	cannotRun := iac.cannotRun != ""
	for _, t := range iac.tools {
		if t.Required && t.State != sdk.ToolAvailable {
			cannotRun = true
		}
	}
	if out.Interrupted != nil {
		cannotRun = true
	}
	rep := engine.Assemble(engine.AssembleInput{
		Tool: opts.Tool, Profile: profile, EffectivePolicy: policyHeader(eff.PolicyEntries),
		Tools: iac.tools, Findings: all, Skipped: skipped, Passed: out.Passed,
		MinSeverity: minSev, FailOn: failOn, Strict: opts.Strict,
		CannotRun: cannotRun, Internal: len(out.Errors) > 0,
	})

	if opts.Debug {
		writeDebug(stderr, eff, iac, rep, append([]string{envNote}, iac.notes...))
	}
	if iac.cannotRun != "" {
		fmt.Fprintf(stderr, "error: %s\n", findings.Redact(iac.cannotRun))
	}
	for _, re := range out.Errors {
		fmt.Fprintf(stderr, "error: %s\n", findings.Redact(re.Error()))
	}
	if out.Interrupted != nil {
		fmt.Fprintf(stderr, "error: the run was interrupted: %v; %d rules did not run\n", out.Interrupted, len(out.NotRun))
	}

	if err := writeReports(ctx, p.Root, start, formats, opts, eff, cat, rep, stdout); err != nil {
		return exitErr(sdk.ExitCannotRun, "write the report: %w", err)
	}
	if rep.ExitCode != sdk.ExitOK {
		return &ExitError{Code: rep.ExitCode}
	}
	return nil
}

// selectRules resolves the rule set. A --rules selector replaces the configuration. Otherwise the configured packs
// are used, and a pack rule that is native but has no implementation in this build is reported as a skipped
// check instead of failing the run, so a partial build stays honest about what it did not check.
func selectRules(cat *engine.Catalogue, eff config.Effective, cli rules.Selector, _ []string) ([]rules.Entry, []sdk.SkippedCheck, error) {
	reg := cat.Registry
	if !cli.IsZero() {
		e, err := reg.Resolve(cat.Packs, rules.Selection{}, cli)
		return e, nil, err
	}
	names := eff.Rules.Packs
	if len(names) == 0 {
		names = []string{rules.DefaultPack}
	}
	var missing []sdk.SkippedCheck
	seen := map[string]bool{}
	packs := make([]rules.Pack, 0, len(cat.Packs))
	for _, pk := range cat.Packs {
		cp := pk
		cp.Rules = nil
		for _, id := range pk.Rules {
			e, ok := reg.Get(id)
			if ok && !e.Executable() && e.Meta.Implementation.Owner == "native" {
				if slices.Contains(names, pk.ID) && !seen[id] {
					seen[id] = true
					missing = append(missing, sdk.SkippedCheck{
						RuleID: id, RuleVersion: e.Meta.Version, Reason: ReasonNotImplemented,
						Detail:            "the rule is in the catalogue but has no implementation in this build",
						MissingCapability: "rule implementation",
					})
				}
				continue
			}
			cp.Rules = append(cp.Rules, id)
		}
		packs = append(packs, cp)
	}
	e, err := reg.Resolve(packs, rules.Selection{Packs: eff.Rules.Packs, Include: eff.Rules.Include, Exclude: eff.Rules.Exclude}, cli)
	if err != nil {
		return nil, nil, err
	}
	// A rule excluded by configuration is not "not implemented"; drop it from the missing list.
	return e, missing, nil
}

func applyBaseline(ctx context.Context, root string, eff config.Effective, in []sdk.Finding) ([]sdk.Finding, *ExitError) {
	path := eff.Adoption.Baseline
	if path == "" {
		return in, nil
	}
	b, err := baseline.Read(ctx, root, path)
	switch {
	case err == nil:
		marked, _ := baseline.Apply(in, b)
		return marked, nil
	case errors.Is(err, fs.ErrNotExist) && eff.Sources["adoption.baseline"] != config.SourceFlag:
		return in, nil
	}
	return nil, exitErr(sdk.ExitCannotRun, "baseline %s: %w", path, err)
}

func applySuppressions(ctx context.Context, root string, eff config.Effective, in []sdk.Finding, st rules.Stamper, profile string, now time.Time) ([]sdk.Finding, *ExitError) {
	path := eff.Adoption.Suppressions
	if path == "" {
		return in, nil
	}
	f, err := suppress.Read(ctx, root, path)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist) && eff.Sources["adoption.suppressions"] != config.SourceFlag:
		return in, nil
	case ctx.Err() != nil, errors.Is(err, fs.ErrNotExist):
		return nil, exitErr(sdk.ExitCannotRun, "suppressions %s: %w", path, err)
	default:
		rel := filepath.ToSlash(filepath.Clean(path))
		d, derr := engine.NewDiagnostic(st, profile, engine.Diagnostic{
			ID:             engine.DiagSuppressionInvalid,
			Resource:       sdk.ResourceRef{Kind: "file", Name: rel},
			Location:       sdk.Location{File: rel},
			Evidence:       "The suppression file cannot be used, so no suppression is applied: " + err.Error(),
			Recommendation: "Fix the suppression file: every entry needs a rule, a target, a reason, an owner and a YYYY-MM-DD expiry.",
			Key:            "file",
		})
		if derr != nil {
			return nil, exitErr(sdk.ExitInternal, "suppression diagnostic: %w", derr)
		}
		return append(in, seal(d)), nil
	}
	res := suppress.Apply(in, f, now)
	out := res.Findings
	for _, d := range res.Diagnostics {
		d.Profile = profile
		out = append(out, seal(d))
	}
	return out, nil
}

func writeBaseline(ctx context.Context, root string, opts Options, in []sdk.Finding) ([]sdk.Finding, *ExitError) {
	var live []sdk.Finding
	for _, f := range in {
		if f.Suppressed == nil {
			live = append(live, f)
		}
	}
	file, err := baseline.Generate(live, opts.Tool.Name+" "+opts.Tool.Version, opts.Now().UTC().Format(time.DateOnly))
	if err != nil {
		return nil, exitErr(sdk.ExitInternal, "generate the baseline: %w", err)
	}
	if err := baseline.Write(ctx, root, opts.WriteBaseline, file); err != nil {
		return nil, exitErr(sdk.ExitCannotRun, "write the baseline: %w", err)
	}
	marked, _ := baseline.Apply(in, file)
	return marked, nil
}

// policyHeader builds the report's effective-policy header from the resolved entries. Values are policy
// settings, not secrets; they are redacted anyway.
func policyHeader(entries []config.PolicyValue) map[string]any {
	m := make(map[string]any, len(entries))
	for _, e := range entries {
		v := findings.Redact(e.Value)
		if v == "" {
			v = "(not set)"
		}
		v += " [" + e.Source + "]"
		if e.Note != "" {
			v += " - " + e.Note
		}
		m[e.Key] = v
	}
	return m
}

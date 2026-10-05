package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/baseline"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/idn"
	netrules "github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/net"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/suppress"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

// ToolVersion is reported in SARIF/JSON. It is a constant so output stays
// byte-for-byte reproducible; release builds may override it later.
const ToolVersion = "0.1.0-dev"

// RepoConfigPath is the repo-relative location of the unified config.
const RepoConfigPath = ".foundry-doctor/config.yaml"

// builtinRules is the single registration point for native rule packages. The
// cfg and env rules receive providers backed by the discovered project; every
// other package is stateless. cleanup releases file handles held by providers.
func builtinRules(in RunInput) (rs []sdk.Rule, cleanup func()) {
	layout, closeLayout := newLayoutProvider(in.Source)
	store := &envStore{src: in.Source, view: in.AzureYAML}
	p := cfg.Providers{Env: store}
	if layout != nil {
		p.Layout = layout
	}
	rs = append(rs, cfg.RegisterWith(p)...)
	rs = append(rs, env.RegisterWith(store)...)
	rs = append(rs, sec.Register()...)
	rs = append(rs, idn.Register()...)
	rs = append(rs, netrules.Register()...)
	return rs, closeLayout
}

// Options override environment access for tests and embedding. The zero value
// is the production configuration.
type Options struct {
	Getenv func(string) string
	// Bicep controls CLI discovery; BicepRunner replaces process execution.
	Bicep       bicep.DiscoverOptions
	BicepRunner bicep.Runner
	// AzdVersion replaces `azd version` detection (tests, embedding).
	AzdVersion func(ctx context.Context) string
}

// DefaultServices wires the production pipeline. It needs no Azure
// credentials: Bicep is compiled offline when the CLI is present and the
// stage is skipped (input-unavailable) otherwise.
func DefaultServices(stderr io.Writer) Services { return NewServices(stderr, Options{}) }

// NewServices is DefaultServices with explicit options.
func NewServices(stderr io.Writer, o Options) Services {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	cat := &lazyCatalog{}
	azd := o.AzdVersion
	if azd == nil {
		azd = detectAzdVersion
	}
	disc := o.Bicep
	if disc.Runner == nil {
		disc.Runner = o.BicepRunner
	}
	return Services{
		Project:   projectAdapter{},
		Config:    configAdapter{getenv: getenv},
		ARM:       armLoader{disc: disc, runner: o.BicepRunner},
		Engine:    engineAdapter{cat: cat, azd: azd},
		Preflight: preflightEngine{cat: cat, azd: azd, getenv: getenv},
		Runtime:   runtimeEngine{cat: cat, azd: azd, getenv: getenv},
		Baseline:  baselineFilter{},
		Suppress:  suppressFilter{},
		Reporter:  reportAdapter{},
		Explainer: lazyExplainer{cat: cat},
		Now:       time.Now,
		Stderr:    stderr,
	}
}

// --- bicep / ARM ---------------------------------------------------------

type armLoader struct {
	disc   bicep.DiscoverOptions
	runner bicep.Runner
}

func unavailablef(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrARMUnavailable, fmt.Sprintf(format, a...))
}

func (l armLoader) Load(ctx context.Context, src Source) (sdk.ARMModel, error) {
	if src.Dir == "" {
		return nil, unavailablef("no project directory")
	}
	infra := src.InfraPath
	if infra == "" {
		infra = project.DefaultInfraPath
	}
	entry := path.Join(infra, "main.bicep")
	root, err := os.OpenRoot(src.Dir)
	if err != nil {
		return nil, unavailablef("cannot open project directory")
	}
	fi, err := root.Stat(entry)
	_ = root.Close()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, unavailablef("%s not found", entry)
	}
	tool, err := bicep.Discover(ctx, l.disc)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, unavailablef("%s", bicepReason(err))
	}
	comp := bicep.Compiler{Tool: tool, Runner: l.runner, Root: src.Dir}
	res, err := comp.Compile(ctx, filepath.Join(src.Dir, filepath.FromSlash(entry)))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, unavailablef("%s", bicepReason(err))
	}
	if !res.OK {
		return nil, unavailablef("%s failed to compile (%d diagnostics)", entry, len(res.Diagnostics))
	}
	m, err := armmodel.FromARM(res.ARM, armmodel.Options{Entry: entry})
	if err != nil {
		return nil, unavailablef("cannot read compiled ARM template")
	}
	return rawModel{Model: m, raw: res.ARM}, nil
}

// rawModel keeps the compiled ARM JSON next to the model so the preflight
// what-if can submit it. Outputs and Resources are promoted from the model.
type rawModel struct {
	*armmodel.Model
	raw []byte
}

// RawTemplate returns the compiled ARM template JSON.
func (m rawModel) RawTemplate() []byte { return m.raw }

func bicepReason(err error) string {
	var te *bicep.ToolError
	if errors.As(err, &te) {
		return te.Error()
	}
	return "bicep CLI could not be used"
}

// lazyCatalog loads the embedded catalogue once, on first use.
type lazyCatalog struct {
	once  sync.Once
	rules []catalog.Rule
	err   error
}

func (l *lazyCatalog) get(ctx context.Context) ([]catalog.Rule, error) {
	l.once.Do(func() {
		l.rules, l.err = catalog.Load(ctx, ruledata.FS, ruledata.Root)
		if l.err != nil {
			l.err = fmt.Errorf("load rule catalogue: %w", l.err)
		}
	})
	return l.rules, l.err
}

type lazyExplainer struct{ cat *lazyCatalog }

func (e lazyExplainer) Explain(ctx context.Context, id, format string, w io.Writer) error {
	rs, err := e.cat.get(ctx)
	if err != nil {
		return err
	}
	return NewExplainer(rs).Explain(ctx, id, format, w)
}

// --- project -------------------------------------------------------------

type projectAdapter struct{}

func (projectAdapter) Load(ctx context.Context, dir string, in Inputs) (Source, error) {
	p, err := project.Discover(ctx, dir, project.Options{
		AzureYAML: in.AzureYAML, InfraPath: in.InfraPath, Environment: in.Environment,
	})
	if err != nil {
		switch {
		case errors.Is(err, project.ErrNoAzureYAML), errors.Is(err, project.ErrTooLarge),
			errors.Is(err, project.ErrNotRegularYML):
			return Source{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		case errors.Is(err, project.ErrUnsafePath), errors.Is(err, project.ErrBadEnvName):
			return Source{}, Usagef("%s", err)
		}
		return Source{}, err
	}
	return Source{
		AzureYAML: p.AzureYAMLData, AzureYAMLAt: p.AzureYAML, InfraPath: p.InfraPath,
		Environment: p.Environment, Warnings: p.Warnings, Dir: dir, Environments: p.Environments,
	}, nil
}

// --- config --------------------------------------------------------------

type configAdapter struct {
	getenv func(string) string
}

// profileFlag maps the PRD profile names to the curated profile names.
func profileFlag(p string) string {
	if p == "" || strings.HasPrefix(p, "foundry-") {
		return p
	}
	return "foundry-" + p
}

func (c configAdapter) Resolve(_ context.Context, r ConfigRequest) (Settings, error) {
	dir := r.Dir
	if dir == "" {
		dir = "."
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: open project directory: %w", ErrUnavailable, err)
	}
	defer root.Close()

	var cfg *config.Config
	data, err := readRootFile(root, RepoConfigPath, 2*config.MaxConfigBytes)
	switch {
	case err == nil:
		if cfg, err = config.Load(data); err != nil {
			return Settings{}, configError(RepoConfigPath, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		// No repo config: profile defaults apply.
	default:
		return Settings{}, fmt.Errorf("read %s: %w", RepoConfigPath, err)
	}

	env := map[string]string{}
	for _, k := range []string{config.EnvProfile, config.EnvEnvironment, config.EnvOutputDir, config.EnvFormats} {
		if v := c.getenv(k); v != "" {
			env[k] = v
		}
	}
	if env[config.EnvEnvironment] == "" {
		// azd exports the selected environment name to extensions.
		if v := c.getenv("AZURE_ENV_NAME"); v != "" {
			env[config.EnvEnvironment] = v
		}
	}
	eff, err := config.Resolve(cfg, config.Flags{
		Profile: profileFlag(r.Profile), Environment: r.Environment,
		Baseline: r.Baseline, Suppressions: r.Suppress,
	}, env)
	if err != nil {
		return Settings{}, configError("configuration", err)
	}

	set := Settings{
		Profile: eff.Profile.Name, Environment: eff.Environment,
		Inputs: Inputs{AzureYAML: eff.Inputs.AzureYAML, InfraPath: eff.Inputs.InfraPath, Environment: eff.Environment},
		Policy: map[string]any(eff.Policy),
		Rules:  slices.Clone(eff.Rules.Include), Exclude: slices.Clone(eff.Rules.Exclude),
	}
	if len(r.Rules) > 0 {
		set.Rules = slices.Clone(r.Rules)
	}
	if set.Baseline, err = adoptionPath(root, dir, r.Baseline, cfgBaseline(cfg), eff.Adoption.Baseline); err != nil {
		return Settings{}, err
	}
	if set.Suppressions, err = adoptionPath(root, dir, r.Suppress, cfgSuppress(cfg), eff.Adoption.Suppressions); err != nil {
		return Settings{}, err
	}
	return set, nil
}

func cfgBaseline(c *config.Config) string {
	if c == nil {
		return ""
	}
	return c.Adoption.Baseline
}

func cfgSuppress(c *config.Config) string {
	if c == nil {
		return ""
	}
	return c.Adoption.Suppressions
}

// adoptionPath returns the file to read, or "" when none applies. An explicit
// flag or config value must exist (checked when read); the built-in default is
// used only when the file exists.
func adoptionPath(root *os.Root, dir, flagVal, cfgVal, effective string) (string, error) {
	if flagVal != "" {
		return flagVal, nil // user-supplied on the command line; may be absolute
	}
	rel := filepath.ToSlash(effective)
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", Usagef("adoption path %q must stay inside the project directory", effective)
	}
	if cfgVal == "" {
		if _, err := root.Stat(rel); err != nil {
			return "", nil
		}
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}

func configError(what string, err error) error {
	if errors.Is(err, config.ErrInvalid) {
		return Usagef("%s: %s", what, err)
	}
	// config.Load reports unknown keys and syntax errors; treat any error from
	// the config layer as invalid configuration (exit 2), never a crash.
	return Usagef("%s: %s", what, err)
}

func readRootFile(root *os.Root, rel string, limit int) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, Usagef("%s exceeds %d bytes", rel, limit)
	}
	return data, nil
}

func readFileBounded(p string, limit int) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s not found", ErrUnavailable, filepath.Base(p))
		}
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, Usagef("%s exceeds %d bytes", filepath.Base(p), limit)
	}
	return data, nil
}

// --- baseline / suppress ---------------------------------------------------

type baselineFilter struct{}

func (baselineFilter) Apply(_ context.Context, p string, in []sdk.Finding, _ time.Time) ([]sdk.Finding, error) {
	data, err := readFileBounded(p, baseline.MaxBytes)
	if err != nil {
		return nil, err
	}
	f, err := baseline.Parse(data)
	if err != nil {
		return nil, Usagef("%s", err)
	}
	f.Apply(in)
	return in, nil
}

type suppressFilter struct{}

func (suppressFilter) Apply(_ context.Context, p string, in []sdk.Finding, now time.Time) ([]sdk.Finding, error) {
	data, err := readFileBounded(p, suppress.MaxBytes)
	if err != nil {
		return nil, err
	}
	f, err := suppress.Parse(data)
	if err != nil {
		return nil, Usagef("%s", err)
	}
	res := f.Apply(now, in)
	return append(in, res.Findings...), nil
}

// --- reporting ---------------------------------------------------------------

type reportAdapter struct{}

func (reportAdapter) Render(w io.Writer, format string, r Report) error {
	f, err := report.ParseFormat(format)
	if err != nil {
		return Usagef("%s", err)
	}
	return report.Write(w, f, r.Findings, report.Run{
		ToolVersion: ToolVersion, Profile: r.Profile, Skipped: r.Skipped, ExitCode: r.ExitCode,
		Readiness: r.Readiness,
	})
}

// --- rules ---------------------------------------------------------------------

type engineAdapter struct {
	cat *lazyCatalog
	azd func(context.Context) string
}

func (e engineAdapter) Run(ctx context.Context, in RunInput) (RunOutput, error) {
	cat, err := e.cat.get(ctx)
	if err != nil {
		return RunOutput{}, err
	}
	reg := rules.NewRegistry()
	builtin, cleanup := builtinRules(in)
	defer cleanup()
	for _, r := range builtin {
		if err := reg.Register(r); err != nil {
			return RunOutput{}, err
		}
	}
	sel, required, err := BuildSelector(cat, in.Selectors, in.Exclude)
	if err != nil {
		return RunOutput{}, err
	}
	eng := &rules.Engine{Catalog: cat, Registry: reg, Selector: sel, Required: required}
	rep, err := eng.Run(ctx, &sdk.Input{
		Profile: in.Profile, Environment: in.Environment, AzdVersion: e.azdVersion(ctx, in),
		AzureYAML: in.AzureYAML, ARM: in.ARM, Policy: in.Policy,
	})
	if err != nil {
		return RunOutput{}, err
	}
	return RunOutput{Findings: rep.Findings, Skipped: rep.Skipped}, nil
}

// azdVersion prefers an explicitly configured version and otherwise probes the
// installed azd. Empty means unknown: version-ranged rules then skip.
func (e engineAdapter) azdVersion(ctx context.Context, in RunInput) string {
	if in.AzdVersion != "" {
		return in.AzdVersion
	}
	if e.azd == nil {
		return ""
	}
	return e.azd(ctx)
}

var categories = []string{"must-have", "nice-to-have"}

// BuildSelector converts user selectors into an engine selector. A token is a
// category (must-have, nice-to-have), a rule ID or glob such as FND-CFG-*, a
// group name (CFG) or a pillar. Unknown tokens are a usage error so a typo can
// never silently select nothing. Exact rule IDs are returned as required: a
// skip of an explicitly requested rule means the validation could not run.
func BuildSelector(cat []catalog.Rule, include, exclude []string) (rules.Selector, []string, error) {
	var sel rules.Selector
	var required []string
	for _, tok := range include {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "":
			continue
		case slices.Contains(categories, tok):
			sel.Categories = append(sel.Categories, tok)
		case strings.ContainsAny(tok, "*?["):
			ids, err := matchIDs(cat, tok)
			if err != nil {
				return sel, nil, err
			}
			sel.IDs = append(sel.IDs, ids...)
		default:
			if i := slices.IndexFunc(cat, func(r catalog.Rule) bool { return strings.EqualFold(r.ID, tok) }); i >= 0 {
				sel.IDs = append(sel.IDs, cat[i].ID)
				required = append(required, cat[i].ID)
			} else if slices.ContainsFunc(cat, func(r catalog.Rule) bool { return strings.EqualFold(r.Group, tok) }) {
				sel.Groups = append(sel.Groups, tok)
			} else if slices.ContainsFunc(cat, func(r catalog.Rule) bool { return strings.EqualFold(r.Pillar, tok) }) {
				sel.Pillars = append(sel.Pillars, tok)
			} else {
				return sel, nil, Usagef("unknown rule selector %q", tok)
			}
		}
	}
	sel.Exclude = slices.Clone(exclude)
	return sel, required, nil
}

func matchIDs(cat []catalog.Rule, pattern string) ([]string, error) {
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, Usagef("invalid rule glob %q", pattern)
	}
	var ids []string
	for _, r := range cat {
		if ok, _ := path.Match(strings.ToUpper(pattern), r.ID); ok {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return nil, Usagef("rule selector %q matches no rules", pattern)
	}
	return ids, nil
}

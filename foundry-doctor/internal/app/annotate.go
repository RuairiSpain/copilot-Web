package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	ann "github.com/ruairispain/copilot-web/foundry-doctor/internal/annotate"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	goyaml "go.yaml.in/yaml/v3"
)

type AnnotateRequest struct {
	Dir         string
	Profile     string
	Rules       []string
	MinSeverity sdk.Severity
	FailOn      sdk.Severity
	Baseline    string
	Suppress    string
	Strict      bool
	Format      string
	Out         string
	Diff        bool
}

const AnnotateFormatDefault = ann.FormatReview

func (r *AnnotateRequest) Validate() error {
	if r.Format == "" {
		r.Format = ann.FormatReview
	}
	switch r.Format {
	case ann.FormatReview, ann.FormatGitHub, ann.FormatSARIF:
	default:
		return Usagef("invalid --format %q for annotate (want review, github or sarif)", r.Format)
	}
	if r.Profile != "" && !slices.Contains([]string{"dev", "test", "prod"}, r.Profile) {
		return Usagef("invalid --profile %q (want dev, test or prod)", r.Profile)
	}
	if r.MinSeverity == "" {
		r.MinSeverity = sdk.SeverityInfo
	}
	if r.FailOn == "" {
		r.FailOn = sdk.SeverityError
	}
	if !r.MinSeverity.Valid() {
		return Usagef("invalid --min-severity %q", r.MinSeverity)
	}
	if !r.FailOn.Valid() {
		return Usagef("invalid --fail-on %q", r.FailOn)
	}
	if r.FailOn.Rank() < r.MinSeverity.Rank() {
		return Usagef("--fail-on %s is below --min-severity %s", r.FailOn, r.MinSeverity)
	}
	if r.Format == ann.FormatReview && r.Out == "" {
		return Usagef("--out is required for annotate --format review")
	}
	if r.Dir == "" {
		r.Dir = "."
	}
	return nil
}

func Annotate(ctx context.Context, svc Services, req AnnotateRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	run, err := collectAnnotationRun(ctx, svc, req)
	if err != nil {
		return ExitCodeForError(err), err
	}
	code := ExitCode(Outcome{Findings: run.shownFindings, Skips: run.skips, FailOn: req.FailOn, Strict: req.Strict})
	switch req.Format {
	case ann.FormatGitHub:
		var buf bytes.Buffer
		if err := ann.GitHubWorkflowCommands(&buf, run.entries); err != nil {
			return ExitInternal, err
		}
		if err := rejectInputOverlap(run.source, req.Out); err != nil {
			return ExitCodeForError(err), err
		}
		if err := emitAnnotateOutput(req.Dir, req.Out, buf.Bytes(), stdout); err != nil {
			return ExitCodeForError(err), err
		}
	case ann.FormatSARIF:
		var buf bytes.Buffer
		if err := report.SARIF(&buf, toFindings(run.entries), report.Run{
			ToolVersion: ToolVersion, Profile: run.settings.Profile, Skipped: run.skips, ExitCode: code,
		}); err != nil {
			return ExitInternal, fmt.Errorf("render sarif annotations: %w", err)
		}
		if err := rejectInputOverlap(run.source, req.Out); err != nil {
			return ExitCodeForError(err), err
		}
		if err := emitAnnotateOutput(req.Dir, req.Out, buf.Bytes(), stdout); err != nil {
			return ExitCodeForError(err), err
		}
	default:
		outDir := req.Out
		if filepath.IsAbs(outDir) {
			if rel, err := filepath.Rel(req.Dir, outDir); err == nil && filepath.IsLocal(rel) {
				outDir = rel
			}
		}
		if _, err := ann.WriteReview(ctx, ann.ReviewRequest{
			RootDir:     req.Dir,
			OutDir:      outDir,
			AzureYAML:   run.source.AzureYAMLAt,
			InfraPath:   run.source.InfraPath,
			Entries:     run.entries,
			Profile:     run.settings.Profile,
			ToolVersion: ToolVersion,
			WriteDiff:   req.Diff,
		}); err != nil {
			return ExitCodeForError(err), err
		}
		if err := validateReviewCopy(ctx, req.Dir, req.Out, run.source.AzureYAMLAt, run.compile); err != nil {
			_ = removeReviewCopy(req.Dir, req.Out)
			return ExitCodeForError(err), err
		}
	}
	return code, nil
}

func emitAnnotateOutput(rootDir, out string, data []byte, stdout io.Writer) error {
	if out == "" {
		_, err := stdout.Write(data)
		if err != nil {
			return fmt.Errorf("write annotations: %w", err)
		}
		return nil
	}
	out, err := safeRepoOutputPath(out)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := root.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("write --out %q: %w", out, err)
	}
	return nil
}

func safeRepoOutputPath(out string) (string, error) {
	out = filepath.ToSlash(out)
	if out == "" || strings.HasPrefix(out, "/") || filepath.IsAbs(filepath.FromSlash(out)) || !filepath.IsLocal(filepath.FromSlash(out)) {
		return "", Usagef("--out must stay inside the project directory")
	}
	return path.Clean(out), nil
}

func rejectInputOverlap(src Source, out string) error {
	if out == "" {
		return nil
	}
	out, err := safeRepoOutputPath(out)
	if err != nil {
		return err
	}
	for _, in := range []string{src.AzureYAMLAt, src.InfraPath} {
		if in == "" {
			continue
		}
		in = path.Clean(filepath.ToSlash(in))
		if out == in || strings.HasPrefix(out, in+"/") || strings.HasPrefix(in, out+"/") {
			return Usagef("--out must not overlap deployable source inputs")
		}
	}
	return nil
}

func removeReviewCopy(rootDir, out string) error {
	out, err := safeRepoOutputPath(out)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(out)
}

type annotationRun struct {
	settings      Settings
	source        Source
	shownFindings []sdk.Finding
	skips         []sdk.Skip
	entries       []ann.Entry
	compile       *compilePair
}

func collectAnnotationRun(ctx context.Context, svc Services, req AnnotateRequest) (annotationRun, error) {
	set, err := svc.Config.Resolve(ctx, ConfigRequest{
		Dir: req.Dir, Profile: req.Profile, Rules: req.Rules, Baseline: req.Baseline, Suppress: req.Suppress,
	})
	if err != nil {
		return annotationRun{}, fmt.Errorf("resolve config: %w", err)
	}
	src, err := svc.Project.Load(ctx, req.Dir, set.Inputs)
	if err != nil {
		return annotationRun{}, fmt.Errorf("discover project: %w", err)
	}
	doc, err := azureyaml.Parse(src.AzureYAML, src.AzureYAMLAt)
	if err != nil {
		return annotationRun{}, fmt.Errorf("%w: parse azure.yaml: %w", ErrUnavailable, err)
	}
	var arm sdk.ARMModel
	if svc.ARM != nil {
		arm, err = svc.ARM.Load(ctx, src)
		switch {
		case err == nil:
		case errors.Is(err, ErrARMUnavailable) && ctx.Err() == nil:
			arm = nil
		default:
			return annotationRun{}, fmt.Errorf("load bicep/arm model: %w", err)
		}
	}
	out, err := svc.Engine.Run(ctx, RunInput{
		Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
		Selectors: set.Rules, Exclude: set.Exclude, Source: src, Local: true,
		AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
	})
	if err != nil {
		return annotationRun{}, fmt.Errorf("run rules: %w", err)
	}
	fs := out.Findings
	if set.Baseline != "" && svc.Baseline != nil {
		if fs, err = svc.Baseline.Apply(ctx, set.Baseline, fs, svc.now()); err != nil {
			return annotationRun{}, fmt.Errorf("apply baseline: %w", err)
		}
	}
	if set.Suppressions != "" && svc.Suppress != nil {
		if fs, err = svc.Suppress.Apply(ctx, set.Suppressions, fs, svc.now()); err != nil {
			return annotationRun{}, fmt.Errorf("apply suppressions: %w", err)
		}
	}
	shown := make([]sdk.Finding, 0, len(fs))
	for _, f := range fs {
		f = findings.RedactFinding(f)
		if f.Severity.Rank() >= req.MinSeverity.Rank() && f.Suppressed == nil && !f.Baselined {
			shown = append(shown, f)
		}
	}
	findings.Sort(shown)
	skips := slices.Clone(out.Skipped)
	slices.SortFunc(skips, func(a, b sdk.Skip) int {
		if a.RuleID != b.RuleID {
			if a.RuleID < b.RuleID {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	compile, diags, err := compileDiagnostics(ctx, svc, src, doc, req.MinSeverity)
	if err != nil {
		if req.Format != ann.FormatReview && errors.Is(err, ErrUnavailable) {
			compile, diags = nil, nil
		} else {
			return annotationRun{}, err
		}
	}
	entries := ann.Collect(shown, diags)
	return annotationRun{
		settings:      set,
		source:        src,
		shownFindings: shown,
		skips:         skips,
		entries:       entries,
		compile:       compile,
	}, nil
}

type compilePair struct {
	entryRel string
	original bicep.Result
	do       func(context.Context, string) (bicep.Result, error)
}

func compileDiagnostics(ctx context.Context, svc Services, src Source, doc *azureyaml.Document, min sdk.Severity) (*compilePair, []bicep.Diagnostic, error) {
	loader, ok := svc.ARM.(armLoader)
	if !ok {
		return nil, nil, nil
	}
	if hasInfraLayers(doc) {
		return nil, nil, fmt.Errorf("%w: annotate review does not yet support infra.layers Bicep validation", ErrUnavailable)
	}
	entryRel, ok := resolveBicepEntry(src, doc)
	if !ok {
		return nil, nil, nil
	}
	root, err := os.OpenRoot(src.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	if _, err := root.Stat(entryRel); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat bicep entry: %w", err)
	}
	tool, err := bicep.Discover(ctx, loader.disc)
	if err != nil {
		if errors.Is(err, bicep.ErrCLIMissing) || errors.Is(err, bicep.ErrVersionUnsupported) {
			return nil, nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
		}
		return nil, nil, fmt.Errorf("discover bicep: %w", err)
	}
	doCompile := func(ctx context.Context, root string) (bicep.Result, error) {
		c := bicep.Compiler{Tool: tool, Runner: loader.runner, Root: root}
		return c.Compile(ctx, filepath.Join(root, filepath.FromSlash(entryRel)))
	}
	res, err := doCompile(ctx, src.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("compile bicep for annotation: %w", err)
	}
	var diags []bicep.Diagnostic
	for _, d := range res.Diagnostics {
		sev := sdk.Severity(strings.ToLower(string(d.Severity)))
		if sev.Rank() >= min.Rank() {
			diags = append(diags, d)
		}
	}
	return &compilePair{entryRel: entryRel, original: res, do: doCompile}, diags, nil
}

func resolveBicepEntry(src Source, doc *azureyaml.Document) (string, bool) {
	infra := src.InfraPath
	if doc != nil {
		if v, _, ok := doc.Lookup("infra", "path"); ok && strings.TrimSpace(v) != "" {
			infra = v
		}
	}
	if infra == "" {
		infra = project.DefaultInfraPath
	}
	module := "main.bicep"
	if doc != nil {
		if v, _, ok := doc.Lookup("infra", "module"); ok && strings.TrimSpace(v) != "" {
			module = v
		}
	}
	if path.Ext(module) == "" {
		module += ".bicep"
	}
	entryRel := path.Clean(path.Join(filepath.ToSlash(infra), filepath.ToSlash(module)))
	return entryRel, isProjectRelative(entryRel)
}

func hasInfraLayers(doc *azureyaml.Document) bool {
	if doc == nil {
		return false
	}
	layers, _, ok := doc.LookupAny("infra", "layers")
	if !ok {
		return false
	}
	seq, ok := layers.([]any)
	return ok && len(seq) > 0
}

func isProjectRelative(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(filepath.FromSlash(p)) || !filepath.IsLocal(filepath.FromSlash(p)) {
		return false
	}
	cleaned := path.Clean(filepath.ToSlash(p))
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "/../")
}

func validateReviewCopy(ctx context.Context, rootDir, outDir, azureYAMLRel string, compile *compilePair) error {
	reviewRoot := filepath.Join(rootDir, filepath.FromSlash(outDir))
	root, err := os.OpenRoot(reviewRoot)
	if err != nil {
		return fmt.Errorf("open review directory: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(azureYAMLRel)
	if err != nil {
		return fmt.Errorf("read annotated azure.yaml: %w", err)
	}
	if _, err := azureyaml.Parse(data, azureYAMLRel); err != nil {
		return fmt.Errorf("annotated azure.yaml does not parse: %w", err)
	}
	var beforeYAML, afterYAML any
	if original, err := compileYAMLSource(rootDir, azureYAMLRel); err == nil {
		if err := goyaml.Unmarshal(original, &beforeYAML); err == nil {
			if err := goyaml.Unmarshal(data, &afterYAML); err == nil && !reflect.DeepEqual(beforeYAML, afterYAML) {
				return fmt.Errorf("annotated azure.yaml changed YAML semantics")
			}
		}
	}
	if compile == nil {
		return nil
	}
	got, err := compile.do(ctx, reviewRoot)
	if err != nil {
		return fmt.Errorf("compile annotated bicep review copy: %w", err)
	}
	if compile.original.OK != got.OK {
		return fmt.Errorf("annotated bicep changed compile status")
	}
	if compile.original.OK && got.OK && !bytes.Equal(bytes.TrimSpace(compile.original.ARM), bytes.TrimSpace(got.ARM)) {
		return fmt.Errorf("annotated bicep changed compiled ARM output")
	}
	if !sameDiagnostics(compile.original.Diagnostics, got.Diagnostics) {
		return fmt.Errorf("annotated bicep changed compiler diagnostics")
	}
	return nil
}

func compileYAMLSource(rootDir, rel string) ([]byte, error) {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(rel)
}

func sameDiagnostics(a, b []bicep.Diagnostic) bool {
	return slices.Equal(signatures(a), signatures(b))
}

func signatures(in []bicep.Diagnostic) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		out = append(out, fmt.Sprintf("%s|%s|%s", d.Severity, d.Code, d.Message))
	}
	slices.Sort(out)
	return out
}

func toFindings(entries []ann.Entry) []sdk.Finding {
	out := make([]sdk.Finding, 0, len(entries))
	for _, e := range entries {
		out = append(out, sdk.Finding{
			RuleID:         e.RuleID,
			Severity:       e.Severity,
			Category:       sdk.Category(e.Category),
			Location:       sdk.Location{File: e.File, Line: e.Line, Column: e.Column},
			Evidence:       e.Message,
			Recommendation: e.Recommendation,
			Fingerprint:    e.ID,
			Adapter:        e.Adapter,
		})
	}
	return out
}

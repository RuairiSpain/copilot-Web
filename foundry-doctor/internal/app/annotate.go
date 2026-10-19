package app

import (
	"bytes"
	"context"
	"encoding/json"
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
	out, err := annotateOutPath(req.Dir, req.Out)
	if err != nil {
		return ExitCodeForError(err), err
	}
	run, err := collectAnnotationRun(ctx, svc, req)
	if err != nil {
		return ExitCodeForError(err), err
	}
	inputs := annotateInputs(run)
	code := ExitCode(Outcome{Findings: run.shownFindings, Skips: run.skips, FailOn: req.FailOn, Strict: req.Strict})
	switch req.Format {
	case ann.FormatGitHub:
		var buf bytes.Buffer
		if err := ann.GitHubWorkflowCommands(&buf, run.entries); err != nil {
			return ExitInternal, err
		}
		if err := emitAnnotateOutput(req.Dir, out, inputs, buf.Bytes(), stdout); err != nil {
			return ExitCodeForError(err), err
		}
	case ann.FormatSARIF:
		var buf bytes.Buffer
		if err := report.SARIF(&buf, toFindings(run.entries), report.Run{
			ToolVersion: ToolVersion, Profile: run.settings.Profile, Skipped: run.skips, ExitCode: code,
		}); err != nil {
			return ExitInternal, fmt.Errorf("render sarif annotations: %w", err)
		}
		if err := emitAnnotateOutput(req.Dir, out, inputs, buf.Bytes(), stdout); err != nil {
			return ExitCodeForError(err), err
		}
	default:
		res, err := ann.WriteReview(ctx, ann.ReviewRequest{
			RootDir:     req.Dir,
			OutDir:      out,
			AzureYAML:   run.source.AzureYAMLAt,
			InfraPath:   run.infra.Dir,
			Entries:     unparseableToManifestOnly(req.Dir, run.entries),
			Profile:     run.settings.Profile,
			ToolVersion: ToolVersion,
			WriteDiff:   req.Diff,
		})
		if err != nil {
			err = outputError(err)
			return ExitCodeForError(err), err
		}
		if svc.Stderr != nil {
			for _, sk := range res.Skipped {
				_, _ = fmt.Fprintf(svc.Stderr, "warning: not copied into the review tree: %s\n", findings.Redact(sk))
			}
		}
		if err := validateReviewCopy(ctx, req.Dir, out, res.Manifest.Files, run.source.AzureYAMLAt, run.compile); err != nil {
			_ = ann.RemoveReview(req.Dir, out)
			return ExitCodeForError(err), err
		}
	}
	return code, nil
}

// outputError turns a refused --out into a usage error (exit 2).
func outputError(err error) error {
	if errors.Is(err, ann.ErrUnsafeOutput) {
		return Usagef("%s", err.Error())
	}
	return err
}

// annotateOutPath returns --out as a project-relative, slash-separated path,
// or "" for stdout. An absolute --out inside --dir is made relative, so every
// later step (write, validation, cleanup) uses the same path.
func annotateOutPath(dir, out string) (string, error) {
	out = normalizeOutPath(out)
	if out == "" {
		return "", nil
	}
	if native := filepath.FromSlash(out); filepath.IsAbs(native) {
		if absDir, err := filepath.Abs(dir); err == nil {
			if rel, err := filepath.Rel(absDir, filepath.Clean(native)); err == nil && filepath.IsLocal(rel) {
				out = filepath.ToSlash(rel)
			} else if realDir, err := filepath.EvalSymlinks(absDir); err == nil {
				if rel, err := filepath.Rel(realDir, filepath.Clean(native)); err == nil && filepath.IsLocal(rel) {
					out = filepath.ToSlash(rel)
				}
			}
		}
	}
	return safeRepoOutputPath(out)
}

// annotateInputs are the deployable inputs an output must never overlap. An
// infra directory of "." (the project root) is left out for file outputs;
// review mode refuses it in WriteReview.
func annotateInputs(run annotationRun) []string {
	var in []string
	for _, p := range []string{run.source.AzureYAMLAt, run.infra.Dir, run.source.InfraPath} {
		if p != "" && p != "." && !slices.Contains(in, p) {
			in = append(in, p)
		}
	}
	return in
}

func emitAnnotateOutput(rootDir, out string, inputs []string, data []byte, stdout io.Writer) error {
	if out == "" {
		_, err := stdout.Write(data)
		if err != nil {
			return fmt.Errorf("write annotations: %w", err)
		}
		return nil
	}
	return outputError(ann.WriteOutputFile(rootDir, out, inputs, data))
}

// normalizeOutPath treats a backslash as a path separator on every OS, so a
// Windows-style --out value is handled the same way on Linux and macOS.
func normalizeOutPath(out string) string {
	return strings.ReplaceAll(out, "\\", "/")
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' && (p[0]|0x20) >= 'a' && (p[0]|0x20) <= 'z'
}

func safeRepoOutputPath(out string) (string, error) {
	out = normalizeOutPath(filepath.ToSlash(out))
	if out == "" || hasDriveLetter(out) || strings.HasPrefix(out, "/") || filepath.IsAbs(filepath.FromSlash(out)) || !filepath.IsLocal(filepath.FromSlash(out)) {
		return "", Usagef("--out must stay inside the project directory")
	}
	return path.Clean(out), nil
}

type annotationRun struct {
	settings      Settings
	source        Source
	infra         infraTarget
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
	infra, infraOK := resolveInfra(src, doc)
	var arm sdk.ARMModel
	// A non-Bicep provider (terraform, an extension provider) is never
	// compiled, even when a main.bicep happens to exist.
	if svc.ARM != nil && (!infraOK || infra.isBicep()) {
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
	compile, diags, err := compileDiagnostics(ctx, svc, src, doc, infra, infraOK, req.MinSeverity)
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
		infra:         infra,
		shownFindings: shown,
		skips:         skips,
		entries:       entries,
		compile:       compile,
	}, nil
}

// infraTarget is the one resolved azd infrastructure location. The review
// copy, the annotate compiles and the ARM loader all use it.
type infraTarget struct {
	// Dir is the project-relative, slash-separated infra directory.
	Dir string
	// Entry is the project-relative entry module, inside Dir.
	Entry string
	// Provider is azure.yaml infra.provider, lower-cased; "" means bicep.
	Provider string
}

// isBicep reports whether azd provisions this project with Bicep. Built-in
// providers are bicep (the default) and terraform; extensions can register
// more (https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-schema,
// infra section, verified 2026-10-18). Only bicep is compiled.
func (t infraTarget) isBicep() bool { return t.Provider == "" || t.Provider == "bicep" }

// resolveInfra resolves the infra directory and entry module. Precedence: a
// Foundry Doctor inputs.infraPath that differs from the default "infra"; then
// azure.yaml infra.path; then "infra". The azure.yaml value and the default
// are relative to the directory that contains azure.yaml, which azd treats as
// the project directory. A configured inputs.infraPath of exactly "infra"
// cannot be told apart from the default. The module defaults to "main", and a
// module without an extension gets ".bicep". ok is false when a path is
// absolute, escapes the project, or the entry leaves the infra directory.
func resolveInfra(src Source, doc *azureyaml.Document) (infraTarget, bool) {
	var t infraTarget
	if doc != nil {
		if v, _, ok := doc.Lookup("infra", "provider"); ok {
			t.Provider = strings.ToLower(strings.TrimSpace(v))
		}
	}
	base := "."
	if src.AzureYAMLAt != "" {
		base = path.Dir(path.Clean(filepath.ToSlash(src.AzureYAMLAt)))
	}
	configured := ""
	if src.InfraPath != "" {
		configured = path.Clean(filepath.ToSlash(src.InfraPath))
	}
	switch {
	case configured != "" && configured != project.DefaultInfraPath:
		t.Dir = configured
	default:
		infra := project.DefaultInfraPath
		if doc != nil {
			if v, _, ok := doc.Lookup("infra", "path"); ok && strings.TrimSpace(v) != "" {
				infra = strings.TrimSpace(v)
			}
		}
		infra = filepath.ToSlash(infra)
		if strings.HasPrefix(infra, "/") || hasDriveLetter(infra) || filepath.IsAbs(filepath.FromSlash(infra)) {
			return t, false
		}
		t.Dir = path.Join(base, infra)
	}
	if t.Dir != "." && !isProjectRelative(t.Dir) {
		return t, false
	}
	module := "main"
	if doc != nil {
		if v, _, ok := doc.Lookup("infra", "module"); ok && strings.TrimSpace(v) != "" {
			module = strings.TrimSpace(v)
		}
	}
	module = filepath.ToSlash(module)
	if path.Ext(module) == "" {
		module += ".bicep"
	}
	t.Entry = path.Clean(path.Join(t.Dir, module))
	if !isProjectRelative(t.Entry) || (t.Dir != "." && !strings.HasPrefix(t.Entry, t.Dir+"/")) {
		return t, false
	}
	return t, true
}

type compilePair struct {
	entryRel string
	original bicep.Result
	do       func(context.Context, string) (bicep.Result, error)
}

func compileDiagnostics(ctx context.Context, svc Services, src Source, doc *azureyaml.Document, infra infraTarget, infraOK bool, min sdk.Severity) (*compilePair, []bicep.Diagnostic, error) {
	if !infraOK {
		return nil, nil, fmt.Errorf("%w: azure.yaml infra.path or infra.module is not a project-relative path inside the infra directory", ErrUnavailable)
	}
	loader, ok := svc.ARM.(armLoader)
	if !ok {
		return nil, nil, nil
	}
	if hasInfraLayers(doc) {
		return nil, nil, fmt.Errorf("%w: annotate review does not yet support infra.layers Bicep validation", ErrUnavailable)
	}
	if !infra.isBicep() || path.Ext(infra.Entry) != ".bicep" {
		return nil, nil, nil
	}
	entryRel := infra.Entry
	root, err := os.OpenRoot(src.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("open project root: %w", err)
	}
	fi, err := root.Stat(entryRel)
	root.Close()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat bicep entry: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, nil
	}
	tool, err := bicep.Discover(ctx, loader.disc)
	if err != nil {
		if errors.Is(err, bicep.ErrCLIMissing) || errors.Is(err, bicep.ErrVersionUnsupported) {
			return nil, nil, fmt.Errorf("%w: %s", ErrUnavailable, err)
		}
		return nil, nil, fmt.Errorf("discover bicep: %w", err)
	}
	// Both the original and the review-copy compile run the ADR-018 file-load
	// guard first; a refused entry is never handed to the Bicep CLI.
	doCompile := func(ctx context.Context, dir string) (bicep.Result, error) {
		r, err := os.OpenRoot(dir)
		if err != nil {
			return bicep.Result{}, fmt.Errorf("open bicep root: %w", err)
		}
		gerr := bicep.GuardFileLoads(r, entryRel)
		r.Close()
		if gerr != nil {
			return bicep.Result{}, fmt.Errorf("%w: %s not compiled: %s", ErrUnavailable, entryRel, findings.Redact(gerr.Error()))
		}
		c := bicep.Compiler{Tool: tool, Runner: loader.runner, Root: dir}
		return c.Compile(ctx, filepath.Join(dir, filepath.FromSlash(entryRel)))
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

// resolveBicepEntry returns the Bicep entry module for the ARM loader and the
// preflight. It is false for a non-Bicep provider or an unsafe path.
func resolveBicepEntry(src Source, doc *azureyaml.Document) (string, bool) {
	t, ok := resolveInfra(src, doc)
	if !ok || !t.isBicep() {
		return "", false
	}
	return t.Entry, true
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

// validateReviewCopy checks the written review copy: every annotated or
// bannered YAML file still parses to the same documents, and the Bicep entry
// in the copy gives the same compile status, compiled ARM and diagnostic set.
// Any difference fails the run.
func validateReviewCopy(ctx context.Context, rootDir, outDir string, files []ann.ManifestFile, azureYAMLRel string, compile *compilePair) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer root.Close()
	for _, f := range files {
		if f.CommentStyle != "yaml" {
			continue
		}
		original, err := root.ReadFile(f.Path)
		if err != nil {
			return fmt.Errorf("read original %s: %w", f.Path, err)
		}
		annotated, err := root.ReadFile(f.ReviewPath)
		if err != nil {
			return fmt.Errorf("read annotated %s: %w", f.ReviewPath, err)
		}
		if f.Path == azureYAMLRel {
			if _, err := azureyaml.Parse(annotated, azureYAMLRel); err != nil {
				return fmt.Errorf("annotated azure.yaml does not parse: %w", err)
			}
		}
		before, err := yamlDocuments(original)
		if err != nil {
			// The original is not valid YAML (a duplicate key, for example), so
			// it got the banner only and no inline comments; there is no
			// document to compare. That is the file's own problem and the
			// doctor reports it as a finding.
			continue
		}
		after, err := yamlDocuments(annotated)
		if err != nil {
			return fmt.Errorf("annotated %s does not parse: %w", f.Path, err)
		}
		if !reflect.DeepEqual(before, after) {
			return fmt.Errorf("annotated %s changed YAML semantics", f.Path)
		}
	}
	if compile == nil {
		return nil
	}
	got, err := compile.do(ctx, filepath.Join(rootDir, filepath.FromSlash(outDir)))
	if err != nil {
		return fmt.Errorf("compile annotated bicep review copy: %w", err)
	}
	if compile.original.OK != got.OK {
		return fmt.Errorf("annotated bicep changed compile status")
	}
	if compile.original.OK && got.OK && !sameARM(compile.original.ARM, got.ARM) {
		return fmt.Errorf("annotated bicep changed compiled ARM output")
	}
	if !sameDiagnostics(compile.original.Diagnostics, got.Diagnostics) {
		return fmt.Errorf("annotated bicep changed compiler diagnostics")
	}
	return nil
}

// sameARM compares two compiled templates. The Bicep generator metadata
// (metadata._generator: name, version, templateHash) is left out at every
// nesting level, so the check does not depend on whether templateHash is
// sensitive to comment-only source edits (unverified); every other part of
// the template must be identical.
func sameARM(a, b []byte) bool {
	ca, errA := canonicalARM(a)
	cb, errB := canonicalARM(b)
	if errA != nil || errB != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	return bytes.Equal(ca, cb)
}

func canonicalARM(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	stripGenerator(v)
	return json.Marshal(v)
}

func stripGenerator(v any) {
	switch t := v.(type) {
	case map[string]any:
		if md, ok := t["metadata"].(map[string]any); ok {
			delete(md, "_generator")
		}
		for _, child := range t {
			stripGenerator(child)
		}
	case []any:
		for _, child := range t {
			stripGenerator(child)
		}
	}
}

// unparseableToManifestOnly keeps findings in a YAML file that does not decode
// out of the review copy. A comment cannot be shown to be harmless in a file
// whose meaning cannot be compared, so those findings stay in the manifest.
func unparseableToManifestOnly(dir string, entries []ann.Entry) []ann.Entry {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return entries
	}
	defer root.Close()
	bad := map[string]bool{}
	out := make([]ann.Entry, len(entries))
	for i, e := range entries {
		out[i] = e
		ext := strings.ToLower(path.Ext(e.File))
		if !e.Inline || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		unparseable, seen := bad[e.File]
		if !seen {
			data, err := root.ReadFile(e.File)
			if err == nil {
				_, err = yamlDocuments(data)
			}
			unparseable = err != nil
			bad[e.File] = unparseable
		}
		if unparseable {
			out[i].Inline, out[i].InlineReason = false, "source-not-parseable"
		}
	}
	return out
}

// yamlDocuments decodes every document in a YAML stream.
func yamlDocuments(data []byte) ([]any, error) {
	dec := goyaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, v)
	}
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

// toFindings maps entries to SARIF findings. A location that is not
// inline-eligible keeps at most its project-relative file: no line or column
// is emitted, so code scanning never shows an untrusted location as exact.
func toFindings(entries []ann.Entry) []sdk.Finding {
	out := make([]sdk.Finding, 0, len(entries))
	for _, e := range entries {
		loc := sdk.Location{File: e.File}
		if e.Inline {
			loc.Line, loc.Column = e.Line, e.Column
		}
		fp := e.Fingerprint
		if fp == "" {
			fp = e.ID
		}
		out = append(out, sdk.Finding{
			RuleID:         e.RuleID,
			Severity:       e.Severity,
			Category:       sdk.Category(e.Category),
			Location:       loc,
			Evidence:       e.Message,
			Recommendation: e.Recommendation,
			Fingerprint:    fp,
			Adapter:        e.Adapter,
		})
	}
	return out
}

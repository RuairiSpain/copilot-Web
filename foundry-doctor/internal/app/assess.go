package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess/waf"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess/wara"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
	developerreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/developer"
	evidencereport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/evidence"
	htmlreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/html"
	ownerreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/owner"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

// AssessAudience selects which WAF report view to render.
type AssessAudience string

// Supported assessment audiences.
const (
	AssessAudienceOwner     AssessAudience = "owner"
	AssessAudienceDeveloper AssessAudience = "developer"
	AssessAudienceEvidence  AssessAudience = "evidence"
)

// AssessRequest mirrors the assess waf flags.
type AssessRequest struct {
	Dir          string
	Profile      string
	Baseline     string
	Suppress     string
	Strict       bool
	Audience     AssessAudience
	Format       string
	Out          string
	EvidencePack bool
	LLMExplain   bool
	LLMProvider  string
	LLMTimeout   time.Duration
}

// Validate checks flag values and applies defaults.
func (r *AssessRequest) Validate() error {
	if r.Dir == "" {
		r.Dir = "."
	}
	if r.Profile != "" && !slices.Contains([]string{"dev", "test", "prod"}, r.Profile) {
		return Usagef("invalid --profile %q (want dev, test or prod)", r.Profile)
	}
	if r.Audience == "" {
		r.Audience = AssessAudienceOwner
	}
	if r.EvidencePack {
		r.Audience = AssessAudienceEvidence
	}
	switch r.Audience {
	case AssessAudienceOwner, AssessAudienceDeveloper, AssessAudienceEvidence:
	default:
		return Usagef("invalid --audience %q (want owner, developer or evidence)", r.Audience)
	}
	if r.Format == "" {
		r.Format = "markdown"
	}
	switch r.Format {
	case "markdown", "json", "html":
	default:
		return Usagef("invalid --format %q (want markdown, json or html)", r.Format)
	}
	if r.Format == "html" && r.Audience == AssessAudienceEvidence {
		return Usagef("--format html is not supported with --audience evidence")
	}
	if r.Format == "json" && r.Audience != AssessAudienceEvidence {
		return Usagef("--format json is only supported with --audience evidence")
	}
	if r.LLMExplain {
		if r.Audience == AssessAudienceEvidence {
			return Usagef("--llm-explain is not supported with --audience evidence")
		}
		if r.LLMTimeout != 0 && (r.LLMTimeout < llm.MinimumTimeout || r.LLMTimeout > llm.MaximumTimeout) {
			return Usagef("--llm-timeout must be between %s and %s", llm.MinimumTimeout, llm.MaximumTimeout)
		}
	}
	return nil
}

// AssessWAF runs the WAF assessment aggregation and rendering.
func AssessWAF(ctx context.Context, svc Services, req AssessRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	code, err := assessWAF(ctx, svc, req, stdout)
	if err != nil {
		return ExitCodeForError(err), err
	}
	return code, nil
}

func assessWAF(ctx context.Context, svc Services, req AssessRequest, stdout io.Writer) (int, error) {
	catalogRules, err := catalog.Load(ctx, ruledata.FS, ruledata.Root)
	if err != nil {
		return 0, fmt.Errorf("load rule catalogue: %w", err)
	}
	defs := waf.Definitions(catalogRules)
	selectors := make([]string, len(defs))
	for i, d := range defs {
		selectors[i] = d.RuleID
	}
	set, err := svc.Config.Resolve(ctx, ConfigRequest{
		Dir: req.Dir, Profile: req.Profile, Rules: selectors, Baseline: req.Baseline, Suppress: req.Suppress,
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
				_, _ = fmt.Fprintf(svc.Stderr, "warning: %s; template-dependent assessment controls are skipped\n", findings.Redact(err.Error()))
			}
		default:
			return 0, fmt.Errorf("load bicep/arm model: %w", err)
		}
	}
	if svc.Engine == nil {
		return 0, fmt.Errorf("run rules: %w", ErrUnavailable)
	}
	rep, err := svc.Engine.Run(ctx, RunInput{
		Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
		Selectors: selectors, Source: src, AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
	})
	if err != nil {
		return 0, fmt.Errorf("run rules: %w", err)
	}
	fs := rep.Findings
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
	findings.Sort(fs)
	skips := slices.Clone(rep.Skipped)
	slices.SortFunc(skips, func(a, b sdk.Skip) int {
		if a.RuleID != b.RuleID {
			if a.RuleID < b.RuleID {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	scope := "local project evidence only"
	if src.AzureYAMLAt != "" {
		scope = filepath.ToSlash(filepath.Clean(src.AzureYAMLAt)) + " and compiled infrastructure evidence where available"
	}
	assessment, err := assess.Build(assess.Input{
		Profile:     set.Profile,
		ProjectPath: req.Dir,
		Catalogue:   catalogRules,
		Definitions: defs,
		Findings:    fs,
		Skipped:     skips,
		Evaluated:   rep.Evaluated,
		Policy:      set.Policy,
		ToolVersion: ToolVersion,
		ScopeNote:   scope,
		WARALookups: wara.Map(),
	})
	if err != nil {
		return 0, err
	}
	code := ExitCode(Outcome{Findings: fs, Skips: skips, FailOn: "", Strict: req.Strict})
	var buf bytes.Buffer
	if err := renderAssessment(&buf, assessment, req); err != nil {
		return 0, err
	}
	output := buf.Bytes()
	if req.LLMExplain && svc.Narrator != nil {
		narrative, note, err := assessmentNarrative(ctx, svc, req, assessment)
		if err != nil {
			return ExitCodeForError(err), err
		}
		output = appendRenderedNarrative(output, req.Format, narrative, note)
	}
	if err := svc.emit(req.Out, output, stdout); err != nil {
		return 0, err
	}
	return code, nil
}

func assessmentNarrative(ctx context.Context, svc Services, req AssessRequest, assessment assess.Assessment) (*llm.Narrative, string, error) {
	var audience llm.Audience
	switch req.Audience {
	case AssessAudienceOwner:
		audience = llm.AudienceOwner
	default:
		audience = llm.AudienceDeveloper
	}
	narrative, err := svc.Narrator.ExplainAssessment(ctx, assessment, audience, llm.Options{
		Provider: req.LLMProvider,
		Timeout:  req.LLMTimeout,
	})
	if err == nil {
		return &narrative, "", nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, "", err
	}
	return nil, "LLM advisory unavailable (" + findings.Redact(strings.TrimSpace(err.Error())) + "). Deterministic findings remain authoritative.", nil
}

func renderAssessment(w io.Writer, a assess.Assessment, req AssessRequest) error {
	switch req.Audience {
	case AssessAudienceOwner:
		if req.Format == "html" {
			return htmlreport.Render(w, a)
		}
		return ownerreport.Markdown(w, a)
	case AssessAudienceDeveloper:
		if req.Format == "html" {
			return htmlreport.Render(w, a)
		}
		return developerreport.Markdown(w, a)
	case AssessAudienceEvidence:
		if req.Format == "json" {
			return evidencereport.JSON(w, a)
		}
		return evidencereport.Markdown(w, a)
	default:
		return Usagef("unsupported audience %q", req.Audience)
	}
}

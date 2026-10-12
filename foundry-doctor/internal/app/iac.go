package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// bicepCommand is the redacted adapter command shown by --debug. File names are not part of it.
const bicepCommand = "bicep build <entry.bicep> --stdout --no-restore"

// iacResult is the outcome of the infrastructure stage.
type iacResult struct {
	arm   *model.ARMTemplate
	tools []sdk.ToolStatus
	// bicepVersion is the detected compiler version, empty when none ran.
	bicepVersion string
	diags        []model.Diagnostic
	// toolMissing is true when a Bicep project could not be compiled because the CLI is unusable.
	toolMissing bool
	// cannotRun is set when the run must exit 2 (a required tool is unusable); the report is still written.
	cannotRun string
	notes     []string
}

// loadIaC produces the ARM model for the project. Bicep is compiled with the Bicep CLI; there is no Bicep
// parser here (CLAUDE.md). A missing CLI is exit 2 when validation.bicep is required and a skipped
// check with reason tool-missing:bicep when it is optional (PRD section 9).
func loadIaC(ctx context.Context, opts Options, eff config.Effective, disc *project.Discovery, azd *project.StandaloneAzdContext) (iacResult, *ExitError) {
	var res iacResult
	p := disc.Project
	switch p.IaC {
	case model.IaCARMJSON:
		return loadARMJSON(ctx, p, disc, azd)
	case model.IaCBicep:
	default:
		return res, nil
	}

	mode := eff.Validation.Bicep
	if mode == config.ModeDisabled {
		res.tools = []sdk.ToolStatus{{Name: bicep.ToolName, State: sdk.ToolDisabled, Detail: "validation.bicep is disabled"}}
		return res, nil
	}
	required := mode == config.ModeRequired
	look := opts.Bicep.LookPath
	if exe := eff.Advanced.Bicep.Executable; exe != "" && exe != "auto" {
		inner := look
		name := exe
		look = func(string) (string, error) {
			if inner != nil {
				return inner(name)
			}
			return lookPath(name)
		}
	}
	d := bicep.Discover(ctx, bicep.DiscoverOptions{Required: required, LookPath: look, Runner: opts.Bicep.Runner})
	res.tools = []sdk.ToolStatus{d.Status}
	if !d.Available() {
		res.toolMissing = true
		if required {
			res.cannotRun = fmt.Sprintf("the Bicep CLI is required (validation.bicep: required) but is %s: %s", d.Status.State, d.Status.Detail)
		}
		return res, nil
	}
	res.bicepVersion = d.Version.String()

	comp, err := bicep.NewCompiler(d, bicep.CompilerOptions{ProjectDir: p.Root, Runner: opts.Bicep.Runner})
	if err != nil {
		return res, exitErr(sdk.ExitCannotRun, "prepare the Bicep compiler: %w", err)
	}
	entry := path.Join(p.InfraPath, p.InfraModule+".bicep")
	cres, err := comp.Compile(ctx, filepath.FromSlash(entry))
	if err != nil {
		return res, compileFailure(ctx, "compile "+entry, err)
	}
	res.diags = append(res.diags, cres.Diagnostics...)
	for _, s := range cres.Skipped {
		res.notes = append(res.notes, "bicep compile skipped: "+s.Reason)
	}

	var values map[string]any
	if paramFile := path.Join(p.InfraPath, p.InfraModule+".bicepparam"); slices.Contains(disc.Infra.BicepParam, paramFile) {
		pres, perr := comp.CompileParams(ctx, filepath.FromSlash(paramFile))
		if perr != nil {
			return res, compileFailure(ctx, "compile "+paramFile, perr)
		}
		res.diags = append(res.diags, pres.Diagnostics...)
		for _, s := range pres.Skipped {
			res.notes = append(res.notes, "bicepparam skipped: "+s.Reason)
		}
		values = pres.Values
	}
	if !cres.OK {
		return res, nil
	}
	n, err := bicep.Normalise(cres.ARM, bicep.Options{File: entry, ParameterValues: values})
	if err != nil {
		return res, exitErr(sdk.ExitInternal, "normalise the compiled template: %w", err)
	}
	res.arm = n.Template
	for _, w := range n.Warnings {
		res.notes = append(res.notes, "arm: "+w)
	}
	return res, nil
}

func loadARMJSON(ctx context.Context, p model.Project, disc *project.Discovery, azd *project.StandaloneAzdContext) (iacResult, *ExitError) {
	var res iacResult
	file := path.Join(p.InfraPath, p.InfraModule+".json")
	if !slices.Contains(disc.Infra.ARMJSON, file) {
		res.notes = append(res.notes, "no "+file+" ARM template; ARM rules are skipped")
		return res, nil
	}
	raw, err := azd.ReadFile(ctx, file)
	if err != nil {
		return res, exitErr(sdk.ExitCannotRun, "read %s: %w", file, err)
	}
	n, err := bicep.Normalise(raw, bicep.Options{File: file})
	if err != nil {
		res.notes = append(res.notes, file+" is not a readable ARM template; ARM rules are skipped")
		return res, nil
	}
	res.arm = n.Template
	return res, nil
}

// compileFailure maps a compiler error that is not a diagnostic: cancellation and a missing CLI are
// exit 2; a protocol failure (no parsable diagnostic, bad output) is exit 4 (PRD section 7).
func compileFailure(ctx context.Context, what string, err error) *ExitError {
	switch {
	case ctx.Err() != nil:
		return exitErr(sdk.ExitCannotRun, "%s: interrupted: %w", what, ctx.Err())
	case errors.Is(err, bicep.ErrCLIUnavailable), errors.Is(err, bicep.ErrBadInput), errors.Is(err, bicep.ErrTimeout):
		return exitErr(sdk.ExitCannotRun, "%s: %w", what, err)
	}
	return exitErr(sdk.ExitInternal, "%s: %w", what, err)
}

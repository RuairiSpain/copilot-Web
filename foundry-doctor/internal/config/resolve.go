package config

import (
	"fmt"
	"slices"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Flags are the command-line values that override the file. The zero value of every field means "not given".
// Policy keys are never flags (ADR-007 decision 1).
type Flags struct {
	Profile      string   // --profile dev|test|prod or the long form
	Environment  string   // --environment; selects environments.<name>
	AzureYAML    string   // --azure-yaml / project path
	InfraPath    string   // --infra
	Rules        []string // --rules selector; replaces rules.include
	Baseline     string   // --baseline
	Suppressions string   // --suppressions
	Formats      []Format // --format
	OutDir       string   // --out directory
}

// Effective is the fully resolved configuration. It is a value the engine reads; nothing in it is guessed.
type Effective struct {
	Profile       Profile
	ProfileSource string
	Environment   string // selected environment name, empty if none
	Inputs        Inputs
	Rules         Rules
	Validation    Validation
	Adoption      Adoption
	Outputs       Outputs
	Advanced      Advanced
	// Policy has the merged configured values. Absent keys stay nil (rules skip); baselines are applied by the
	// Effective* methods of model.Policy, not written here.
	Policy model.Policy
	// PolicyEntries is the "effective policy" header: every ADR-007 key with its value and source, baselines marked.
	PolicyEntries []PolicyValue
	// Sources maps the non-policy keys (for example "inputs.azureYaml") to flag, repository or default.
	Sources map[string]string
}

// Defaults are the PRD section 6 example values for keys that have no better answer. They are not policy.
func defaultFile() File {
	return File{
		Inputs:     Inputs{AzureYAML: "./azure.yaml", InfraPath: "./infra"},
		Rules:      Rules{Include: []string{"must-have", "nice-to-have"}, Packs: []string{"foundry-core"}},
		Validation: Validation{Bicep: ModeRequired, Azure: ModeOptional, Runtime: ModeDisabled},
		Adoption:   Adoption{Baseline: ".foundry-doctor/baseline.yaml", Suppressions: ".foundry-doctor/suppressions.yaml"},
		Outputs:    Outputs{Formats: []Format{FormatConsole}, Directory: ".foundry-doctor/out"},
		Advanced:   Advanced{PSRule: Tool{Enabled: ToggleAuto}, Checkov: Tool{Enabled: ToggleFalse}, Bicep: BicepTool{Executable: "auto"}},
	}
}

// Resolve applies the PRD section 6 precedence: flags, then environments.<envName>, then the repository file,
// then curated profile defaults (and, for keys with none, the built-in defaults). It performs no I/O.
//
// repo may be nil (no config file). envName selects environments.<envName>.policy; when empty it falls back to
// flags.Environment, then inputs.environment of the file. An environment with no entry in the file is not an error.
// Only policy can be set per environment (ADR-010 decision 7).
func Resolve(flags Flags, repo *File, envName string, profile ProfileSet) (Effective, error) {
	var is issues
	if repo != nil {
		if err := repo.Validate(); err != nil {
			return Effective{}, err
		}
	}
	if repo == nil {
		repo = &File{}
	}
	d := defaultFile()
	eff := Effective{Sources: map[string]string{}}

	// Environment selection: argument > flag > file.
	envSrc := SourceFlag
	switch {
	case envName != "":
	case flags.Environment != "":
		envName = flags.Environment
	default:
		envName, envSrc = repo.Inputs.Environment, SourceRepository
	}
	if envName != "" && !envNameRe.MatchString(envName) {
		is.add("environment", "%q must match %s", envName, envNameRe)
	}
	eff.Environment = envName
	if envName != "" {
		eff.Sources["inputs.environment"] = envSrc
	}

	// Profile: flag > file > default.
	raw, src := DefaultProfile, SourceDefault
	switch {
	case flags.Profile != "":
		raw, src = flags.Profile, SourceFlag
	case repo.Profile != "":
		raw, src = repo.Profile, SourceRepository
	}
	name, err := NormalizeProfile(raw)
	if err != nil {
		return Effective{}, err
	}
	p, ok := profile.Get(name)
	if !ok {
		return Effective{}, &Error{Msg: fmt.Sprintf("profile %q is not in the profile set", name)}
	}
	eff.Profile, eff.ProfileSource = p, src

	// Flag value shapes.
	Inputs{AzureYAML: flags.AzureYAML, InfraPath: flags.InfraPath}.validate("flag", &is, false)
	Outputs{Formats: flags.Formats, Directory: flags.OutDir}.validate("flag", &is, false)
	Rules{Include: flags.Rules}.validate("flag.rules", &is)
	checkPath("flag.baseline", flags.Baseline, false, &is)
	checkPath("flag.suppressions", flags.Suppressions, false, &is)
	if len(is) > 0 {
		ps := make([]*Error, len(is))
		for i, x := range is {
			ps[i] = &Error{Path: x.Path, Msg: x.Msg}
		}
		return Effective{}, newInvalid(ps)
	}

	// Scalars and lists: flag > repository > default.
	pick := func(key, flag, file, def string) string {
		switch {
		case flag != "":
			eff.Sources[key] = SourceFlag
			return flag
		case file != "":
			eff.Sources[key] = SourceRepository
			return file
		}
		eff.Sources[key] = SourceDefault
		return def
	}
	pickList := func(key string, flag, file, def []string) []string {
		switch {
		case flag != nil:
			eff.Sources[key] = SourceFlag
			return slices.Clone(flag)
		case file != nil:
			eff.Sources[key] = SourceRepository
			return slices.Clone(file)
		}
		eff.Sources[key] = SourceDefault
		return slices.Clone(def)
	}
	pickMode := func(key string, file, def Mode) Mode {
		if file != "" {
			eff.Sources[key] = SourceRepository
			return file
		}
		eff.Sources[key] = SourceDefault
		return def
	}
	pickToggle := func(key string, file, def Toggle) Toggle {
		if file != "" {
			eff.Sources[key] = SourceRepository
			return file
		}
		eff.Sources[key] = SourceDefault
		return def
	}

	eff.Inputs = Inputs{
		AzureYAML:   pick("inputs.azureYaml", flags.AzureYAML, repo.Inputs.AzureYAML, d.Inputs.AzureYAML),
		InfraPath:   pick("inputs.infraPath", flags.InfraPath, repo.Inputs.InfraPath, d.Inputs.InfraPath),
		Environment: envName,
	}
	eff.Rules = Rules{
		Include: pickList("rules.include", flags.Rules, repo.Rules.Include, d.Rules.Include),
		Exclude: pickList("rules.exclude", nil, repo.Rules.Exclude, nil),
		Packs:   pickList("rules.packs", nil, repo.Rules.Packs, d.Rules.Packs),
	}
	eff.Validation = Validation{
		Bicep:   pickMode("validation.bicep", repo.Validation.Bicep, d.Validation.Bicep),
		Azure:   pickMode("validation.azure", repo.Validation.Azure, d.Validation.Azure),
		Runtime: pickMode("validation.runtime", repo.Validation.Runtime, d.Validation.Runtime),
	}
	eff.Adoption = Adoption{
		Baseline:     pick("adoption.baseline", flags.Baseline, repo.Adoption.Baseline, d.Adoption.Baseline),
		Suppressions: pick("adoption.suppressions", flags.Suppressions, repo.Adoption.Suppressions, d.Adoption.Suppressions),
	}
	var formats []Format
	switch {
	case flags.Formats != nil:
		formats, eff.Sources["outputs.formats"] = slices.Clone(flags.Formats), SourceFlag
	case repo.Outputs.Formats != nil:
		formats, eff.Sources["outputs.formats"] = slices.Clone(repo.Outputs.Formats), SourceRepository
	default:
		formats, eff.Sources["outputs.formats"] = slices.Clone(d.Outputs.Formats), SourceDefault
	}
	eff.Outputs = Outputs{Formats: formats, Directory: pick("outputs.directory", flags.OutDir, repo.Outputs.Directory, d.Outputs.Directory)}
	eff.Advanced = Advanced{
		PSRule:   Tool{Enabled: pickToggle("advanced.psrule.enabled", repo.Advanced.PSRule.Enabled, d.Advanced.PSRule.Enabled), Config: cloneValue(repo.Advanced.PSRule.Config)},
		Checkov:  Tool{Enabled: pickToggle("advanced.checkov.enabled", repo.Advanced.Checkov.Enabled, d.Advanced.Checkov.Enabled), Config: cloneValue(repo.Advanced.Checkov.Config)},
		Bicep:    BicepTool{Executable: pick("advanced.bicep.executable", "", repo.Advanced.Bicep.Executable, d.Advanced.Bicep.Executable), Config: cloneValue(repo.Advanced.Bicep.Config)},
		Adapters: pickList("advanced.adapters", nil, repo.Advanced.Adapters, nil),
	}

	// Policy: repository, then environments.<name>.policy over it. Flags carry no policy keys.
	srcs := map[string]string{}
	mergePolicy(&eff.Policy, repo.Policy, srcs, SourceRepository)
	if envName != "" {
		if e, ok := repo.Environments[envName]; ok {
			mergePolicy(&eff.Policy, e.Policy, srcs, SourceEnvironment(envName))
		}
	}
	eff.PolicyEntries = effectiveEntries(eff.Policy, srcs)
	return eff, nil
}

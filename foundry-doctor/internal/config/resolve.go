package config

import (
	"fmt"
	"slices"
	"strings"
)

// Environment variable names consulted by Resolve. The caller injects the
// values; this package never reads the process environment.
const (
	EnvProfile     = "FOUNDRY_DOCTOR_PROFILE"
	EnvEnvironment = "FOUNDRY_DOCTOR_ENVIRONMENT"
	EnvOutputDir   = "FOUNDRY_DOCTOR_OUTPUT_DIR"
	EnvFormats     = "FOUNDRY_DOCTOR_FORMATS"
)

// Flags are command-line overrides. Empty values mean "not set".
type Flags struct {
	Profile      string
	Environment  string
	AzureYAML    string
	InfraPath    string
	Formats      []string
	OutputDir    string
	Baseline     string
	Suppressions string
}

// Effective is the fully resolved configuration.
type Effective struct {
	Profile     Profile
	Environment string
	Inputs      Inputs
	Rules       Rules
	Validation  Validation
	Adoption    Adoption
	Outputs     Outputs
	Advanced    Advanced
	Policy      EffectivePolicy
	// Sources records which layer supplied the profile, environment and each
	// policy key: flag, env, repo, environment or default.
	Sources map[string]string
}

// Resolve merges flags > env > environments.<name>.policy > repo > profile
// defaults. cfg may be nil (no config file). env is the injected environment.
func Resolve(cfg *Config, flags Flags, env map[string]string) (*Effective, error) {
	if cfg == nil {
		cfg = &Config{Version: Version}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	src := map[string]string{}
	pick := func(key string, layers ...[2]string) string {
		for _, l := range layers {
			if l[1] != "" {
				src[key] = l[0]
				return l[1]
			}
		}
		src[key] = "default"
		return ""
	}

	profName := pick("profile", [2]string{"flag", flags.Profile}, [2]string{"env", env[EnvProfile]}, [2]string{"repo", cfg.Profile})
	if profName == "" {
		profName = DefaultProfile
	}
	prof, ok := LookupProfile(profName)
	if !ok {
		return nil, fmt.Errorf("%w: profile %q is not one of %s", ErrInvalid, profName, strings.Join(ProfileNames(), ", "))
	}

	e := &Effective{Profile: prof, Sources: src}
	e.Environment = pick("environment", [2]string{"flag", flags.Environment}, [2]string{"env", env[EnvEnvironment]}, [2]string{"repo", cfg.Inputs.Environment})
	if e.Environment != "" && !envNameRe.MatchString(e.Environment) {
		return nil, fmt.Errorf("%w: environment %q is not a valid environment name", ErrInvalid, e.Environment)
	}
	e.Inputs = Inputs{
		AzureYAML:   firstNonEmpty(flags.AzureYAML, cfg.Inputs.AzureYAML, "./azure.yaml"),
		InfraPath:   firstNonEmpty(flags.InfraPath, cfg.Inputs.InfraPath, "./infra"),
		Environment: e.Environment,
	}
	e.Adoption = Adoption{
		Baseline:     firstNonEmpty(flags.Baseline, cfg.Adoption.Baseline, ".foundry-doctor/baseline.yaml"),
		Suppressions: firstNonEmpty(flags.Suppressions, cfg.Adoption.Suppressions, ".foundry-doctor/suppressions.yaml"),
	}
	formats := flags.Formats
	if len(formats) == 0 && env[EnvFormats] != "" {
		for _, f := range strings.Split(env[EnvFormats], ",") {
			if f = strings.TrimSpace(f); f != "" {
				formats = append(formats, f)
			}
		}
	}
	if len(formats) == 0 {
		formats = cfg.Outputs.Formats
	}
	if len(formats) == 0 {
		formats = prof.Formats
	}
	for _, f := range formats {
		if !slices.Contains(formatsSet, f) {
			return nil, fmt.Errorf("%w: output format %q must be one of %s", ErrInvalid, f, strings.Join(formatsSet, ", "))
		}
	}
	e.Outputs = Outputs{
		Formats:   dedupe(formats),
		Directory: firstNonEmpty(flags.OutputDir, env[EnvOutputDir], cfg.Outputs.Directory, ".foundry-doctor/out"),
	}

	e.Rules = Rules{Include: slices.Clone(cfg.Rules.Include), Exclude: slices.Clone(cfg.Rules.Exclude), Packs: slices.Clone(cfg.Rules.Packs)}
	if len(e.Rules.Include) == 0 {
		e.Rules.Include = []string{"must-have", "nice-to-have"}
	}
	if len(e.Rules.Packs) == 0 {
		e.Rules.Packs = []string{"foundry-core"}
	}

	e.Validation = Validation{
		Bicep:   modeOr(cfg.Validation.Bicep, prof.Validation.Bicep),
		Azure:   modeOr(cfg.Validation.Azure, prof.Validation.Azure),
		Runtime: modeOr(cfg.Validation.Runtime, prof.Validation.Runtime),
	}
	e.Advanced = cfg.Advanced
	e.Advanced.Adapters = slices.Clone(cfg.Advanced.Adapters)
	if e.Advanced.PSRule.Enabled == "" {
		e.Advanced.PSRule.Enabled = prof.PSRule
	}
	if e.Advanced.Checkov.Enabled == "" {
		e.Advanced.Checkov.Enabled = prof.Checkov
	}
	if e.Advanced.Bicep.Executable == "" {
		e.Advanced.Bicep.Executable = "auto"
	}

	// Policy: defaults < repo < environment.
	pol := EffectivePolicy{
		"resourceScope":                       DefaultResourceScope,
		"allowedExternalScopes":               []string{},
		"network.publicAccess":                DefaultPublicAccess,
		"network.dnsManagedByPolicy":          false,
		"network.centralDns":                  false,
		"monitoring.publicTelemetry":          false,
		"managedByAzurePolicy":                []string{},
		"preflight.deploymentHistoryMargin":   DefaultDeploymentHistoryMargin,
		"preflight.regionMatrixStalenessDays": DefaultRegionMatrixStalenessDays,
	}
	pols := map[string]string{}
	for k := range pol {
		pols[k] = "default"
	}
	for k, v := range cfg.Policy.flatten() {
		pol[k], pols[k] = v, "repo"
	}
	if ec, ok := cfg.Environments[e.Environment]; ok && e.Environment != "" {
		for k, v := range ec.Policy.flatten() {
			pol[k], pols[k] = v, "environment"
		}
	}
	e.Policy = pol
	for k, v := range pols {
		src["policy."+k] = v
	}
	return e, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func modeOr(m, def Mode) Mode {
	if m != "" {
		return m
	}
	return def
}

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

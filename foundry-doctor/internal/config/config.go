// Package config loads the version 1 unified configuration, validates it
// strictly (unknown keys fail), and resolves the effective settings with the
// precedence flags > environment > repository > curated profile defaults.
//
// Profiles set severities and tool defaults only; they never supply
// organisation values. Policy keys and their defaults are defined by ADR-007.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is the only supported configuration schema version.
const Version = 1

// MaxConfigBytes bounds the size of a configuration file.
const MaxConfigBytes = 1 << 20

// ErrInvalid wraps every content problem in a configuration file so callers can
// separate it from I/O failures with errors.Is.
var ErrInvalid = errors.New("invalid configuration")

// Config is the repository configuration file. Every field is optional except
// version.
type Config struct {
	Version      int                  `yaml:"version"`
	Profile      string               `yaml:"profile,omitempty"`
	Inputs       Inputs               `yaml:"inputs,omitempty"`
	Rules        Rules                `yaml:"rules,omitempty"`
	Validation   Validation           `yaml:"validation,omitempty"`
	Policy       Policy               `yaml:"policy,omitempty"`
	Adoption     Adoption             `yaml:"adoption,omitempty"`
	Outputs      Outputs              `yaml:"outputs,omitempty"`
	Advanced     Advanced             `yaml:"advanced,omitempty"`
	Environments map[string]EnvConfig `yaml:"environments,omitempty"`
}

// EnvConfig holds per-environment overrides. Only policy may be overridden.
type EnvConfig struct {
	Policy Policy `yaml:"policy,omitempty"`
}

// Inputs locates the project files.
type Inputs struct {
	AzureYAML   string `yaml:"azureYaml,omitempty"`
	InfraPath   string `yaml:"infraPath,omitempty"`
	Environment string `yaml:"environment,omitempty"`
}

// Rules selects which rules run. Include holds categories; Exclude holds
// categories or rule IDs; Packs names rule packs.
type Rules struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
	Packs   []string `yaml:"packs,omitempty"`
}

// Mode is the requirement level of a validation plane.
type Mode string

// Validation plane modes.
const (
	ModeRequired Mode = "required"
	ModeOptional Mode = "optional"
	ModeDisabled Mode = "disabled"
)

// Validation sets how each validation plane behaves.
type Validation struct {
	Bicep   Mode `yaml:"bicep,omitempty"`
	Azure   Mode `yaml:"azure,omitempty"`
	Runtime Mode `yaml:"runtime,omitempty"`
}

// Adoption locates the baseline and suppression files.
type Adoption struct {
	Baseline     string `yaml:"baseline,omitempty"`
	Suppressions string `yaml:"suppressions,omitempty"`
}

// Outputs selects report formats and the output directory.
type Outputs struct {
	Formats   []string `yaml:"formats,omitempty"`
	Directory string   `yaml:"directory,omitempty"`
}

// Tristate is auto, true or false.
type Tristate string

// Tristate values.
const (
	Auto  Tristate = "auto"
	True  Tristate = "true"
	False Tristate = "false"
)

// UnmarshalYAML accepts the scalars auto, true and false only.
func (t *Tristate) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: enabled must be auto, true or false", n.Line)
	}
	switch n.Value {
	case "auto", "true", "false":
		*t = Tristate(n.Value)
		return nil
	}
	return fmt.Errorf("line %d: enabled must be auto, true or false, got %q", n.Line, n.Value)
}

// MarshalYAML renders true/false as booleans and auto as a string.
func (t Tristate) MarshalYAML() (any, error) {
	switch t {
	case True:
		return true, nil
	case False:
		return false, nil
	}
	return string(t), nil
}

// ToolConfig configures an optional lower-level scanner.
type ToolConfig struct {
	Enabled Tristate `yaml:"enabled,omitempty"`
	Config  *string  `yaml:"config,omitempty"`
}

// BicepConfig configures the Bicep CLI adapter.
type BicepConfig struct {
	Executable string  `yaml:"executable,omitempty"`
	Config     *string `yaml:"config,omitempty"`
}

// Advanced is the escape hatch for integrated tools.
type Advanced struct {
	PSRule   ToolConfig  `yaml:"psrule,omitempty"`
	Checkov  ToolConfig  `yaml:"checkov,omitempty"`
	Bicep    BicepConfig `yaml:"bicep,omitempty"`
	Adapters []string    `yaml:"adapters,omitempty"`
}

var (
	envNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	ruleIDRe   = regexp.MustCompile(`^FND-[A-Z]+-[0-9]{3}$`)
	formatsSet = []string{"console", "json", "markdown", "sarif"}
)

// Load decodes and validates a configuration file. Unknown keys, multiple
// documents and invalid values are errors wrapping ErrInvalid.
func Load(data []byte) (*Config, error) {
	if len(data) > MaxConfigBytes {
		return nil, fmt.Errorf("%w: file exceeds %d bytes", ErrInvalid, MaxConfigBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: file is empty", ErrInvalid)
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%w: multiple YAML documents are not supported", ErrInvalid)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks every field and returns all problems joined, wrapping ErrInvalid.
func (c *Config) Validate() error {
	var probs []error
	add := func(format string, a ...any) { probs = append(probs, fmt.Errorf(format, a...)) }
	if c.Version != Version {
		add("version must be %d, got %d", Version, c.Version)
	}
	if c.Profile != "" {
		if _, ok := LookupProfile(c.Profile); !ok {
			add("profile %q is not one of %s", c.Profile, strings.Join(ProfileNames(), ", "))
		}
	}
	if c.Inputs.Environment != "" && !envNameRe.MatchString(c.Inputs.Environment) {
		add("inputs.environment %q is not a valid environment name", c.Inputs.Environment)
	}
	for _, p := range []struct{ k, v string }{
		{"inputs.azureYaml", c.Inputs.AzureYAML}, {"inputs.infraPath", c.Inputs.InfraPath},
		{"adoption.baseline", c.Adoption.Baseline}, {"adoption.suppressions", c.Adoption.Suppressions},
		{"outputs.directory", c.Outputs.Directory},
	} {
		if strings.ContainsRune(p.v, 0) {
			add("%s contains a NUL byte", p.k)
		}
	}
	for _, v := range c.Rules.Include {
		if v != "must-have" && v != "nice-to-have" {
			add("rules.include %q must be must-have or nice-to-have", v)
		}
	}
	for _, v := range c.Rules.Exclude {
		if v != "must-have" && v != "nice-to-have" && !ruleIDRe.MatchString(v) {
			add("rules.exclude %q must be a category or a rule ID (FND-<GROUP>-<NNN>)", v)
		}
	}
	for _, v := range c.Rules.Packs {
		if strings.TrimSpace(v) == "" {
			add("rules.packs entries must not be empty")
		}
	}
	for _, m := range []struct {
		k string
		v Mode
	}{{"validation.bicep", c.Validation.Bicep}, {"validation.azure", c.Validation.Azure}, {"validation.runtime", c.Validation.Runtime}} {
		if m.v != "" && m.v != ModeRequired && m.v != ModeOptional && m.v != ModeDisabled {
			add("%s %q must be required, optional or disabled", m.k, m.v)
		}
	}
	for _, f := range c.Outputs.Formats {
		if !slices.Contains(formatsSet, f) {
			add("outputs.formats %q must be one of %s", f, strings.Join(formatsSet, ", "))
		}
	}
	for _, a := range c.Advanced.Adapters {
		if strings.TrimSpace(a) == "" {
			add("advanced.adapters entries must not be empty")
		}
	}
	probs = append(probs, c.Policy.validate("policy")...)
	names := make([]string, 0, len(c.Environments))
	for n := range c.Environments {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if !envNameRe.MatchString(n) {
			add("environments.%s: invalid environment name", n)
			continue
		}
		probs = append(probs, c.Environments[n].Policy.validate("environments."+n+".policy")...)
	}
	if len(probs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalid, errors.Join(probs...))
}

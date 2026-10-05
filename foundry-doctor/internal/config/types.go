// Package config loads, validates and resolves the Foundry Doctor configuration (PRD section 6,
// ADR-007, ADR-010): the strict file loader, the closed policy schema, the curated profiles and
// the precedence rule flags > environments.<name> > repository file > curated profile defaults.
//
// Everything except Load and LoadOptional is a pure function.
package config

import (
	"fmt"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"go.yaml.in/yaml/v3"
)

// DefaultPath is the repository config file, relative to the project root (ADR-010 decision 5).
const DefaultPath = ".foundry-doctor/config.yaml"

// SchemaVersion is the only accepted value of the version key.
const SchemaVersion = 1

// File is the decoded .foundry-doctor/config.yaml. The yaml tags are the closed schema.
// Unset scalars are the zero value, unset lists are nil.
type File struct {
	Version      int                    `yaml:"version"`
	Profile      string                 `yaml:"profile,omitempty"`
	Inputs       Inputs                 `yaml:"inputs,omitempty"`
	Rules        Rules                  `yaml:"rules,omitempty"`
	Validation   Validation             `yaml:"validation,omitempty"`
	Policy       model.Policy           `yaml:"policy,omitempty"`
	Environments map[string]Environment `yaml:"environments,omitempty"`
	Adoption     Adoption               `yaml:"adoption,omitempty"`
	Outputs      Outputs                `yaml:"outputs,omitempty"`
	Advanced     Advanced               `yaml:"advanced,omitempty"`

	name  string         // file name used in errors
	lines map[string]int // key path -> 1-based line, filled by the loader
}

// Inputs is the inputs block.
type Inputs struct {
	AzureYAML   string `yaml:"azureYaml,omitempty"`
	InfraPath   string `yaml:"infraPath,omitempty"`
	Environment string `yaml:"environment,omitempty"`
}

// Rules is the rules block: selectors and packs.
type Rules struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
	Packs   []string `yaml:"packs,omitempty"`
}

// Mode is a validation plane mode.
type Mode string

// Mode values (PRD section 6 example: bicep required, azure optional, runtime disabled).
const (
	ModeRequired Mode = "required"
	ModeOptional Mode = "optional"
	ModeDisabled Mode = "disabled"
)

// Validation is the validation block.
type Validation struct {
	Bicep   Mode `yaml:"bicep,omitempty"`
	Azure   Mode `yaml:"azure,omitempty"`
	Runtime Mode `yaml:"runtime,omitempty"`
}

// Environment is environments.<name>; only policy is allowed (ADR-010 decision 7).
type Environment struct {
	Policy model.Policy `yaml:"policy,omitempty"`
}

// Adoption is the adoption block.
type Adoption struct {
	Baseline     string `yaml:"baseline,omitempty"`
	Suppressions string `yaml:"suppressions,omitempty"`
}

// Format is an output format.
type Format string

// Format values (PRD section 4 and 9).
const (
	FormatConsole  Format = "console"
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
	FormatSARIF    Format = "sarif"
)

// Outputs is the outputs block.
type Outputs struct {
	Formats   []Format `yaml:"formats,omitempty"`
	Directory string   `yaml:"directory,omitempty"`
}

// Toggle is "auto", "true" or "false". YAML booleans are accepted, as in the PRD example (checkov enabled: false).
type Toggle string

// Toggle values.
const (
	ToggleAuto  Toggle = "auto"
	ToggleTrue  Toggle = "true"
	ToggleFalse Toggle = "false"
)

// UnmarshalYAML accepts a scalar only; the value is validated later with the other fields.
func (t *Toggle) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: expected auto, true or false", n.Line)
	}
	*t = Toggle(n.Value)
	return nil
}

// Tool is the shared shape of the psrule and checkov blocks.
type Tool struct {
	Enabled Toggle  `yaml:"enabled,omitempty"`
	Config  *string `yaml:"config,omitempty"`
}

// BicepTool is advanced.bicep. Executable is "auto" or a bare command name (never a path: a repository
// file must not be able to point the doctor at an arbitrary binary).
type BicepTool struct {
	Executable string  `yaml:"executable,omitempty"`
	Config     *string `yaml:"config,omitempty"`
}

// Advanced is the advanced block.
type Advanced struct {
	PSRule   Tool      `yaml:"psrule,omitempty"`
	Checkov  Tool      `yaml:"checkov,omitempty"`
	Bicep    BicepTool `yaml:"bicep,omitempty"`
	Adapters []string  `yaml:"adapters,omitempty"`
}

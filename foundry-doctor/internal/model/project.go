// Package model holds the plain data the rule engine reads: the project, the azure.yaml view,
// the compiled ARM model, environment values, the effective policy, the dependency graph and the
// Input bundle handed to every rule. It imports no azd SDK and no rule package (ADR-005).
package model

// IaCMode says where the infrastructure definition comes from (ADR-004).
type IaCMode string

// IaCMode values.
const (
	IaCBicep     IaCMode = "bicep"     // a Bicep module exists on disk and was compiled
	IaCARMJSON   IaCMode = "arm-json"  // an ARM JSON template exists on disk
	IaCSynthetic IaCMode = "synthetic" // azure.yaml only; the provider synthesises the template
	IaCNone      IaCMode = "none"      // no infrastructure at all
)

// Project is the discovered project identity. Paths are relative to Root with forward slashes.
type Project struct {
	Root               string // absolute; used for reading only, never printed in a report
	Name               string // azure.yaml name, or the directory base name
	AzureYAMLPath      string // usually "azure.yaml"
	IaC                IaCMode
	InfraPath          string // "infra" by default
	InfraModule        string // "main" by default
	Environments       []string
	CurrentEnvironment string
}

// HasARM reports whether a compiled ARM template can exist for this project.
func (p Project) HasARM() bool { return p.IaC == IaCBicep || p.IaC == IaCARMJSON }

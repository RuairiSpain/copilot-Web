// Package plan produces the DeploymentPlan: the validated, normalised and ordered output
// of the schema engine.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/graph"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/validate"
	"github.com/RuairiSpain/copilot-Web/x-foundry/schemas"
)

// Node is one resource in deployment order.
type Node struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Stage     int      `json:"stage"`
	Scope     string   `json:"scope,omitempty"`
	Existing  bool     `json:"existing,omitempty"`
	DependsOn []string `json:"dependsOn"`
}

// Plan is everything later phases need: normalised configuration and the ordered graph.
type Plan struct {
	SchemaVersion string            `json:"schemaVersion"`
	Config        *normalise.Config `json:"config"`
	Nodes         []Node            `json:"nodes"`
	Layers        [][]string        `json:"layers"`
	Warnings      []diag.Diagnostic `json:"warnings,omitempty"`
}

// Order returns node ids with every dependency before its dependents.
func (p *Plan) Order() []string {
	out := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		out[i] = n.ID
	}
	return out
}

// Node returns the node with the given id.
func (p *Plan) Node(id string) (Node, bool) {
	for _, n := range p.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// JSON renders the plan; indent "" gives compact output.
func (p *Plan) JSON(indent string) ([]byte, error) {
	if indent == "" {
		return json.Marshal(p)
	}
	return json.MarshalIndent(p, "", indent)
}

// Analysis is the non-failing result: a plan when there are no errors, plus every diagnostic.
type Analysis struct {
	Plan        *Plan
	Diagnostics []diag.Diagnostic
}

// OK reports whether a plan was produced.
func (a Analysis) OK() bool { return a.Plan != nil }

// Err returns a *diag.Failure when there is no plan.
func (a Analysis) Err() error {
	if a.Plan != nil {
		return nil
	}
	return &diag.Failure{Diagnostics: a.Diagnostics}
}

func build(cfg *normalise.Config, warnings []diag.Diagnostic) (*Plan, error) {
	g := graph.Build(cfg)
	order, err := g.TopologicalOrder()
	if err != nil {
		return nil, err
	}
	layers, err := g.Layers()
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(order))
	for _, id := range order {
		n := g.Node(id)
		nodes = append(nodes, Node{ID: id, Kind: n.Kind, Stage: n.Stage, Scope: n.Scope, Existing: n.Existing, DependsOn: g.DependenciesOf(id)})
	}
	return &Plan{SchemaVersion: schemas.SchemaVersion, Config: cfg, Nodes: nodes, Layers: layers, Warnings: warnings}, nil
}

// Options adjust an analysis without editing the document.
type Options struct {
	// Environment overrides defaults.environment (dev, test or prod), for example to preview
	// the Well-Architected recommendations of a stricter environment.
	Environment string
}

// Environments are the valid environment profiles.
var Environments = []string{"dev", "test", "prod"}

// AnalyseParsed validates, normalises and plans an already-parsed document. Each phase
// that reports errors stops the pipeline so later phases can rely on earlier guarantees.
func AnalyseParsed(p *parser.Parsed) (Analysis, error) { return AnalyseParsedWith(p, Options{}) }

// AnalyseParsedWith is AnalyseParsed with options.
func AnalyseParsedWith(p *parser.Parsed, opts Options) (Analysis, error) {
	if opts.Environment != "" {
		valid := false
		for _, e := range Environments {
			valid = valid || e == opts.Environment
		}
		if !valid {
			return Analysis{}, fmt.Errorf("unknown environment %q (use dev, test or prod)", opts.Environment)
		}
		clone := *p
		clone.Config = config.Clone(p.Config)
		clone.Config.Defaults.Environment = opts.Environment
		p = &clone
	}
	diags := validate.Declared(p.Config)
	if diag.HasErrors(diags) {
		return Analysis{Diagnostics: diags}, nil
	}
	res := normalise.Normalise(p.Config)
	diags = append(diags, res.Diagnostics...)
	if diag.HasErrors(diags) {
		return Analysis{Diagnostics: diags}, nil
	}
	diags = append(diags, validate.Effective(res.Config, p.Config)...)
	if diag.HasErrors(diags) {
		return Analysis{Diagnostics: diags}, nil
	}
	var warnings []diag.Diagnostic
	for _, d := range diags {
		if d.Severity == diag.Warning {
			warnings = append(warnings, d)
		}
	}
	pl, err := build(res.Config, warnings)
	if err != nil {
		return Analysis{}, err
	}
	return Analysis{Plan: pl, Diagnostics: diags}, nil
}

func analyse(opts Options, p *parser.Parsed, err error) (Analysis, error) {
	var failure *diag.Failure
	if errors.As(err, &failure) {
		return Analysis{Diagnostics: failure.Diagnostics}, nil
	}
	if err != nil {
		return Analysis{}, err
	}
	return AnalyseParsedWith(p, opts)
}

// AnalyseText analyses azure.yaml text.
func AnalyseText(text, source string) (Analysis, error) {
	p, err := parser.ParseText(text, source)
	return analyse(Options{}, p, err)
}

// AnalyseFile analyses an azure.yaml file.
func AnalyseFile(path string) (Analysis, error) { return AnalyseFileWith(path, Options{}) }

// AnalyseFileWith is AnalyseFile with options.
func AnalyseFileWith(path string, opts Options) (Analysis, error) {
	p, err := parser.ParseFile(path)
	return analyse(opts, p, err)
}

// AnalyseMapping analyses an already-loaded azure.yaml mapping.
func AnalyseMapping(doc any, source string) (Analysis, error) {
	p, err := parser.ParseMapping(doc, source)
	return analyse(Options{}, p, err)
}

func must(a Analysis, err error) (*Plan, error) {
	if err != nil {
		return nil, err
	}
	if !a.OK() {
		return nil, a.Err()
	}
	return a.Plan, nil
}

// BuildFile returns the plan for a file, or a *diag.Failure listing every error.
func BuildFile(path string) (*Plan, error) { return must(AnalyseFile(path)) }

// BuildText returns the plan for azure.yaml text.
func BuildText(text string) (*Plan, error) { return must(AnalyseText(text, "<string>")) }

// BuildMapping returns the plan for a loaded azure.yaml mapping.
func BuildMapping(doc any) (*Plan, error) { return must(AnalyseMapping(doc, "<mapping>")) }

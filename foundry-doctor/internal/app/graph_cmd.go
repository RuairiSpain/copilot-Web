package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	graphviz "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/graphviz"
	graphjson "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/json"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/mermaid"
	graphview "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	graphreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var graphClient = defaultAzureClient

// graphvizRenderer is the optional SVG/PNG adapter; tests replace it.
var graphvizRenderer = graphviz.Binary{}

type GraphTarget struct {
	SubscriptionID string
	ResourceGroup  string
	Account        string
	Project        string
}

type GraphRequest struct {
	Dir            string
	Mode           graphview.Mode
	Format         string
	Out            string
	RedactIDs      bool
	MaxNodes       int
	MaxEdges       int
	CollapseAfter  int
	FindingsReport []string
	Target         GraphTarget
	// DiffBase and DiffTarget are graph JSON files (from --format json) for
	// the environment-difference graph (Mode diff).
	DiffBase   string
	DiffTarget string
}

func (r *GraphRequest) Validate() error {
	if r.Mode == "" && (r.DiffBase != "" || r.DiffTarget != "") {
		r.Mode = graphview.ModeDiff
	}
	if r.Mode == "" {
		r.Mode = graphview.ModeSource
	}
	switch r.Mode {
	case graphview.ModeSource, graphview.ModeDeployed, graphview.ModeCombined, graphview.ModeDiff:
	default:
		return Usagef("invalid graph mode %q", r.Mode)
	}
	if r.Format == "" {
		r.Format = "mermaid"
	}
	switch r.Format {
	case "mermaid", "json", "markdown", "dot", "html", "svg", "png":
	default:
		return Usagef("invalid --format %q for graph (want mermaid, json, markdown, dot, html, svg or png)", r.Format)
	}
	if r.Mode == graphview.ModeDiff {
		if r.DiffBase == "" || r.DiffTarget == "" {
			return Usagef("--diff-base and --diff-target are both required for an environment-difference graph")
		}
		if r.Target != (GraphTarget{}) || len(r.FindingsReport) > 0 {
			return Usagef("--diff-base/--diff-target compare two rendered graph JSON files; Azure target and --findings-report flags do not apply")
		}
		return nil
	}
	if r.DiffBase != "" || r.DiffTarget != "" {
		return Usagef("--diff-base/--diff-target cannot be combined with --%s", r.Mode)
	}
	if r.CollapseAfter < 0 || r.MaxNodes < 0 || r.MaxEdges < 0 {
		return Usagef("graph limits must be non-negative")
	}
	if (r.Mode == graphview.ModeDeployed || r.Mode == graphview.ModeCombined) && r.Target.SubscriptionID == "" {
		return Usagef("--subscription is required for --%s", r.Mode)
	}
	if r.Target.Account != "" && r.Target.ResourceGroup == "" {
		return Usagef("--resource-group is required with --account")
	}
	if r.Target.Project != "" && (r.Target.Account == "" || r.Target.ResourceGroup == "") {
		return Usagef("--project requires --account and --resource-group")
	}
	for _, v := range []struct {
		name  string
		value string
	}{
		{name: "resource-group", value: r.Target.ResourceGroup},
		{name: "account", value: r.Target.Account},
		{name: "project", value: r.Target.Project},
	} {
		if strings.ContainsAny(v.value, `/\`) {
			return Usagef("--%s must be a single path segment", v.name)
		}
	}
	return nil
}

func Graph(ctx context.Context, svc Services, req GraphRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	if req.Dir == "" {
		req.Dir = "."
	}
	if req.Mode == graphview.ModeDiff {
		return graphDiff(ctx, svc, req, stdout)
	}
	var (
		doc           *azureyaml.Document
		coreServices  []coregraph.Service
		coreResources []coregraph.Resource
		inventory     []azure.Resource
		runtimeIn     graphview.RuntimeInput
		scope         string
		notes         []string
	)
	if req.Mode != graphview.ModeDeployed {
		set, err := svc.Config.Resolve(ctx, ConfigRequest{Dir: req.Dir})
		if err != nil {
			return ExitCodeForError(err), fmt.Errorf("resolve config: %w", err)
		}
		src, err := svc.Project.Load(ctx, req.Dir, set.Inputs)
		if err != nil {
			return ExitCodeForError(err), fmt.Errorf("discover project: %w", err)
		}
		doc, err = azureyaml.Parse(src.AzureYAML, src.AzureYAMLAt)
		if err != nil {
			err = fmt.Errorf("%w: parse azure.yaml: %w", ErrUnavailable, err)
			return ExitCodeForError(err), err
		}
		for _, w := range src.Warnings {
			if svc.Stderr != nil {
				_, _ = fmt.Fprintf(svc.Stderr, "warning: %s\n", w)
			}
		}
		coreServices = sourceServices(doc)

		if svc.ARM != nil {
			arm, loadErr := svc.ARM.Load(ctx, src)
			switch {
			case loadErr == nil:
				if pm, ok := arm.(interface{ Placements() []armmodel.Placement }); ok && arm != nil {
					// The flattened model has the module parameters
					// substituted and symbols qualified per deployment.
					coreResources = append(coreResources, graphview.ModelResources(arm.Resources(), pm.Placements())...)
				} else if raw, ok := arm.(interface{ RawTemplate() []byte }); ok && len(raw.RawTemplate()) > 0 {
					if p, err := bicep.ParseARM(raw.RawTemplate()); err == nil {
						coreResources = append(coreResources, templateResources(p)...)
					}
				}
				if len(coreResources) == 0 {
					coreResources = append(coreResources, armResources(arm)...)
				}
			case errors.Is(loadErr, ErrARMUnavailable):
				if svc.Stderr != nil {
					_, _ = fmt.Fprintf(svc.Stderr, "warning: %s; source graph omits template-only nodes\n", loadErr)
				}
			default:
				return ExitCodeForError(loadErr), fmt.Errorf("load bicep/arm model: %w", loadErr)
			}
		}
	}

	if req.Mode == graphview.ModeDeployed || req.Mode == graphview.ModeCombined {
		client, err := graphClient()
		if err != nil {
			return ExitCodeForError(ErrUnavailable), fmt.Errorf("%w: azure adapter: %w", ErrUnavailable, err)
		}
		scope = "/subscriptions/" + req.Target.SubscriptionID
		if req.Target.ResourceGroup != "" {
			scope += "/resourceGroups/" + req.Target.ResourceGroup
		}
		list, err := client.Inventory.ListResources(ctx, azure.InventoryQuery{
			Scope:         scope,
			PropertyPaths: []string{"properties.provisioningState"},
			MaxResults:    max(2000, req.MaxNodes*4),
		})
		if err != nil {
			return ExitCodeForError(err), fmt.Errorf("list Azure inventory: %w", err)
		}
		if list.Truncated {
			return ExitCodeForError(ErrUnavailable), fmt.Errorf("%w: Azure inventory scope exceeded graph collection limit", ErrUnavailable)
		}
		inventory = list.Resources
		if req.Target.Account != "" {
			if acct := findAccount(inventory, req.Target.Account); acct != nil {
				runtimeIn.Account = acct
			} else if svc.Stderr != nil {
				_, _ = fmt.Fprintf(svc.Stderr, "warning: Foundry account %q is not in the Azure inventory for %s; runtime overlay nodes are drawn without an account node\n", req.Target.Account, scope)
			}
			sub, rg, acct, proj := req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account, req.Target.Project
			// overlay runs one read-only overlay call. A permission or
			// availability failure skips that overlay with a note instead of
			// failing the graph; any other failure stops the command.
			overlay := func(op, typ string, call func() error) error {
				err := call()
				if err == nil {
					runtimeIn.ObservedTypes = append(runtimeIn.ObservedTypes, typ)
					return nil
				}
				if errors.Is(err, azure.ErrUnavailable) {
					notes = append(notes, op+" overlay skipped: not readable with the current permissions or unavailable")
					if svc.Stderr != nil {
						_, _ = fmt.Fprintf(svc.Stderr, "warning: %s overlay skipped: %v\n", op, err)
					}
					return nil
				}
				return err
			}
			type overlayStep struct {
				op, typ string
				call    func() error
			}
			steps := []overlayStep{{"account deployments", "Microsoft.CognitiveServices/accounts/deployments", func() error {
				v, err := client.Foundry.ListAccountDeployments(ctx, sub, rg, acct)
				if err == nil {
					runtimeIn.Deployments = v
				}
				return err
			}}}
			var projectConns, accountConns []azure.FoundryConnection
			listAccountConns := overlayStep{"account connections", "Microsoft.CognitiveServices/accounts/connections", func() error {
				v, err := client.Foundry.ListAccountConnections(ctx, sub, rg, acct)
				if err == nil {
					accountConns = v
				}
				return err
			}}
			if proj != "" {
				steps = append(steps,
					overlayStep{"project", "Microsoft.CognitiveServices/accounts/projects", func() error {
						p, err := client.Foundry.GetProject(ctx, sub, rg, acct, proj)
						if err == nil {
							runtimeIn.Project = &p
						}
						return err
					}},
					overlayStep{"project connections", "Microsoft.CognitiveServices/accounts/projects/connections", func() error {
						v, err := client.Foundry.ListProjectConnections(ctx, sub, rg, acct, proj)
						if err == nil {
							projectConns = v
						}
						return err
					}},
					listAccountConns,
					overlayStep{"project capability hosts", "Microsoft.CognitiveServices/accounts/projects/capabilityHosts", func() error {
						v, err := client.Foundry.ListProjectCapabilityHosts(ctx, sub, rg, acct, proj)
						if err == nil {
							runtimeIn.Hosts = v
						}
						return err
					}},
				)
			} else {
				steps = append(steps,
					listAccountConns,
					overlayStep{"account capability hosts", "Microsoft.CognitiveServices/accounts/capabilityHosts", func() error {
						v, err := client.Foundry.ListAccountCapabilityHosts(ctx, sub, rg, acct)
						if err == nil {
							runtimeIn.Hosts = v
						}
						return err
					}},
				)
			}
			for _, st := range steps {
				if err := overlay(st.op, st.typ, st.call); err != nil {
					return graphOverlayError("read "+st.op, err)
				}
			}
			runtimeIn.Connections = mergeFoundryConnections(projectConns, accountConns)
		}
	}

	finds, err := loadGraphFindings(req.FindingsReport)
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		return ExitCodeForError(err), err
	}
	g, err := graphview.Build(graphview.Input{
		Source: graphview.SourceInput{
			Document:  doc,
			Services:  coreServices,
			Resources: coreResources,
		},
		Inventory: inventory,
		Runtime:   runtimeIn,
		Findings:  finds,
	}, graphview.Options{
		Mode:          req.Mode,
		RedactIDs:     req.RedactIDs,
		MaxNodes:      req.MaxNodes,
		MaxEdges:      req.MaxEdges,
		CollapseAfter: req.CollapseAfter,
		Scope:         scope,
	})
	if err != nil {
		return ExitInternal, fmt.Errorf("build graph: %w", err)
	}
	g.Summary.Notes = notes
	return emitGraph(ctx, svc, req, g, stdout)
}

func emitGraph(ctx context.Context, svc Services, req GraphRequest, g graphview.Graph, stdout io.Writer) (int, error) {
	data, err := renderGraph(ctx, g, req.Format)
	if err != nil {
		return ExitCodeForError(err), err
	}
	if err := svc.emit(req.Out, data, stdout); err != nil {
		return ExitInternal, err
	}
	return ExitOK, nil
}

// graphDiff renders the environment-difference graph of two graph JSON files.
func graphDiff(ctx context.Context, svc Services, req GraphRequest, stdout io.Writer) (int, error) {
	base, err := loadGraphJSON(req.DiffBase)
	if err != nil {
		return ExitCodeForError(err), err
	}
	target, err := loadGraphJSON(req.DiffTarget)
	if err != nil {
		return ExitCodeForError(err), err
	}
	g, err := graphview.Diff(base, target, graphview.Options{Mode: graphview.ModeDiff, RedactIDs: req.RedactIDs})
	if err != nil {
		err = Usagef("graph diff: %v", err)
		return ExitCodeForError(err), err
	}
	return emitGraph(ctx, svc, req, g, stdout)
}

func loadGraphJSON(path string) (graphview.Graph, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return graphview.Graph{}, fmt.Errorf("%w: read graph %q: %w", ErrUnavailable, path, err)
	}
	var g graphview.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return graphview.Graph{}, Usagef("graph %q is not graph JSON (render it with --format json): %v", path, err)
	}
	if g.SchemaVersion == "" {
		return graphview.Graph{}, Usagef("graph %q has no schemaVersion; render it with `graph --format json`", path)
	}
	return g, nil
}

func sourceServices(doc *azureyaml.Document) []coregraph.Service {
	if doc == nil {
		return nil
	}
	out := make([]coregraph.Service, 0, len(doc.ServiceNames()))
	for _, name := range doc.ServiceNames() {
		host, _, _ := doc.Lookup("services", name, "host")
		var uses []string
		for _, ref := range doc.Uses(name) {
			uses = append(uses, ref.Name)
		}
		out = append(out, coregraph.Service{Name: name, Host: host, Uses: uses})
	}
	return out
}

func templateResources(tpl *bicep.Template) []coregraph.Resource {
	if tpl == nil {
		return nil
	}
	var out []coregraph.Resource
	tpl.Walk(func(r bicep.Resource, _ int) {
		if r.IsModule() {
			return
		}
		out = append(out, coregraph.Resource{
			ID: r.Pointer, Type: r.Type, Name: r.Name, SymbolicName: r.SymbolicName, DependsOn: slices.Clone(r.DependsOn),
			Conditional: r.Conditional, Condition: r.Condition, Looped: r.Copy != nil,
		})
	})
	return out
}

func armResources(arm sdk.ARMModel) []coregraph.Resource {
	if arm == nil {
		return nil
	}
	res := arm.Resources()
	out := make([]coregraph.Resource, 0, len(res))
	for i, r := range res {
		out = append(out, coregraph.Resource{
			ID: fmt.Sprintf("/resources/%d", i), Type: r.Type, Name: r.Name,
			Conditional: r.Conditional, Condition: r.Condition, Looped: r.Looped,
		})
	}
	return out
}

func loadGraphFindings(paths []string) ([]sdk.Finding, error) {
	var out []sdk.Finding
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return nil, fmt.Errorf("read findings report %q: %w", p, err)
		}
		var rep struct {
			Tool     string        `json:"tool"`
			Findings []sdk.Finding `json:"findings"`
		}
		if err := json.Unmarshal(data, &rep); err != nil {
			return nil, fmt.Errorf("parse findings report %q: %w", p, err)
		}
		if rep.Tool != "" && rep.Tool != report.ToolName {
			return nil, Usagef("report %q is from %q, want %q JSON output", p, rep.Tool, report.ToolName)
		}
		out = append(out, rep.Findings...)
	}
	return out, nil
}

func findAccount(resources []azure.Resource, name string) *azure.Resource {
	for i := range resources {
		if strings.EqualFold(resources[i].Name, name) && strings.EqualFold(resources[i].Type, "Microsoft.CognitiveServices/accounts") {
			return &resources[i]
		}
	}
	return nil
}

func graphOverlayError(op string, err error) (int, error) {
	if errors.Is(err, azure.ErrUnavailable) || errors.Is(err, azure.ErrInvalidInput) {
		wrapped := fmt.Errorf("%w: %s: %w", ErrUnavailable, op, err)
		return ExitCodeForError(wrapped), wrapped
	}
	wrapped := fmt.Errorf("%s: %w", op, err)
	return ExitCodeForError(wrapped), wrapped
}

func renderGraph(ctx context.Context, g graphview.Graph, format string) ([]byte, error) {
	switch format {
	case "mermaid":
		return mermaid.Render(g), nil
	case "json":
		b, err := graphjson.Render(g)
		if err != nil {
			return nil, fmt.Errorf("render graph json: %w", err)
		}
		return append(b, '\n'), nil
	case "markdown":
		return graphreport.Markdown(g), nil
	case "dot":
		return graphviz.RenderDOT(g), nil
	case "html":
		return graphreport.HTML(g), nil
	case "svg", "png":
		out, err := graphvizRenderer.Render(ctx, graphviz.RenderDOT(g), format)
		if errors.Is(err, graphviz.ErrNotFound) {
			return nil, fmt.Errorf("%w: optional Graphviz renderer not installed (`dot` not on PATH); use --format mermaid, markdown or dot", ErrUnavailable)
		}
		if err != nil {
			return nil, fmt.Errorf("render graph %s: %w", format, err)
		}
		return out, nil
	default:
		return nil, Usagef("invalid graph format %q", format)
	}
}

func mergeFoundryConnections(groups ...[]azure.FoundryConnection) []azure.FoundryConnection {
	seen := map[string]bool{}
	var out []azure.FoundryConnection
	for _, group := range groups {
		for _, conn := range group {
			key := strings.ToLower(strings.TrimSpace(conn.ID))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(conn.Name))
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, conn)
		}
	}
	return out
}

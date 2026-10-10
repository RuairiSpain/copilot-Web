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
}

func (r *GraphRequest) Validate() error {
	if r.Mode == "" {
		r.Mode = graphview.ModeSource
	}
	switch r.Mode {
	case graphview.ModeSource, graphview.ModeDeployed, graphview.ModeCombined:
	default:
		return Usagef("invalid graph mode %q", r.Mode)
	}
	if r.Format == "" {
		r.Format = "mermaid"
	}
	switch r.Format {
	case "mermaid", "json", "markdown", "dot", "html":
	default:
		return Usagef("invalid --format %q for graph (want mermaid, json, markdown, dot or html)", r.Format)
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
	var (
		doc           *azureyaml.Document
		coreServices  []coregraph.Service
		coreResources []coregraph.Resource
		inventory     []azure.Resource
		runtimeIn     graphview.RuntimeInput
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
				if raw, ok := arm.(interface{ RawTemplate() []byte }); ok && len(raw.RawTemplate()) > 0 {
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
		scope := "/subscriptions/" + req.Target.SubscriptionID
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
			}
			runtimeIn.Deployments, err = client.Foundry.ListAccountDeployments(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account)
			if err != nil {
				return graphOverlayError("list account deployments", err)
			}
			if req.Target.Project != "" {
				proj, err := client.Foundry.GetProject(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account, req.Target.Project)
				if err != nil {
					return graphOverlayError("get project runtime", err)
				}
				runtimeIn.Project = &proj
				projectConns, err := client.Foundry.ListProjectConnections(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account, req.Target.Project)
				if err != nil {
					return graphOverlayError("list project connections", err)
				}
				accountConns, err := client.Foundry.ListAccountConnections(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account)
				if err != nil {
					return graphOverlayError("list account connections", err)
				}
				runtimeIn.Connections = mergeFoundryConnections(projectConns, accountConns)
				runtimeIn.Hosts, err = client.Foundry.ListProjectCapabilityHosts(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account, req.Target.Project)
				if err != nil {
					return graphOverlayError("list project capability hosts", err)
				}
			} else {
				runtimeIn.Connections, err = client.Foundry.ListAccountConnections(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account)
				if err != nil {
					return graphOverlayError("list account connections", err)
				}
				runtimeIn.Hosts, err = client.Foundry.ListAccountCapabilityHosts(ctx, req.Target.SubscriptionID, req.Target.ResourceGroup, req.Target.Account)
				if err != nil {
					return graphOverlayError("list account capability hosts", err)
				}
			}
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
	})
	if err != nil {
		return ExitInternal, fmt.Errorf("build graph: %w", err)
	}
	data, err := renderGraph(g, req.Format)
	if err != nil {
		return ExitInternal, err
	}
	if err := svc.emit(req.Out, data, stdout); err != nil {
		return ExitInternal, err
	}
	return ExitOK, nil
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
		out = append(out, coregraph.Resource{ID: fmt.Sprintf("/resources/%d", i), Type: r.Type, Name: r.Name})
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

func renderGraph(g graphview.Graph, format string) ([]byte, error) {
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

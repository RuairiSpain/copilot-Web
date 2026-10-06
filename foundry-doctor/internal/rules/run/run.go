// Package run implements the Phase 3 runtime diagnosis rules.
package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	foundrydp "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/foundry"
	monprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/monitor"
	netprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/network"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/rbac"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Target identifies the deployed Foundry project to inspect.
type Target struct {
	SubscriptionID string
	ResourceGroup  string
	Account        string
	Project        string
}

// Deps are the probe dependencies used by the runtime rules.
type Deps struct {
	FoundryMgmt azure.RuntimeFoundry
	RBAC        azure.RuntimeRBAC
	DNS         azure.RuntimeDNS
	Monitor     azure.RuntimeMonitor
	ProjectData foundrydp.ProjectReader
	SearchData  searchprobe.Client
	Resolver    netprobe.Resolver

	Target  Target
	Options runtime.Options
	Now     func() time.Time
}

// RuleIDs returns the runtime rule ids in order.
func RuleIDs() []string {
	ids := make([]string, 0, 7)
	for i := 1; i <= 7; i++ {
		ids = append(ids, fmt.Sprintf("FND-RUN-%03d", i))
	}
	return ids
}

// Register returns the runtime rules in ID order.
func Register(d Deps) []sdk.Rule {
	if d.Now == nil {
		d.Now = time.Now
	}
	e := &env{d: d}
	fns := []func(context.Context, *sdk.Input) (sdk.Result, error){
		e.run001, e.run002, e.run003, e.run004, e.run005, e.run006, e.run007,
	}
	out := make([]sdk.Rule, len(fns))
	ids := RuleIDs()
	for i, fn := range fns {
		out[i] = rule{id: ids[i], fn: fn}
	}
	return out
}

type rule struct {
	id string
	fn func(context.Context, *sdk.Input) (sdk.Result, error)
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if in == nil {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: sdk.SkipInputUnavailable + ": no input"}}, nil
	}
	res, err := r.fn(ctx, in)
	if err != nil {
		if ctx.Err() != nil {
			return sdk.Result{}, ctx.Err()
		}
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: azureReason(err)}}, nil
	}
	if res.Skipped != nil {
		res.Skipped.RuleID = r.id
		res.Findings = nil
	}
	return res, nil
}

type env struct {
	d          Deps
	mu         sync.Mutex
	project    *azure.FoundryProject
	projectErr error
	hosts      []azure.CapabilityHost
	hostsErr   error
	pconns     []azure.FoundryConnection
	pconnsErr  error
	aconns     []azure.FoundryConnection
	aconnsErr  error
	deploys    []azure.AccountDeployment
	deployErr  error
}

type acc struct {
	findings []sdk.Finding
	skips    []string
	notes    []string
}

func (a *acc) add(c runtime.Correlation, evidence string) {
	a.findings = append(a.findings, sdk.Finding{Resource: c.Resource, Location: c.Source, Evidence: evidence})
}
func (a *acc) skip(reason string) { a.skips = append(a.skips, reason) }
func (a *acc) note(reason string) { a.notes = append(a.notes, reason) }
func (a *acc) result() sdk.Result {
	if len(a.findings) > 0 {
		findingsSort(a.findings)
		return sdk.Result{Findings: a.findings}
	}
	if len(a.skips) > 0 {
		sort.Strings(a.skips)
		return sdk.Result{Skipped: &sdk.Skip{Reason: strings.Join(unique(a.skips), "; ")}}
	}
	if len(a.notes) > 0 {
		sort.Strings(a.notes)
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + strings.Join(unique(a.notes), "; ")}}
	}
	return sdk.Result{}
}

func findingsSort(fs []sdk.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Resource.ID != fs[j].Resource.ID {
			return fs[i].Resource.ID < fs[j].Resource.ID
		}
		if fs[i].Resource.Name != fs[j].Resource.Name {
			return fs[i].Resource.Name < fs[j].Resource.Name
		}
		return fs[i].Evidence < fs[j].Evidence
	})
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func needTarget(t Target, needProject bool) string {
	switch {
	case t.SubscriptionID == "":
		return sdk.SkipInputUnavailable + ": target subscription is not set (use --subscription)"
	case t.ResourceGroup == "":
		return sdk.SkipInputUnavailable + ": target resource group is not set (use --resource-group)"
	case t.Account == "":
		return sdk.SkipInputUnavailable + ": target account is not set (use --account)"
	case needProject && t.Project == "":
		return sdk.SkipInputUnavailable + ": target project is not set (use --project)"
	default:
		return ""
	}
}

func azureReason(err error) string {
	if u, ok := azure.AsUnavailable(err); ok {
		msg := sdk.SkipInputUnavailable + ": capability " + strings.TrimSpace(u.Capability)
		if u.Permission != "" {
			msg += "; missing permission " + strings.TrimSpace(u.Permission)
		}
		if u.Reason != "" {
			msg += "; " + strings.TrimSpace(u.Reason)
		}
		return msg
	}
	return sdk.SkipInputUnavailable + ": " + strings.TrimSpace(err.Error())
}

func (e *env) projectInfo(ctx context.Context) (azure.FoundryProject, error) {
	e.mu.Lock()
	if e.project != nil || e.projectErr != nil {
		defer e.mu.Unlock()
		if e.project == nil {
			return azure.FoundryProject{}, e.projectErr
		}
		return *e.project, nil
	}
	e.mu.Unlock()
	p, err := e.d.FoundryMgmt.GetProject(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account, e.d.Target.Project)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.projectErr = err
		return azure.FoundryProject{}, err
	}
	e.project = &p
	return p, nil
}

func (e *env) capabilityHosts(ctx context.Context) ([]azure.CapabilityHost, error) {
	e.mu.Lock()
	if e.hosts != nil || e.hostsErr != nil {
		defer e.mu.Unlock()
		return append([]azure.CapabilityHost(nil), e.hosts...), e.hostsErr
	}
	e.mu.Unlock()
	hosts, err := e.d.FoundryMgmt.ListProjectCapabilityHosts(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account, e.d.Target.Project)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hosts, e.hostsErr = append([]azure.CapabilityHost(nil), hosts...), err
	return append([]azure.CapabilityHost(nil), hosts...), err
}

func (e *env) projectConnections(ctx context.Context) ([]azure.FoundryConnection, error) {
	e.mu.Lock()
	if e.pconns != nil || e.pconnsErr != nil {
		defer e.mu.Unlock()
		return append([]azure.FoundryConnection(nil), e.pconns...), e.pconnsErr
	}
	e.mu.Unlock()
	conns, err := e.d.FoundryMgmt.ListProjectConnections(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account, e.d.Target.Project)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pconns, e.pconnsErr = append([]azure.FoundryConnection(nil), conns...), err
	return append([]azure.FoundryConnection(nil), conns...), err
}

func (e *env) accountConnections(ctx context.Context) ([]azure.FoundryConnection, error) {
	e.mu.Lock()
	if e.aconns != nil || e.aconnsErr != nil {
		defer e.mu.Unlock()
		return append([]azure.FoundryConnection(nil), e.aconns...), e.aconnsErr
	}
	e.mu.Unlock()
	conns, err := e.d.FoundryMgmt.ListAccountConnections(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.aconns, e.aconnsErr = append([]azure.FoundryConnection(nil), conns...), err
	return append([]azure.FoundryConnection(nil), conns...), err
}

func (e *env) deployments(ctx context.Context) ([]azure.AccountDeployment, error) {
	e.mu.Lock()
	if e.deploys != nil || e.deployErr != nil {
		defer e.mu.Unlock()
		return append([]azure.AccountDeployment(nil), e.deploys...), e.deployErr
	}
	e.mu.Unlock()
	ds, err := e.d.FoundryMgmt.ListAccountDeployments(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deploys, e.deployErr = append([]azure.AccountDeployment(nil), ds...), err
	return append([]azure.AccountDeployment(nil), ds...), err
}

func (e *env) correlator(in *sdk.Input) runtime.Correlator { return runtime.NewCorrelator(in) }

func (e *env) projectEndpoint() (string, error) {
	return foundrydp.Endpoint(e.d.Target.Account, e.d.Target.Project)
}

func (e *env) boundConnections(ctx context.Context) ([]azure.FoundryConnection, []string, error) {
	hosts, err := e.capabilityHosts(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(hosts) == 0 {
		return nil, nil, nil
	}
	projectConns, err := e.projectConnections(ctx)
	if err != nil {
		return nil, nil, err
	}
	accountConns, err := e.accountConnections(ctx)
	if err != nil {
		return nil, nil, err
	}
	index := map[string]azure.FoundryConnection{}
	for _, c := range append(projectConns, accountConns...) {
		index[strings.ToLower(c.Name)] = c
	}
	names := []string{}
	out := []azure.FoundryConnection{}
	for _, h := range hosts {
		for _, n := range append(append(append([]string{}, h.AIServiceConnections...), h.StorageConnections...), append(h.ThreadStorageConnections, h.VectorStoreConnections...)...) {
			names = append(names, n)
			if c, ok := index[strings.ToLower(n)]; ok {
				out = append(out, c)
			}
		}
	}
	return out, unique(names), nil
}

func (e *env) agentNames(in *sdk.Input) []string {
	if in == nil || in.AzureYAML == nil {
		return nil
	}
	names := []string{}
	for _, svc := range in.AzureYAML.ServiceNames() {
		host, _, ok := in.AzureYAML.Lookup("services", svc, "host")
		if !ok || !strings.EqualFold(host, "azure.ai.agent") {
			continue
		}
		agentName, _, ok := in.AzureYAML.Lookup("services", svc, "name")
		if !ok || strings.TrimSpace(agentName) == "" {
			agentName = svc
		}
		names = append(names, agentName)
	}
	sort.Strings(names)
	return names
}

func isSkipAuth(auth string) bool {
	auth = strings.ToLower(auth)
	return strings.Contains(auth, "key") || strings.Contains(auth, "sas") || strings.Contains(auth, "serviceprincipal")
}

func (e *env) run001(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.FoundryMgmt == nil || e.d.RBAC == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": runtime RBAC metadata is not available in this build"}}, nil
	}
	project, err := e.projectInfo(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	conns, _, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	if len(conns) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": project has no capability host or no bound resources"}}, nil
	}
	corr := e.correlator(in)
	a := &acc{}
	reader := rbac.ReaderAdapter{Roles: e.d.RBAC}
	for _, c := range conns {
		if isSkipAuth(c.AuthType) {
			a.skip(sdk.SkipInputUnavailable + ": connection " + c.Name + " uses " + c.AuthType + "; effective access cannot be proven without reading credentials")
			continue
		}
		target := strings.ToLower(c.Target.ResourceID)
		switch {
		case strings.Contains(target, "/providers/microsoft.search/searchservices/"):
			evs, err := rbac.Check(ctx, reader, project.PrincipalID, c.Target.ResourceID, rbac.SearchServiceContributor, rbac.SearchIndexDataContributor)
			if err != nil {
				return sdk.Result{}, err
			}
			for _, ev := range evs {
				if ev.Allowed {
					continue
				}
				if ev.Uncertain {
					a.note(ev.Reason)
					continue
				}
				a.add(corr.Resource("Microsoft.Search/searchServices", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID), rbac.MissingMessage(ev))
			}
		case strings.Contains(target, "/providers/microsoft.storage/storageaccounts/"):
			evs, err := rbac.Check(ctx, reader, project.PrincipalID, c.Target.ResourceID, rbac.StorageBlobDataContributor, rbac.StorageBlobDataOwner, rbac.StorageAccountContributor)
			if err != nil {
				return sdk.Result{}, err
			}
			for _, ev := range evs {
				if ev.Allowed {
					continue
				}
				if ev.Uncertain {
					a.note(ev.Reason)
					continue
				}
				a.add(corr.Resource("Microsoft.Storage/storageAccounts", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID), rbac.MissingMessage(ev))
			}
		case strings.Contains(target, "/providers/microsoft.documentdb/databaseaccounts/"):
			evs, err := rbac.CheckCosmos(ctx, reader, project.PrincipalID, c.Target.ResourceID, strings.Contains(strings.ToLower(project.IdentityType), "systemassigned"))
			if err != nil {
				return sdk.Result{}, err
			}
			for _, ev := range evs {
				if ev.Allowed {
					continue
				}
				if ev.Uncertain {
					a.note(ev.Reason)
					continue
				}
				a.add(corr.Resource("Microsoft.DocumentDB/databaseAccounts", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID), rbac.MissingMessage(ev))
			}
		}
	}
	return a.result(), nil
}

func (e *env) run002(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.Resolver == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": DNS resolver is not available"}}, nil
	}
	conns, _, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	hosts := []struct {
		typ  string
		name string
		id   string
		fqdn string
	}{
		{"Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountID(e.d.Target), e.d.Target.Account + ".cognitiveservices.azure.com"},
		{"Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountID(e.d.Target), e.d.Target.Account + ".openai.azure.com"},
		{"Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountID(e.d.Target), e.d.Target.Account + ".services.ai.azure.com"},
	}
	for _, c := range conns {
		switch {
		case strings.Contains(strings.ToLower(c.Target.ResourceID), "/providers/microsoft.search/searchservices/"):
			hosts = append(hosts, struct {
				typ  string
				name string
				id   string
				fqdn string
			}{"Microsoft.Search/searchServices", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID, lastIDSegment(c.Target.ResourceID) + ".search.windows.net"})
		case strings.Contains(strings.ToLower(c.Target.ResourceID), "/providers/microsoft.documentdb/databaseaccounts/"):
			hosts = append(hosts, struct {
				typ  string
				name string
				id   string
				fqdn string
			}{"Microsoft.DocumentDB/databaseAccounts", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID, lastIDSegment(c.Target.ResourceID) + ".documents.azure.com"})
		case strings.Contains(strings.ToLower(c.Target.ResourceID), "/providers/microsoft.storage/storageaccounts/"):
			hosts = append(hosts, struct {
				typ  string
				name string
				id   string
				fqdn string
			}{"Microsoft.Storage/storageAccounts", lastIDSegment(c.Target.ResourceID), c.Target.ResourceID, lastIDSegment(c.Target.ResourceID) + ".blob.core.windows.net"})
		}
	}
	if len(hosts) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no private-endpoint-backed dependencies are in scope"}}, nil
	}
	corr := e.correlator(in)
	a := &acc{}
	resolver := netprobe.TimeoutResolver{Inner: e.d.Resolver, Timeout: e.d.Options.Timeout}
	for _, h := range hosts {
		r := netprobe.ResolvePrivate(ctx, resolver, e.d.Options.Vantage, h.fqdn)
		switch r.State {
		case runtime.StatePass:
		case runtime.StateFail:
			a.add(corr.Resource(h.typ, h.name, h.id), h.fqdn+": "+r.Reason)
		case runtime.StateSkipped:
			a.skip(r.Reason)
		default:
			a.note(r.Reason)
		}
	}
	return a.result(), nil
}

func (e *env) run003(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.FoundryMgmt == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": Foundry management metadata is not available"}}, nil
	}
	project, err := e.projectInfo(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	hosts, err := e.capabilityHosts(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	if len(hosts) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": project has no capability host"}}, nil
	}
	corr := e.correlator(in)
	a := &acc{}
	projCorr := corr.Resource("Microsoft.CognitiveServices/accounts/projects", e.d.Target.Project, project.ID)
	switch strings.ToLower(project.ProvisioningState) {
	case "succeeded":
	case "failed", "canceled":
		a.add(projCorr, "project provisioningState is "+project.ProvisioningState)
	default:
		a.note("project provisioningState is " + project.ProvisioningState)
	}
	conns, names, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	index := map[string]azure.FoundryConnection{}
	for _, c := range conns {
		index[strings.ToLower(c.Name)] = c
	}
	for _, h := range hosts {
		hostCorr := corr.Resource("Microsoft.CognitiveServices/accounts/projects/capabilityHosts", h.Name, h.ID)
		switch strings.ToLower(h.ProvisioningState) {
		case "succeeded":
		case "failed", "canceled":
			a.add(hostCorr, "capability host provisioningState is "+h.ProvisioningState)
		default:
			a.note("capability host provisioningState is " + h.ProvisioningState)
		}
	}
	for _, name := range names {
		c, ok := index[strings.ToLower(name)]
		if !ok {
			a.add(projCorr, "bound connection "+name+" does not exist on the project or account")
			continue
		}
		if strings.TrimSpace(c.Error) != "" {
			a.add(corr.Resource("", c.Name, c.ID), "connection "+c.Name+" reports error "+findings.Redact(c.Error))
		}
	}
	return a.result(), nil
}

func (e *env) run004(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, false); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.FoundryMgmt == nil || e.d.Monitor == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": deployment or metric metadata is not available"}}, nil
	}
	deploys, err := e.deployments(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	if len(deploys) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": account has no model deployments"}}, nil
	}
	corr := e.correlator(in)
	a := &acc{}
	for _, d := range deploys {
		rc := corr.Resource("Microsoft.CognitiveServices/accounts/deployments", d.Name, accountID(e.d.Target)+"/deployments/"+d.Name)
		switch strings.ToLower(d.ProvisioningState) {
		case "succeeded":
		case "failed", "canceled", "disabled":
			a.add(rc, "deployment provisioningState is "+d.ProvisioningState)
		default:
			a.note("deployment " + d.Name + " provisioningState is " + d.ProvisioningState)
		}
	}
	mc := monprobe.FixedClient{Inner: e.d.Monitor}
	points, err := mc.Query429(ctx, accountID(e.d.Target))
	if err != nil {
		return sdk.Result{}, err
	}
	for _, p := range points {
		if p.Total > 0 {
			a.add(corr.Resource("Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountID(e.d.Target)), fmt.Sprintf("deployment %s returned %.0f HTTP 429 responses in the metric window", p.Series, p.Total))
		}
	}
	util, err := mc.QueryProvisionedUtilization(ctx, accountID(e.d.Target))
	if err != nil {
		return sdk.Result{}, err
	}
	for _, p := range util {
		if p.Total >= 100 {
			a.add(corr.Resource("Microsoft.CognitiveServices/accounts/deployments", p.Series, accountID(e.d.Target)+"/deployments/"+p.Series), fmt.Sprintf("deployment %s reached %.0f%% provisioned utilization", p.Series, p.Total))
		}
	}
	if len(points) == 0 && len(util) == 0 {
		a.note("monitor metrics returned no data points")
	}
	return a.result(), nil
}

func (e *env) run005(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.ProjectData == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": Foundry project data plane is not available"}}, nil
	}
	endpoint, err := e.projectEndpoint()
	if err != nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: azureReason(err)}}, nil
	}
	if err := e.d.ProjectData.Ping(ctx, endpoint); err != nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: azureReason(err)}}, nil
	}
	a := &acc{}
	corr := e.correlator(in)
	agents, err := e.d.ProjectData.ListAgents(ctx, endpoint)
	if err != nil {
		return sdk.Result{}, err
	}
	index := map[string]string{}
	for _, ag := range agents {
		index[strings.ToLower(ag.Name)] = ag.LatestVersion
	}
	for _, agent := range e.agentNames(in) {
		ver, ok := index[strings.ToLower(agent)]
		if !ok {
			a.add(corr.Resource("azure.ai.agent", agent, ""), "agent "+agent+" is not listed on the project endpoint")
			continue
		}
		v, err := e.d.ProjectData.GetAgentVersion(ctx, endpoint, agent, ver)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				a.note("agent " + agent + " status request timed out")
				continue
			}
			if strings.Contains(err.Error(), "404") {
				a.add(corr.Resource("azure.ai.agent", agent, ""), "agent "+agent+" version "+ver+" was not found")
				continue
			}
			return sdk.Result{}, err
		}
		switch strings.ToLower(v.Status) {
		case "active":
		case "creating":
			a.note("agent " + agent + " version " + v.Version + " is still creating")
		default:
			a.add(corr.Resource("azure.ai.agent", agent, ""), "agent "+agent+" version "+v.Version+" status is "+v.Status)
		}
	}
	dpConns, err := e.d.ProjectData.ListConnections(ctx, endpoint)
	if err != nil {
		return sdk.Result{}, err
	}
	_, names, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	have := map[string]bool{}
	for _, c := range dpConns {
		have[strings.ToLower(c.Name)] = true
	}
	for _, name := range names {
		if !have[strings.ToLower(name)] {
			a.add(corr.Resource("", name, ""), "project endpoint does not list bound connection "+name)
		}
	}
	return a.result(), nil
}

func (e *env) run006(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.SearchData == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": Search data plane is not available"}}, nil
	}
	conns, _, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	corr := e.correlator(in)
	a := &acc{}
	seen := false
	for _, c := range conns {
		if !strings.Contains(strings.ToLower(c.Target.ResourceID), "/providers/microsoft.search/searchservices/") || len(c.Target.IndexNames) == 0 {
			continue
		}
		seen = true
		service := lastIDSegment(c.Target.ResourceID)
		for _, indexName := range c.Target.IndexNames {
			rc := corr.Resource("Microsoft.Search/searchServices", service, c.Target.ResourceID)
			idx, err := e.d.SearchData.GetIndex(ctx, service, indexName)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					a.add(rc, "search index "+indexName+" does not exist")
					continue
				}
				return sdk.Result{}, err
			}
			if !searchprobe.HasUsableVectorField(idx) {
				a.add(rc, searchprobe.MissingSchema(idx))
			}
			stats, err := e.d.SearchData.GetIndexStatistics(ctx, service, indexName)
			if err != nil {
				return sdk.Result{}, err
			}
			zeroDocs := stats.DocumentCount == 0
			zeroDocsUncertain := false
			for _, indexer := range c.Target.IndexerNames {
				st, err := e.d.SearchData.GetIndexerStatus(ctx, service, indexer)
				if err != nil {
					return sdk.Result{}, err
				}
				lastResult := strings.ToLower(st.LastResultStatus)
				switch lastResult {
				case "failed", "error":
					a.add(rc, formatIndexerFailure(indexer, st))
				case "transientfailure":
					a.note("search indexer " + indexer + " reports transient failure")
				case "inprogress":
					a.note("search indexer " + indexer + " is still in progress")
					zeroDocsUncertain = true
				}
				switch strings.ToLower(st.Status) {
				case "error":
					if lastResult != "failed" && lastResult != "error" {
						a.add(rc, "search indexer "+indexer+" is in error state")
					}
				}
				if zeroDocs && st.LastSuccessAgo > 0 && st.LastSuccessAgo < 2 {
					zeroDocsUncertain = true
				}
			}
			if zeroDocs {
				if zeroDocsUncertain {
					a.note("search index " + indexName + " has documentCount 0 but the indexer changed recently")
				} else {
					a.add(rc, "search index "+indexName+" has documentCount 0")
				}
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": project has no Search connection with index metadata"}}, nil
	}
	return a.result(), nil
}

func formatIndexerFailure(indexer string, st searchprobe.IndexerStatus) string {
	parts := []string{"search indexer " + indexer + " last result failed"}
	if st.SafeErrorCode != "" {
		parts = append(parts, "errorCode="+st.SafeErrorCode)
	}
	if st.ItemsFailed > 0 {
		parts = append(parts, fmt.Sprintf("itemsFailed=%d", st.ItemsFailed))
	}
	return strings.Join(parts, " ")
}

func (e *env) run007(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, false); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.Monitor == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": Azure Monitor metadata is not available"}}, nil
	}
	accountRID := accountID(e.d.Target)
	corr := e.correlator(in)
	rc := corr.Resource("Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountRID)
	client := monprobe.FixedClient{Inner: e.d.Monitor}
	settings, err := client.ListDiagnosticSettings(ctx, accountRID)
	if err != nil {
		return sdk.Result{}, err
	}
	var setting monprobe.DiagnosticSetting
	found := false
	for _, s := range settings {
		if s.WorkspaceID != "" && (slices.Contains(s.Categories, "RequestResponse") || slices.Contains(s.Categories, "Audit")) {
			setting, found = s, true
			break
		}
	}
	if !found {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no diagnostic setting sends Audit or RequestResponse logs to a workspace"}}, nil
	}
	traffic, err := client.QueryTraffic(ctx, accountRID)
	if err != nil {
		return sdk.Result{}, err
	}
	var trafficTotal float64
	for _, p := range traffic {
		trafficTotal += p.Total
	}
	if trafficTotal == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "no traffic in the metric window"}}, nil
	}
	logs, err := client.QueryDiagnosticCounts(ctx, setting.WorkspaceID, e.d.Target.Account, 1)
	if err != nil {
		return sdk.Result{}, err
	}
	if !strings.EqualFold(logs.Table, "AzureDiagnostics") && logs.Table != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "workspace uses resource-specific tables"}}, nil
	}
	var count int64
	for _, v := range logs.Counts {
		count += v
	}
	if count == 0 {
		if setting.AgeHours > 0 && setting.AgeHours < 2 {
			return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "diagnostic setting was created recently"}}, nil
		}
		return sdk.Result{Findings: []sdk.Finding{{Resource: rc.Resource, Location: rc.Source, Evidence: "traffic is present but diagnostic logs are not arriving in the workspace"}}}, nil
	}
	return sdk.Result{}, nil
}

func accountID(t Target) string {
	return "/subscriptions/" + t.SubscriptionID + "/resourceGroups/" + t.ResourceGroup + "/providers/Microsoft.CognitiveServices/accounts/" + t.Account
}

func lastIDSegment(id string) string {
	id = strings.TrimRight(id, "/")
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

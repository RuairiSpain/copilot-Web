// Package run implements the Phase 3 runtime diagnosis rules.
package run

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
	Monitor     azure.RuntimeMonitor
	ProjectData foundrydp.ProjectReader
	SearchData  searchprobe.Client
	Resolver    netprobe.Resolver

	Target Target
	// ApprovedScopes are extra scopes (resource IDs) that connection targets may
	// lie in besides the project's resource group (--approved-scope, ADR-022).
	ApprovedScopes []string
	Options        runtime.Options
	Now            func() time.Time
}

// RuleIDs returns the runtime rule ids in order.
func RuleIDs() []string {
	ids := make([]string, 0, 7)
	for i := 1; i <= 7; i++ {
		ids = append(ids, fmt.Sprintf("FND-RUN-%03d", i))
	}
	return ids
}

// classes maps each rule to the probe class it runs under.
var classes = map[string]runtime.Class{
	"FND-RUN-001": runtime.ClassControlPlane,
	"FND-RUN-002": runtime.ClassVNetOnly,
	"FND-RUN-003": runtime.ClassControlPlane,
	"FND-RUN-004": runtime.ClassControlPlane,
	"FND-RUN-005": runtime.ClassDataPlane,
	"FND-RUN-006": runtime.ClassDataPlane,
	"FND-RUN-007": runtime.ClassControlPlane,
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
		if cerr := ctx.Err(); cerr != nil {
			if errors.Is(cerr, context.DeadlineExceeded) {
				// A per-probe timeout (--timeout), not a cancelled run.
				return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: sdk.SkipInputUnavailable + ": the probe exceeded its --timeout before it finished"}}, nil
			}
			return sdk.Result{}, cerr
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

// addLikely adds a finding whose confidence is capped at medium ("likely").
func (a *acc) addLikely(c runtime.Correlation, evidence string) {
	a.findings = append(a.findings, sdk.Finding{Resource: c.Resource, Location: c.Source, Evidence: evidence, Confidence: sdk.ConfidenceMedium})
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

// azureReason maps an error to a skip reason. Permission and network problems
// are "input-unavailable". Defects of this tool (a request outside the
// allow-list, a rejected input, an undecodable response) must not hide behind
// that wording: they are "uncertain: internal ..." so that they surface.
func azureReason(err error) string {
	switch {
	case errors.Is(err, azure.ErrNotAllowed):
		return runtime.UncertainPrefix + "internal error: a request was refused by the read-only allow-list (" + findings.Redact(err.Error()) + ")"
	case errors.Is(err, azure.ErrInvalidInput):
		return runtime.UncertainPrefix + "internal error: an input was rejected before any request (" + findings.Redact(err.Error()) + ")"
	case errors.Is(err, azure.ErrDecode):
		return runtime.UncertainPrefix + "internal error: a response did not have the expected shape (" + findings.Redact(err.Error()) + ")"
	}
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
	return sdk.SkipInputUnavailable + ": " + strings.TrimSpace(findings.Redact(err.Error()))
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

var (
	safeNameRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	resourceIDRe = regexp.MustCompile(`(?i)^/subscriptions/[0-9a-f-]{36}/resourcegroups/[A-Za-z0-9._()~-]{1,90}/providers/[A-Za-z0-9.]+(?:/[A-Za-z0-9._()~-]{1,90}){2,}$`)
)

// validResourceID reports whether id is a well-formed literal ARM resource ID.
// Connection metadata is untrusted: a malformed ID is never sent anywhere.
func validResourceID(id string) bool {
	id = strings.TrimSpace(id)
	for _, seg := range strings.Split(id, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return resourceIDRe.MatchString(id)
}

func looksLikeExpression(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "[") || strings.Contains(t, "${") || strings.Contains(t, "parameters(") || strings.Contains(t, "reference(") || strings.Contains(t, "resourceId(")
}

func isSystemAssigned(p azure.FoundryProject) bool {
	return p.PrincipalID != "" && strings.Contains(strings.ToLower(p.IdentityType), "systemassigned")
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
	principals := project.IdentityPrincipals()
	if len(principals) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": the project reports no managed identity principal (identity.principalId or identity.userAssignedIdentities)"}}, nil
	}
	system := ""
	if isSystemAssigned(project) {
		system = project.PrincipalID
	}
	corr := e.correlator(in)
	a := &acc{}
	reader := rbac.ReaderAdapter{Roles: e.d.RBAC}
	report := func(typ string, c azure.FoundryConnection, evs []rbac.Evaluation) {
		for _, ev := range evs {
			switch ev.Verdict {
			case rbac.Granted:
			case rbac.Uncertain:
				a.note(ev.Role + " on " + lastIDSegment(c.Target.ResourceID) + ": " + ev.Reason)
			default:
				a.addLikely(corr.Resource(typ, lastIDSegment(c.Target.ResourceID), c.Target.ResourceID), rbac.MissingMessage(ev))
			}
		}
	}
	for _, c := range conns {
		if isSkipAuth(c.AuthType) {
			a.skip(sdk.SkipInputUnavailable + ": connection " + c.Name + " uses " + c.AuthType + "; effective access cannot be proven without reading credentials")
			continue
		}
		target := strings.ToLower(c.Target.ResourceID)
		kind := ""
		switch {
		case strings.Contains(target, "/providers/microsoft.search/searchservices/"):
			kind = "search"
		case strings.Contains(target, "/providers/microsoft.storage/storageaccounts/"):
			kind = "storage"
		case strings.Contains(target, "/providers/microsoft.documentdb/databaseaccounts/"):
			kind = "cosmos"
		default:
			continue
		}
		if !validResourceID(c.Target.ResourceID) {
			a.note("connection " + c.Name + " has a malformed or unresolved target resource ID; effective access cannot be checked")
			continue
		}
		id := strings.TrimSpace(c.Target.ResourceID)
		c.Target.ResourceID = id
		switch kind {
		case "search":
			evs, err := rbac.Check(ctx, reader, principals, id, rbac.SearchServiceContributor, rbac.SearchIndexDataContributor)
			if err != nil {
				return sdk.Result{}, err
			}
			report("Microsoft.Search/searchServices", c, evs)
		case "storage":
			evs, err := rbac.Check(ctx, reader, principals, id, rbac.StorageBlobDataContributor, rbac.StorageBlobDataOwner)
			if err != nil {
				return sdk.Result{}, err
			}
			report("Microsoft.Storage/storageAccounts", c, evs)
			// Storage Account Contributor at account level is required for the
			// system-assigned identity only (FND-RUN-001 catalogue evidence).
			if system != "" {
				sev, err := rbac.Check(ctx, reader, []string{system}, id, rbac.StorageAccountContributor)
				if err != nil {
					return sdk.Result{}, err
				}
				report("Microsoft.Storage/storageAccounts", c, sev)
			}
		case "cosmos":
			evs, err := rbac.CheckCosmos(ctx, reader, principals, system, id)
			if err != nil {
				return sdk.Result{}, err
			}
			report("Microsoft.DocumentDB/databaseAccounts", c, evs)
		}
	}
	return a.result(), nil
}

// dependency is one DNS name the project relies on and the proof, if any, that
// a private endpoint backs it.
type dependency struct {
	typ, name, id, fqdn string
	proof               string
}

func (e *env) run002(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if m := needTarget(e.d.Target, true); m != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: m}}, nil
	}
	if e.d.Resolver == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": DNS resolver is not available"}}, nil
	}
	if e.d.Options.Vantage != runtime.VantageVNet {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": private DNS resolves only from inside the VNet; re-run with --vantage vnet from a machine inside the VNet (the vantage is asserted by you and not verified)"}}, nil
	}
	if e.d.FoundryMgmt == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": Foundry management metadata is not available, so private-endpoint backing cannot be established"}}, nil
	}
	a := &acc{}
	var deps []dependency
	// The Foundry account: proven private-endpoint backed when public network
	// access is Disabled or private endpoint connections exist (Accounts_Get).
	net, err := e.d.FoundryMgmt.GetAccountNetwork(ctx, e.d.Target.SubscriptionID, e.d.Target.ResourceGroup, e.d.Target.Account)
	switch {
	case err != nil && (errors.Is(err, azure.ErrNotAllowed) || errors.Is(err, azure.ErrInvalidInput) || errors.Is(err, azure.ErrDecode)):
		return sdk.Result{}, err
	case err != nil:
		if _, ok := azure.AsUnavailable(err); !ok && ctx.Err() != nil {
			return sdk.Result{}, err
		}
		a.note("the Foundry account's network exposure could not be read (" + azureReason(err) + "), so its private-endpoint backing is not proven")
	default:
		proof := ""
		switch {
		case strings.EqualFold(net.PublicNetworkAccess, "Disabled"):
			proof = "publicNetworkAccess is Disabled on the account"
		case net.PrivateEndpointConnections > 0:
			proof = "the account has " + strconv.Itoa(net.PrivateEndpointConnections) + " private endpoint connection(s)"
		}
		if proof != "" {
			for _, suffix := range []string{".cognitiveservices.azure.com", ".openai.azure.com", ".services.ai.azure.com"} {
				deps = append(deps, dependency{"Microsoft.CognitiveServices/accounts", e.d.Target.Account, accountID(e.d.Target), e.d.Target.Account + suffix, proof})
			}
		}
	}
	conns, _, err := e.boundConnections(ctx)
	if err != nil {
		return sdk.Result{}, err
	}
	for _, c := range conns {
		lower := strings.ToLower(c.Target.ResourceID)
		var d dependency
		switch {
		case strings.Contains(lower, "/providers/microsoft.search/searchservices/"):
			d = dependency{typ: "Microsoft.Search/searchServices", fqdn: lastIDSegment(c.Target.ResourceID) + ".search.windows.net"}
		case strings.Contains(lower, "/providers/microsoft.documentdb/databaseaccounts/"):
			d = dependency{typ: "Microsoft.DocumentDB/databaseAccounts", fqdn: lastIDSegment(c.Target.ResourceID) + ".documents.azure.com"}
		case strings.Contains(lower, "/providers/microsoft.storage/storageaccounts/"):
			d = dependency{typ: "Microsoft.Storage/storageAccounts", fqdn: lastIDSegment(c.Target.ResourceID) + ".blob.core.windows.net"}
		default:
			continue
		}
		d.name, d.id = lastIDSegment(c.Target.ResourceID), c.Target.ResourceID
		if !validResourceID(d.id) || !safeNameRe.MatchString(d.name) || runtime.ValidateDataPlaneName("dependency", strings.ToLower(d.name)) != nil {
			a.note("connection " + c.Name + " has a malformed target resource ID; its DNS name is not probed")
			continue
		}
		d.fqdn = strings.ToLower(d.fqdn)
		// peStatus Active is the connection-level proof (ManagedPEStatus). The
		// target's own private endpoint connections are not read for Search,
		// Storage or Cosmos DB.
		if strings.EqualFold(c.PEStatus, "Active") {
			d.proof = "the connection reports an active managed private endpoint (peStatus Active)"
			deps = append(deps, d)
		} else {
			a.note("connection " + c.Name + " (" + d.name + "): private-endpoint backing is not proven (peStatus is " + orDash(c.PEStatus) + "); its DNS name is not probed")
		}
	}
	if len(deps) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no private-endpoint-backed dependencies are in scope"}}, nil
	}
	corr := e.correlator(in)
	resolver := netprobe.TimeoutResolver{Inner: e.d.Resolver, Timeout: e.d.Options.Timeout}
	for _, d := range deps {
		r := netprobe.ResolvePrivate(ctx, resolver, e.d.Options.Vantage, d.fqdn, true)
		switch r.State {
		case runtime.StatePass:
		case runtime.StateFail:
			a.add(corr.Resource(d.typ, d.name, d.id), d.fqdn+": "+r.Reason+" ("+d.proof+")")
		case runtime.StateSkipped:
			a.skip(r.Reason)
		default:
			a.note(d.fqdn + ": " + r.Reason)
		}
	}
	return a.result(), nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not reported"
	}
	return s
}

func (e *env) rgScope() string {
	return "/subscriptions/" + e.d.Target.SubscriptionID + "/resourceGroups/" + e.d.Target.ResourceGroup
}

// resourceBacked reports whether a connection category names an Azure resource
// whose ARM ID should be available (spec ConnectionCategory values).
func resourceBacked(category string) bool {
	switch strings.ToLower(category) {
	case "cognitivesearch", "cosmosdb", "azurestorageaccount", "azureblob", "adlsgen2", "azureopenai", "aiservices":
		return true
	}
	return false
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
	scopes := append([]string{e.rgScope()}, e.d.ApprovedScopes...)
	for _, name := range names {
		c, ok := index[strings.ToLower(name)]
		if !ok {
			a.add(projCorr, "bound connection "+name+" does not exist on the project or account")
			continue
		}
		cc := corr.Resource("", c.Name, c.ID)
		if strings.TrimSpace(c.Error) != "" {
			a.add(cc, "connection "+c.Name+" reports error "+findings.Redact(c.Error))
		}
		// Scope policy (ADR-021 e, ADR-022): a literal ARM ID target outside the
		// project's resource group and every approved scope is a finding.
		rid := strings.TrimSpace(c.Target.ResourceID)
		switch {
		case rid == "":
			if resourceBacked(c.Category) {
				a.note("connection " + c.Name + " exposes no target resource ID (metadata ResourceId is unverified as a schema key), so the scope policy was not evaluated")
			}
		case looksLikeExpression(rid):
			a.note("connection " + c.Name + " targets an unresolved expression; the scope policy was not evaluated")
		case azure.IsARMResourceID(rid):
			if !azure.WithinScope(rid, scopes...) {
				a.add(cc, "connection "+c.Name+" targets "+rid+", outside the project resource group "+e.rgScope()+" and not in an approved scope; pass --approved-scope to allow it")
			}
		}
	}
	return a.result(), nil
}

var (
	httpCodeRe = regexp.MustCompile(`^[1-5][0-9]{2}$`)
)

// seriesName returns a deployment name that is safe to put in evidence and IDs.
func seriesName(s monprobe.Series) (string, bool) {
	n := s.Dimensions[strings.ToLower(monprobe.DimDeployment)]
	return n, safeNameRe.MatchString(n)
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
	acct := accountID(e.d.Target)
	acctCorr := corr.Resource("Microsoft.CognitiveServices/accounts", e.d.Target.Account, acct)

	// Throttling: split by StatusCode and match values client-side. The docs do
	// not enumerate the StatusCode values (they speak of categories), so 429 is
	// only reported when a value is literally 429.
	status, err := mc.QueryRequestsByStatus(ctx, acct)
	if err != nil {
		return sdk.Result{}, err
	}
	if status.ErrorCode != "" {
		a.note("the requests metric query reported errorCode " + status.ErrorCode)
	}
	if status.Truncated {
		a.note("the requests metric returned the maximum number of series; some deployments may be missing")
	}
	switch {
	case len(status.Series) == 0:
		// Not a zero count: no time series at all (no requests in the window, or
		// the metric was not emitted).
		a.note("Azure Monitor returned no time series for AzureOpenAIRequests in the last hour; this is not a count of zero, so throttling cannot be assessed")
	default:
		identified, numeric := false, true
		observed := map[string]bool{}
		for _, s := range status.Series {
			code := strings.TrimSpace(s.Dimensions[strings.ToLower(monprobe.DimStatusCode)])
			if code != "" && safeNameRe.MatchString(code) {
				observed[code] = true
			}
			if !httpCodeRe.MatchString(code) {
				numeric = false
			}
			if code != "429" {
				continue
			}
			identified = true
			if s.Value <= 0 {
				continue
			}
			if name, ok := seriesName(s); ok {
				a.add(corr.Resource("Microsoft.CognitiveServices/accounts/deployments", name, acct+"/deployments/"+name), fmt.Sprintf("deployment %s returned %.0f HTTP 429 responses in the last hour", name, s.Value))
			} else {
				a.add(acctCorr, fmt.Sprintf("the account returned %.0f HTTP 429 responses in the last hour (series not attributable to a deployment)", s.Value))
			}
		}
		if !identified && !numeric {
			vals := make([]string, 0, len(observed))
			for v := range observed {
				vals = append(vals, v)
			}
			sort.Strings(vals)
			if len(vals) > 10 {
				vals = vals[:10]
			}
			a.note("no StatusCode value could be identified as 429 (observed: " + strings.Join(vals, ", ") + "); the metric docs describe categories, so throttling cannot be assessed from it")
		}
	}

	// Provisioned utilisation: maximum per deployment.
	util, err := mc.QueryProvisionedUtilization(ctx, acct)
	if err != nil {
		return sdk.Result{}, err
	}
	if util.ErrorCode != "" {
		a.note("the utilization metric query reported errorCode " + util.ErrorCode)
	}
	if util.Truncated {
		a.note("the utilization metric returned the maximum number of series; some deployments may be missing")
	}
	for _, s := range util.Series {
		if s.Value < 100 {
			continue
		}
		if name, ok := seriesName(s); ok {
			a.add(corr.Resource("Microsoft.CognitiveServices/accounts/deployments", name, acct+"/deployments/"+name), fmt.Sprintf("deployment %s reached %.0f%% provisioned-managed utilization (maximum in the last hour)", name, s.Value))
		} else {
			a.add(acctCorr, fmt.Sprintf("a deployment reached %.0f%% provisioned-managed utilization (series not attributable to a deployment name)", s.Value))
		}
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
	list, err := e.d.ProjectData.ListAgents(ctx, endpoint)
	if err != nil {
		return sdk.Result{}, err
	}
	index := map[string]foundrydp.Agent{}
	for _, ag := range list.Agents {
		index[strings.ToLower(ag.Name)] = ag
	}
	for _, agent := range e.agentNames(in) {
		ac := corr.Agent(agent)
		ag, ok := index[strings.ToLower(agent)]
		if !ok {
			if list.Truncated {
				a.note("agent " + agent + " was not found in the first " + strconv.Itoa(len(list.Agents)) + " agents; the listing was capped")
			} else {
				a.add(ac, "agent "+agent+" is not listed on the project endpoint")
			}
			continue
		}
		// versions.latest.status: creating|active|failed|deleting|deleted; absent
		// means active for non-hosted agents (Foundry v1 spec).
		switch strings.ToLower(ag.LatestStatus) {
		case "", "active":
		case "creating":
			a.note("agent " + agent + " version " + ag.LatestVersion + " is still creating")
		case "failed", "deleting", "deleted":
			a.add(ac, "agent "+agent+" version "+ag.LatestVersion+" status is "+ag.LatestStatus)
		default:
			a.note("agent " + agent + " reports an unrecognised version status")
		}
		if strings.EqualFold(ag.State, "disabled") {
			a.note("agent " + agent + " is disabled (state disabled): its endpoint rejects requests")
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
	for _, c := range dpConns.Connections {
		have[strings.ToLower(c.Name)] = true
	}
	for _, name := range names {
		if have[strings.ToLower(name)] {
			continue
		}
		if dpConns.Truncated {
			a.note("bound connection " + name + " was not found in the first " + strconv.Itoa(len(dpConns.Connections)) + " connections; the listing was capped")
			continue
		}
		a.add(corr.Resource("", name, ""), "project endpoint does not list bound connection "+name)
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
	seenSearch := false
	for _, c := range conns {
		if !strings.Contains(strings.ToLower(c.Target.ResourceID), "/providers/microsoft.search/searchservices/") {
			continue
		}
		seenSearch = true
		// The index and indexer names come from connection metadata keys the
		// Foundry spec does not define (UNVERIFIED hints). Without them the index
		// to check is unknown, and the rule says so instead of passing.
		if len(c.Target.IndexNames) == 0 {
			a.skip(sdk.SkipInputUnavailable + ": search connection " + c.Name + " carries no index name hint (metadata keys index and indexName are unverified optional inputs), so the index to check is unknown")
			continue
		}
		service := lastIDSegment(c.Target.ResourceID)
		for _, indexName := range c.Target.IndexNames {
			rc := corr.Resource("Microsoft.Search/searchServices", service, c.Target.ResourceID)
			if !searchprobe.ValidObjectName(indexName) {
				a.note("search connection " + c.Name + " has an index hint that is not a valid index name; it was not queried")
				continue
			}
			idx, err := e.d.SearchData.GetIndex(ctx, service, indexName)
			if err != nil {
				if runtime.IsNotFound(err) {
					a.add(rc, "search index "+indexName+" does not exist")
					continue
				}
				return sdk.Result{}, err
			}
			if !searchprobe.HasUsableVectorField(idx) {
				a.add(rc, searchprobe.MissingSchema(idx))
			} else if missing := searchprobe.UnresolvedProfiles(idx); len(missing) > 0 {
				a.add(rc, "index "+indexName+" has vector field(s) whose vectorSearchProfile is not defined in vectorSearch.profiles: "+strings.Join(missing, ", "))
			}
			stats, err := e.d.SearchData.GetIndexStatistics(ctx, service, indexName)
			if err != nil {
				return sdk.Result{}, err
			}
			zeroDocs := stats.DocumentCount == 0
			zeroDocsUncertain := false
			neverSucceeded := false
			for _, indexer := range c.Target.IndexerNames {
				if !searchprobe.ValidObjectName(indexer) {
					a.note("search connection " + c.Name + " has an indexer hint that is not a valid indexer name; it was not queried")
					continue
				}
				st, err := e.d.SearchData.GetIndexerStatus(ctx, service, indexer)
				if err != nil {
					return sdk.Result{}, err
				}
				switch st.LastResultStatus {
				case searchprobe.ResultTransientFailure:
					a.note("search indexer " + indexer + " last run ended in a transient failure" + itemsFailed(st))
				case searchprobe.ResultInProgress:
					a.note("search indexer " + indexer + " is still in progress")
					zeroDocsUncertain = true
				case searchprobe.ResultReset:
					a.note("search indexer " + indexer + " was reset")
					zeroDocsUncertain = true
				}
				if st.Status == searchprobe.StatusError {
					a.add(rc, "search indexer "+indexer+" is in error state"+itemsFailed(st))
				}
				switch {
				case !st.HasSuccess:
					neverSucceeded = true
					if !zeroDocs {
						a.note("search indexer " + indexer + " shows no successful run in its visible execution history")
					}
				case zeroDocs && st.LastSuccessMinutes < searchprobe.FreshSuccessMinutes:
					zeroDocsUncertain = true
				}
			}
			if zeroDocs {
				switch {
				case zeroDocsUncertain:
					a.note("search index " + indexName + " has documentCount 0 but an indexer succeeded within the last " + strconv.Itoa(searchprobe.FreshSuccessMinutes) + " minutes or is running; statistics lag, so this is not treated as a failure")
				case neverSucceeded:
					a.add(rc, "search index "+indexName+" has documentCount 0 and no indexer has a successful run in its visible history")
				default:
					a.add(rc, "search index "+indexName+" has documentCount 0")
				}
			}
		}
	}
	if !seenSearch {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": project has no Search connection with index metadata"}}, nil
	}
	return a.result(), nil
}

func itemsFailed(st searchprobe.IndexerStatus) string {
	if st.ItemsFailed > 0 {
		return fmt.Sprintf(" (itemsFailed=%d)", st.ItemsFailed)
	}
	return ""
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
		if s.WorkspaceID != "" && s.SendsAuditOrRequestResponse() {
			setting, found = s, true
			break
		}
	}
	if !found {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no diagnostic setting sends Audit or RequestResponse logs to a workspace"}}, nil
	}
	// logAnalyticsDestinationType Dedicated means resource-specific tables, which
	// the fixed AzureDiagnostics count does not read. The table names of those
	// tables are not verified, so the result is uncertain, never "logs missing".
	if setting.DedicatedTables() {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "diagnostic setting " + setting.Name + " writes to resource-specific tables (logAnalyticsDestinationType Dedicated); the AzureDiagnostics count does not apply and those tables are not queried"}}, nil
	}
	traffic, err := client.QueryTraffic(ctx, accountRID)
	if err != nil {
		return sdk.Result{}, err
	}
	if len(traffic.Series) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "Azure Monitor returned no time series for AzureOpenAIRequests; traffic is unknown, so missing logs cannot be judged"}}, nil
	}
	var trafficTotal float64
	for _, s := range traffic.Series {
		trafficTotal += s.Value
	}
	if trafficTotal == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "no traffic in the metric window"}}, nil
	}
	logs, err := client.QueryDiagnosticCounts(ctx, setting.WorkspaceID, e.d.Target.Account, 1)
	if err != nil {
		return sdk.Result{}, err
	}
	var count int64
	for _, v := range logs.Counts {
		count += v
	}
	if count > 0 {
		return sdk.Result{}, nil
	}
	if !setting.AgeKnown {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "no log rows yet, but the diagnostic setting's age is not reported, so a recent change cannot be excluded"}}, nil
	}
	if setting.AgeMinutes < 120 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: runtime.UncertainPrefix + "diagnostic setting was created recently"}}, nil
	}
	return sdk.Result{Findings: []sdk.Finding{{Resource: rc.Resource, Location: rc.Source, Evidence: "traffic is present but diagnostic logs are not arriving in the workspace"}}}, nil
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

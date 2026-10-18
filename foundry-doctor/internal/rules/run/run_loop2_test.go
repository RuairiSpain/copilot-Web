package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	foundrydp "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/foundry"
	searchprobe "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/search"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// ---- helpers ----------------------------------------------------------------------

// mapYAML is a configurable azure.yaml: service -> field -> value.
type mapYAML map[string]map[string]string

func (mapYAML) Path() string { return "azure.yaml" }
func (m mapYAML) ServiceNames() []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func (m mapYAML) Lookup(path ...string) (string, sdk.Location, bool) {
	if len(path) == 3 && path[0] == "services" {
		if v, ok := m[path[1]][path[2]]; ok {
			return v, sdk.Location{File: "azure.yaml", Line: 3}, true
		}
	}
	return "", sdk.Location{}, false
}

func agentInput(y mapYAML) *sdk.Input { return &sdk.Input{AzureYAML: y} }

func assertUncertain(t *testing.T, res sdk.Result, want ...string) {
	t.Helper()
	if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, rt.UncertainPrefix) {
		t.Fatalf("want an uncertain result, got %+v (skip %+v)", res, res.Skipped)
	}
	for _, w := range want {
		if !strings.Contains(res.Skipped.Reason, w) {
			t.Errorf("uncertain reason %q lacks %q", res.Skipped.Reason, w)
		}
	}
}

// ---- C1: RUN-001 never passes with nothing checked -----------------------------------

func TestRun001ResourceBackedConnectionWithoutResourceIDIsUncertain(t *testing.T) {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{
		{Name: "search-conn", Category: "CognitiveSearch"},
		{Name: "storage-conn", Category: "AzureStorageAccount"},
		{Name: "cosmos-conn", Category: "CosmosDB"},
	}
	d.FoundryMgmt = m
	d.RBAC = allGranted()
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "search-conn", "storage-conn", "cosmos-conn", "exposes no target resource ID; effective access was not checked")
	if rules.ClassifySkip(*res.Skipped) != rules.SkipHidesCheck {
		t.Errorf("an unchecked connection must count as a hidden check under --strict: %+v", res.Skipped)
	}
}

func TestRun001OneCheckedAndOneUnresolvedIsStillUncertain(t *testing.T) {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{
		{Name: "search-conn", Category: "CognitiveSearch", Target: azure.ConnectionTarget{ResourceID: searchID}},
		{Name: "storage-conn", Category: "AzureStorageAccount"},
		{Name: "cosmos-conn", Category: "CosmosDB", Target: azure.ConnectionTarget{ResourceID: cosmosID}},
	}
	d.FoundryMgmt = m
	d.RBAC = allGranted()
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "storage-conn", "no target resource ID")
}

func TestRun001OnlyModelConnectionsIsAByDesignSkipNotAPass(t *testing.T) {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.projectConnections = []azure.FoundryConnection{{Name: "aoai", Category: "AzureOpenAI", Target: azure.ConnectionTarget{ResourceID: rgScope + "/providers/Microsoft.CognitiveServices/accounts/aoai"}}}
	m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", AIServiceConnections: []string{"aoai"}}}
	d.FoundryMgmt = m
	res, err := evalRule(t, "FND-RUN-001", d, baseInput())
	if err != nil || len(res.Findings) != 0 || res.Skipped == nil {
		t.Fatalf("nothing checked must not be a pass: %+v err=%v", res, err)
	}
	if rules.ClassifySkip(*res.Skipped) != rules.SkipByDesign {
		t.Errorf("a project with no Search, Storage or Cosmos DB binding hid nothing: %+v", res.Skipped)
	}
}

// ---- C2: RUN-002 --------------------------------------------------------------------

type recResolver struct {
	mu    sync.Mutex
	hosts []string
	inner fakeResolver
}

func (r *recResolver) LookupHost(ctx context.Context, h string) ([]string, error) {
	r.mu.Lock()
	r.hosts = append(r.hosts, h)
	r.mu.Unlock()
	return r.inner.LookupHost(ctx, h)
}

func run002Deps(net azure.AccountNetwork, netErr error, conns []azure.FoundryConnection) Deps {
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.net, m.netErr = net, netErr
	m.projectConnections = conns
	m.hosts = nil
	if len(conns) > 0 {
		m.hosts = []azure.CapabilityHost{{ID: "h", Name: "default", ProvisioningState: "Succeeded", VectorStoreConnections: []string{conns[0].Name}}}
	}
	d.FoundryMgmt = m
	d.Options.Vantage = rt.VantageVNet
	d.Resolver = fakeResolver{}
	return d
}

func TestRun002UnreadableAccountIsNeverTheByDesignSkip(t *testing.T) {
	for name, netErr := range map[string]error{
		"403 permission":  &azure.UnavailableError{Capability: "accounts/read", Permission: "Microsoft.CognitiveServices/accounts/read"},
		"transport error": errors.New("connection reset"),
	} {
		t.Run(name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-002", run002Deps(azure.AccountNetwork{}, netErr, nil), baseInput())
			if err != nil {
				t.Fatal(err)
			}
			assertUncertain(t, res, "could not be read", "not proven")
			if rules.ClassifySkip(*res.Skipped) != rules.SkipHidesCheck {
				t.Errorf("an unreadable account must hide a check, not be by design: %+v", res.Skipped)
			}
		})
	}
}

func TestRun002UnreadableAccountStaysUncertainEvenWhenAConnectionPasses(t *testing.T) {
	conns := []azure.FoundryConnection{{Name: "search-conn", Category: "CognitiveSearch", PEStatus: "Active", Target: azure.ConnectionTarget{ResourceID: searchID}}}
	d := run002Deps(azure.AccountNetwork{}, &azure.UnavailableError{Capability: "accounts/read"}, conns)
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "could not be read")
}

func TestRun002NothingInScopeSkipIsByDesignAndSaysBackingWasNotProven(t *testing.T) {
	d := run002Deps(azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct"}, nil, nil)
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil || res.Skipped == nil || len(res.Findings) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Skipped.Reason, "private-endpoint backing was not proven") || strings.Contains(res.Skipped.Reason, "no private-endpoint-backed dependencies") {
		t.Errorf("reason = %q", res.Skipped.Reason)
	}
	if rules.ClassifySkip(*res.Skipped) != rules.SkipByDesign {
		t.Errorf("a public account with nothing private in scope hid nothing: %+v", res.Skipped)
	}
}

func TestRun002UnprovenConnectionMakesTheSkipUncertain(t *testing.T) {
	conns := []azure.FoundryConnection{{Name: "search-conn", Category: "CognitiveSearch", PEStatus: "NotApplicable", Target: azure.ConnectionTarget{ResourceID: searchID}}}
	d := run002Deps(azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct"}, nil, conns)
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "search-conn", "private-endpoint backing is not proven")
}

func TestRun002OnlyApprovedPrivateEndpointConnectionsProve(t *testing.T) {
	public := map[string][]string{"acct.cognitiveservices.azure.com": {"52.1.1.1"}, "acct.openai.azure.com": {"52.1.1.1"}, "acct.services.ai.azure.com": {"52.1.1.1"}}
	for name, c := range map[string]struct {
		net      azure.AccountNetwork
		wantFind bool
	}{
		"rejected only":   {azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct", PrivateEndpointConnections: 2}, false},
		"approved":        {azure.AccountNetwork{PublicNetworkAccess: "Enabled", CustomSubDomainName: "acct", PrivateEndpointConnections: 2, ApprovedPrivateEndpointConnections: 1}, true},
		"public disabled": {azure.AccountNetwork{PublicNetworkAccess: "Disabled", CustomSubDomainName: "acct"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			d := run002Deps(c.net, nil, nil)
			d.Resolver = fakeResolver{answers: public}
			res, err := evalRule(t, "FND-RUN-002", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			if c.wantFind {
				if len(res.Findings) != 3 {
					t.Fatalf("want three account findings, got %+v", res)
				}
				return
			}
			assertUncertain(t, res, "none is Approved")
		})
	}
}

func TestRun002ReportsLikelyConfidenceBecauseTheVantageIsAsserted(t *testing.T) {
	d := run002Deps(azure.AccountNetwork{PublicNetworkAccess: "Disabled", CustomSubDomainName: "acct"}, nil, nil)
	d.Resolver = fakeResolver{answers: map[string][]string{"acct.cognitiveservices.azure.com": {"52.1.1.1"}}}
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil || len(res.Findings) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if res.Findings[0].Confidence != sdk.ConfidenceMedium {
		t.Errorf("confidence = %q, want %q (the vantage is asserted, not verified)", res.Findings[0].Confidence, sdk.ConfidenceMedium)
	}
}

func TestRun002OnlyAClearlyPublicAddressFails(t *testing.T) {
	net := azure.AccountNetwork{PublicNetworkAccess: "Disabled", CustomSubDomainName: "acct"}
	for name, c := range map[string]struct {
		answer   string
		wantFind bool
	}{
		"public":   {"20.50.1.1", true},
		"CGNAT":    {"100.64.0.9", false},
		"loopback": {"127.0.0.1", false},
		"linkloc":  {"169.254.0.4", false},
		"ULA":      {"fd12::4", false}, // private: passes
	} {
		t.Run(name, func(t *testing.T) {
			d := run002Deps(net, nil, nil)
			d.Resolver = fakeResolver{answers: map[string][]string{"acct.cognitiveservices.azure.com": {c.answer}}}
			res, err := evalRule(t, "FND-RUN-002", d, baseInput())
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.wantFind:
				if len(res.Findings) != 1 {
					t.Fatalf("res=%+v", res)
				}
			case name == "ULA":
				if res.Skipped != nil || len(res.Findings) != 0 {
					t.Fatalf("a ULA answer is private: %+v", res)
				}
			default:
				assertUncertain(t, res, "neither a private nor a clearly public")
			}
		})
	}
}

// ---- NB1/NB2: the real subdomain is customSubDomainName -------------------------------

func TestRun002UsesCustomSubDomainNameNotTheARMAccountName(t *testing.T) {
	rec := &recResolver{}
	d := run002Deps(azure.AccountNetwork{PublicNetworkAccess: "Disabled", CustomSubDomainName: "contoso-ai"}, nil, nil)
	d.Resolver = rec
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got := strings.Join(rec.hosts, ",")
	for _, want := range []string{"contoso-ai.cognitiveservices.azure.com", "contoso-ai.openai.azure.com", "contoso-ai.services.ai.azure.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("host %s was not resolved; resolved: %s", want, got)
		}
	}
	if strings.Contains(got, "acct.") {
		t.Errorf("the ARM account name was used as a DNS label: %s", got)
	}
}

func TestRun002AccountWithoutCustomSubDomainNameIsUncertain(t *testing.T) {
	rec := &recResolver{}
	d := run002Deps(azure.AccountNetwork{PublicNetworkAccess: "Disabled"}, nil, nil)
	d.Resolver = rec
	res, err := evalRule(t, "FND-RUN-002", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "customSubDomainName")
	if len(rec.hosts) != 0 {
		t.Errorf("a guessed host name was resolved: %v", rec.hosts)
	}
}

type recProject struct {
	fakeProjectReader
	pinged atomic.Value // string
	called atomic.Int32
}

func (r *recProject) Ping(ctx context.Context, endpoint string) error {
	r.called.Add(1)
	r.pinged.Store(endpoint)
	return r.fakeProjectReader.Ping(ctx, endpoint)
}

func TestRun005UsesCustomSubDomainNameForTheProjectEndpoint(t *testing.T) {
	rp := &recProject{fakeProjectReader: fakeProjectReader{
		agents:      foundrydp.AgentList{Agents: []foundrydp.Agent{{Name: "support-agent", LatestStatus: "active"}}},
		connections: foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}}},
	}}
	d := baseDeps()
	m := d.FoundryMgmt.(fakeMgmt)
	m.net = azure.AccountNetwork{CustomSubDomainName: "contoso-ai"}
	d.FoundryMgmt = m
	d.ProjectData = rp
	res, err := evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got, _ := rp.pinged.Load().(string); got != "https://contoso-ai.services.ai.azure.com/api/projects/proj" {
		t.Errorf("project endpoint = %q", got)
	}

	// Absent subdomain: uncertain, and no request is made to a guessed host.
	rp2 := &recProject{}
	m.net = azure.AccountNetwork{}
	d.FoundryMgmt = m
	d.ProjectData = rp2
	res, err = evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "customSubDomainName")
	if rp2.called.Load() != 0 {
		t.Error("the project data plane was called without a known subdomain")
	}

	// Unreadable account: a skip naming the missing capability, never a guess.
	m.netErr = &azure.UnavailableError{Capability: "accounts/read", Permission: "Microsoft.CognitiveServices/accounts/read"}
	d.FoundryMgmt = m
	res, err = evalRule(t, "FND-RUN-005", d, baseInput())
	if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, "input-unavailable") || rp2.called.Load() != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// The same mismatch through the real adapter and data-plane clients: every
// Foundry data-plane request goes to the customSubDomainName host.
func TestSubDomainMismatchThroughTheRealAdapter(t *testing.T) {
	routes := contractRoutes()
	routes["GET management.test"+acctPath+"/providers/Microsoft.Insights/metrics"] = contractMetrics
	routes["GET management.test"+acctPath] = `{"id":"` + acctPath + `","name":"acct","properties":{"publicNetworkAccess":"Disabled","customSubDomainName":"contoso-ai","privateEndpointConnections":[{"id":"pe1","properties":{"privateLinkServiceConnectionState":{"status":"Approved"}}}]}}`
	for _, p := range []string{"/api/projects/proj/agents", "/api/projects/proj/connections"} {
		routes["GET contoso-ai.services.ai.azure.com"+p] = routes["GET acct.services.ai.azure.com"+p]
		delete(routes, "GET acct.services.ai.azure.com"+p)
	}
	h := &hostRT{t: t, routes: routes}
	d := contractDeps(t, h)
	rec := &recResolver{}
	d.Resolver = rec
	in := baseInput()
	for _, id := range []string{"FND-RUN-002", "FND-RUN-005"} {
		res, err := evalRule(t, id, d, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Findings) != 0 || (res.Skipped != nil && strings.HasPrefix(res.Skipped.Reason, "input-unavailable")) {
			t.Errorf("%s = %+v (skip %+v)", id, res, res.Skipped)
		}
	}
	if !h.called("GET contoso-ai.services.ai.azure.com/api/projects/proj/agents") || h.called("GET acct.services.ai.azure.com") {
		t.Errorf("the data plane must use the customSubDomainName host; calls: %v", h.calls)
	}
	if !strings.Contains(strings.Join(rec.hosts, ","), "contoso-ai.openai.azure.com") {
		t.Errorf("DNS probes did not use the subdomain: %v", rec.hosts)
	}
}

// ---- C3: RUN-005 agent names ------------------------------------------------------------

func agentListDeps(listed ...string) Deps {
	var agents []foundrydp.Agent
	for _, n := range listed {
		agents = append(agents, foundrydp.Agent{Name: n, LatestStatus: "active"})
	}
	return agentDeps(fakeProjectReader{
		agents:      foundrydp.AgentList{Agents: agents},
		connections: foundrydp.ConnectionList{Connections: []foundrydp.Connection{{Name: "search-conn"}, {Name: "storage-conn"}, {Name: "cosmos-conn"}}},
	})
}

func TestRun005AgentNameResolution(t *testing.T) {
	cases := []struct {
		name     string
		yaml     mapYAML
		listed   []string
		wantFind string // finding evidence substring
		wantNote string // uncertain reason substring
	}{
		{"service key differs from the agent name: key is only an assumption",
			mapYAML{"support": {"host": "azure.ai.agent"}}, []string{"billing-bot"}, "", "assumed to be the azure.yaml service key"},
		{"inline definition name is authoritative and listed",
			mapYAML{"support": {"host": "azure.ai.agent", "name": "billing-bot"}}, []string{"billing-bot"}, "", ""},
		{"inline definition name differs from the key and is not listed: a finding about that name",
			mapYAML{"support": {"host": "azure.ai.agent", "name": "billing-bot"}}, []string{"support"}, "agent billing-bot is not listed", ""},
		{"prompt agent without a name is named after the service key (azd applyPromptAgentServiceName)",
			mapYAML{"support": {"host": "azure.ai.agent", "kind": "prompt"}}, []string{"other"}, "agent support is not listed", ""},
		{"assumed key that is listed passes",
			mapYAML{"support": {"host": "azure.ai.agent"}}, []string{"support"}, "", ""},
		{"a service with another host is ignored",
			mapYAML{"web": {"host": "containerapp"}}, nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := evalRule(t, "FND-RUN-005", agentListDeps(c.listed...), agentInput(c.yaml))
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.wantFind != "":
				if len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, c.wantFind) {
					t.Fatalf("res=%+v", res)
				}
			case c.wantNote != "":
				assertUncertain(t, res, c.wantNote, "support")
			default:
				if res.Skipped != nil || len(res.Findings) != 0 {
					t.Fatalf("want pass, got %+v (skip %+v)", res, res.Skipped)
				}
			}
		})
	}
}

// ---- N1: the env caches ---------------------------------------------------------------------

type countingMgmt struct {
	fakeMgmt
	calls atomic.Int32
	// failFirst, when set, makes the first GetProject return this error.
	failFirst error
	delay     time.Duration
}

func (c *countingMgmt) GetProject(ctx context.Context, a, b, cc, d string) (azure.FoundryProject, error) {
	n := c.calls.Add(1)
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return azure.FoundryProject{}, ctx.Err()
		}
	}
	if n == 1 && c.failFirst != nil {
		return azure.FoundryProject{}, c.failFirst
	}
	return c.fakeMgmt.GetProject(ctx, a, b, cc, d)
}

func TestEnvNeverCachesContextErrors(t *testing.T) {
	for name, first := range map[string]error{"deadline": context.DeadlineExceeded, "cancelled": context.Canceled, "wrapped": fmt.Errorf("x: %w", context.DeadlineExceeded)} {
		t.Run(name, func(t *testing.T) {
			cm := &countingMgmt{fakeMgmt: baseDeps().FoundryMgmt.(fakeMgmt), failFirst: first}
			e := &env{d: baseDeps()}
			e.d.FoundryMgmt = cm
			if _, err := e.projectInfo(context.Background()); !errors.Is(err, first) {
				t.Fatalf("first call err = %v", err)
			}
			p, err := e.projectInfo(context.Background())
			if err != nil || p.Name != "proj" {
				t.Fatalf("a context error was cached: %+v err=%v", p, err)
			}
			if cm.calls.Load() != 2 {
				t.Fatalf("calls = %d, want 2", cm.calls.Load())
			}
			// Now cached: no third request.
			if _, err := e.projectInfo(context.Background()); err != nil || cm.calls.Load() != 2 {
				t.Fatalf("success must be cached: calls=%d err=%v", cm.calls.Load(), err)
			}
		})
	}
}

func TestEnvStillCachesOrdinaryErrors(t *testing.T) {
	boom := errors.New("boom")
	cm := &countingMgmt{fakeMgmt: baseDeps().FoundryMgmt.(fakeMgmt), failFirst: boom}
	e := &env{d: baseDeps()}
	e.d.FoundryMgmt = cm
	for i := 0; i < 3; i++ {
		if _, err := e.projectInfo(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("call %d err = %v", i, err)
		}
	}
	if cm.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", cm.calls.Load())
	}
}

func TestEnvFetchIsSingleFlightAndRaceFree(t *testing.T) {
	cm := &countingMgmt{fakeMgmt: baseDeps().FoundryMgmt.(fakeMgmt), delay: 40 * time.Millisecond}
	e := &env{d: baseDeps()}
	e.d.FoundryMgmt = cm
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := e.projectInfo(context.Background())
			if err != nil || p.Name != "proj" {
				t.Errorf("p=%+v err=%v", p, err)
			}
		}()
	}
	wg.Wait()
	if cm.calls.Load() != 1 {
		t.Fatalf("concurrent callers made %d requests, want 1", cm.calls.Load())
	}
}

func TestEnvWaiterHonoursItsOwnContext(t *testing.T) {
	cm := &countingMgmt{fakeMgmt: baseDeps().FoundryMgmt.(fakeMgmt), delay: 300 * time.Millisecond}
	e := &env{d: baseDeps()}
	e.d.FoundryMgmt = cm
	go func() { _, _ = e.projectInfo(context.Background()) }()
	time.Sleep(30 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := e.projectInfo(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a waiter past its deadline must return its own context error, got %v", err)
	}
}

func TestEnvCachedSlicesAreNotAliased(t *testing.T) {
	e := &env{d: baseDeps()}
	a, _ := e.projectConnections(context.Background())
	a[0].Name = "mutated"
	b, _ := e.projectConnections(context.Background())
	if b[0].Name == "mutated" {
		t.Fatal("a caller mutated the cached connection list")
	}
}

// ---- NB7 / N6: RUN-006 -------------------------------------------------------------------------

func TestRun006ZeroDocumentsWithoutIndexerEvidenceIsUncertain(t *testing.T) {
	d := searchDeps(fakeSearch{index: goodIndex(), stats: searchprobe.Stats{DocumentCount: 0}}, azure.ConnectionTarget{IndexNames: []string{"knowledge"}})
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "documentCount 0", "no indexer status was read", "collected periodically")

	// With indexer evidence of an old success the failure is kept.
	d = searchDeps(fakeSearch{index: goodIndex(), stats: searchprobe.Stats{DocumentCount: 0},
		status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}}, hint)
	res, err = evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "documentCount 0") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRun006KeywordOnlyIndexIsUncertainButUnresolvedProfilesStillFail(t *testing.T) {
	kw := searchprobe.Index{Name: "knowledge", Fields: []searchprobe.Field{{Name: "id", Type: "Edm.String"}, {Name: "text", Type: "Edm.String"}}}
	d := searchDeps(fakeSearch{index: kw, stats: searchprobe.Stats{DocumentCount: 5},
		status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}}, hint)
	res, err := evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	assertUncertain(t, res, "keyword-only", "not known")

	bad := goodIndex()
	bad.VectorProfiles = nil
	d = searchDeps(fakeSearch{index: bad, stats: searchprobe.Stats{DocumentCount: 5},
		status: searchprobe.IndexerStatus{Status: "running", LastResultStatus: "success", HasSuccess: true, LastSuccessMinutes: 500}}, hint)
	res, err = evalRule(t, "FND-RUN-006", d, baseInput())
	if err != nil || len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "not defined in vectorSearch.profiles") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// ---- NB10: metrics truncation through the real adapter -----------------------------------------------

func TestRun004TruncatedSeriesWithout429IsUncertainThroughTheRealAdapter(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"timespan":"x","value":[{"id":"m","type":"t","name":{"value":"m"},"unit":"Count","errorCode":"Success","timeseries":[`)
	for i := 0; i < 50; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"metadatavalues":[{"name":{"value":"ModelDeploymentName"},"value":"dep-%02d"},{"name":{"value":"StatusCode"},"value":"200"}],"data":[{"timeStamp":"a","total":5}]}`, i)
	}
	sb.WriteString(`]}]}`)
	routes := contractRoutes()
	routes["GET management.test"+acctPath+"/providers/Microsoft.Insights/metrics"] = sb.String()
	h := &hostRT{t: t, routes: routes}
	res, err := evalRule(t, "FND-RUN-004", contractDeps(t, h), baseInput())
	if err != nil {
		t.Fatal(err)
	}
	// The 429 series, if it exists, is beyond the 50th: the rule must not pass.
	assertUncertain(t, res, "maximum number of series")
}

// ---- NB10: a data-plane 400 (for example an unknown api-version) is a skip -----------------------------

func TestDataPlane400OnAnUnknownAPIVersionBecomesASkip(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"UnsupportedApiVersion","message":"The api-version is not supported"}}`))
	}))
	defer srv.Close()

	d := baseDeps()
	d.ProjectData = foundrydp.HTTPClient{HTTP: rewriteClient(srv), Credential: staticCred{}}
	d.SearchData = searchprobe.HTTPClient{HTTP: rewriteClient(srv), Credential: staticCred{}}
	for _, id := range []string{"FND-RUN-005", "FND-RUN-006"} {
		res, err := evalRule(t, id, d, baseInput())
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if len(res.Findings) != 0 || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) || !strings.Contains(res.Skipped.Reason, "400") {
			t.Errorf("%s: a 400 must be a skip, not a finding or a pass: %+v (skip %+v)", id, res, res.Skipped)
		}
	}
	if hits.Load() == 0 {
		t.Fatal("the server was never reached")
	}
}

// ---- internal helpers ---------------------------------------------------------------------------------------

func TestRbacBackedAndRequestRejectedReason(t *testing.T) {
	for cat, want := range map[string]bool{"CognitiveSearch": true, "CosmosDB": true, "AzureStorageAccount": true, "AzureBlob": true, "ADLSGen2": true, "AzureOpenAI": false, "": false} {
		if rbacBacked(cat) != want {
			t.Errorf("rbacBacked(%q) = %v", cat, !want)
		}
	}
	msg := azureReason(fmt.Errorf("%w: method \"POST\" is not allowed", rt.ErrRequestRejected))
	if !strings.HasPrefix(msg, rt.UncertainPrefix+"internal error") {
		t.Errorf("a refused request is a defect of the tool, not a missing capability: %q", msg)
	}
}

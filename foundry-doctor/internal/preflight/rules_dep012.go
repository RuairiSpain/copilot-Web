package preflight

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// regionMatrixDate is the Learn ms.date of the "Foundry Agent Service limits,
// quotas, and regional support" article the matrix below was taken from
// (rules/catalog/dep/FND-DEP-012.yaml, verified 2026-10-04).
var regionMatrixDate = time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)

// regionMatrixStaleAfter is a Foundry Doctor product decision (the catalogue
// leaves the threshold open): past it the documented matrix is not used.
const regionMatrixStaleAfter = 365 * 24 * time.Hour

// regionSupport is one row of the Learn "Supported regions" table.
type regionSupport struct{ responses, agents, voice, privateVNet bool }

// documentedRegions is the Learn Supported regions table, keyed by normLoc of
// the region display name. Every listed region supports Responses API, Agents
// and Private VNet; voice-based agents (preview) is "No" only where noted.
// It is documentation data, not an API: it can lag the service.
var documentedRegions = func() map[string]regionSupport {
	yes := regionSupport{true, true, true, true}
	noVoice := regionSupport{true, true, false, true}
	names := []string{
		"Australia East", "Brazil South", "Canada Central", "Canada East", "Central US", "East US", "East US 2",
		"France Central", "Germany West Central", "Italy North", "Japan East", "Japan West", "Korea Central",
		"North Central US", "Norway East", "South Africa North", "South Central US", "Southeast Asia",
		"South India", "Sweden Central", "Switzerland North", "Switzerland West", "UAE North", "UK South",
		"West Central US", "West Europe", "West US", "West US 3",
	}
	m := map[string]regionSupport{}
	for _, n := range names {
		m[normLoc(n)] = yes
	}
	for _, n := range []string{"Poland Central", "Spain Central"} {
		m[normLoc(n)] = noVoice
	}
	return m
}()

// relType strips the provider namespace from a template resource type: the
// Providers_Get resourceTypes[] entries are relative ("accounts/deployments").
func relType(typ string) string {
	if i := strings.Index(typ, "/"); i >= 0 {
		return strings.ToLower(typ[i+1:])
	}
	return strings.ToLower(typ)
}

// DEP-012: regional availability. (1) the target location is a subscription
// location; (2) the provider offers each template resource type there; (3) the
// dated Learn matrix, which can only produce an uncertain result.
func (e *env) dep012(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if len(rs) == 0 {
		return sdk.Result{}, nil
	}
	if e.d.Regions == nil {
		return skipRes("", missingCap("subscription locations and provider resource-type locations (Subscriptions_ListLocations, Providers_Get)", "Microsoft.Resources/subscriptions/locations/read and Microsoft.Resources/subscriptions/providers/read")), nil
	}
	if m := needTarget(false, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	target := e.d.Target.Location
	if target == "" || isExpr(target) {
		return skipRes("", sdk.SkipInputUnavailable+": target location is not set (use --location or AZURE_LOCATION)"), nil
	}
	var a acc

	// (1) Subscription locations.
	locs, err := e.d.Regions.ListLocations(ctx, e.sub())
	if err != nil {
		return sdk.Result{}, err
	}
	canon := map[string]string{} // normalised name or display name -> Location.name
	display := map[string]string{}
	for _, l := range locs {
		canon[normLoc(l.Name)] = l.Name
		if l.DisplayName != "" {
			canon[normLoc(l.DisplayName)] = l.Name
		}
		if l.RegionalDisplayName != "" {
			canon[normLoc(l.RegionalDisplayName)] = l.Name
		}
		display[strings.ToLower(l.Name)] = l.DisplayName
	}
	targetName, known := canon[normLoc(target)]
	if !known {
		a.add(idFinding("Microsoft.Resources/subscriptions/locations", e.subScope()+"/locations/"+normLoc(target),
			fmt.Sprintf("location %q is not in the subscription's location list", target)))
	}

	// (2) Provider resource-type locations.
	nsSet := map[string]string{}
	for _, r := range rs {
		if ns := namespaceOf(r.Type); ns != "" && !isExpr(r.Type) {
			nsSet[strings.ToLower(ns)] = ns
		}
	}
	var nss []string
	for _, v := range nsSet {
		nss = append(nss, v)
	}
	sort.Strings(nss)
	offered := map[string]map[string][]string{} // lower ns -> lower relative type -> locations
	for _, ns := range nss {
		pts, err := e.d.Regions.ProviderResourceTypes(ctx, e.sub(), ns)
		if err != nil {
			return sdk.Result{}, err
		}
		m := map[string][]string{}
		for _, p := range pts {
			m[strings.ToLower(p.ResourceType)] = p.Locations
		}
		offered[strings.ToLower(ns)] = m
	}
	for _, r := range rs {
		if isExpr(r.Type) {
			a.note("a resource type is not a literal value and was not checked for regional availability")
			continue
		}
		if strings.EqualFold(r.Type, "Microsoft.Authorization/roleAssignments") {
			continue
		}
		loc := e.regionOf(r)
		if loc == "" || isExpr(loc) {
			a.note("location of a %s resource is not a literal value", r.Type)
			continue
		}
		ls, found := offered[strings.ToLower(namespaceOf(r.Type))][relType(r.Type)]
		if !found {
			a.note("provider %s returned no entry for %s", namespaceOf(r.Type), r.Type)
			continue
		}
		if len(ls) == 0 {
			a.note("%s has no locations entry (global resource); regional availability is not checked", r.Type)
			continue
		}
		want := canon[normLoc(loc)]
		if want == "" {
			want = loc
		}
		hit := false
		for _, l := range ls {
			n := normLoc(l)
			if n == normLoc(want) || n == normLoc(display[strings.ToLower(want)]) || strings.EqualFold(l, "global") {
				hit = true
				break
			}
		}
		if !hit {
			a.add(tmplFinding(r, fmt.Sprintf("the provider does not offer %s in %s", r.Type, loc)))
		}
	}

	// (3) Documented Foundry Agent Service regional matrix: uncertain only.
	e.checkRegionMatrix(in, &a, rs, targetName, target)
	return a.result(), nil
}

func (e *env) checkRegionMatrix(in *sdk.Input, a *acc, rs []sdk.ARMResource, targetName, target string) {
	accts := byType(rs, typeAccount)
	if len(accts) == 0 {
		return
	}
	now := e.d.Now()
	staleAfter := time.Duration(policyInt(in, "preflight.regionMatrixStalenessDays", int(regionMatrixStaleAfter/(24*time.Hour)))) * 24 * time.Hour
	if now.Sub(regionMatrixDate) > staleAfter {
		a.skip(sdk.SkipInputUnavailable + ": capability documented Foundry regional support matrix (dated " + regionMatrixDate.Format("2006-01-02") + ") is older than the staleness threshold")
		return
	}
	key := normLoc(target)
	if targetName != "" {
		key = normLoc(targetName)
	}
	sup, ok := lookupRegion(key)
	date := regionMatrixDate.Format("2006-01-02")
	if !ok {
		a.note("region %s is not in the Foundry Agent Service supported-regions table (Learn, %s); the page can lag the service", target, date)
		return
	}
	if !sup.agents || !sup.responses {
		a.note("the Foundry Agent Service table (Learn, %s) does not list Agents and Responses API support in %s; the page can lag the service", date, target)
	}
	for _, r := range accts {
		if len(injections(r)) > 0 && !sup.privateVNet {
			a.note("the Foundry Agent Service table (Learn, %s) does not list Private VNet support in %s; the page can lag the service", date, target)
			break
		}
	}
}

// lookupRegion resolves a normalised Location.name ("eastus2") against the
// documented table, which is keyed by display name ("East US 2" -> "eastus2").
func lookupRegion(key string) (regionSupport, bool) {
	s, ok := documentedRegions[key]
	return s, ok
}

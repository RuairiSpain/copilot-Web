package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Context implements ContextProvider.
func (a *Adapter) Context(ctx context.Context, req ContextRequest) (Context, error) {
	if req.SubscriptionID == "" {
		return Context{}, &UnavailableError{Capability: "Microsoft.Resources/subscriptions/read", Reason: "no subscription id supplied (set AZURE_SUBSCRIPTION_ID or run `azd env set`)"}
	}
	if err := requireSub(req.SubscriptionID); err != nil {
		return Context{}, err
	}
	var sub struct {
		DisplayName string `json:"displayName"`
		State       string `json:"state"`
		TenantID    string `json:"tenantId"`
	}
	if err := a.c.get(ctx, subPath(req.SubscriptionID, ""), nil, &sub); err != nil {
		if isNotFound(err) {
			return Context{}, &UnavailableError{Capability: "Microsoft.Resources/subscriptions/read at " + subPath(req.SubscriptionID, ""), Reason: "subscription not found or not visible to this identity", Err: err}
		}
		return Context{}, err
	}
	out := Context{
		TenantID: req.TenantID, SubscriptionID: req.SubscriptionID, SubscriptionName: sub.DisplayName,
		SubscriptionState: sub.State, SubscriptionTenantID: sub.TenantID, Cloud: cloudOf(a.c.origin),
	}
	if cl, err := a.c.principal(ctx); err == nil {
		out.PrincipalObjectID, out.PrincipalType = cl.ObjectID, cl.principalType()
		if out.TenantID == "" {
			out.TenantID = cl.TenantID
		}
	}
	if out.TenantID == "" {
		out.TenantID = sub.TenantID
	}
	return out, nil
}

func cloudOf(origin string) string {
	switch {
	case strings.HasSuffix(origin, "//management.azure.com"):
		return "AzureCloud"
	case strings.HasSuffix(origin, "//management.usgovcloudapi.net"):
		return "AzureUSGovernment"
	case strings.HasSuffix(origin, "//management.chinacloudapi.cn"):
		return "AzureChinaCloud"
	}
	return ""
}

// ProviderStates implements ContextProvider; it only reads registration state.
func (a *Adapter) ProviderStates(ctx context.Context, subscriptionID string, namespaces []string) ([]ProviderState, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	for _, ns := range namespaces {
		if !segOnlyRe.MatchString(ns) {
			return nil, fmt.Errorf("%w: provider namespace", ErrInvalidInput)
		}
	}
	out := make([]ProviderState, len(namespaces))
	errs := make([]error, len(namespaces))
	var wg sync.WaitGroup
	for i, ns := range namespaces {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var p struct {
				RegistrationState string `json:"registrationState"`
			}
			err := a.c.get(ctx, subPath(subscriptionID, "/providers/"+ns), nil, &p)
			switch {
			case isNotFound(err):
				out[i] = ProviderState{Namespace: ns, State: "NotFound"}
			case err != nil:
				errs[i] = err
			default:
				out[i] = ProviderState{Namespace: ns, State: p.RegistrationState}
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

var resourceTypeRe = regexp.MustCompile(`^[A-Za-z0-9.]+(/[A-Za-z0-9.]+)*$`)

// ListResources implements Inventory using ARM list calls (source "arm-list").
// PropertyPaths other than "properties.provisioningState" are not available
// from the list API and are ignored.
func (a *Adapter) ListResources(ctx context.Context, q InventoryQuery) (ResourceList, error) {
	if q.Scope == "" {
		return ResourceList{}, fmt.Errorf("%w: scope", ErrInvalidInput)
	}
	vals := url.Values{"$expand": {"provisioningState"}}
	var terms []string
	for _, t := range q.Types {
		if !resourceTypeRe.MatchString(t) {
			return ResourceList{}, fmt.Errorf("%w: resource type", ErrInvalidInput)
		}
		terms = append(terms, "resourceType eq '"+t+"'")
	}
	if len(terms) > 0 {
		vals.Set("$filter", strings.Join(terms, " or "))
	}
	limit := q.MaxResults
	if limit <= 0 {
		limit = defaultResources
	}
	items, truncated, err := a.c.list(ctx, strings.TrimRight(q.Scope, "/")+"/resources", vals, limit)
	if err != nil {
		return ResourceList{}, err
	}
	wantProv := false
	for _, p := range q.PropertyPaths {
		if strings.EqualFold(p, "properties.provisioningState") {
			wantProv = true
		}
	}
	res := ResourceList{Truncated: truncated, Source: "arm-list", Resources: []Resource{}}
	for _, raw := range items {
		var r struct {
			ID, Name, Type, Location, Kind string
			SKU                            struct{ Name string }
			Tags                           map[string]string
			ProvisioningState              string `json:"provisioningState"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return ResourceList{}, fmt.Errorf("azure: decode resource: %w", err)
		}
		if len(q.Types) > 0 && !containsFold(q.Types, r.Type) {
			continue
		}
		out := Resource{ID: r.ID, Name: r.Name, Type: r.Type, Location: r.Location, ResourceGroup: resourceGroupOf(r.ID), Kind: r.Kind, SKUName: r.SKU.Name, Tags: r.Tags}
		if wantProv && r.ProvisioningState != "" {
			out.Properties = map[string]string{"properties.provisioningState": r.ProvisioningState}
		}
		res.Resources = append(res.Resources, out)
	}
	sort.Slice(res.Resources, func(i, j int) bool { return res.Resources[i].ID < res.Resources[j].ID })
	return res, nil
}

// ResourceGroup implements Inventory. A 404 is Exists=false, not an error.
func (a *Adapter) ResourceGroup(ctx context.Context, subscriptionID, name string) (ResourceGroupInfo, error) {
	if err := requireSub(subscriptionID); err != nil {
		return ResourceGroupInfo{}, err
	}
	if !segOnlyRe.MatchString(name) {
		return ResourceGroupInfo{}, fmt.Errorf("%w: resource group name", ErrInvalidInput)
	}
	var rg struct{ Name, Location string }
	err := a.c.get(ctx, subPath(subscriptionID, "/resourcegroups/"+name), nil, &rg)
	if isNotFound(err) {
		return ResourceGroupInfo{Name: name}, nil
	}
	if err != nil {
		return ResourceGroupInfo{}, err
	}
	return ResourceGroupInfo{Name: rg.Name, Location: rg.Location, Exists: true}, nil
}

// ListLocks implements Inventory.
func (a *Adapter) ListLocks(ctx context.Context, scope string) ([]Lock, error) {
	items, _, err := a.c.list(ctx, strings.TrimRight(scope, "/")+"/providers/Microsoft.Authorization/locks", nil, 0)
	if err != nil {
		return nil, err
	}
	out := []Lock{}
	for _, raw := range items {
		var l struct {
			ID, Name   string
			Properties struct{ Level string }
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("azure: decode lock: %w", err)
		}
		out = append(out, Lock{ID: l.ID, Name: l.Name, Scope: lockScope(l.ID), Level: l.Properties.Level})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func lockScope(id string) string {
	const marker = "/providers/Microsoft.Authorization/locks/"
	if i := strings.Index(strings.ToLower(id), strings.ToLower(marker)); i >= 0 {
		return id[:i]
	}
	return ""
}

// GetSubnet implements Inventory.
func (a *Adapter) GetSubnet(ctx context.Context, subnetID string) (Subnet, error) {
	var s struct {
		ID, Name   string
		Properties struct {
			AddressPrefix   string
			AddressPrefixes []string
			Delegations     []struct{ Properties struct{ ServiceName string } }
			NSG             struct{ ID string } `json:"networkSecurityGroup"`
			RouteTable      struct{ ID string }
			PrivateEPs      []struct{ ID string } `json:"privateEndpoints"`
		}
	}
	if err := a.c.get(ctx, subnetID, nil, &s); err != nil {
		return Subnet{}, err
	}
	p := s.Properties
	out := Subnet{ID: s.ID, Name: s.Name, NSGID: p.NSG.ID, RouteTableID: p.RouteTable.ID}
	if i := strings.LastIndex(strings.ToLower(s.ID), "/subnets/"); i >= 0 {
		out.VNetID = s.ID[:i]
	}
	prefixes := p.AddressPrefixes
	if p.AddressPrefix != "" {
		prefixes = append(prefixes, p.AddressPrefix)
	}
	out.AddressPrefixes = sortedUnique(prefixes)
	var del, pes []string
	for _, d := range p.Delegations {
		del = append(del, d.Properties.ServiceName)
	}
	for _, pe := range p.PrivateEPs {
		pes = append(pes, pe.ID)
	}
	out.DelegationServices, out.PrivateEndpointIDs = sortedUnique(del), sortedUnique(pes)
	return out, nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// Region availability (FND-DEP-012)

// ListLocations implements RegionAvailability (Subscriptions_ListLocations 2022-12-01).
func (a *Adapter) ListLocations(ctx context.Context, subscriptionID string) ([]LocationInfo, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	items, _, err := a.c.list(ctx, subPath(subscriptionID, "/locations"), nil, 0)
	if err != nil {
		return nil, err
	}
	out := []LocationInfo{}
	for _, raw := range items {
		var l struct {
			Name, DisplayName, RegionalDisplayName string
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("azure: decode location: %w", err)
		}
		out = append(out, LocationInfo{Name: l.Name, DisplayName: l.DisplayName, RegionalDisplayName: l.RegionalDisplayName})
	}
	sortBy(out, func(l LocationInfo) string { return l.Name })
	return out, nil
}

// ProviderResourceTypes implements RegionAvailability (Providers_Get 2025-04-01).
func (a *Adapter) ProviderResourceTypes(ctx context.Context, subscriptionID, namespace string) ([]ProviderResourceType, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(namespace) {
		return nil, fmt.Errorf("%w: provider namespace", ErrInvalidInput)
	}
	var p struct {
		ResourceTypes []struct {
			ResourceType string
			Locations    []string
		}
	}
	if err := a.c.get(ctx, subPath(subscriptionID, "/providers/"+namespace), nil, &p); err != nil {
		return nil, err
	}
	out := make([]ProviderResourceType, 0, len(p.ResourceTypes))
	for _, rt := range p.ResourceTypes {
		out = append(out, ProviderResourceType{ResourceType: rt.ResourceType, Locations: sortedUnique(rt.Locations)})
	}
	sortBy(out, func(r ProviderResourceType) string { return strings.ToLower(r.ResourceType) })
	return out, nil
}

// NormalizeLocation lowercases a location and removes spaces ("East US 2" -> "eastus2").
func NormalizeLocation(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
}

// LocationOffered reports whether loc appears in offered, comparing the
// normalised name and, when known, the normalised display name. Whether the
// providers API returns names or display names is unverified (FND-DEP-012),
// so both forms are accepted.
func LocationOffered(offered []string, loc LocationInfo) bool {
	keys := map[string]bool{}
	for _, k := range []string{loc.Name, loc.DisplayName, loc.RegionalDisplayName} {
		if k != "" {
			keys[NormalizeLocation(k)] = true
		}
	}
	for _, o := range offered {
		if keys[NormalizeLocation(o)] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Deployment history (FND-DEP-010)

// CountDeployments implements DeploymentHistory. Only the number of entries is
// retained; the counting stops after DeploymentHistoryLimit+1 entries
// (Truncated = true) to bound work on groups that already exceed the limit.
func (a *Adapter) CountDeployments(ctx context.Context, subscriptionID, resourceGroup string) (DeploymentCount, error) {
	if err := requireSub(subscriptionID); err != nil {
		return DeploymentCount{}, err
	}
	if !segOnlyRe.MatchString(resourceGroup) {
		return DeploymentCount{}, fmt.Errorf("%w: resource group", ErrInvalidInput)
	}
	items, truncated, err := a.c.list(ctx, subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.Resources/deployments"), nil, DeploymentHistoryLimit+1)
	if err != nil {
		return DeploymentCount{}, err
	}
	return DeploymentCount{Count: len(items), Truncated: truncated}, nil
}

// ---------------------------------------------------------------------------
// Subnet links (FND-DEP-011)

// SubnetLinks implements the SubnetLinks interface. subnetID must be a subnet resource ID.
func (a *Adapter) SubnetLinks(ctx context.Context, subnetID string) (SubnetLinkSet, error) {
	id := strings.TrimRight(subnetID, "/")
	if !strings.Contains(strings.ToLower(id), "/subnets/") {
		return SubnetLinkSet{}, fmt.Errorf("%w: subnet id", ErrInvalidInput)
	}
	read := func(suffix string) ([]NetworkLink, error) {
		items, _, err := a.c.list(ctx, id+"/"+suffix, nil, 0)
		if err != nil {
			return nil, err
		}
		out := []NetworkLink{}
		for _, raw := range items {
			var l struct {
				Name       string
				Properties struct {
					LinkedResourceType string `json:"linkedResourceType"`
					Link               string `json:"link"`
				}
			}
			if err := json.Unmarshal(raw, &l); err != nil {
				return nil, fmt.Errorf("azure: decode subnet link: %w", err)
			}
			out = append(out, NetworkLink{Name: l.Name, LinkedResourceType: l.Properties.LinkedResourceType, Link: l.Properties.Link})
		}
		sortBy(out, func(n NetworkLink) string { return n.Name })
		return out, nil
	}
	sal, err := read("ServiceAssociationLinks")
	if err != nil {
		return SubnetLinkSet{}, err
	}
	rnl, err := read("ResourceNavigationLinks")
	if err != nil {
		return SubnetLinkSet{}, err
	}
	return SubnetLinkSet{ServiceAssociationLinks: sal, ResourceNavigationLinks: rnl}, nil
}

// ---------------------------------------------------------------------------
// Permissions API evidence (FND-DEP-003)

// ReportedPermissions implements PermissionEvidence. The API exists for
// resource group and resource scopes only; a subscription scope yields an
// *UnavailableError so callers fall back to Permissions.EffectiveActions.
func (a *Adapter) ReportedPermissions(ctx context.Context, scope string) ([]PermissionSet, error) {
	scope = strings.TrimRight(scope, "/")
	parts := strings.Split(strings.Trim(scope, "/"), "/")
	if len(parts) < 2 || !strings.EqualFold(parts[0], "subscriptions") {
		return nil, fmt.Errorf("%w: scope", ErrInvalidInput)
	}
	if len(parts) == 2 {
		return nil, &UnavailableError{
			Capability: "Microsoft.Authorization/permissions/read at " + scope,
			Permission: "Microsoft.Authorization/permissions/read",
			Reason:     "the Permissions API has no subscription-scope variant; use Permissions.EffectiveActions",
		}
	}
	items, _, err := a.c.list(ctx, scope+"/providers/Microsoft.Authorization/permissions", url.Values{}, 0)
	if err != nil {
		return nil, err
	}
	out := make([]PermissionSet, 0, len(items))
	for _, raw := range items {
		var p PermissionSet
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("azure: decode permissions: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

// CheckReportedActions implements PermissionEvidence. An action is listed when
// one entry's actions match it and that same entry's notActions do not.
func (a *Adapter) CheckReportedActions(ctx context.Context, req PermissionRequest) ([]ActionDecision, error) {
	if len(req.Actions) == 0 {
		return []ActionDecision{}, nil
	}
	sets, err := a.ReportedPermissions(ctx, req.Scope)
	if err != nil {
		return nil, err
	}
	out := make([]ActionDecision, 0, len(req.Actions))
	for _, act := range req.Actions {
		d := ActionDecision{Action: act, Decision: DecisionDenied, Reason: ReasonNoGrantingRole}
		for _, s := range sets {
			if (rolePermission(s)).grants(act, req.IsDataAction) {
				d.Decision, d.Reason = DecisionAllowed, ReasonReportedNotProof
				break
			}
		}
		out = append(out, d)
	}
	return out, nil
}

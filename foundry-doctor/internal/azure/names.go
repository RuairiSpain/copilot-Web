package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// CheckName implements Names using the ADR-006 allow-listed name-availability POSTs.
func (a *Adapter) CheckName(ctx context.Context, c NameCheck) (NameResult, error) {
	if !nameRe.MatchString(c.Name) {
		return NameResult{}, fmt.Errorf("%w: resource name must match [A-Za-z0-9._-]{1,64}", ErrInvalidInput)
	}
	if err := requireSub(c.SubscriptionID); err != nil {
		return NameResult{}, err
	}
	var path string
	var body map[string]any
	switch c.Kind {
	case NameKeyVault:
		path, body = subPath(c.SubscriptionID, "/providers/Microsoft.KeyVault/checkNameAvailability"), map[string]any{"name": c.Name, "type": "Microsoft.KeyVault/vaults"}
	case NameStorage:
		path, body = subPath(c.SubscriptionID, "/providers/Microsoft.Storage/checkNameAvailability"), map[string]any{"name": c.Name, "type": "Microsoft.Storage/storageAccounts"}
	case NameACR:
		path, body = subPath(c.SubscriptionID, "/providers/Microsoft.ContainerRegistry/checkNameAvailability"), map[string]any{"name": c.Name, "type": "Microsoft.ContainerRegistry/registries"}
	case NameAPIM:
		path, body = subPath(c.SubscriptionID, "/providers/Microsoft.ApiManagement/checkNameAvailability"), map[string]any{"name": c.Name}
	case NameSearch:
		path, body = subPath(c.SubscriptionID, "/providers/Microsoft.Search/checkNameAvailability"), map[string]any{"name": c.Name, "type": "searchServices"}
	case NameFoundry:
		// FND-DEP-007: subscription-level, no location segment; kind is the account kind.
		path = subPath(c.SubscriptionID, "/providers/Microsoft.CognitiveServices/checkDomainAvailability")
		body = map[string]any{"subdomainName": c.Name, "type": "Microsoft.CognitiveServices/accounts", "kind": "AIServices"}
	default:
		return NameResult{}, fmt.Errorf("%w: name kind %q", ErrInvalidInput, c.Kind)
	}
	r, err := a.c.do(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return NameResult{}, err
	}
	var v struct {
		NameAvailable        *bool  `json:"nameAvailable"`
		IsNameAvailable      *bool  `json:"isNameAvailable"`
		IsSubdomainAvailable *bool  `json:"isSubdomainAvailable"`
		Reason               string `json:"reason"`
	}
	if err := json.Unmarshal(r.body, &v); err != nil {
		return NameResult{}, fmt.Errorf("azure: decode name availability: %w", err)
	}
	var avail *bool
	for _, p := range []*bool{v.NameAvailable, v.IsNameAvailable, v.IsSubdomainAvailable} {
		if p != nil {
			avail = p
			break
		}
	}
	if avail == nil {
		return NameResult{}, fmt.Errorf("azure: name availability response had no availability field")
	}
	res := NameResult{Available: *avail}
	if !*avail && safeCode.MatchString(v.Reason) {
		res.Reason = v.Reason
	}
	return res, nil
}

// ListSoftDeleted implements Names. Storage and Search have no soft-deleted
// name holders, so those kinds return an empty list.
func (a *Adapter) ListSoftDeleted(ctx context.Context, kind NameKind, subscriptionID string) ([]SoftDeleted, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	var path string
	switch kind {
	case NameKeyVault:
		path = "/providers/Microsoft.KeyVault/deletedVaults"
	case NameAPIM:
		path = "/providers/Microsoft.ApiManagement/deletedservices"
	case NameFoundry:
		path = "/providers/Microsoft.CognitiveServices/deletedAccounts"
	case NameStorage, NameACR, NameSearch:
		return []SoftDeleted{}, nil
	default:
		return nil, fmt.Errorf("%w: name kind %q", ErrInvalidInput, kind)
	}
	items, _, err := a.c.list(ctx, subPath(subscriptionID, path), nil, 0)
	if err != nil {
		return nil, err
	}
	out := []SoftDeleted{}
	for _, raw := range items {
		var d struct {
			ID, Name, Location string
			Properties         struct {
				Location               string
				DeletionDate           string
				ScheduledPurgeDate     string
				PurgeProtectionEnabled bool
			}
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("azure: decode soft-deleted resource: %w", err)
		}
		loc := d.Location
		if loc == "" {
			loc = d.Properties.Location
		}
		out = append(out, SoftDeleted{Kind: kind, Name: d.Name, ID: d.ID, Location: loc, DeletionDate: d.Properties.DeletionDate,
			ScheduledPurgeDate: d.Properties.ScheduledPurgeDate, PurgeProtection: d.Properties.PurgeProtectionEnabled})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

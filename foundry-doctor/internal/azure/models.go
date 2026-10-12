package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ListModels implements Models (FND-DEP-004: Microsoft.CognitiveServices locations/models).
func (a *Adapter) ListModels(ctx context.Context, q ModelQuery) ([]ModelAvailability, error) {
	if err := requireSub(q.SubscriptionID); err != nil {
		return nil, err
	}
	if err := requireLocation(q.Location); err != nil {
		return nil, err
	}
	items, _, err := a.c.list(ctx, subPath(q.SubscriptionID, "/providers/Microsoft.CognitiveServices/locations/"+q.Location+"/models"), nil, 0)
	if err != nil {
		return nil, err
	}
	out := []ModelAvailability{}
	for _, raw := range items {
		var m struct {
			Model struct {
				Format          string
				Name            string
				Version         string
				LifecycleStatus string
				Deprecation     struct{ Inference string }
				SKUs            []struct {
					Name            string
					DeprecationDate string
					UsageName       string
					Capacity        struct{ Default, Minimum, Maximum int32 }
				} `json:"skus"`
			}
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("azure: decode model: %w", err)
		}
		md := m.Model
		if (q.Format != "" && !strings.EqualFold(q.Format, md.Format)) || (q.Name != "" && !strings.EqualFold(q.Name, md.Name)) {
			continue
		}
		av := ModelAvailability{Format: md.Format, Name: md.Name, Version: md.Version, LifecycleStatus: md.LifecycleStatus, DeprecationDate: md.Deprecation.Inference}
		var dates []string
		for _, s := range md.SKUs {
			av.SKUs = append(av.SKUs, ModelSKU{Name: s.Name, DefaultCapacity: s.Capacity.Default, MinCapacity: s.Capacity.Minimum, MaxCapacity: s.Capacity.Maximum, UsageName: s.UsageName})
			if s.DeprecationDate != "" {
				dates = append(dates, s.DeprecationDate)
			}
		}
		sort.Slice(av.SKUs, func(i, j int) bool { return av.SKUs[i].Name < av.SKUs[j].Name })
		if av.DeprecationDate == "" && len(dates) > 0 {
			sort.Strings(dates)
			av.DeprecationDate = dates[0]
		}
		out = append(out, av)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Format != b.Format {
			return a.Format < b.Format
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Version < b.Version
	})
	return out, nil
}

// ListUsages implements Models (FND-DEP-005: locations/usages).
func (a *Adapter) ListUsages(ctx context.Context, subscriptionID, location string) ([]QuotaUsage, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if err := requireLocation(location); err != nil {
		return nil, err
	}
	items, _, err := a.c.list(ctx, subPath(subscriptionID, "/providers/Microsoft.CognitiveServices/locations/"+location+"/usages"), nil, 0)
	if err != nil {
		return nil, err
	}
	out := []QuotaUsage{}
	for _, raw := range items {
		var u struct {
			Name         struct{ Value string }
			CurrentValue float64
			Limit        float64
			Unit         string
		}
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, fmt.Errorf("azure: decode usage: %w", err)
		}
		out = append(out, QuotaUsage{Name: u.Name.Value, Current: u.CurrentValue, Limit: u.Limit, Unit: u.Unit})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

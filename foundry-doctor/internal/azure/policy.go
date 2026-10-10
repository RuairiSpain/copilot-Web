package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ListAssignments implements Policy. Parameter values are not decoded.
func (a *Adapter) ListAssignments(ctx context.Context, scope string) ([]PolicyAssignment, error) {
	items, _, err := a.c.list(ctx, strings.TrimRight(scope, "/")+"/providers/Microsoft.Authorization/policyAssignments", nil, 0)
	if err != nil {
		return nil, err
	}
	out := []PolicyAssignment{}
	for _, raw := range items {
		var p struct {
			ID, Name   string
			Properties struct {
				Scope              string
				PolicyDefinitionID string `json:"policyDefinitionId"`
				DisplayName        string
				EnforcementMode    string
				NotScopes          []string
			}
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("azure: decode policy assignment: %w", err)
		}
		pr := p.Properties
		out = append(out, PolicyAssignment{ID: p.ID, Name: p.Name, Scope: pr.Scope, DefinitionID: pr.PolicyDefinitionID,
			DisplayName: pr.DisplayName, EnforcementMode: pr.EnforcementMode, NotScopes: sortedUnique(pr.NotScopes)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListExemptions implements Policy.
func (a *Adapter) ListExemptions(ctx context.Context, scope string) ([]PolicyExemption, error) {
	items, _, err := a.c.list(ctx, strings.TrimRight(scope, "/")+"/providers/Microsoft.Authorization/policyExemptions", nil, 0)
	if err != nil {
		return nil, err
	}
	out := []PolicyExemption{}
	for _, raw := range items {
		var p struct {
			ID, Name   string
			Properties struct {
				PolicyAssignmentID string `json:"policyAssignmentId"`
				ExemptionCategory  string
				ExpiresOn          string
			}
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("azure: decode policy exemption: %w", err)
		}
		out = append(out, PolicyExemption{ID: p.ID, Name: p.Name, Scope: exemptionScope(p.ID), AssignmentID: p.Properties.PolicyAssignmentID,
			Category: p.Properties.ExemptionCategory, ExpiresOn: p.Properties.ExpiresOn})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func exemptionScope(id string) string {
	const marker = "/providers/microsoft.authorization/policyexemptions/"
	if i := strings.Index(strings.ToLower(id), marker); i >= 0 {
		return id[:i]
	}
	return ""
}

const maxMessage = 200

// CheckRestrictions implements Policy via the checkPolicyRestrictions POST
// (a read-only evaluation). Whether exemptions are honoured is unverified, so
// results are labelled potential-conflict unless the service reported a deny.
func (a *Adapter) CheckRestrictions(ctx context.Context, req PolicyRestrictionRequest) ([]PolicyRestriction, error) {
	if req.Scope == "" || req.ResourceType == "" || req.APIVersion == "" {
		return nil, fmt.Errorf("%w: scope, resource type and api version are required", ErrInvalidInput)
	}
	content := map[string]any{}
	for k, v := range req.Content {
		content[k] = v
	}
	content["type"] = req.ResourceType
	if req.Location != "" {
		content["location"] = req.Location
	}
	details := map[string]any{"resourceContent": content, "apiVersion": req.APIVersion}
	if req.ResourceScope != "" {
		details["scope"] = req.ResourceScope
	}
	body := map[string]any{
		"resourceDetails":    details,
		"pendingFields":      []any{},
		"includeAuditEffect": req.IncludeAudit,
	}
	r, err := a.c.do(ctx, http.MethodPost, strings.TrimRight(req.Scope, "/")+"/providers/Microsoft.PolicyInsights/checkPolicyRestrictions", nil, body)
	if err != nil {
		return nil, err
	}
	type policyRef struct {
		PolicyAssignmentID string `json:"policyAssignmentId"`
	}
	var resp struct {
		FieldRestrictions []struct {
			Field        string
			Restrictions []struct {
				Result       string
				Policy       policyRef
				PolicyEffect string `json:"policyEffect"`
				Reason       string
			}
		}
		ContentEvaluationResult struct {
			PolicyEvaluations []struct {
				PolicyInfo       policyRef
				EvaluationResult string
				EffectDetails    struct {
					PolicyEffect string `json:"policyEffect"`
				}
			}
		}
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return nil, fmt.Errorf("azure: decode checkPolicyRestrictions: %w", err)
	}
	// Honest mapping (FND-DEP-009): only an explicit deny effect is a likely
	// denial; audit/modify/append and unknown effects are potential conflicts.
	effects := map[string]string{} // assignment id -> effect reported by field restrictions
	out := []PolicyRestriction{}
	for _, f := range resp.FieldRestrictions {
		for _, rs := range f.Restrictions {
			if rs.PolicyEffect != "" {
				effects[strings.ToLower(rs.Policy.PolicyAssignmentID)] = rs.PolicyEffect
			}
			out = append(out, PolicyRestriction{Kind: kindForEffect(rs.PolicyEffect), AssignmentID: rs.Policy.PolicyAssignmentID,
				Effect: rs.PolicyEffect, Result: rs.Result, Field: f.Field, Message: truncate(rs.Reason, maxMessage)})
		}
	}
	for _, e := range resp.ContentEvaluationResult.PolicyEvaluations {
		if !strings.EqualFold(e.EvaluationResult, "NonCompliant") {
			continue
		}
		eff := e.EffectDetails.PolicyEffect
		if eff == "" {
			eff = effects[strings.ToLower(e.PolicyInfo.PolicyAssignmentID)]
		}
		out = append(out, PolicyRestriction{Kind: kindForEffect(eff), AssignmentID: e.PolicyInfo.PolicyAssignmentID, Effect: eff})
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		if x.AssignmentID != y.AssignmentID {
			return x.AssignmentID < y.AssignmentID
		}
		return x.Field < y.Field
	})
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func kindForEffect(effect string) string {
	if strings.EqualFold(effect, "deny") {
		return RestrictionObservedDenial
	}
	return RestrictionPotentialConflict
}

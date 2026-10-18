package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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

const maxMessage = 200

// secretKeyRe matches property names that can carry a secret. It is a
// deliberately wide heuristic: a dropped property only makes the policy
// check less complete (the caller reports that), never wrong.
var secretKeyRe = regexp.MustCompile(`(?i)(secret|passw|pwd|token|credential|connectionstring|accesskey|accountkey|apikey|api_key|primarykey|secondarykey|sharedkey|privatekey|sastoken|sasurl|certificate|authorizationkey)`)

// secretValueRe matches string values that look like a credential whatever the key.
var secretValueRe = regexp.MustCompile(`(?i)(accountkey=|sharedaccesskey=|sharedaccesssignature=|[?&]sig=[A-Za-z0-9%+/=]{16,}|-----BEGIN [A-Z ]*PRIVATE KEY|^eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.)`)

// isARMExpression reports whether s is an unresolved ARM template expression
// ("[...]"; "[[" is the escape for a literal bracket).
func isARMExpression(s string) bool {
	return strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "[[")
}

// SanitizePolicyContent returns a copy of the planned resource content that is
// safe to send to checkPolicyRestrictions (ADR-006 point 8: non-secret template
// content only). It omits, and names in dropped (dotted paths, never values):
//
//   - any property whose value is or contains an unresolved ARM expression
//     (the policy service would treat "[parameters('x')]" as a literal value
//     and could report a false restriction or a false clean result);
//   - any property whose key looks like a secret holder;
//   - any string value shaped like a credential.
//
// An array that contains a dropped element is dropped whole, because element
// positions matter to policy aliases. The result is deterministic.
func SanitizePolicyContent(in map[string]any) (clean map[string]any, dropped []string) {
	clean, _ = sanitizeMap(in, "", &dropped)
	if clean == nil {
		clean = map[string]any{}
	}
	sort.Strings(dropped)
	return clean, sortedUnique(dropped)
}

func sanitizeMap(m map[string]any, path string, dropped *[]string) (map[string]any, bool) {
	out := make(map[string]any, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := k
		if path != "" {
			p = path + "." + k
		}
		if secretKeyRe.MatchString(k) {
			*dropped = append(*dropped, p)
			continue
		}
		v, ok := sanitizeValue(m[k], p, dropped)
		if !ok {
			*dropped = append(*dropped, p)
			continue
		}
		out[k] = v
	}
	return out, true
}

func sanitizeValue(v any, path string, dropped *[]string) (any, bool) {
	switch x := v.(type) {
	case string:
		if isARMExpression(x) || secretValueRe.MatchString(x) {
			return nil, false
		}
		return x, true
	case map[string]any:
		return sanitizeMap(x, path, dropped)
	case []any:
		out := make([]any, 0, len(x))
		mark := len(*dropped)
		for i, e := range x {
			ev, ok := sanitizeValue(e, fmt.Sprintf("%s[%d]", path, i), dropped)
			if !ok || len(*dropped) != mark {
				// Anything removed inside an element changes the array's shape:
				// drop the whole array and report only its own path.
				*dropped = (*dropped)[:mark]
				return nil, false
			}
			out = append(out, ev)
		}
		return out, true
	default:
		return v, true
	}
}

// CheckRestrictions implements Policy via the checkPolicyRestrictions POST
// (PolicyInsights 2024-10-01, a read-only evaluation). The request content is
// sanitised again here (SanitizePolicyContent) so no caller can send an
// unresolved expression or a secret-shaped value. Whether exemptions are
// honoured is unverified (ADR-006), so a result is only a likely denial when
// the service reports a deny.
func (a *Adapter) CheckRestrictions(ctx context.Context, req PolicyRestrictionRequest) ([]PolicyRestriction, error) {
	if req.Scope == "" || req.ResourceType == "" || req.APIVersion == "" {
		return nil, fmt.Errorf("%w: scope, resource type and api version are required", ErrInvalidInput)
	}
	content, _ := SanitizePolicyContent(req.Content)
	content["type"] = req.ResourceType
	if req.Location != "" && !isARMExpression(req.Location) {
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
	// Member names verified against PolicyInsights/stable/2024-10-01/openapi.json:
	// CheckRestrictionsResult.fieldRestrictions[].{field, restrictions[]
	// {result (Required|Removed|Deny|Audit), policy.policyAssignmentId,
	// policyEffect, reason}} and contentEvaluationResult.policyEvaluations[]
	// {policyInfo.policyAssignmentId, evaluationResult, effectDetails.policyEffect}.
	type policyRef struct {
		PolicyAssignmentID string `json:"policyAssignmentId"`
	}
	var resp struct {
		FieldRestrictions []struct {
			Field        string `json:"field"`
			Restrictions []struct {
				Result       string    `json:"result"`
				Policy       policyRef `json:"policy"`
				PolicyEffect string    `json:"policyEffect"`
				Reason       string    `json:"reason"`
			} `json:"restrictions"`
		} `json:"fieldRestrictions"`
		ContentEvaluationResult struct {
			PolicyEvaluations []struct {
				PolicyInfo       policyRef `json:"policyInfo"`
				EvaluationResult string    `json:"evaluationResult"`
				EffectDetails    struct {
					PolicyEffect string `json:"policyEffect"`
				} `json:"effectDetails"`
			} `json:"policyEvaluations"`
		} `json:"contentEvaluationResult"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return nil, fmt.Errorf("azure: decode checkPolicyRestrictions: %w", err)
	}
	// Honest mapping (FND-DEP-009): only an explicit deny is a likely denial;
	// audit, modify, append and unknown effects are potential conflicts.
	effects := map[string]string{} // assignment id -> effect reported by field restrictions
	out := []PolicyRestriction{}
	for _, f := range resp.FieldRestrictions {
		for _, rs := range f.Restrictions {
			eff := rs.PolicyEffect
			if eff == "" && strings.EqualFold(rs.Result, "Deny") {
				// FieldRestrictionResult "Deny": the field and/or values will be
				// denied by policy; the service named no effect string.
				eff = "deny"
			}
			if eff != "" {
				effects[strings.ToLower(rs.Policy.PolicyAssignmentID)] = eff
			}
			out = append(out, PolicyRestriction{Kind: kindForEffect(eff), AssignmentID: rs.Policy.PolicyAssignmentID,
				Effect: eff, Result: rs.Result, Field: f.Field, Message: truncate(rs.Reason, maxMessage)})
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

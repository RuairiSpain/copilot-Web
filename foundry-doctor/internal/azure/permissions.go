package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

const everyonePrincipal = "00000000-0000-0000-0000-000000000000"

type rolePermission struct {
	Actions        []string
	NotActions     []string
	DataActions    []string
	NotDataActions []string
}

func (p rolePermission) grants(action string, data bool) bool {
	allow, deny := p.Actions, p.NotActions
	if data {
		allow, deny = p.DataActions, p.NotDataActions
	}
	return anyMatch(allow, action) && !anyMatch(deny, action)
}

func anyMatch(patterns []string, action string) bool {
	for _, p := range patterns {
		if actionMatch(p, action) {
			return true
		}
	}
	return false
}

// actionMatch implements RBAC wildcard matching (case-insensitive, * only).
func actionMatch(pattern, action string) bool {
	re := "(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	ok, err := regexp.MatchString(re, action)
	return err == nil && ok
}

func scopeCovers(parent, child string) bool {
	p, c := strings.ToLower(strings.TrimRight(parent, "/")), strings.ToLower(strings.TrimRight(child, "/"))
	return p == "" || c == p || strings.HasPrefix(c, p+"/")
}

// EffectiveActions implements Permissions by reading role assignments, role
// definitions and deny assignments. ABAC conditions are not evaluated: a grant
// that carries a condition yields DecisionUnknown with ConditionSkipped.
func (a *Adapter) EffectiveActions(ctx context.Context, req PermissionRequest) ([]ActionDecision, error) {
	if req.Scope == "" {
		return nil, fmt.Errorf("%w: scope", ErrInvalidInput)
	}
	if len(req.Actions) == 0 {
		return []ActionDecision{}, nil
	}
	scope := strings.TrimRight(req.Scope, "/")
	cl, err := a.c.principal(ctx)
	if err != nil {
		return nil, err
	}
	if !subIDRe.MatchString(cl.ObjectID) {
		return nil, &UnavailableError{Capability: "caller identity", Reason: "token has no object id claim"}
	}
	oid := cl.ObjectID

	items, _, err := a.c.list(ctx, scope+"/providers/Microsoft.Authorization/roleAssignments",
		url.Values{"$filter": {"assignedTo('" + oid + "')"}}, 0)
	if err != nil {
		return nil, err // *UnavailableError when Reader-less / forbidden
	}
	type assignment struct{ roleDef, condition string }
	var assigns []assignment
	for _, raw := range items {
		var r struct {
			Properties struct {
				RoleDefinitionID string `json:"roleDefinitionId"`
				Scope            string
				Condition        string
			}
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("azure: decode role assignment: %w", err)
		}
		if scopeCovers(r.Properties.Scope, scope) {
			assigns = append(assigns, assignment{r.Properties.RoleDefinitionID, strings.TrimSpace(r.Properties.Condition)})
		}
	}

	// Role definitions, fetched in parallel (bounded by the client semaphore).
	defIDs := map[string]bool{}
	for _, as := range assigns {
		defIDs[as.roleDef] = true
	}
	defs := map[string][]rolePermission{}
	unreadable := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var fatal error
	for id := range defIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var d struct {
				Properties struct{ Permissions []rolePermission }
			}
			err := a.c.get(ctx, id, nil, &d)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				defs[id] = d.Properties.Permissions
			case ctx.Err() != nil:
				fatal = ctx.Err()
			default:
				unreadable[id] = true
			}
		}()
	}
	wg.Wait()
	if fatal != nil {
		return nil, fatal
	}

	// Deny assignments.
	type deny struct {
		id    string
		perms []rolePermission
	}
	var denies []deny
	denyUnreadable := false
	ditems, _, derr := a.c.list(ctx, scope+"/providers/Microsoft.Authorization/denyAssignments",
		url.Values{"$filter": {"atScope()"}}, 0)
	if derr != nil {
		if _, ok := AsUnavailable(derr); !ok {
			return nil, derr
		}
		denyUnreadable = true
	}
	for _, raw := range ditems {
		var d struct {
			ID         string
			Properties struct {
				Permissions       []rolePermission
				Principals        []struct{ ID string }
				ExcludePrincipals []struct{ ID string }
			}
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("azure: decode deny assignment: %w", err)
		}
		applies := false
		for _, p := range d.Properties.Principals {
			if strings.EqualFold(p.ID, oid) || p.ID == everyonePrincipal {
				applies = true
			}
		}
		for _, p := range d.Properties.ExcludePrincipals {
			if strings.EqualFold(p.ID, oid) {
				applies = false
			}
		}
		if applies {
			denies = append(denies, deny{d.ID, d.Properties.Permissions})
		}
	}

	out := make([]ActionDecision, 0, len(req.Actions))
	for _, action := range req.Actions {
		dec := ActionDecision{Action: action}
		var denyIDs []string
		for _, d := range denies {
			for _, p := range d.perms {
				if p.grants(action, req.IsDataAction) {
					denyIDs = append(denyIDs, d.id)
					break
				}
			}
		}
		granted, conditional, roleUnreadable := false, false, false
		for _, as := range assigns {
			if unreadable[as.roleDef] {
				roleUnreadable = true
				continue
			}
			for _, p := range defs[as.roleDef] {
				if p.grants(action, req.IsDataAction) {
					if as.condition != "" {
						conditional = true
					} else {
						granted = true
					}
					break
				}
			}
		}
		switch {
		case len(denyIDs) > 0:
			dec.Decision, dec.Reason, dec.DenyAssignmentIDs = DecisionDenied, ReasonDenyAssignment, sortedUnique(denyIDs)
		case granted && denyUnreadable:
			dec.Decision, dec.Reason = DecisionUnknown, ReasonDenyAssignmentsUnreadable
		case granted:
			dec.Decision = DecisionAllowed
		case conditional:
			dec.Decision, dec.Reason, dec.ConditionSkipped = DecisionUnknown, ReasonConditionNotEvaluated, true
		case roleUnreadable:
			dec.Decision, dec.Reason = DecisionUnknown, ReasonAssignmentsUnreadable
		default:
			dec.Decision, dec.Reason = DecisionDenied, ReasonNoGrantingRole
		}
		out = append(out, dec)
	}
	return out, nil
}

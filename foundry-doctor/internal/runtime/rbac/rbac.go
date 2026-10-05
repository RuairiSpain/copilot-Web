// Package rbac evaluates the effective access evidence used by the Phase 3
// runtime rules. It works only with metadata: principal ids, role definition
// ids and target resource ids.
package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

const (
	SearchServiceContributor   = "search-service-contributor"
	SearchIndexDataContributor = "search-index-data-contributor"
	StorageBlobDataContributor = "storage-blob-data-contributor"
	StorageBlobDataOwner       = "storage-blob-data-owner"
	StorageAccountContributor  = "storage-account-contributor"
	CosmosDBOperator           = "cosmos-db-operator"
	CosmosDBDataContributor    = "cosmos-db-built-in-data-contributor"
)

var roleGUIDSuffix = map[string]string{
	SearchServiceContributor:   "7ca78c08-252a-4471-8644-bb5ff32d4ba0",
	SearchIndexDataContributor: "8ebe5a00-799e-43f5-93ac-243d3dce84a7",
	StorageBlobDataContributor: "ba92f5b4-2d11-453d-a403-e96b0029c9fe",
	StorageBlobDataOwner:       "b7e6dc6d-f1e8-4753-8033-0f276bb0955b",
	StorageAccountContributor:  "17d1049b-9a84-46fb-8f53-869881c3d3ab",
	CosmosDBOperator:           "230815da-be43-4aae-9cb4-875f7bd000aa",
}

// Assignment is the metadata-only role assignment evidence.
type Assignment struct {
	RoleDefinitionID string
	Scope            string
	Condition        string
}

// SQLAssignment is a Cosmos DB SQL role assignment.
type SQLAssignment struct {
	RoleDefinitionID string
	Scope            string
}

// Reader exposes the read-only metadata needed for runtime RBAC checks.
type Reader interface {
	ListAssignments(context.Context, string, string) ([]Assignment, error)
	ListSQLAssignments(context.Context, string, string) ([]SQLAssignment, error)
}

// Evaluation is the result for one expected role family at one target.
type Evaluation struct {
	Identity   string
	TargetID   string
	RoleFamily string
	Allowed    bool
	Uncertain  bool
	Reason     string
}

// Check verifies that principalID holds each expected role family at scope.
func Check(ctx context.Context, r Reader, principalID, scope string, expected ...string) ([]Evaluation, error) {
	assignments, err := r.ListAssignments(ctx, scope, principalID)
	if err != nil {
		return nil, err
	}
	out := make([]Evaluation, 0, len(expected))
	for _, family := range expected {
		ev := Evaluation{Identity: principalID, TargetID: scope, RoleFamily: family}
		for _, a := range assignments {
			if a.Condition != "" {
				ev.Uncertain = true
				ev.Reason = "conditional assignment cannot be evaluated"
				continue
			}
			if suffix := roleGUIDSuffix[family]; suffix != "" && strings.HasSuffix(strings.ToLower(a.RoleDefinitionID), strings.ToLower(suffix)) {
				ev.Allowed = true
				ev.Uncertain = false
				ev.Reason = ""
				break
			}
		}
		if !ev.Allowed && !ev.Uncertain {
			ev.Reason = "missing expected role family"
		}
		out = append(out, ev)
	}
	return out, nil
}

// CheckCosmos verifies the required SQL role assignment and optional operator role.
func CheckCosmos(ctx context.Context, r Reader, principalID, accountID string, requireOperator bool) ([]Evaluation, error) {
	out := []Evaluation{}
	if requireOperator {
		roles, err := Check(ctx, r, principalID, accountID, CosmosDBOperator)
		if err != nil {
			return nil, err
		}
		out = append(out, roles...)
	}
	sqlAssignments, err := r.ListSQLAssignments(ctx, accountID, principalID)
	if err != nil {
		return nil, err
	}
	ev := Evaluation{Identity: principalID, TargetID: accountID, RoleFamily: CosmosDBDataContributor}
	for _, a := range sqlAssignments {
		if strings.Contains(strings.ToLower(a.Scope), "/dbs/enterprise_memory") || strings.EqualFold(strings.TrimRight(a.Scope, "/"), "/") {
			ev.Allowed = true
			break
		}
	}
	if !ev.Allowed {
		ev.Reason = "missing Cosmos DB Built-in Data Contributor SQL role assignment"
	}
	out = append(out, ev)
	return out, nil
}

// ReaderAdapter adapts the Azure runtime interfaces to the Reader contract.
type ReaderAdapter struct {
	Roles azure.RuntimeRBAC
}

func (a ReaderAdapter) ListAssignments(ctx context.Context, scope, principalID string) ([]Assignment, error) {
	got, err := a.Roles.ListRoleAssignments(ctx, scope, principalID)
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, 0, len(got))
	for _, g := range got {
		out = append(out, Assignment{RoleDefinitionID: g.RoleDefinitionID, Scope: g.Scope, Condition: g.Condition})
	}
	return out, nil
}

func (a ReaderAdapter) ListSQLAssignments(ctx context.Context, accountID, principalID string) ([]SQLAssignment, error) {
	got, err := a.Roles.ListCosmosSQLRoleAssignments(ctx, accountID, principalID)
	if err != nil {
		return nil, err
	}
	out := make([]SQLAssignment, 0, len(got))
	for _, g := range got {
		out = append(out, SQLAssignment{RoleDefinitionID: g.RoleDefinitionID, Scope: g.Scope})
	}
	return out, nil
}

// MissingMessage renders a deterministic finding message.
func MissingMessage(ev Evaluation) string {
	return fmt.Sprintf("identity %s lacks %s on %s", ev.Identity, ev.RoleFamily, ev.TargetID)
}

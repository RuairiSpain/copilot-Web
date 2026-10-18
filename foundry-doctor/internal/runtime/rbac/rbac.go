// Package rbac evaluates the effective access evidence used by the Phase 3
// runtime rules. It works only with metadata: principal ids, role definition
// ids and target resource ids.
//
// Limits (stated in every missing-role message): assignments made to groups
// the identity belongs to, deny assignments, PIM eligibility and ABAC
// conditions are not evaluated. A missing role is therefore "likely", never
// certain, and anything the evidence cannot settle is uncertain.
package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// Role family keys.
const (
	SearchServiceContributor   = "search-service-contributor"
	SearchIndexDataContributor = "search-index-data-contributor"
	StorageBlobDataContributor = "storage-blob-data-contributor"
	StorageBlobDataOwner       = "storage-blob-data-owner"
	StorageAccountContributor  = "storage-account-contributor"
	CosmosDBOperator           = "cosmos-db-operator"
	CosmosDBDataContributor    = "cosmos-db-built-in-data-contributor"
)

type roleKind int

const (
	kindData roleKind = iota // grants data actions only
	kindMgmt                 // grants management-plane actions
)

type roleInfo struct {
	GUID    string
	Display string
	Kind    roleKind
}

// roles holds the built-in role GUIDs. Every GUID and name here was found in
// MicrosoftDocs/azure-docs articles/role-based-access-control/built-in-roles.md
// (2026-10-18). The Cosmos DB Built-in Data Contributor role is a Cosmos SQL
// role, not an Azure RBAC role; its ID is handled in CheckCosmos.
var roles = map[string]roleInfo{
	SearchServiceContributor:   {"7ca78c08-252a-4471-8644-bb5ff32d4ba0", "Search Service Contributor", kindMgmt},
	SearchIndexDataContributor: {"8ebe5a00-799e-43f5-93ac-243d3dce84a7", "Search Index Data Contributor", kindData},
	StorageBlobDataContributor: {"ba92f5b4-2d11-453d-a403-e96b0029c9fe", "Storage Blob Data Contributor", kindData},
	StorageBlobDataOwner:       {"b7e6dc6d-f1e8-4753-8033-0f276bb0955b", "Storage Blob Data Owner", kindData},
	StorageAccountContributor:  {"17d1049b-9a84-46fb-8f53-869881c3d3ab", "Storage Account Contributor", kindMgmt},
	CosmosDBOperator:           {"230815da-be43-4aae-9cb4-875f7bd000aa", "Cosmos DB Operator", kindMgmt},
}

// Owner, Contributor and Reader (built-in-roles.md): management-plane roles
// that carry no data actions. They never satisfy a data role, and Owner and
// Contributor may satisfy a management role, so they make a management-role
// check uncertain rather than missing.
const (
	guidOwner       = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	guidContributor = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	guidReader      = "acdd72a7-3385-48ef-bd42-f606fba81ae7"
)

// satisfiedBy lists additional role keys that satisfy a family: Storage Blob
// Data Owner includes everything Storage Blob Data Contributor grants.
var satisfiedBy = map[string][]string{
	StorageBlobDataContributor: {StorageBlobDataOwner},
}

// CosmosDataContributorID is the Cosmos DB Built-in Data Contributor SQL role
// definition ID. UNVERIFIED: no primary source reachable in this phase confirmed
// the value (the Cosmos data-plane RBAC docs were not available). A mismatch
// therefore yields uncertain, never a missing-role finding on its own.
const CosmosDataContributorID = "00000000-0000-0000-0000-000000000002"

// CosmosCataloguedDatabase is the database the Foundry standard agent setup
// uses, as cited by the FND-RUN-001 catalogue evidence (not re-verified here).
const CosmosCataloguedDatabase = "enterprise_memory"

// Display returns the Azure built-in role name for a family key.
func Display(family string) string {
	if r, ok := roles[family]; ok {
		return r.Display
	}
	if family == CosmosDBDataContributor {
		return "Cosmos DB Built-in Data Contributor"
	}
	return family
}

// Assignment is the metadata-only role assignment evidence.
type Assignment struct {
	RoleDefinitionID string
	Scope            string
	Condition        string
}

// AssignmentSet splits a principal's assignments by relation to the target.
type AssignmentSet struct {
	Covering   []Assignment
	Below      []Assignment
	Unresolved []Assignment
	Truncated  bool
}

// SQLAssignment is a Cosmos DB SQL role assignment.
type SQLAssignment struct {
	RoleDefinitionID string
	Scope            string
}

// Reader exposes the read-only metadata needed for runtime RBAC checks.
type Reader interface {
	ListAssignments(context.Context, string, string) (AssignmentSet, error)
	ListSQLAssignments(context.Context, string, string) ([]SQLAssignment, error)
}

// Verdict is the outcome for one expected role.
type Verdict string

const (
	Granted   Verdict = "granted"
	Missing   Verdict = "missing"
	Uncertain Verdict = "uncertain"
)

// Evaluation is the result for one expected role at one target.
type Evaluation struct {
	// Identity lists the principals considered (comma separated, sorted by input order).
	Identity string
	TargetID string
	// RoleFamily is the family key; Role is the Azure built-in role name.
	RoleFamily string
	Role       string
	Verdict    Verdict
	// Allowed and Uncertain mirror Verdict for callers that predate it.
	Allowed   bool
	Uncertain bool
	Reason    string
}

func roleGUID(roleDefinitionID string) string {
	id := strings.ToLower(strings.TrimRight(strings.TrimSpace(roleDefinitionID), "/"))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

func matches(roleDefinitionID, family string) bool {
	g := roleGUID(roleDefinitionID)
	if r, ok := roles[family]; ok && g == strings.ToLower(r.GUID) {
		return true
	}
	for _, alt := range satisfiedBy[family] {
		if g == strings.ToLower(roles[alt].GUID) {
			return true
		}
	}
	return false
}

// classify decides what one covering assignment means for family.
// It returns granted, a reason for uncertainty (empty when irrelevant).
func classify(a Assignment, family string) (granted bool, uncertain string) {
	g := roleGUID(a.RoleDefinitionID)
	relevant := matches(a.RoleDefinitionID, family)
	if relevant {
		if a.Condition != "" {
			return false, "conditional assignment cannot be evaluated"
		}
		return true, ""
	}
	known := false
	for _, r := range roles {
		if g == strings.ToLower(r.GUID) {
			known = true
		}
	}
	switch g {
	case guidReader:
		return false, ""
	case guidOwner, guidContributor:
		if roles[family].Kind == kindMgmt {
			return false, "an Owner or Contributor assignment may satisfy this management role; its actions are not resolved"
		}
		return false, ""
	}
	if known {
		return false, "" // another recognised built-in role; irrelevant to this family
	}
	return false, "a custom or unrecognised role covers the target and its actions are not resolved"
}

// Check verifies that the principals hold each expected role family at scope.
// A family is granted when any principal holds it, missing only when every
// principal's evidence is complete and shows no grant, and uncertain otherwise.
func Check(ctx context.Context, r Reader, principals []string, scope string, expected ...string) ([]Evaluation, error) {
	if len(principals) == 0 {
		return nil, fmt.Errorf("rbac: no principal to check")
	}
	sets := make([]AssignmentSet, 0, len(principals))
	for _, p := range principals {
		s, err := r.ListAssignments(ctx, scope, p)
		if err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}
	out := make([]Evaluation, 0, len(expected))
	for _, family := range expected {
		ev := Evaluation{Identity: strings.Join(principals, ","), TargetID: scope, RoleFamily: family, Role: Display(family), Verdict: Missing}
		var reasons []string
		for _, s := range sets {
			if s.Truncated {
				reasons = append(reasons, "assignment listing was truncated")
			}
			if len(s.Unresolved) > 0 {
				reasons = append(reasons, "an assignment at a root or management-group scope cannot be tied to the target")
			}
			// An assignment strictly below the target (for example a container) never
			// grants at the target but means the grant may exist at a narrower scope.
			for _, b := range s.Below {
				if matches(b.RoleDefinitionID, family) {
					reasons = append(reasons, "the role is assigned at a scope below the target (for example a container); it cannot be tied to the project's resources")
				}
			}
			for _, a := range s.Covering {
				granted, why := classify(a, family)
				if granted {
					ev.Verdict, ev.Allowed, ev.Reason = Granted, true, ""
					break
				}
				if why != "" {
					reasons = append(reasons, why)
				}
			}
			if ev.Allowed {
				break
			}
		}
		if !ev.Allowed {
			if rs := dedupe(reasons); len(rs) > 0 {
				ev.Verdict, ev.Uncertain, ev.Reason = Uncertain, true, strings.Join(rs, "; ")
			} else {
				ev.Reason = "no covering assignment holds the role"
			}
		}
		out = append(out, ev)
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// CheckCosmos verifies the required Cosmos SQL data role and, for a
// system-assigned identity, the Cosmos DB Operator role at account level.
func CheckCosmos(ctx context.Context, r Reader, principals []string, systemPrincipal, accountID string) ([]Evaluation, error) {
	var out []Evaluation
	if systemPrincipal != "" {
		ops, err := Check(ctx, r, []string{systemPrincipal}, accountID, CosmosDBOperator)
		if err != nil {
			return nil, err
		}
		out = append(out, ops...)
	}
	ev := Evaluation{Identity: strings.Join(principals, ","), TargetID: accountID, RoleFamily: CosmosDBDataContributor, Role: Display(CosmosDBDataContributor), Verdict: Missing}
	var reasons []string
	for _, p := range principals {
		sqls, err := r.ListSQLAssignments(ctx, accountID, p)
		if err != nil {
			return nil, err
		}
		for _, a := range sqls {
			if strings.ToLower(roleGUID(a.RoleDefinitionID)) != CosmosDataContributorID {
				reasons = append(reasons, "a Cosmos SQL assignment with another role exists; custom roles are not resolved")
				continue
			}
			scope := strings.TrimRight(a.Scope, "/")
			switch {
			case strings.EqualFold(scope, strings.TrimRight(accountID, "/")):
				ev.Verdict, ev.Allowed, ev.Reason = Granted, true, ""
			case isDBScope(scope, accountID):
				if strings.EqualFold(lastSegment(scope), CosmosCataloguedDatabase) {
					ev.Verdict, ev.Allowed, ev.Reason = Granted, true, ""
				} else {
					reasons = append(reasons, "the role is assigned on a database whose name cannot be tied to the project")
				}
			default:
				reasons = append(reasons, "the role is assigned at a container scope or an unrecognised path")
			}
			if ev.Allowed {
				break
			}
		}
		if ev.Allowed {
			break
		}
	}
	if !ev.Allowed {
		if rs := dedupe(reasons); len(rs) > 0 {
			ev.Verdict, ev.Uncertain, ev.Reason = Uncertain, true, strings.Join(rs, "; ")
		} else {
			ev.Reason = "no Cosmos SQL role assignment for the identity"
		}
	}
	return append(out, ev), nil
}

// isDBScope reports whether scope is exactly <account>/dbs/<name>.
func isDBScope(scope, accountID string) bool {
	prefix := strings.ToLower(strings.TrimRight(accountID, "/")) + "/dbs/"
	s := strings.ToLower(scope)
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	return rest != "" && !strings.Contains(rest, "/")
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ReaderAdapter adapts the Azure runtime interfaces to the Reader contract.
type ReaderAdapter struct {
	Roles azure.RuntimeRBAC
}

func conv(in []azure.RoleAssignment) []Assignment {
	out := make([]Assignment, 0, len(in))
	for _, g := range in {
		out = append(out, Assignment{RoleDefinitionID: g.RoleDefinitionID, Scope: g.Scope, Condition: g.Condition})
	}
	return out
}

func (a ReaderAdapter) ListAssignments(ctx context.Context, scope, principalID string) (AssignmentSet, error) {
	got, err := a.Roles.ListRoleAssignments(ctx, scope, principalID)
	if err != nil {
		return AssignmentSet{}, err
	}
	return AssignmentSet{Covering: conv(got.Covering), Below: conv(got.Below), Unresolved: conv(got.Unresolved), Truncated: got.Truncated}, nil
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

// MissingMessage renders a deterministic finding message. It names the
// identity, the target, the Azure built-in role and its family, and states
// what the check cannot see.
func MissingMessage(ev Evaluation) string {
	return fmt.Sprintf("identity %s has no assignment of the %s role (role family %s) at or above %s; group assignments and deny assignments are not evaluated, so this is a likely gap, not a certainty",
		ev.Identity, ev.Role, ev.RoleFamily, ev.TargetID)
}

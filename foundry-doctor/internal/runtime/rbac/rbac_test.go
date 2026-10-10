package rbac

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

type fakeReader struct {
	assignments []Assignment
	sql         []SQLAssignment
}

func (f fakeReader) ListAssignments(context.Context, string, string) ([]Assignment, error) {
	return f.assignments, nil
}

func (f fakeReader) ListSQLAssignments(context.Context, string, string) ([]SQLAssignment, error) {
	return f.sql, nil
}

func TestCheck(t *testing.T) {
	got, err := Check(context.Background(), fakeReader{assignments: []Assignment{{RoleDefinitionID: "/x/" + roleGUIDSuffix[SearchServiceContributor]}}}, "principal", "/scope", SearchServiceContributor)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Allowed {
		t.Fatalf("expected allowed, got %+v", got)
	}
	got, err = Check(context.Background(), fakeReader{assignments: []Assignment{{Condition: "x"}}}, "principal", "/scope", SearchServiceContributor)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Uncertain {
		t.Fatalf("expected uncertain, got %+v", got)
	}
}

func TestCheckCosmos(t *testing.T) {
	got, err := CheckCosmos(context.Background(), fakeReader{
		assignments: []Assignment{{RoleDefinitionID: "/x/" + roleGUIDSuffix[CosmosDBOperator]}},
		sql:         []SQLAssignment{{Scope: "/dbs/enterprise_memory"}},
	}, "principal", "/subscriptions/s/resourceGroups/rg/providers/Microsoft.DocumentDB/databaseAccounts/db", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Allowed || !got[1].Allowed {
		t.Fatalf("expected allowed results, got %+v", got)
	}
}

type fakeAzureRBAC struct{}

func (fakeAzureRBAC) ListRoleAssignments(context.Context, string, string) ([]azure.RoleAssignment, error) {
	return []azure.RoleAssignment{{RoleDefinitionID: "x", Scope: "/scope", Condition: ""}}, nil
}
func (fakeAzureRBAC) ListCosmosSQLRoleAssignments(context.Context, string, string) ([]azure.CosmosSQLRoleAssignment, error) {
	return []azure.CosmosSQLRoleAssignment{{RoleDefinitionID: "y", Scope: "/"}}, nil
}

func TestReaderAdapterAndMissingMessage(t *testing.T) {
	r := ReaderAdapter{Roles: fakeAzureRBAC{}}
	assign, err := r.ListAssignments(context.Background(), "/scope", "principal")
	if err != nil || len(assign) != 1 {
		t.Fatalf("assignments = %+v err=%v", assign, err)
	}
	sql, err := r.ListSQLAssignments(context.Background(), "/acct", "principal")
	if err != nil || len(sql) != 1 {
		t.Fatalf("sql = %+v err=%v", sql, err)
	}
	msg := MissingMessage(Evaluation{Identity: "p", RoleFamily: SearchServiceContributor, TargetID: "/scope"})
	if !strings.Contains(msg, "lacks") {
		t.Fatalf("message = %q", msg)
	}
}

package rbac

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

const (
	sub      = "11111111-1111-1111-1111-111111111111"
	rg       = "/subscriptions/" + sub + "/resourceGroups/rg"
	search   = rg + "/providers/Microsoft.Search/searchServices/searchsvc"
	storage  = rg + "/providers/Microsoft.Storage/storageAccounts/storageacct"
	cosmos   = rg + "/providers/Microsoft.DocumentDB/databaseAccounts/cosmosacct"
	pSystem  = "22222222-2222-2222-2222-222222222222"
	pUser    = "33333333-3333-3333-3333-333333333333"
	roleBase = "/subscriptions/" + sub + "/providers/Microsoft.Authorization/roleDefinitions/"
)

func role(key string) string { return roleBase + roles[key].GUID }

type fakeReader struct {
	byPrincipal map[string]AssignmentSet
	sql         map[string][]SQLAssignment
}

func (f fakeReader) ListAssignments(_ context.Context, _ string, p string) (AssignmentSet, error) {
	return f.byPrincipal[p], nil
}

func (f fakeReader) ListSQLAssignments(_ context.Context, _ string, p string) ([]SQLAssignment, error) {
	return f.sql[p], nil
}

func one(a ...Assignment) fakeReader {
	return fakeReader{byPrincipal: map[string]AssignmentSet{pSystem: {Covering: a}}}
}

func TestRoleTableMatchesBuiltInRoleNames(t *testing.T) {
	want := map[string]string{
		SearchServiceContributor: "Search Service Contributor", SearchIndexDataContributor: "Search Index Data Contributor",
		StorageBlobDataContributor: "Storage Blob Data Contributor", StorageBlobDataOwner: "Storage Blob Data Owner",
		StorageAccountContributor: "Storage Account Contributor", CosmosDBOperator: "Cosmos DB Operator",
		CosmosDBDataContributor: "Cosmos DB Built-in Data Contributor",
	}
	for k, v := range want {
		if Display(k) != v {
			t.Errorf("Display(%s) = %q, want %q", k, Display(k), v)
		}
	}
}

func TestCheckVerdicts(t *testing.T) {
	custom := roleBase + "99999999-9999-9999-9999-999999999999"
	cases := []struct {
		name    string
		reader  fakeReader
		family  string
		verdict Verdict
		reason  string
	}{
		{"granted at the target", one(Assignment{RoleDefinitionID: role(SearchServiceContributor), Scope: search}), SearchServiceContributor, Granted, ""},
		{"granted by case-insensitive guid", one(Assignment{RoleDefinitionID: strings.ToUpper(role(SearchServiceContributor))}), SearchServiceContributor, Granted, ""},
		{"owner satisfies contributor", one(Assignment{RoleDefinitionID: role(StorageBlobDataOwner)}), StorageBlobDataContributor, Granted, ""},
		{"contributor does not satisfy owner", one(Assignment{RoleDefinitionID: role(StorageBlobDataContributor)}), StorageBlobDataOwner, Missing, "no covering assignment"},
		{"no assignments is missing", one(), SearchIndexDataContributor, Missing, "no covering assignment"},
		{"conditional assignment is uncertain", one(Assignment{RoleDefinitionID: role(SearchServiceContributor), Condition: "(!(ActionMatches...))"}), SearchServiceContributor, Uncertain, "conditional"},
		{"custom role is uncertain", one(Assignment{RoleDefinitionID: custom}), SearchIndexDataContributor, Uncertain, "custom or unrecognised"},
		{"reader is irrelevant", one(Assignment{RoleDefinitionID: roleBase + "acdd72a7-3385-48ef-bd42-f606fba81ae7"}), SearchIndexDataContributor, Missing, ""},
		{"contributor cannot grant a data role", one(Assignment{RoleDefinitionID: roleBase + "b24988ac-6180-42a0-ab88-20f7382dd24c"}), SearchIndexDataContributor, Missing, ""},
		{"contributor may satisfy a management role", one(Assignment{RoleDefinitionID: roleBase + "b24988ac-6180-42a0-ab88-20f7382dd24c"}), StorageAccountContributor, Uncertain, "Owner or Contributor"},
		{"another built-in role is irrelevant", one(Assignment{RoleDefinitionID: role(SearchServiceContributor)}), SearchIndexDataContributor, Missing, ""},
		{"role only below the target (container scope) is uncertain", fakeReader{byPrincipal: map[string]AssignmentSet{pSystem: {Below: []Assignment{{RoleDefinitionID: role(StorageBlobDataOwner), Scope: storage + "/blobServices/default/containers/c"}}}}}, StorageBlobDataOwner, Uncertain, "below the target"},
		{"unrelated role below the target is ignored", fakeReader{byPrincipal: map[string]AssignmentSet{pSystem: {Below: []Assignment{{RoleDefinitionID: role(StorageBlobDataContributor)}}}}}, StorageBlobDataOwner, Missing, ""},
		{"truncated listing is uncertain", fakeReader{byPrincipal: map[string]AssignmentSet{pSystem: {Truncated: true}}}, SearchServiceContributor, Uncertain, "truncated"},
		{"management-group assignment is uncertain", fakeReader{byPrincipal: map[string]AssignmentSet{pSystem: {Unresolved: []Assignment{{RoleDefinitionID: role(SearchServiceContributor), Scope: "/providers/Microsoft.Management/managementGroups/mg"}}}}}, SearchServiceContributor, Uncertain, "management-group"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Check(context.Background(), c.reader, []string{pSystem}, search, c.family)
			if err != nil || len(got) != 1 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if got[0].Verdict != c.verdict || got[0].Allowed != (c.verdict == Granted) || got[0].Uncertain != (c.verdict == Uncertain) {
				t.Errorf("verdict = %+v, want %s", got[0], c.verdict)
			}
			if !strings.Contains(got[0].Reason, c.reason) {
				t.Errorf("reason = %q, want it to contain %q", got[0].Reason, c.reason)
			}
		})
	}
}

func TestCheckUsesAnyOfTheProjectIdentities(t *testing.T) {
	r := fakeReader{byPrincipal: map[string]AssignmentSet{
		pSystem: {},
		pUser:   {Covering: []Assignment{{RoleDefinitionID: role(SearchIndexDataContributor)}}},
	}}
	got, err := Check(context.Background(), r, []string{pSystem, pUser}, search, SearchIndexDataContributor, SearchServiceContributor)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Verdict != Granted || got[1].Verdict != Missing {
		t.Fatalf("got %+v", got)
	}
	if _, err := Check(context.Background(), r, nil, search, SearchIndexDataContributor); err == nil {
		t.Error("no principal must be an error")
	}
}

func TestCheckCosmos(t *testing.T) {
	sqlRole := cosmos + "/sqlRoleDefinitions/" + CosmosDataContributorID
	other := cosmos + "/sqlRoleDefinitions/11111111-0000-0000-0000-000000000000"
	cases := []struct {
		name    string
		sql     []SQLAssignment
		verdict Verdict
		reason  string
	}{
		{"account scope", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: cosmos}}, Granted, ""},
		{"account scope with trailing slash and case", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: strings.ToUpper(cosmos) + "/"}}, Granted, ""},
		{"catalogued database scope", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: cosmos + "/dbs/enterprise_memory"}}, Granted, ""},
		{"other database scope is uncertain, not hard-coded", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: cosmos + "/dbs/orders"}}, Uncertain, "database whose name"},
		{"container scope is uncertain", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: cosmos + "/dbs/enterprise_memory/colls/c"}}, Uncertain, "container scope"},
		{"other role is uncertain", []SQLAssignment{{RoleDefinitionID: other, Scope: cosmos}}, Uncertain, "custom roles"},
		{"no assignments", nil, Missing, "no Cosmos SQL role assignment"},
		{"legacy root scope string is not an account scope", []SQLAssignment{{RoleDefinitionID: sqlRole, Scope: "/"}}, Uncertain, "unrecognised path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := fakeReader{sql: map[string][]SQLAssignment{pSystem: c.sql}, byPrincipal: map[string]AssignmentSet{pSystem: {Covering: []Assignment{{RoleDefinitionID: role(CosmosDBOperator)}}}}}
			got, err := CheckCosmos(context.Background(), r, []string{pSystem}, pSystem, cosmos)
			if err != nil || len(got) != 2 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if got[0].RoleFamily != CosmosDBOperator || got[0].Verdict != Granted {
				t.Errorf("operator = %+v", got[0])
			}
			if got[1].Verdict != c.verdict || !strings.Contains(got[1].Reason, c.reason) {
				t.Errorf("data role = %+v, want %s containing %q", got[1], c.verdict, c.reason)
			}
		})
	}
}

func TestCheckCosmosOperatorOnlyForSystemAssigned(t *testing.T) {
	// A user-assigned-only project is not required to hold Cosmos DB Operator (catalogue: system-assigned only).
	r := fakeReader{sql: map[string][]SQLAssignment{pUser: {{RoleDefinitionID: cosmos + "/sqlRoleDefinitions/" + CosmosDataContributorID, Scope: cosmos}}}}
	got, err := CheckCosmos(context.Background(), r, []string{pUser}, "", cosmos)
	if err != nil || len(got) != 1 || got[0].RoleFamily != CosmosDBDataContributor || got[0].Verdict != Granted {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

type fakeAzureRBAC struct{}

func (fakeAzureRBAC) ListRoleAssignments(context.Context, string, string) (azure.RoleAssignmentSet, error) {
	return azure.RoleAssignmentSet{
		Covering:   []azure.RoleAssignment{{RoleDefinitionID: "x", Scope: "/scope"}},
		Below:      []azure.RoleAssignment{{RoleDefinitionID: "y", Scope: "/scope/child"}},
		Unresolved: []azure.RoleAssignment{{RoleDefinitionID: "z"}},
		Truncated:  true,
	}, nil
}
func (fakeAzureRBAC) ListCosmosSQLRoleAssignments(context.Context, string, string) ([]azure.CosmosSQLRoleAssignment, error) {
	return []azure.CosmosSQLRoleAssignment{{RoleDefinitionID: "y", Scope: "/"}}, nil
}

func TestReaderAdapter(t *testing.T) {
	r := ReaderAdapter{Roles: fakeAzureRBAC{}}
	set, err := r.ListAssignments(context.Background(), "/scope", "principal")
	if err != nil || len(set.Covering) != 1 || len(set.Below) != 1 || len(set.Unresolved) != 1 || !set.Truncated {
		t.Fatalf("set = %+v err=%v", set, err)
	}
	sql, err := r.ListSQLAssignments(context.Background(), "/acct", "principal")
	if err != nil || len(sql) != 1 {
		t.Fatalf("sql = %+v err=%v", sql, err)
	}
}

func TestMissingMessageCarriesIdentityTargetRoleAndLimits(t *testing.T) {
	ev := Evaluation{Identity: pSystem, TargetID: search, RoleFamily: SearchServiceContributor, Role: Display(SearchServiceContributor)}
	msg := MissingMessage(ev)
	for _, want := range []string{pSystem, search, "Search Service Contributor", SearchServiceContributor, "group assignments", "deny assignments", "likely"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

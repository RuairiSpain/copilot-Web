package azure

import (
	"context"
	"slices"
	"testing"
)

func TestListResourcesUserAssignedIdentities(t *testing.T) {
	uaB := testRG + "/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-b"
	uaA := testRG + "/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-a"
	body := `{"value":[` +
		`{"id":"` + testRG + `/providers/Microsoft.CognitiveServices/accounts/acct","name":"acct","type":"Microsoft.CognitiveServices/accounts",` +
		`"identity":{"type":"SystemAssigned, UserAssigned","principalId":"` + testOID + `","userAssignedIdentities":{"` + uaB + `":{"principalId":"` + testOID + `"},"` + uaA + `":{}}}},` +
		`{"id":"` + testRG + `/providers/Microsoft.Storage/storageAccounts/st","name":"st","type":"Microsoft.Storage/storageAccounts"}` +
		`]}`
	f := newFake(t).on("GET", testRG+"/resources", reply{status: 200, body: body})
	a, _ := newAdapter(t, f)
	got, err := a.ListResources(context.Background(), InventoryQuery{Scope: testRG})
	if err != nil || len(got.Resources) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, r := range got.Resources {
		switch r.Name {
		case "acct":
			if want := []string{uaA, uaB}; !slices.Equal(r.UserAssignedIdentities, want) {
				t.Errorf("acct identities = %v, want %v", r.UserAssignedIdentities, want)
			}
		case "st":
			if len(r.UserAssignedIdentities) != 0 {
				t.Errorf("st identities = %v, want none", r.UserAssignedIdentities)
			}
		}
	}
}

// An unexpected identity shape yields no identity IDs; it never fails the
// shared inventory call.
func TestListResourcesToleratesUnexpectedIdentityShapes(t *testing.T) {
	shapes := []string{
		`"identity":null`,
		`"identity":{"type":"SystemAssigned","userAssignedIdentities":null}`,
		`"identity":"SystemAssigned"`,
		`"identity":{"userAssignedIdentities":["not-a-map"]}`,
		`"identity":{"userAssignedIdentities":"not-a-map"}`,
		`"identity":[1,2]`,
	}
	for _, shape := range shapes {
		body := `{"value":[{"id":"` + testRG + `/providers/Microsoft.Storage/storageAccounts/st","name":"st","type":"Microsoft.Storage/storageAccounts",` + shape + `}]}`
		a, _ := newAdapter(t, newFake(t).on("GET", testRG+"/resources", reply{status: 200, body: body}))
		got, err := a.ListResources(context.Background(), InventoryQuery{Scope: testRG})
		if err != nil || len(got.Resources) != 1 {
			t.Fatalf("%s: got %+v, %v", shape, got, err)
		}
		if ids := got.Resources[0].UserAssignedIdentities; len(ids) != 0 {
			t.Errorf("%s: identities = %v, want none", shape, ids)
		}
	}
}

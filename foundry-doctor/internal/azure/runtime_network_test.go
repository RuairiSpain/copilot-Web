package azure

import (
	"context"
	"errors"
	"testing"
)

func TestGetAccountNetwork(t *testing.T) {
	// synthetic, shaped like Account (cognitiveservices.json 2026-09-01).
	body := `{"id":"x","name":"acct","properties":{"publicNetworkAccess":"Disabled","privateEndpointConnections":[{"id":"pe1","name":"pe1","properties":{}},{"id":"pe2"}]}}`
	a, _ := newAdapter(t, newFake(t).on("GET", rtAcct, reply{status: 200, body: body}))
	got, err := a.GetAccountNetwork(context.Background(), testSub, "rg-demo", "acct")
	if err != nil || got.PublicNetworkAccess != "Disabled" || got.PrivateEndpointConnections != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for _, c := range [][2]string{{"rg/..", "acct"}, {"rg-demo", "a b"}} {
		if _, err := a.GetAccountNetwork(context.Background(), testSub, c[0], c[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%v err = %v", c, err)
		}
	}
	a2, _ := newAdapter(t, newFake(t).on403("GET", rtAcct))
	_, err = a2.GetAccountNetwork(context.Background(), testSub, "rg-demo", "acct")
	if u, ok := AsUnavailable(err); !ok || u.Permission != "Microsoft.CognitiveServices/accounts/read" {
		t.Errorf("restricted err = %v", err)
	}
}

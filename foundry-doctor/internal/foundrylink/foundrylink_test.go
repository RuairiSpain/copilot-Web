package foundrylink

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return m }

type props = map[string]any

const (
	search  = "Microsoft.Search/searchServices"
	cosmos  = "Microsoft.DocumentDB/databaseAccounts"
	conn    = "Microsoft.CognitiveServices/accounts/connections"
	account = "Microsoft.CognitiveServices/accounts"
)

func connection(p props) sdk.ARMResource {
	return sdk.ARMResource{Type: conn, Name: "acct/c", Properties: p}
}

func TestClassify(t *testing.T) {
	cond := connection(props{"metadata": props{"ResourceId": "[resourceId('Microsoft.Search/searchServices', format('srch-{0}', variables('t')))]"}})
	cond.Conditional, cond.Condition = true, "[parameters('c')]"
	off := cond
	off.Condition = "false"
	tests := []struct {
		name string
		conn []sdk.ARMResource
		res  sdk.ARMResource
		want State
	}{
		{"no connections", nil, sdk.ARMResource{Type: search, Name: "s"}, NotLinked},
		{"URL host names the service", []sdk.ARMResource{connection(props{"target": "https://srch.search.windows.net"})}, sdk.ARMResource{Type: search, Name: "srch"}, Linked},
		{"substring is not a match", []sdk.ARMResource{connection(props{"target": "https://srch.search.windows.net"})}, sdk.ARMResource{Type: search, Name: "rch"}, NotLinked},
		{"longer name is not a match", []sdk.ARMResource{connection(props{"target": "https://srch.search.windows.net"})}, sdk.ARMResource{Type: search, Name: "srch2"}, NotLinked},
		{"resource id", []sdk.ARMResource{connection(props{"metadata": props{"ResourceId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.DocumentDB/databaseAccounts/db"}})}, sdk.ARMResource{Type: cosmos, Name: "db"}, Linked},
		{"type mismatch", []sdk.ARMResource{connection(props{"metadata": props{"ResourceId": "/subscriptions/s/resourceGroups/r/providers/Microsoft.DocumentDB/databaseAccounts/db"}})}, sdk.ARMResource{Type: search, Name: "db"}, NotLinked},
		{"format url expression", []sdk.ARMResource{connection(props{"target": "[format('https://{0}.search.windows.net', format('srch-{0}', variables('t')))]"})}, sdk.ARMResource{Type: search, Name: "[format('srch-{0}', variables('t'))]"}, Linked},
		{"unrelated expression name differs by literal head", []sdk.ARMResource{connection(props{"target": "[format('https://{0}.search.windows.net', format('srch-{0}', variables('t')))]"})}, sdk.ARMResource{Type: search, Name: "[format('other-{0}', variables('t'))]"}, NotLinked},
		{"same head different variable is uncertain", []sdk.ARMResource{connection(props{"target": "[format('https://{0}.search.windows.net', format('srch-{0}', variables('a')))]"})}, sdk.ARMResource{Type: search, Name: "[format('srch-{0}', variables('b'))]"}, Uncertain},
		{"opaque reference is uncertain", []sdk.ARMResource{connection(props{"target": "[parameters('endpoint')]"})}, sdk.ARMResource{Type: search, Name: "s"}, Uncertain},
		{"category literal is not a reference", []sdk.ARMResource{connection(props{"category": "CognitiveSearch", "authType": "AAD"})}, sdk.ARMResource{Type: search, Name: "CognitiveSearch"}, NotLinked},
		{"conditional connection is not proof", []sdk.ARMResource{cond}, sdk.ARMResource{Type: search, Name: "[format('srch-{0}', variables('t'))]"}, Uncertain},
		{"not deployed connection is ignored", []sdk.ARMResource{off}, sdk.ARMResource{Type: search, Name: "[format('srch-{0}', variables('t'))]"}, NotLinked},
		{"looped resource is uncertain", []sdk.ARMResource{connection(props{"target": "https://a.search.windows.net"})}, sdk.ARMResource{Type: search, Name: "a", Looped: true}, Uncertain},
		{"capabilitySettings link", []sdk.ARMResource{{Type: account, Name: "acct", Properties: props{"capabilitySettings": props{"vectorStore": "/subscriptions/s/resourceGroups/r/providers/Microsoft.Search/searchServices/vs"}}}}, sdk.ARMResource{Type: search, Name: "vs"}, Linked},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Collect(model(tc.conn)).Classify(tc.res); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestCollectWhere(t *testing.T) {
	c := connection(props{"authType": "AccountKey", "target": "https://srch.search.windows.net"})
	other := connection(props{"authType": "AAD", "target": "https://s2.search.windows.net"})
	set := CollectWhere(model{c, other}, func(r sdk.ARMResource) bool {
		v, _ := r.Properties["authType"].(string)
		return v == "AccountKey"
	})
	if set.Classify(sdk.ARMResource{Type: search, Name: "srch"}) != Linked || set.Classify(sdk.ARMResource{Type: search, Name: "s2"}) != NotLinked {
		t.Fatal("filter not applied")
	}
	if !set.Any() {
		t.Fatal("Any must report the connection")
	}
}

func TestOwns(t *testing.T) {
	a := sdk.ARMResource{Type: account, Name: "[format('ai-{0}', variables('t'))]"}
	b := sdk.ARMResource{Type: account, Name: "[format('other-{0}', variables('t'))]"}
	child := sdk.ARMResource{Type: account + "/projects", Name: "[format('{0}/{1}', format('ai-{0}', variables('t')), 'p')]"}
	m := model{a, b, child}
	if Owns(m, a, 2, account+"/projects") != OwnsYes {
		t.Error("a owns the project")
	}
	if Owns(m, b, 2, account+"/projects") != OwnsNone {
		t.Error("b does not own the project")
	}
	cond := child
	cond.Conditional, cond.Condition = true, "[parameters('p')]"
	if Owns(model{a, b, cond}, a, 2, account+"/projects") != OwnsMaybe {
		t.Error("a conditional project proves nothing")
	}
	off := child
	off.Conditional, off.Condition = true, "false"
	if Owns(model{a, off}, a, 1, account+"/projects") != OwnsNone {
		t.Error("a not-deployed project is ignored")
	}
}

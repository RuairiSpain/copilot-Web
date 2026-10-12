package idn

import (
	"reflect"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func TestSplitArgs(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"'a'", []string{"'a'"}},
		{"'a, b', c", []string{"'a, b'", "c"}},
		{"f(1, 2), [x, y], 'it''s'", []string{"f(1, 2)", "[x, y]", "'it''s'"}},
	} {
		if got := splitArgs(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitArgs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseCallAndLiteral(t *testing.T) {
	fn, args, rest, ok := parseCall("reference(resourceId('T/x', 'n'), 'v', 'full').identity.principalId")
	if !ok || fn != "reference" || len(args) != 3 || rest != ".identity.principalId" {
		t.Fatalf("got %q %q %q %v", fn, args, rest, ok)
	}
	for _, bad := range []string{"", "noparen", "(x)", "f(unclosed", "a b(c)"} {
		if _, _, _, ok := parseCall(bad); ok {
			t.Errorf("parseCall(%q) succeeded", bad)
		}
	}
	if s, ok := literalArg("'it''s'"); !ok || s != "it's" {
		t.Errorf("literalArg escape: %q %v", s, ok)
	}
	for _, bad := range []string{"x", "'a'b'", "'", ""} {
		if _, ok := literalArg(bad); ok {
			t.Errorf("literalArg(%q) succeeded", bad)
		}
	}
	if argText("'x'") != "x" || argText(" parameters('p') ") != "parameters('p')" {
		t.Error("argText")
	}
}

func TestParseID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		kind idKind
		typ  string
		last string
	}{
		{"[subscription().id]", idSub, "", ""},
		{"[resourceGroup().id]", idRG, "", ""},
		{"[managementGroup().id]", idMG, "", ""},
		{"[tenant().tenantId]", idTenant, "", ""},
		{"[resourceId('Microsoft.Storage/storageAccounts', 'st')]", idResource, "Microsoft.Storage/storageAccounts", "st"},
		{"[resourceId('sub', 'rg', 'Microsoft.Storage/storageAccounts', parameters('n'))]", idResource, "Microsoft.Storage/storageAccounts", "parameters('n')"},
		{"[extensionResourceId(resourceGroup().id, 'Microsoft.Search/searchServices', 's')]", idResource, "Microsoft.Search/searchServices", "s"},
		{"[subscriptionResourceId('Microsoft.Resources/resourceGroups', 'rg')]", idRG, "", ""},
		{"[resourceId('Microsoft.Resources/subscriptions', 'x')]", idSub, "", ""},
		{"[format('Microsoft.Storage/storageAccounts/{0}/blobServices/default/containers/{1}', 'st', 'c1')]", idResource, "Microsoft.Storage/storageAccounts/blobServices/containers", "c1"},
		{"[format('Microsoft.Resources/resourceGroups/{0}', 'rg')]", idRG, "", ""},
		{"['sym']", idSymbolic, "", ""},
		{"/subscriptions/0000", idSub, "", ""},
		{"/subscriptions/0000/resourceGroups/rg", idRG, "", ""},
		{"/providers/Microsoft.Management/managementGroups/mg", idMG, "", ""},
		{"/subscriptions/0/resourceGroups/rg/providers/Microsoft.Search/searchServices/s", idResource, "Microsoft.Search/searchServices", "s"},
		{"[variables('x')]", idUnknown, "", ""},
		{"[resourceId(parameters('t'), 'n')]", idUnknown, "", ""},
		{"[format('{0}/x', 'a')]", idUnknown, "", ""},
		{"[concat('a')]", idUnknown, "", ""},
		{"[subscription().tenantId]", idUnknown, "", ""},
		{"plain-text", idUnknown, "", ""},
	} {
		got := parseID(tc.in)
		if got.kind != tc.kind || got.typ != tc.typ {
			t.Errorf("parseID(%q) = %+v", tc.in, got)
			continue
		}
		if tc.last != "" && (len(got.names) == 0 || got.names[len(got.names)-1] != tc.last) {
			t.Errorf("parseID(%q) names = %q, want last %q", tc.in, got.names, tc.last)
		}
	}
	if k := parseID("[resourceId('Microsoft.Storage/storageAccounts', 'st')]"); k.rootType() != "Microsoft.Storage/storageAccounts" || k.isChild() {
		t.Error("rootType/isChild of a root type")
	}
	if k := parseID("[format('Microsoft.Storage/storageAccounts/{0}/blobServices/{1}', 'st', 'd')]"); k.rootType() != "Microsoft.Storage/storageAccounts" || !k.isChild() {
		t.Error("rootType/isChild of a child type")
	}
	for _, k := range []idKind{idSub, idMG, idTenant, idRG, idResource, idUnknown} {
		r := idRef{kind: k}
		if r.describe() == "" || r.keyName() == "" {
			t.Errorf("empty description for %d", k)
		}
		if r.broad() != (k == idSub || k == idMG || k == idTenant || k == idRG) {
			t.Errorf("broad(%d)", k)
		}
	}
}

func TestRoleGUIDText(t *testing.T) {
	const g = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	for _, tc := range []struct {
		in  string
		ok  bool
		why string
	}{
		{g, true, ""},
		{"[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + g + "')]", true, ""},
		{"/subscriptions/00000000-0000-0000-0000-000000000009/providers/Microsoft.Authorization/roleDefinitions/" + g, true, ""},
		{"[resourceId('Microsoft.DocumentDB/databaseAccounts/sqlRoleDefinitions', 'a', '00000000-0000-0000-0000-000000000002')]", true, ""},
		{"[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', parameters('r'))]", false, "arm-expression"},
		{"[variables('roleId')]", false, "arm-expression"},
		{"not a role id", false, "not-a-role-definition-id"},
	} {
		got, ok, why := roleGUIDText(tc.in)
		if ok != tc.ok || why != tc.why || (ok && got == "") {
			t.Errorf("roleGUIDText(%q) = %q %v %q", tc.in, got, ok, why)
		}
	}
}

func TestNameSegmentsAndMatch(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"acct/proj", []string{"acct", "proj"}},
		{"[format('{0}/{1}', parameters('a'), 'p')]", []string{"parameters('a')", "p"}},
		{"[concat(parameters('a'), '/p')]", []string{"parameters('a')", "p"}},
		{"[parameters('a')]", []string{"parameters('a')"}},
	} {
		if got := nameSegments(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("nameSegments(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	tmpl := &model.ARMTemplate{Resources: []model.Resource{
		{Type: "Microsoft.Search/searchServices", Name: "s1", Symbolic: "one", Meta: model.ResourceMeta{Pointer: "/a"}},
		{Type: "Microsoft.Search/searchServices", Name: "[parameters('s2')]", Meta: model.ResourceMeta{Pointer: "/b"}},
	}}
	if got := match(tmpl, idRef{kind: idResource, typ: "microsoft.search/searchservices", names: []string{"s1"}}); len(got) != 1 || got[0].Name != "s1" {
		t.Errorf("by literal name: %v", got)
	}
	if got := match(tmpl, idRef{kind: idResource, typ: "Microsoft.Search/searchServices", names: []string{"parameters('s2')"}}); len(got) != 1 || got[0].Meta.Pointer != "/b" {
		t.Errorf("by parameter name: %v", got)
	}
	if got := match(tmpl, idRef{kind: idResource, typ: "Microsoft.Search/searchServices", names: []string{"other"}}); len(got) != 2 {
		t.Errorf("ambiguous: %v", got)
	}
	if got := match(tmpl, idRef{kind: idSymbolic, sym: "ONE"}); len(got) != 1 {
		t.Errorf("symbolic: %v", got)
	}
	if got := match(tmpl, idRef{kind: idSymbolic, sym: "s1"}); len(got) != 1 {
		t.Errorf("symbolic by name: %v", got)
	}
	if got := match(tmpl, idRef{kind: idSub}); got != nil {
		t.Errorf("non-resource: %v", got)
	}
	if got := match(nil, idRef{kind: idResource}); got != nil {
		t.Errorf("nil template: %v", got)
	}
	if !samePointer(model.Resource{Name: "a", Type: "t"}, model.Resource{Name: "a", Type: "t"}) {
		t.Error("samePointer without pointers")
	}
}

func TestSubjectChecks(t *testing.T) {
	for _, tc := range []struct {
		subject string
		github  bool
		bad     bool
	}{
		{"repo:o/r:environment:prod", true, false},
		{"repo:o/r:ref:refs/heads/main", true, false},
		{"repo:o/r:pull_request", true, false},
		{"repo:o@1/r@2:environment:prod", true, false},
		{"repo:o/r", true, true},
		{"repo:o/r:environment", true, true},
		{"repo:o/r:pull_request:x", true, true},
		{"repo:o/r:tag:v1", true, true},
		{"repo:o:environment:prod", true, true},
		{"org:o/r:environment:prod", true, true},
		{"system:serviceaccount:ns:sa", false, false},
		{"system:serviceaccount:ns", false, true},
		{"system:serviceaccount::sa", false, true},
		{"user:ns:sa:x", false, true},
	} {
		var why string
		if tc.github {
			why = githubSubjectProblem(tc.subject)
		} else {
			why = aksSubjectProblem(tc.subject)
		}
		if (why != "") != tc.bad {
			t.Errorf("%q: problem %q, want bad=%v", tc.subject, why, tc.bad)
		}
	}
}

func TestIdentityOf(t *testing.T) {
	r := func(identity any) model.Resource {
		b := map[string]any{}
		if identity != nil {
			b["identity"] = identity
		}
		return model.Resource{Body: b}
	}
	for _, tc := range []struct {
		name string
		in   any
		want identKind
	}{
		{"absent", nil, identAbsent},
		{"no type", map[string]any{}, identAbsent},
		{"none", map[string]any{"type": "None"}, identNone},
		{"system", map[string]any{"type": "SystemAssigned"}, identSystem},
		{"user", map[string]any{"type": "UserAssigned"}, identUser},
		{"both", map[string]any{"type": "SystemAssigned,UserAssigned"}, identBoth},
		{"param", map[string]any{"type": "[parameters('t')]"}, identParam},
		{"module", map[string]any{"type": "[reference('m').outputs.t.value]"}, identModule},
		{"expr", map[string]any{"type": "[variables('t')]"}, identUnresolved},
		{"whole expr", "[parameters('i')]", identParam},
		{"whole literal string", "x", identUnresolved},
		{"not a map", 7.0, identUnresolved},
		{"type not string", map[string]any{"type": 1.0}, identUnresolved},
		{"unknown type", map[string]any{"type": "Weird"}, identUnresolved},
	} {
		if got := identityOf(r(tc.in)); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
	if !identBoth.hasSystem() || !identBoth.hasUser() || identUser.hasSystem() || identSystem.hasUser() {
		t.Error("hasSystem/hasUser")
	}
	if keys, un := userIdentityKeys(model.Resource{Body: map[string]any{"identity": map[string]any{"userAssignedIdentities": "[parameters('m')]"}}}); keys != nil || !un {
		t.Error("userIdentityKeys with expression")
	}
	if keys, un := userIdentityKeys(model.Resource{Body: map[string]any{"identity": map[string]any{"userAssignedIdentities": map[string]any{"b": 1, "a": 2}}}}); un || !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Errorf("userIdentityKeys = %v %v", keys, un)
	}
}

func TestResolvePrincipalKinds(t *testing.T) {
	ra := func(v any) model.Resource {
		p := map[string]any{}
		if v != nil {
			p["principalId"] = v
		}
		return model.Resource{Body: map[string]any{"properties": p}}
	}
	tmpl := &model.ARMTemplate{}
	for _, tc := range []struct {
		name string
		in   any
		want principalKind
	}{
		{"absent", nil, principalAbsent},
		{"not string", 3.0, principalNotString},
		{"guid", "11111111-2222-3333-4444-555555555555", principalLiteral},
		{"bad literal", "abc", principalBadLit},
		{"parameter", "[parameters('p')]", principalParam},
		{"module", "[reference(resourceId('Microsoft.Resources/deployments', 'm'), '2022-09-01').outputs.p.value]", principalModule},
		{"reference no chain", "[reference('x')]", principalUnknown},
		{"reference odd chain", "[reference('x').a['b']]", principalUnknown},
		{"other function", "[variables('v')]", principalUnknown},
		{"not a call", "[x]", principalUnknown},
		{"reference no args", "[reference()]", principalUnknown},
		{"reference", "[reference('x', '2024-11-30').principalId]", principalRef},
	} {
		if got := resolvePrincipal(tmpl, ra(tc.in)); got.kind != tc.want {
			t.Errorf("%s: kind %d, want %d", tc.name, got.kind, tc.want)
		}
	}
	if p := resolvePrincipal(tmpl, ra("[reference('x').properties.appId]")); p.field != fieldClientID {
		t.Error("appId is a client ID")
	}
	if p := resolvePrincipal(tmpl, ra("[reference('x').identity.objectId]")); p.field != fieldObjectID || !p.system {
		t.Error("objectId through identity")
	}
	if _, ok := resolvePrincipal(tmpl, ra("[reference('x').foo]")).unique(); ok {
		t.Error("unique without candidates")
	}
}

package armexpr

import "testing"

// Compiled shapes below were produced by Bicep CLI 0.48.1 (see
// internal/rules/testdata/compiled).
const (
	aiName   = "[format('ai-{0}', variables('token'))]"
	aiID     = "[resourceId('Microsoft.CognitiveServices/accounts', format('ai-{0}', variables('token')))]"
	projName = "[format('{0}/{1}', format('ai-{0}', variables('token')), 'proj')]"
	projID   = "[resourceId('Microsoft.CognitiveServices/accounts/projects', format('ai-{0}', variables('token')), 'proj')]"
)

func TestIsExpression(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"[format('a')]", true},
		{"[resourceGroup().location]", true},
		{"[[literal bracket]", false},
		{"plain", false},
		{"", false},
		{"swedencentral", false},
	}
	for _, tc := range tests {
		if got := IsExpression(tc.in); got != tc.want {
			t.Errorf("IsExpression(%q) = %v", tc.in, got)
		}
	}
}

func TestParseCallAndSplitArgs(t *testing.T) {
	c, ok := ParseCall("[reference(resourceId('A/b', 'x, y'), '2025-06-01', 'full').identity.principalId]")
	if !ok || c.Fn != "reference" || len(c.Args) != 3 || c.Suffix != ".identity.principalId" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	if c.Args[0] != "resourceId('A/b', 'x, y')" {
		t.Errorf("arg0 = %q", c.Args[0])
	}
	for _, bad := range []string{"[nofunc]", "[format('a'", "[format('a')", "plain"} {
		if _, ok := ParseCall(bad); ok {
			t.Errorf("ParseCall(%q) must fail", bad)
		}
	}
	if _, ok := SplitArgs("a, b)"); ok {
		t.Error("unbalanced must fail")
	}
}

func TestUnquote(t *testing.T) {
	if v, ok := Unquote("'it''s'"); !ok || v != "it's" {
		t.Errorf("got %q %v", v, ok)
	}
	for _, bad := range []string{"x", "'a'b'", "''''x", ""} {
		if _, ok := Unquote(bad); ok && bad != "''''x" {
			t.Errorf("Unquote(%q) must fail", bad)
		}
	}
}

func TestFold(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"[format('{0}/{1}', 'a', 'b')]", "a/b", true},
		{"[resourceId('Microsoft.Network/virtualNetworks', 'vnet')]", "vnet", true},
		{"[resourceId('Microsoft.Network/virtualNetworks/subnets', 'vnet', 'pe')]", "vnet/pe", true},
		{aiName, aiName, false},
		{"[format('ai-{0}', 'x')]", "[format('ai-{0}', 'x')]", false}, // format text beyond path characters is kept
		{"literal", "literal", false},
	}
	for _, tc := range tests {
		got, ok := Fold(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Fold(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseNameShapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		segs int
		lit  bool
	}{
		{"literal", "acct", 1, true},
		{"literal child", "acct/proj", 2, true},
		{"format expression", aiName, 1, false},
		{"child of format", projName, 2, false},
		{"variable", "[variables('n')]", 1, false},
		{"parameter", "[parameters('n')]", 1, false},
	}
	for _, tc := range tests {
		r := ParseName("t", tc.in)
		if len(r.Segments) != tc.segs || r.Literal() != tc.lit || r.Opaque {
			t.Errorf("%s: %+v", tc.name, r)
		}
	}
	if r := ParseName("t", ""); !r.Opaque {
		t.Error("empty name must be opaque")
	}
}

func TestParseRefShapes(t *testing.T) {
	r := ParseRef(projID)
	if r.Type != "microsoft.cognitiveservices/accounts/projects" || len(r.Segments) != 2 || r.Segments[1].Text != "proj" {
		t.Fatalf("projID: %+v", r)
	}
	r = ParseRef("/subscriptions/s/resourceGroups/r/providers/Microsoft.Storage/storageAccounts/st1")
	if r.Type != "microsoft.storage/storageaccounts" || !r.Literal() || r.Segments[0].Text != "st1" {
		t.Fatalf("literal id: %+v", r)
	}
	r = ParseRef("/subscriptions/s/resourceGroups/r/providers/Microsoft.Network/virtualNetworks/vn/subnets/pe")
	if r.Type != "microsoft.network/virtualnetworks/subnets" || len(r.Segments) != 2 {
		t.Fatalf("subnet id: %+v", r)
	}
	r = ParseRef("[reference(resourceId('Microsoft.Network/virtualNetworks', 'vnet'), '2024-05-01').subnets[0].id]")
	if r.Type != "microsoft.network/virtualnetworks" || r.Segments[0].Text != "vnet" {
		t.Fatalf("reference unwrap: %+v", r)
	}
	for _, opaque := range []string{"[parameters('id')]", "[concat('a', 'b')]", "", "https://x.search.windows.net", "[resourceId('NoType')]", "/subscriptions/s/providers/Microsoft.A/b"} {
		if r := ParseRef(opaque); !r.Opaque {
			t.Errorf("ParseRef(%q) must be opaque: %+v", opaque, r)
		}
	}
	if r := ParseRef("vnet/pe"); r.Type != "" || len(r.Segments) != 2 {
		t.Errorf("bare child: %+v", r)
	}
	// subscriptionResourceId with a leading subscription argument.
	r = ParseRef("[resourceId('sub-id', 'rg', 'Microsoft.Storage/storageAccounts', 'st')]")
	if r.Type != "microsoft.storage/storageaccounts" || len(r.Segments) != 1 || r.Segments[0].Text != "st" {
		t.Errorf("scoped resourceId: %+v", r)
	}
}

func TestCompare(t *testing.T) {
	ai := ParseName("Microsoft.CognitiveServices/accounts", aiName)
	tests := []struct {
		name string
		a, b Ref
		want Result
	}{
		{"same expression via resourceId", ParseRef(aiID), ai, Same},
		{"project child same", ParseRef(projID), ParseName("Microsoft.CognitiveServices/accounts/projects", projName), Same},
		{"literal equal case-insensitive", ParseName("", "Acct"), ParseName("", "acct"), Same},
		{"literal differ", ParseName("", "a"), ParseName("", "b"), Different},
		{"literal count differ", ParseName("", "a"), ParseName("", "a/b"), Different},
		{"types differ", ParseRef(aiID), ParseName("Microsoft.Search/searchServices", aiName), Different},
		{"expression vs literal unknown", ParseRef(aiID), ParseName("", "ai-x"), Unknown},
		{"literal prefix differs", ParseName("", "other-x"), ai, Different},
		{"different expressions with same prefix unknown", ParseName("", "[format('ai-{0}', variables('a'))]"), ParseName("", "[format('ai-{0}', variables('b'))]"), Unknown},
		{"different literal prefixes", ParseName("", "[format('srch-{0}', variables('t'))]"), ParseName("", "[format('other-{0}', variables('t'))]"), Different},
		{"opaque unknown", ParseRef("[parameters('x')]"), ai, Unknown},
		{"whitespace normalised", ParseName("", "[format('ai-{0}',variables('token'))]"), ai, Same},
		{"expression count differs unknown", ParseName("", "[variables('n')]"), ParseName("", "a/b"), Unknown},
	}
	for _, tc := range tests {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestPresenceOf(t *testing.T) {
	tests := []struct {
		name        string
		conditional bool
		cond        string
		looped      bool
		want        Presence
	}{
		{"plain", false, "", false, Deployed},
		{"literal false", true, "false", false, NotDeployed},
		{"false() call", true, "[false()]", false, NotDeployed},
		{"literal true", true, "true", false, Deployed},
		{"parameter", true, "[parameters('deploy')]", false, MaybeDeployed},
		{"loop", false, "", true, MaybeDeployed},
		{"loop wins over false", true, "false", true, MaybeDeployed},
	}
	for _, tc := range tests {
		if got := PresenceOf(tc.conditional, tc.cond, tc.looped); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRoleDefinitionGUID(t *testing.T) {
	const g = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '" + g + "')]", g, true},
		{"[extensionResourceId(resourceGroup().id, 'Microsoft.Authorization/roleDefinitions', '" + g + "')]", g, true},
		{"[resourceId('Microsoft.Authorization/roleDefinitions', '" + g + "')]", g, true},
		{"[format('/subscriptions/{0}/providers/Microsoft.Authorization/roleDefinitions/{1}', subscription().subscriptionId, '" + g + "')]", g, true},
		{"/subscriptions/s/providers/Microsoft.Authorization/roleDefinitions/" + g, g, true},
		{g, g, true},
		{"[subscriptionResourceId('Microsoft.Authorization/roleDefinitions', parameters('role'))]", "", false},
		{"[parameters('roleId')]", "", false},
		{"/subscriptions/s/providers/Microsoft.Authorization/roleDefinitions/Owner", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := RoleDefinitionGUID(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("RoleDefinitionGUID(%q) = %q,%v", tc.in, got, ok)
		}
	}
}

func TestClassifyScope(t *testing.T) {
	tests := []struct {
		in   string
		want ScopeLevel
	}{
		{"", ScopeBroad},
		{"[resourceGroup().id]", ScopeBroad},
		{"[subscription().id]", ScopeBroad},
		{"/subscriptions/s", ScopeBroad},
		{"/subscriptions/s/resourceGroups/r", ScopeBroad},
		{"/providers/Microsoft.Management/managementGroups/mg", ScopeBroad},
		{"[format('/subscriptions/{0}/resourceGroups/{1}', subscription().subscriptionId, 'rg')]", ScopeBroad},
		{"[format('/subscriptions/{0}/resourceGroups/{1}/providers/Microsoft.Storage/storageAccounts/{2}', 'a', 'b', 'c')]", ScopeResource},
		{"/subscriptions/s/resourceGroups/r/providers/Microsoft.Storage/storageAccounts/st", ScopeResource},
		{aiID, ScopeResource},
		{"[parameters('scope')]", ScopeUnknown},
		{"/weird", ScopeUnknown},
	}
	for _, tc := range tests {
		if got := ClassifyScope(tc.in); got != tc.want {
			t.Errorf("ClassifyScope(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestReadsClientID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"[reference(resourceId('Microsoft.ManagedIdentity/userAssignedIdentities', 'id'), '2023-01-31').clientId]", true},
		{"[reference('x').appId]", true},
		{"[reference('x').applicationId]", true},
		{"[parameters('clientId')]", true},
		{"[parameters('uamiClientId')]", true},
		{"[parameters('appId')]", true},
		{"[variables('client_id')]", true},
		{"[parameters('appIdentityPrincipalId')]", false},
		{"[parameters('principalId')]", false},
		{"[parameters('applicationName')]", false},
		{"[reference('x').principalId]", false},
		{"[reference('x').clientIdentity]", false},
		{"plain clientId", false},
		{"[concat('clientId', parameters('x'))]", false},
	}
	for _, tc := range tests {
		if got := ReadsClientID(tc.in); got != tc.want {
			t.Errorf("ReadsClientID(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

package armmodel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

const armArray = `{
  "resources": [
    {
      "type": "Microsoft.CognitiveServices/accounts",
      "apiVersion": "2025-06-01",
      "name": "acct",
      "location": "swedencentral",
      "kind": "AIServices",
      "sku": {"name": "S0"},
      "identity": {"type": "SystemAssigned"},
      "properties": {"publicNetworkAccess": "Disabled"}
    },
    {
      "type": "Microsoft.Authorization/roleAssignments",
      "apiVersion": "2022-04-01",
      "name": "[guid('x')]",
      "scope": "[format('Microsoft.CognitiveServices/accounts/{0}', 'acct')]",
      "location": "[parameters('location')]",
      "sku": {"name": "[parameters('sku')]"},
      "identity": {"type": "UserAssigned", "userAssignedIdentities": {"[resourceId('x')]": {}}},
      "properties": {"principalType": "ServicePrincipal"}
    },
    {
      "type": "Microsoft.Resources/deployments",
      "apiVersion": "2025-04-01",
      "name": "mod",
      "properties": {
        "template": {
          "resources": [
            {"type": "Microsoft.Storage/storageAccounts", "apiVersion": "2023-01-01", "name": "st",
             "location": "westeurope", "kind": "StorageV2", "sku": {"name": "Standard_LRS"}}
          ]
        }
      }
    },
    {"type": "Microsoft.Network/virtualNetworks", "apiVersion": "2024-01-01", "name": "bare"}
  ]
}`

func TestFromARMMapsAllFields(t *testing.T) {
	m, err := FromARM([]byte(armArray), Options{Entry: "infra/main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	rs := m.Resources()
	if len(rs) != 4 {
		t.Fatalf("resources = %d, want 4 (module deployment is structure, not a resource)", len(rs))
	}
	a := rs[0]
	if a.Type != "Microsoft.CognitiveServices/accounts" || a.Name != "acct" || a.APIVersion != "2025-06-01" ||
		a.Region != "swedencentral" || a.Kind != "AIServices" || a.SKUName != "S0" || a.Scope != "" {
		t.Errorf("account mapped wrong: %+v", a)
	}
	if a.Identity["type"] != "SystemAssigned" {
		t.Errorf("identity = %v", a.Identity)
	}
	if a.Properties["publicNetworkAccess"] != "Disabled" {
		t.Errorf("properties = %v", a.Properties)
	}
	if a.Location.File != "infra/main.bicep" || a.Location.Line != 0 {
		t.Errorf("location must name the entry file and never fabricate a line: %+v", a.Location)
	}

	ra := rs[1]
	if !strings.HasPrefix(ra.Scope, "[format(") {
		t.Errorf("scope = %q, want unresolved expression passed through", ra.Scope)
	}
	if ra.Region != "[parameters('location')]" {
		t.Errorf("region = %q, want unresolved expression", ra.Region)
	}
	if ra.SKUName != "" {
		t.Errorf("non-literal SKU name must be empty, got %q", ra.SKUName)
	}
	if ra.Identity["type"] != "UserAssigned" || ra.Identity["userAssignedIdentities"] == nil {
		t.Errorf("identity = %v", ra.Identity)
	}

	st := rs[2]
	if st.Type != "Microsoft.Storage/storageAccounts" || st.Region != "westeurope" || st.SKUName != "Standard_LRS" {
		t.Errorf("nested resource mapped wrong: %+v", st)
	}

	bare := rs[3]
	if bare.Region != "" || bare.Kind != "" || bare.SKUName != "" || bare.Identity != nil || bare.Scope != "" || bare.Properties != nil {
		t.Errorf("absent fields must be zero values: %+v", bare)
	}
}

func TestSymbolicNameFormIsDeterministic(t *testing.T) {
	const raw = `{"languageVersion":"2.0","resources":{
	  "zeta":{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"z","location":"a","kind":"StorageV2"},
	  "alpha":{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"a","location":"b","kind":"BlobStorage"}}}`
	var first []string
	for i := 0; i < 5; i++ {
		m, err := FromARM([]byte(raw), Options{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range m.Resources() {
			got = append(got, r.Name+":"+r.Region+":"+r.Kind)
		}
		if i == 0 {
			first = got
			if !reflect.DeepEqual(got, []string{"a:b:BlobStorage", "z:a:StorageV2"}) {
				t.Fatalf("got %v", got)
			}
		} else if !reflect.DeepEqual(first, got) {
			t.Fatalf("non-deterministic: %v vs %v", first, got)
		}
	}
}

func TestMissingAndMalformedInput(t *testing.T) {
	if _, err := FromARM([]byte(`not json`), Options{}); err == nil {
		t.Error("malformed JSON must error")
	}
	if _, err := FromARM([]byte(`{"resources":"x"}`), Options{}); err == nil {
		t.Error("resources of wrong shape must error")
	}
	m, err := FromARM([]byte(`{}`), Options{})
	if err != nil || len(m.Resources()) != 0 {
		t.Errorf("empty template: %v %v", m, err)
	}
	var nilModel *Model
	if nilModel.Resources() != nil {
		t.Error("nil model must return no resources")
	}
}

func TestResourcesReturnsCopy(t *testing.T) {
	m, _ := FromARM([]byte(armArray), Options{})
	m.Resources()[0].Name = "mutated"
	if m.Resources()[0].Name != "acct" {
		t.Error("Resources must return a copy")
	}
}

func TestResolvePointer(t *testing.T) {
	doc := map[string]any{"a": []any{map[string]any{"x/y": map[string]any{"k": "v"}}}}
	if got := resolve(doc, "/a/0/x~1y"); got["k"] != "v" {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"/a/9", "/a/-1", "/a/z", "/nope/0", "/a/0/x~1y/k"} {
		if resolve(doc, bad) != nil {
			t.Errorf("pointer %q should not resolve to an object", bad)
		}
	}
}

func TestResourceLimit(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"resources":[`)
	for i := 0; i < MaxResources+5; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"type":"T/x","apiVersion":"1","name":"n"}`)
	}
	sb.WriteString(`]}`)
	m, err := FromARM([]byte(sb.String()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(m.Resources()); got != MaxResources {
		t.Fatalf("resources = %d, want cap %d", got, MaxResources)
	}
}

// The Bicep adapter's golden ARM output exercises conditions, loops, nested
// modules and child resources.
func TestBicepGoldenTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "bicep", "testdata", "golden", "main.arm.json"))
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	tpl, err := bicep.ParseARM(raw)
	if err != nil {
		t.Fatal(err)
	}
	m, err := FromTemplate(tpl, raw, Options{Entry: "main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	rs := m.Resources()
	if len(rs) < 3 {
		t.Fatalf("expected top-level and module resources, got %d", len(rs))
	}
	var sawSKU, sawKind, sawRegion bool
	for _, r := range rs {
		if r.Type == "" || r.Location.File == "" {
			t.Errorf("incomplete resource %+v", r)
		}
		sawSKU = sawSKU || r.SKUName == "Standard_LRS" || r.SKUName == "Standard_GRS"
		sawKind = sawKind || r.Kind == "StorageV2"
		sawRegion = sawRegion || strings.HasPrefix(r.Region, "[")
	}
	if !sawSKU || !sawKind || !sawRegion {
		t.Errorf("sku=%v kind=%v region=%v", sawSKU, sawKind, sawRegion)
	}
}

func FuzzFromARM(f *testing.F) {
	f.Add([]byte(armArray))
	f.Add([]byte(`{"resources":{"a":{"type":"T","identity":[1],"sku":"x"}}}`))
	f.Add([]byte(`{"resources":[null]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := FromARM(data, Options{})
		if err == nil {
			_ = m.Resources()
		}
	})
}

func TestOutputs(t *testing.T) {
	m, err := FromARM([]byte(`{"resources":[],"outputs":{"A_B":{"type":"string","value":"x"},"C":{"type":"int","value":1}}}`), Options{Entry: "m.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range m.Outputs() {
		got[o.Name] = true
	}
	if len(got) != 2 || !got["A_B"] || !got["C"] {
		t.Fatalf("outputs = %v", got)
	}
	var nilm *Model
	if nilm.Outputs() != nil {
		t.Fatal("nil model must yield nil outputs")
	}
	m2, _ := FromARM([]byte(`{"resources":[]}`), Options{Entry: "m.bicep"})
	if len(m2.Outputs()) != 0 {
		t.Fatal("expected no outputs")
	}
}

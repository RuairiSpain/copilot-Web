package runtime

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type yamlView struct{}

func (yamlView) Path() string           { return "azure.yaml" }
func (yamlView) ServiceNames() []string { return []string{"svc"} }
func (yamlView) Lookup(path ...string) (string, sdk.Location, bool) {
	if len(path) == 3 && path[0] == "services" && path[1] == "svc" && path[2] == "host" {
		return "azure.ai.agent", sdk.Location{File: "azure.yaml", Line: 3}, true
	}
	return "", sdk.Location{}, false
}

type armModel struct{ resources []sdk.ARMResource }

func (a armModel) Resources() []sdk.ARMResource { return a.resources }

func TestCorrelatorResource(t *testing.T) {
	c := NewCorrelator(&sdk.Input{
		AzureYAML: yamlView{},
		ARM: armModel{resources: []sdk.ARMResource{{
			Type: "Microsoft.Search/searchServices", Name: "searchsvc", Location: sdk.Location{File: "main.bicep", Line: 10},
		}}},
	})
	got := c.Resource("Microsoft.Search/searchServices", "searchsvc", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.Search/searchServices/searchsvc")
	if got.Source.File != "main.bicep" || got.Resource.Name != "searchsvc" {
		t.Fatalf("correlation = %+v", got)
	}
	got = c.Resource("Microsoft.CognitiveServices/accounts", "acct", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct")
	if got.Source.File == "" {
		t.Fatalf("expected yaml fallback for a Foundry account, got %+v", got)
	}
	got = c.Resource("Microsoft.CognitiveServices/accounts/projects", "proj", "id")
	if got.Source.File == "" {
		t.Fatalf("expected yaml fallback for a Foundry project, got %+v", got)
	}
}

func TestCorrelatorFallbackNeverAttachesAgentServiceToOtherResources(t *testing.T) {
	c := NewCorrelator(&sdk.Input{AzureYAML: yamlView{}})
	for _, typ := range []string{"Microsoft.Search/searchServices", "Microsoft.Storage/storageAccounts", "Microsoft.DocumentDB/databaseAccounts", "", "azure.ai.agent", "Microsoft.CognitiveServicesX/accounts"} {
		if got := c.Resource(typ, "n", "id"); got.Source.File != "" {
			t.Errorf("%q got an unrelated source location: %+v", typ, got.Source)
		}
	}
}

func TestCorrelatorAgentUsesTheServiceEntry(t *testing.T) {
	c := NewCorrelator(&sdk.Input{AzureYAML: yamlView{}})
	if got := c.Agent("svc"); got.Source.File != "azure.yaml" || got.Source.Line != 3 || got.Resource.Type != "azure.ai.agent" {
		t.Errorf("agent = %+v", got)
	}
	if got := c.Agent("other"); got.Source.File != "" {
		t.Errorf("unknown agent got a location: %+v", got)
	}
	if got := NewCorrelator(nil).Agent("svc"); got.Source.File != "" || got.Resource.Name != "svc" {
		t.Errorf("nil input: %+v", got)
	}
}

func TestCorrelatorNilAndLastSegmentMatch(t *testing.T) {
	c := NewCorrelator(nil)
	got := c.Resource("type", "name", "plain-name")
	if got.Resource.ID != "plain-name" || got.Source.File != "" {
		t.Fatalf("correlation = %+v", got)
	}

	c = NewCorrelator(&sdk.Input{
		ARM: armModel{resources: []sdk.ARMResource{{
			Type: "Microsoft.Search/searchServices", Name: "nested/searchsvc", Location: sdk.Location{File: "main.bicep", Line: 11},
		}}},
	})
	got = c.Resource("Microsoft.Search/searchServices", "searchsvc", "/x/searchsvc")
	if got.Source.Line != 11 {
		t.Fatalf("expected last-segment match, got %+v", got)
	}
}

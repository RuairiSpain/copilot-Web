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
		t.Fatalf("expected yaml fallback, got %+v", got)
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

package foundrylink_test

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func searchServices(rs []sdk.ARMResource) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range rs {
		if strings.EqualFold(r.Type, "Microsoft.Search/searchServices") {
			out = append(out, r)
		}
	}
	return out
}

// B2: the connection module reuses the parameter name `name`; the Search
// module deployed for another purpose must not be reported as linked.
func TestUnrelatedSearchModuleIsNotLinked(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	set := foundrylink.Collect(m)
	if !set.Any() {
		t.Fatal("the connection module must be collected")
	}
	ss := searchServices(m.Resources())
	if len(ss) != 2 {
		t.Fatalf("want 2 search modules, got %d", len(ss))
	}
	if got := set.Classify(ss[0]); got != foundrylink.Linked {
		t.Errorf("search referenced by the connection: %v, want Linked", got)
	}
	if got := set.Classify(ss[1]); got != foundrylink.NotLinked {
		t.Errorf("unrelated search module: %v, want NotLinked", got)
	}
}

// B1: a conditional module's Search is only possibly linked, never definite.
func TestConditionalModuleLinkIsNotDefinite(t *testing.T) {
	m := rulestest.Scenario(t, "twice")
	rs := m.Resources()
	for i := range rs {
		if strings.HasSuffix(strings.ToLower(rs[i].Type), "/connections") {
			rs[i].Conditional, rs[i].Condition = true, "[parameters('deployConn')]"
		}
	}
	set := foundrylink.Collect(sliceModel(rs))
	ss := searchServices(rs)
	if got := set.Classify(ss[0]); got != foundrylink.Uncertain {
		t.Errorf("search linked only from a conditional connection: %v, want Uncertain", got)
	}
}

type sliceModel []sdk.ARMResource

func (s sliceModel) Resources() []sdk.ARMResource { return s }

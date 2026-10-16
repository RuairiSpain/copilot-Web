package view

import (
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// ModelResources converts the flattened ARM model into graph resources. The
// placements are parallel to the resources (armmodel.Model.Placements). Names
// are the model's, with the calling deployment's parameters substituted, and
// symbolic names and dependsOn entries are qualified by deployment path, so two
// instances of one module are two distinct nodes and a module's dependsOn never
// resolves into a sibling instance. A missing or short placements slice leaves
// the remaining resources without a symbolic name or dependsOn.
func ModelResources(res []sdk.ARMResource, ps []armmodel.Placement) []coregraph.Resource {
	out := make([]coregraph.Resource, 0, len(res))
	for i, r := range res {
		cr := coregraph.Resource{
			ID: "/resources/" + strconv.Itoa(i), Type: r.Type, Name: r.Name,
			Conditional: r.Conditional, Condition: r.Condition, Looped: r.Looped,
		}
		if i < len(ps) {
			cr.ID = ps[i].Pointer
			cr.SymbolicName = ps[i].Symbol
			cr.DependsOn = append([]string(nil), ps[i].DependsOn...)
			for _, path := range ps[i].ModuleDeps {
				for j := range ps {
					if j < len(res) && j != i && inModule(ps[j].Module, path) {
						cr.DependsOnIDs = append(cr.DependsOnIDs, ps[j].Pointer)
					}
				}
			}
		}
		out = append(out, cr)
	}
	return out
}

// inModule reports whether a resource declared in module path m is in module
// path of (the module itself or a module nested in it).
func inModule(m, of string) bool {
	return m == of || strings.HasPrefix(m, of+"/")
}

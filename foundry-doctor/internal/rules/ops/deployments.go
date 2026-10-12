package ops

import (
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// deployment is one model deployment read from azure.yaml (services with host azure.ai.project,
// deployments[]) or from Microsoft.CognitiveServices/accounts/deployments in the compiled template.
type deployment struct {
	Name                   string
	Format, Model, Version model.Value
	SKU                    model.Value
	Ref                    sdk.ResourceRef
	Loc                    sdk.Location
	Meta                   model.ResourceMeta // zero for azure.yaml deployments
	FromARM                bool
}

// yamlDeployments reads the azure.yaml deployments in document order.
func yamlDeployments(a *model.AzureYAML) []deployment {
	if a == nil {
		return nil
	}
	var out []deployment
	for _, svc := range a.Services {
		if svc.Host != hostProject || svc.Node == nil {
			continue
		}
		list := svc.Node.Lookup("deployments")
		if list == nil || list.Kind != model.NodeSequence {
			continue
		}
		for i, item := range list.Children {
			if item.Kind != model.NodeMapping {
				continue
			}
			d := deployment{
				Format:  scalarValue(item.Lookup("model", "format")),
				Model:   scalarValue(item.Lookup("model", "name")),
				Version: scalarValue(item.Lookup("model", "version")),
				SKU:     scalarValue(item.Lookup("sku", "name")),
			}
			if n := item.Lookup("name"); n != nil {
				d.Name = n.Value
			}
			ptr := "/deployments/" + strconv.Itoa(i)
			d.Ref = sdk.ResourceRef{Kind: "service", Type: svc.Host, Name: svc.Name + "/" + d.Name, Pointer: ptr}
			line, col := item.KeyPos.Line, item.KeyPos.Column
			if line == 0 {
				line, col = item.Pos.Line, item.Pos.Column
			}
			d.Loc = sdk.Location{File: a.File, Line: line, Column: col, Pointer: ptr}
			out = append(out, d)
		}
	}
	return out
}

// armDeployments reads the compiled template's model deployments.
func armDeployments(t *model.ARMTemplate) []deployment {
	var out []deployment
	for _, r := range t.ResourcesOfType(typeDeployment) {
		out = append(out, deployment{
			Name:    lastSegment(r.Name),
			Format:  r.Get("properties.model.format"),
			Model:   r.Get("properties.model.name"),
			Version: r.Get("properties.model.version"),
			SKU:     r.Get("sku.name"),
			Ref:     armRef(r), Loc: armLoc(r), Meta: r.Meta, FromARM: true,
		})
	}
	return out
}

// allDeployments returns azure.yaml deployments followed by template deployments.
func allDeployments(in *model.Input) []deployment {
	out := yamlDeployments(in.AzureYAML)
	if in.ARM != nil {
		out = append(out, armDeployments(in.ARM)...)
	}
	return out
}

func (d deployment) label() string {
	n := d.Name
	if n == "" {
		n = lastSegment(d.Ref.Name)
	}
	return strings.TrimSpace(n)
}

package runtime

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Correlator projects runtime resources back to source/deployed nodes.
type Correlator struct {
	azure sdk.AzureYAMLView
	arm   sdk.ARMModel
}

// NewCorrelator builds a correlator over the current rule input.
func NewCorrelator(in *sdk.Input) Correlator {
	if in == nil {
		return Correlator{}
	}
	return Correlator{azure: in.AzureYAML, arm: in.ARM}
}

// Resource returns the best-effort source/deployed correlation for typ, name and id.
func (c Correlator) Resource(typ, name, id string) Correlation {
	out := Correlation{Resource: sdk.ResourceRef{Type: typ, Name: name, ID: findings.Redact(id)}}
	if c.arm != nil {
		for _, r := range c.arm.Resources() {
			if !strings.EqualFold(r.Type, typ) {
				continue
			}
			if strings.EqualFold(r.Name, name) || strings.EqualFold(lastSegment(r.Name), lastSegment(name)) {
				out.Source = r.Location
				break
			}
		}
	}
	// The azure.yaml fallback only makes sense for the Foundry account and
	// project: a Search, Storage or Cosmos finding must not point at an
	// unrelated agent service.
	if c.azure != nil && out.Source.File == "" && isFoundryType(typ) {
		for _, svc := range c.azure.ServiceNames() {
			if host, loc, ok := c.azure.Lookup("services", svc, "host"); ok && (strings.Contains(strings.ToLower(host), "ai.") || strings.Contains(strings.ToLower(host), "foundry")) {
				out.Source = loc
				break
			}
		}
	}
	return out
}

func isFoundryType(typ string) bool {
	t := strings.ToLower(typ)
	return t == "microsoft.cognitiveservices/accounts" || strings.HasPrefix(t, "microsoft.cognitiveservices/accounts/")
}

// Agent returns the correlation of an azure.ai.agent service declared in
// azure.yaml: the source location is the service entry itself.
func (c Correlator) Agent(name string) Correlation {
	out := Correlation{Resource: sdk.ResourceRef{Type: "azure.ai.agent", Name: name}}
	if c.azure == nil {
		return out
	}
	for _, svc := range c.azure.ServiceNames() {
		host, loc, ok := c.azure.Lookup("services", svc, "host")
		if !ok || !strings.EqualFold(host, "azure.ai.agent") {
			continue
		}
		agentName := svc
		if n, _, ok := c.azure.Lookup("services", svc, "name"); ok && strings.TrimSpace(n) != "" {
			agentName = n
		}
		if strings.EqualFold(agentName, name) {
			out.Source = loc
			break
		}
	}
	return out
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

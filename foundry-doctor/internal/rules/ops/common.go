package ops

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Resource types shared by several rules of this package.
const (
	typeAccount     = "Microsoft.CognitiveServices/accounts"
	typeProject     = "Microsoft.CognitiveServices/accounts/projects"
	typeDeployment  = "Microsoft.CognitiveServices/accounts/deployments"
	typeSearch      = "Microsoft.Search/searchServices"
	typeStorage     = "Microsoft.Storage/storageAccounts"
	typeCosmos      = "Microsoft.DocumentDB/databaseAccounts"
	typeAPIM        = "Microsoft.ApiManagement/service"
	typeAppInsights = "Microsoft.Insights/components"
	typeWorkspace   = "Microsoft.OperationalInsights/workspaces"
	typeResGroup    = "Microsoft.Resources/resourceGroups"
	hostProject     = "azure.ai.project"
)

// armRef names an ARM resource by logical identity.
func armRef(r model.Resource) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "arm-resource", Type: r.Type, Name: r.Name, Pointer: r.Meta.Pointer}
}

// armLoc is the best-effort source location of an ARM resource.
func armLoc(r model.Resource) sdk.Location {
	return sdk.Location{File: r.File, Line: r.Location.Line, Column: r.Location.Column, Pointer: r.Meta.Pointer}
}

// lowered lowers the confidence of a finding on a resource that may not deploy or that is
// instantiated several times: the finding may not apply to the deployed estate.
func lowered(c sdk.Confidence, m model.ResourceMeta) sdk.Confidence {
	if !m.MayNotDeploy && !m.MultiInstance {
		return c
	}
	if c == sdk.ConfidenceCertain {
		return sdk.ConfidenceLikely
	}
	return sdk.ConfidenceUncertain
}

// projectRef names the project as a whole.
func projectRef(in *model.Input) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "project", Name: in.Project.Name}
}

// needARM returns the synthetic-infrastructure skip when the input has no compiled template.
// The engine short-circuits this too; the guard keeps the rule safe on its own.
func needARM(r rules.Rule, in *model.Input, what string) (rules.Result, bool) {
	if in.ARM != nil {
		return rules.Result{}, false
	}
	return rules.SkipFor(r, rules.ReasonSyntheticInfrastructure, "iac: "+string(in.Project.IaC)+"; "+what, "compiled ARM template",
		projectRef(in)), true
}

// lastSegment returns the part of a child resource name after the last slash.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// scalarValue reads a YAML scalar as a three-state value. A ${VAR} reference is unresolved.
func scalarValue(n *model.Node) model.Value {
	if n == nil || n.Kind != model.NodeScalar {
		return model.Value{}
	}
	if model.IsEnvReference(n.Value) {
		return model.Value{State: model.Unresolved, Reason: "env-reference"}
	}
	return model.Value{State: model.Literal, Lit: n.Value}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

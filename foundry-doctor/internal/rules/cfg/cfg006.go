package cfg

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule006 is FND-CFG-006: every ${VAR} reference has an environment or infrastructure producer.
// ${{...}} Foundry expressions, escaped forms and references with a default are not azd variables.
type rule006 struct{}

func (rule006) ID() string   { return "FND-CFG-006" }
func (rule006) Version() int { return 1 }

const (
	rec006 = "Set the variable in the active azd environment (azd env set), emit it from a Bicep output, give it an inline default, or remove the reference. Do not declare FOUNDRY_ or AGENT_ prefixed names in env."
	fix006 = "# Produce the value\n#   azd env set MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME gpt-5.4-mini\nservices:\n  my-agent:\n    host: azure.ai.agent\n    env:\n      MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME: ${MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME}\n      LOG_LEVEL: ${LOG_LEVEL:-info}\n"
)

// azdStandard are the variables azd sets itself.
var azdStandard = []string{"AZURE_ENV_NAME", "AZURE_LOCATION", "AZURE_SUBSCRIPTION_ID", "AZURE_RESOURCE_GROUP"}

var agentVar = regexp.MustCompile(`^AGENT_(.+)_(NAME|VERSION|ENDPOINT)$`)

// envNormalise maps a service name to the form used inside AGENT_<SERVICE>_*. The extension only says the
// name is "normalized" (no specification), so the match is likely, not certain.
func envNormalise(s string) string {
	return strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(s, "_"))
}

func (r rule006) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	ay := in.AzureYAML
	g := graph.Build(*in)

	agents := map[string]bool{}
	for _, s := range ay.Services {
		if s.Host == hostAgent {
			agents[envNormalise(s.Name)] = true
		}
	}

	// Reserved prefixes in service env.
	for _, s := range sortedServices(ay) {
		env := s.Node.Lookup("env")
		if env == nil || env.Kind != model.NodeMapping {
			continue
		}
		for _, c := range env.Children {
			if reservedEnv(c.Key) {
				x.fail(svcRef(s), x.loc(posOf(c, s.Pos)), "reserved-env:"+s.Name+":"+c.Key, sdk.ConfidenceLikely,
					fmt.Sprintf("service %s declares env key %s, whose FOUNDRY_ or AGENT_ prefix is reserved", s.Name, c.Key), rec006, fix006)
			}
		}
	}

	for _, ref := range g.EnvRefs() {
		if ref.Expression || ref.HasDefault || ref.Name == "" {
			continue
		}
		name := ref.Name
		if g.EnvironmentHas(name) || slices.Contains(azdStandard, name) || len(g.ProducersOf(model.NodeID(model.NodeEnvKey, name))) > 0 {
			continue
		}
		if m := agentVar.FindStringSubmatch(name); m != nil && agents[m[1]] {
			continue
		}
		res := r.owner(ay, ref.Owner)
		switch {
		case !g.HasEnvironment():
			x.skip(rules.ReasonUnresolvedValue("no-environment-selected"), "selected azd environment", name)
		case in.ARM == nil && in.Project.IaC == model.IaCSynthetic:
			x.skip(rules.ReasonSyntheticInfrastructure, "compiled ARM template", name)
		case in.ARM == nil && in.Project.HasARM():
			x.skip(rules.ReasonUnresolvedValue("compiled-arm-unavailable"), "compiled ARM template", name)
		default:
			x.fail(res, x.loc(ref.Pos), "ref:"+name, sdk.ConfidenceLikely,
				fmt.Sprintf("${%s} has no producer: not in the selected environment, not a standard azd variable, not an output of the compiled infrastructure", name),
				rec006, fix006)
		}
	}
	return x.done(), nil
}

func (rule006) owner(ay *model.AzureYAML, id string) sdk.ResourceRef {
	if name, ok := strings.CutPrefix(id, model.NodeService+":"); ok {
		if s, found := serviceByName(ay)[name]; found {
			return svcRef(s)
		}
	}
	return projectRef(ay)
}

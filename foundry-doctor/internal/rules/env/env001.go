package env

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// sharedResourceGroup is FND-ENV-001.
type sharedResourceGroup struct{ src Source }

func (sharedResourceGroup) ID() string   { return "FND-ENV-001" }
func (sharedResourceGroup) Version() int { return 1 }

const env001Rec = "Give each environment its own resource group, for example by including the environment name in the group name " +
	"(resourceGroup: rg-${AZURE_ENV_NAME}), or record an explicit suppression with a reason and expiry when sharing is intentional."

const env001Fix = "name: my-agent-project\nresourceGroup: rg-${AZURE_ENV_NAME}\n"

// Evaluate compares the resource group of every pair of visible environments from both sources of
// the verified precedence (phase-1-fact-check item 2): a service or project resourceGroup in
// azure.yaml wins when resolving a service target, AZURE_RESOURCE_GROUP is the fallback and the
// only value the Bicep provider reads. Group and subscription names are compared in process and
// never printed.
func (r sharedResourceGroup) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", r.ID(), err)
	}
	envs := visible(r.src, in)
	if len(envs) < 2 {
		return []rules.Result{skipSingle(r, in, len(envs))}, nil
	}
	var out []rules.Result
	var y *model.AzureYAML
	if in != nil {
		y = in.AzureYAML
	}
	settings := resourceGroupSettings(y)

	for _, s := range settings {
		if strings.TrimSpace(s.Raw) == "" || hasRef(s.Raw) {
			continue
		}
		f := rules.NewFinding(r, sdk.ResourceRef{Kind: s.Kind, Name: s.Name, Pointer: "/resourceGroup"},
			fmt.Sprintf("azure.yaml %s resourceGroup is a literal with no variable reference, so all %d visible environments (%s) target the same group. "+
				"For a service target this setting wins over AZURE_RESOURCE_GROUP.", s.ID, len(envs), names(envs)), env001Rec)
		f.Key = "literal-resourcegroup:" + s.ID
		f.Location = yamlLocation(y, s.Pos)
		f.Fix = env001Fix
		f.Confidence = sdk.ConfidenceLikely
		out = append(out, rules.Fail(f))
	}

	groups := make([]map[string]string, len(envs))
	for i, e := range envs {
		groups[i] = map[string]string{}
		if g := strings.TrimSpace(value(e, keyResourceGroup)); g != "" {
			groups[i][keyResourceGroup] = g
		}
		for _, s := range settings {
			if g := strings.TrimSpace(expand(s.Raw, e)); g != "" {
				groups[i][s.ID] = g
			}
		}
	}
	for i := range envs {
		for j := i + 1; j < len(envs); j++ {
			subI, subJ := strings.TrimSpace(value(envs[i], keySubscription)), strings.TrimSpace(value(envs[j], keySubscription))
			if !strings.EqualFold(subI, subJ) {
				continue
			}
			for _, src := range sortedKeys(groups[i]) {
				gj, ok := groups[j][src]
				if !ok || !strings.EqualFold(groups[i][src], gj) {
					continue
				}
				f := rules.NewFinding(r, sdk.ResourceRef{Kind: "env-key", Name: sourceKey(src)},
					fmt.Sprintf("Environments %s and %s resolve to the same subscription and resource group through %s. "+
						"The azure.yaml resourceGroup, when set, wins for service targets; the Bicep provider reads only %s.",
						envs[i].Name, envs[j].Name, sourceLabel(src), keyResourceGroup), env001Rec)
				f.Key = "shared-resourcegroup:" + envs[i].Name + "+" + envs[j].Name + ":" + src
				f.Location = envLocation(envs[i].Name)
				f.Fix = env001Fix
				f.Confidence = sdk.ConfidenceLikely
				out = append(out, rules.Fail(f))
			}
		}
	}
	return results(out), nil
}

func sourceKey(src string) string {
	if src == keyResourceGroup {
		return keyResourceGroup
	}
	return "resourceGroup"
}

func sourceLabel(src string) string {
	if src == keyResourceGroup {
		return keyResourceGroup
	}
	return "azure.yaml " + src + " resourceGroup"
}

func names(envs []model.Environment) string {
	n := make([]string, len(envs))
	for i, e := range envs {
		n[i] = e.Name
	}
	return nameList(n, 10)
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }

package normalise

import "github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"

type named interface{ GetName() string }

type layer[T any] struct {
	scope string
	items []T
}

// mergeNamed merges (scope, items) layers; later layers override earlier ones by name. It
// returns the merged list (order of first appearance) and name -> winning scope.
func mergeNamed[T named](layers []layer[T]) ([]T, map[string]string) {
	index := map[string]int{}
	var merged []T
	origins := map[string]string{}
	for _, l := range layers {
		for _, item := range l.items {
			name := item.GetName()
			if i, ok := index[name]; ok {
				merged[i] = item
			} else {
				index[name] = len(merged)
				merged = append(merged, item)
			}
			origins[name] = l.scope
		}
	}
	return merged, origins
}

type modelLayer struct {
	scope  string
	models config.ModelConfiguration
}

// mergeModels combines model configurations from broadest to narrowest scope:
// default is overridden, deployments merge by name, a non-empty allowed set replaces the
// inherited one, denied sets accumulate.
func mergeModels(layers []modelLayer) config.ModelConfiguration {
	var out config.ModelConfiguration
	var deploymentLayers []layer[config.ModelDeployment]
	for _, l := range layers {
		if l.models.Default != "" {
			out.Default = l.models.Default
		}
		if len(l.models.Allowed) > 0 {
			out.Allowed = append([]string(nil), l.models.Allowed...)
		}
		for _, d := range l.models.Denied {
			if !has(out.Denied, d) {
				out.Denied = append(out.Denied, d)
			}
		}
		deploymentLayers = append(deploymentLayers, layer[config.ModelDeployment]{l.scope, l.models.Deployments})
	}
	merged, _ := mergeNamed(deploymentLayers)
	out.Deployments = config.Clone(merged)
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

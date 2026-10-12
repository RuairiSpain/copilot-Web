package cfg

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule011 is FND-CFG-011: preview, deprecated and compatibility-only constructs, and extension versions
// older than the documented minimums. The construct table holds only what the catalogue sources state.
type rule011 struct{}

func (rule011) ID() string   { return "FND-CFG-011" }
func (rule011) Version() int { return 1 }

const (
	rec011 = "Move to the unified service-level shape: put env on the agent service, use kind voice, use host azure.ai.project instead of microsoft.foundry, drop fields the hosted-agent create request ignores, and declare requiredVersions for the extensions."
	fix011 = "requiredVersions:\n  extensions:\n    azure.ai.agents: \">=1.0.0-beta.8\"\n    azure.ai.projects: \">=1.0.0-beta.4\"\nservices:\n  my-agent:\n    host: azure.ai.agent\n    env:\n      MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME: ${MICROSOFT_FOUNDRY_MODEL_DEPLOYMENT_NAME}\n"
)

// extensionMinimums are the documented minimum extension versions (catalogue notes). Versions at which
// individual constructs appeared are not sourced and are not in this table.
var extensionMinimums = map[string]string{
	"azure.ai.agents":   "1.0.0-beta.8",
	"azure.ai.projects": "1.0.0-beta.4",
}

// ignoredHostedFields are accepted on a hosted agent but not used by the create request.
var ignoredHostedFields = []string{"displayName", "inputSchema", "outputSchema"}

var versionToken = regexp.MustCompile(`(<=|<|>=|>|=|~>|~|\^)?\s*v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)`)

// lowerBound returns the version a constraint such as ">=1.0.0-beta.2 <2.0.0" starts at. ok is false for
// "latest", an empty constraint or one with only upper bounds.
func lowerBound(c string) (string, bool) {
	for _, m := range versionToken.FindAllStringSubmatch(c, -1) {
		if strings.HasPrefix(m[1], "<") {
			continue
		}
		return m[2], true
	}
	return "", false
}

func (r rule011) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	ay := in.AzureYAML
	for _, s := range sortedServices(ay) {
		if s.Host == hostLegacyFoundry {
			x.fail(svcRef(s), x.loc(s.Pos), "host-microsoft-foundry:"+s.Name, sdk.ConfidenceCertain,
				fmt.Sprintf("service %s uses the legacy compatibility host microsoft.foundry", s.Name), rec011, fix011)
		}
		if s.Host != hostAgent {
			continue
		}
		if n := s.Node.Lookup("config"); n != nil {
			x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), "config:"+s.Name, sdk.ConfidenceCertain,
				fmt.Sprintf("service %s uses the deprecated service-level config block; env moved to the service level", s.Name), rec011, fix011)
		}
		if kindOf(s) == "prompt-voice" {
			x.fail(svcRef(s), x.loc(posOf(s.Node.Lookup("kind"), s.Pos)), "kind-prompt-voice:"+s.Name, sdk.ConfidenceCertain,
				fmt.Sprintf("service %s uses kind prompt-voice, a compatibility alias of voice", s.Name), rec011, fix011)
		}
		if kindOf(s) == "hosted" {
			for _, f := range ignoredHostedFields {
				if n := s.Node.Lookup(f); n != nil {
					x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), "ignored-field:"+s.Name+":"+f, sdk.ConfidenceCertain,
						fmt.Sprintf("hosted agent %s sets %s, which the create request ignores", s.Name, f), rec011, fix011)
				}
			}
		}
		if env := s.Node.Lookup("env"); env != nil && env.Kind == model.NodeMapping {
			for _, c := range env.Children {
				if reservedEnv(c.Key) {
					x.fail(svcRef(s), x.loc(posOf(c, s.Pos)), "reserved-env:"+s.Name+":"+c.Key, sdk.ConfidenceCertain,
						fmt.Sprintf("service %s declares env key %s, whose FOUNDRY_ or AGENT_ prefix is reserved", s.Name, c.Key), rec011, fix011)
				}
			}
		}
	}

	r.declared(x, ay)
	r.installed(x, in.Versions)
	return x.done(), nil
}

func (rule011) declared(x *run, ay *model.AzureYAML) {
	exts := ay.Root.Lookup("requiredVersions", "extensions")
	if exts == nil || exts.Kind != model.NodeMapping {
		return
	}
	for _, c := range exts.Children {
		floor, known := extensionMinimums[c.Key]
		v, isScalar := scalar(c)
		if !known || !isScalar || hasRef(v) {
			continue
		}
		lb, ok := lowerBound(v)
		if !ok {
			continue
		}
		if ge, err := rules.InRange(lb, ">="+floor); err == nil && !ge {
			x.fail(projectRef(ay), x.loc(posOf(c, model.Pos{})), "required-version:"+c.Key, sdk.ConfidenceCertain,
				fmt.Sprintf("requiredVersions.extensions.%s allows versions below the documented minimum %s", c.Key, floor), rec011, fix011)
		}
	}
}

func (rule011) installed(x *run, v model.Versions) {
	names := make([]string, 0, len(extensionMinimums))
	for n := range extensionMinimums {
		names = append(names, n)
	}
	slices.Sort(names)
	detected := false
	for _, n := range names {
		have := v.Extensions[n]
		if have == "" {
			continue
		}
		detected = true
		if ge, err := rules.InRange(have, ">="+extensionMinimums[n]); err == nil && !ge {
			x.fail(sdk.ResourceRef{Kind: "tool", Name: n}, sdk.Location{}, "installed-version:"+n, sdk.ConfidenceCertain,
				fmt.Sprintf("installed extension %s %s is older than the documented minimum %s", n, have, extensionMinimums[n]), rec011, fix011)
		}
	}
	if !detected {
		x.skip(rules.ReasonUnresolvedValue("installed-extension-versions-unavailable"), "installed azd extension versions", "azure.ai.agents, azure.ai.projects")
	}
}

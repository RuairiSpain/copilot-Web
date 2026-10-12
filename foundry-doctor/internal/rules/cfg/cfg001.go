package cfg

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule001 is FND-CFG-001: valid YAML, duplicate keys and the azure.ai.* schema constraints. The reader
// (internal/azureyaml) does the parsing and the keyword checks; this rule turns its data into findings.
type rule001 struct{}

func (rule001) ID() string   { return "FND-CFG-001" }
func (rule001) Version() int { return 1 }

// schemaVerified lists the extension versions whose schemas were vendored (catalogue compatibility).
var schemaVerified = map[string]string{
	"azure.ai.agents":   ">=1.0.0-beta.18 <=1.0.0-beta.18",
	"azure.ai.projects": ">=1.0.0-beta.13 <=1.0.0-beta.13",
}

const (
	rec001 = "Fix the YAML syntax or the reported schema violation. Add the yaml-language-server schema comment to azure.yaml so the editor flags the same errors, and install azure.ai.agents and azure.ai.projects at the versions declared in requiredVersions."
	fix001 = "# yaml-language-server: $schema=https://raw.githubusercontent.com/Azure/azure-dev/main/schemas/v1.0/azure.yaml.json\nname: my-agent-project\nservices:\n  ai-project:\n    host: azure.ai.project\n  my-agent:\n    host: azure.ai.agent\n    project: src/my-agent\n    uses: [ai-project]\n    kind: hosted\n"
)

func (r rule001) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) || in.YAMLReport == nil {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	rep, ay := in.YAMLReport, in.AzureYAML

	for _, d := range rep.Duplicates {
		pos := model.Pos{}
		lines := make([]string, 0, len(d.Positions))
		for i, p := range d.Positions {
			if i == 1 {
				pos = p
			}
			lines = append(lines, fmt.Sprint(p.Line))
		}
		x.fail(projectRef(ay), x.loc(pos), "duplicate:"+d.Path, sdk.ConfidenceCertain,
			fmt.Sprintf("mapping key %q at %s is defined %d times (lines %s)", d.Key, d.Path, len(d.Positions), strings.Join(lines, ", ")),
			rec001, fix001)
	}

	outside := installedOutsideSchema(in.Versions)
	refServices := map[string]bool{}
	for _, s := range ay.Services {
		if hasRefFile(s) {
			refServices[s.Name] = true
		}
	}
	for _, is := range rep.Issues {
		if is.Level != model.LevelError {
			continue
		}
		if outside != "" {
			x.skip(rules.ReasonUnsupportedVersion, "vendored schema for "+outside, "schema")
			continue
		}
		if svc := serviceOfPath(is.Path, refServices); svc != "" {
			x.skip(rules.ReasonUnresolvedValue("ref-file-not-followed"), capRefFile, svc)
			continue
		}
		// The evidence names the path, code and schema keyword; the issue message is not echoed.
		x.fail(projectRef(ay), x.loc(is.Pos), "schema:"+string(is.Code)+":"+is.Path, sdk.ConfidenceCertain,
			fmt.Sprintf("%s at %s (schema keyword %q)", is.Code, is.Path, is.Keyword), rec001, fix001)
	}
	return x.done(), nil
}

// installedOutsideSchema returns "name version" for the first detected extension whose version is not one
// the schema was vendored for, or "".
func installedOutsideSchema(v model.Versions) string {
	for _, name := range []string{"azure.ai.agents", "azure.ai.projects"} {
		have := v.Extensions[name]
		if have == "" {
			continue
		}
		if ok, err := rules.InRange(have, schemaVerified[name]); err != nil || !ok {
			return name + " " + have
		}
	}
	return ""
}

// serviceOfPath returns the service among refServices that the dotted issue path points into.
func serviceOfPath(path string, refServices map[string]bool) string {
	for name := range refServices {
		p := "services." + name
		if path == p || strings.HasPrefix(path, p+".") {
			return name
		}
	}
	return ""
}

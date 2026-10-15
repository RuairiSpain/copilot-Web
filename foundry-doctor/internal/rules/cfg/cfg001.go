package cfg

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Verified minimum extension versions (catalogue FND-CFG-001 sources).
var cfg001MinVersions = map[string]string{
	"azure.ai.agents":   "1.0.0-beta.18",
	"azure.ai.projects": "1.0.0-beta.13",
}

// Azure.yaml name rule from the azd schema (schemas/v1.0/azure.yaml.json):
// minLength 2 and pattern ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$.
var appNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func evalCFG001(in *sdk.Input) sdk.Result {
	d, ok0 := loadDoc(in)
	if !ok0 {
		return skip(sdk.SkipInputUnavailable)
	}
	var fs []sdk.Finding
	for _, dg := range d.Diagnostics() {
		if strings.HasPrefix(dg.Message, "duplicate key") {
			fs = append(fs, sdk.Finding{Location: sdk.Location{File: dg.Location.File, Line: dg.Location.Line, Column: dg.Location.Column}, Evidence: dg.Message})
		}
	}
	root := d.Resolved()
	switch nameNode := child(root, "name"); {
	case nameNode == nil:
		fs = append(fs, sdk.Finding{Location: loc(d, root), Evidence: "name is required"})
	case nameNode.Kind != yaml.ScalarNode:
		fs = append(fs, sdk.Finding{Location: loc(d, nameNode), Evidence: "name must be a string"})
	default:
		if n := scalar(nameNode); len(n) < 2 || !appNameRe.MatchString(n) {
			fs = append(fs, sdk.Finding{Location: loc(d, nameNode),
				Evidence: "name must be at least 2 characters of lowercase letters, numbers and hyphens, starting and ending with a letter or number"})
		}
	}
	// services is optional; when present the schema requires at least one entry.
	if svcNode := child(root, "services"); svcNode != nil && len(pairs(svcNode)) == 0 {
		fs = append(fs, sdk.Finding{Location: loc(d, svcNode), Evidence: "services must declare at least one service when present"})
	}
	for _, s := range serviceList(d) {
		fs = append(fs, cfg001Service(d, s)...)
	}
	return withNotes(fs, noteTexts(requiredVersionNotes(root, cfg001MinVersions)))
}

func cfg001Service(d document, s service) []sdk.Finding {
	var fs []sdk.Finding
	add := func(n *yaml.Node, f string, a ...any) { fs = append(fs, finding(d, s, n, fmt.Sprintf(f, a...))) }
	if s.Host == "" {
		add(s.Key, "service %q is missing required host", s.Name)
		return fs
	}
	switch s.Host {
	case hostAgent:
		if !has(s.Node, "project") {
			add(s.Key, "azure.ai.agent service %q requires project", s.Name)
		}
		switch {
		case s.isPromptAgent():
			if s.str("model") == "" {
				add(s.Key, "prompt agent %q requires model", s.Name)
			}
			if s.str("instructions") == "" {
				add(s.Key, "prompt agent %q requires instructions", s.Name)
			}
		case s.isVoiceAgent():
			if strings.EqualFold(s.str("modelType"), "hosted_agent") {
				add(s.get("modelType"), "voice agent %q uses unsupported modelType hosted_agent", s.Name)
			}
			if has(s.Node, "targetAgent") {
				add(s.get("targetAgent"), "voice agent %q uses unsupported targetAgent", s.Name)
			}
		}
	case hostProject:
		for _, k := range []string{"project", "runtime", "docker", "image", "config"} {
			if has(s.Node, k) {
				add(s.get(k), "azure.ai.project service %q must not set %s", s.Name, k)
			}
		}
	}
	return fs
}

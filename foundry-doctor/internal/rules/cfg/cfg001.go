package cfg

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
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
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	var fs []sdk.Finding
	var notes []string
	dupN := map[string]int{}
	for _, dg := range d.Diagnostics() {
		if dg.Message == azureyaml.MultiDocumentMessage {
			// azd decodes only the first document, so this cannot change what
			// azd loads: a warning-level note, never a failure.
			notes = append(notes, dg.Message)
			continue
		}
		if strings.HasPrefix(dg.Message, "duplicate key") {
			// The key is the mapping path, the message and the ordinal among
			// identical messages under that path, so two duplicates of one key
			// stay distinct without a line and a duplicate added under another
			// mapping does not renumber them.
			id := dg.Path + "\x00" + dg.Message
			dupN[id]++
			fs = append(fs, sdk.Finding{Key: fmt.Sprintf("duplicate-key:%s:%s#%d", dg.Path, dg.Message, dupN[id]),
				Location: sdk.Location{File: dg.Location.File, Line: dg.Location.Line, Column: dg.Location.Column}, Evidence: dg.Message})
		}
	}
	root := d.Resolved()
	switch nameNode := child(root, "name"); {
	case nameNode == nil:
		fs = append(fs, sdk.Finding{Key: "name-required", Location: loc(d, root), Evidence: "name is required"})
	case nameNode.Kind != yaml.ScalarNode:
		fs = append(fs, sdk.Finding{Key: "name-type", Location: loc(d, nameNode), Evidence: "name must be a string"})
	default:
		if n := scalar(nameNode); len(n) < 2 || !appNameRe.MatchString(n) {
			fs = append(fs, sdk.Finding{Key: "name-format", Location: loc(d, nameNode),
				Evidence: "name must be at least 2 characters of lowercase letters, numbers and hyphens, starting and ending with a letter or number"})
		}
	}
	// services is optional; when present the schema requires at least one entry.
	if svcNode := child(root, "services"); svcNode != nil && len(pairs(svcNode)) == 0 {
		fs = append(fs, sdk.Finding{Key: "services-empty", Location: loc(d, svcNode), Evidence: "services must declare at least one service when present"})
	}
	for _, s := range serviceList(d) {
		fs = append(fs, cfg001Service(d, s)...)
	}
	azdFs, azdNotes := cfg001AzdRange(d, root, in.AzdVersion)
	fs = append(fs, azdFs...)
	notes = append(notes, azdNotes...)
	return withNotes(fs, append(notes, noteTexts(requiredVersionNotes(root, cfg001MinVersions))...))
}

// clip shortens user text quoted in evidence.
func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// cfg001AzdRange checks requiredVersions.azd with the blang/semver/v4 grammar
// (docs/spikes/azd-required-versions.md; azd project.go:63-77). An invalid
// range and a known azd version outside the range both make azd fail to load
// the project, so both are findings. An unknown or unparseable azd version is
// an uncertain note. A local dev build (0.0.0-dev.0) is exempt, as in azd.
func cfg001AzdRange(d document, root *yaml.Node, azdVersion string) ([]sdk.Finding, []string) {
	n := child(child(root, "requiredVersions"), "azd")
	if n == nil || n.Tag == "!!null" {
		return nil, nil // nil pointer in azd: no constraint
	}
	if n.Kind != yaml.ScalarNode {
		return nil, []string{"requiredVersions.azd is not a string; the range was not checked"}
	}
	rng, st := parseAzdRange(n.Value)
	switch st {
	case parseInvalid:
		return []sdk.Finding{{Key: "required-versions-azd-invalid", Location: loc(d, n),
			Evidence: fmt.Sprintf("requiredVersions.azd %q is not a valid semver range; azd fails to load the project", clip(n.Value))}}, nil
	case parseUnverified:
		return nil, []string{"requiredVersions.azd uses a range form whose validity is not verified; the range was not checked"}
	}
	if strings.TrimSpace(azdVersion) == "" {
		return nil, []string{sdk.SkipAzdVersionUnknown + ": requiredVersions.azd is declared but the azd version is unknown (use --azd-version or FOUNDRY_DOCTOR_AZD_VERSION)"}
	}
	v, ok := parseGivenAzdVersion(azdVersion)
	if !ok {
		return nil, []string{"requiredVersions.azd is declared but the azd version is not a valid semantic version; the range was not checked"}
	}
	if isAzdDevBuild(v) || rng.contains(v) {
		return nil, nil
	}
	return []sdk.Finding{{Key: "required-versions-azd-out-of-range", Location: loc(d, n),
		Evidence: fmt.Sprintf("azd %s is outside requiredVersions.azd %q; azd refuses to load the project", v, clip(n.Value))}}, nil
}

func cfg001Service(d document, s service) []sdk.Finding {
	var fs []sdk.Finding
	add := func(key string, n *yaml.Node, f string, a ...any) {
		fs = append(fs, finding(d, s, n, key, fmt.Sprintf(f, a...)))
	}
	if s.Host == "" {
		add("host-missing", s.Key, "service %q is missing required host", s.Name)
		return fs
	}
	switch s.Host {
	case hostAgent:
		if !has(s.Node, "project") {
			add("agent-project", s.Key, "azure.ai.agent service %q requires project", s.Name)
		}
		switch {
		case s.isPromptAgent():
			if s.str("model") == "" {
				add("prompt-model", s.Key, "prompt agent %q requires model", s.Name)
			}
			if s.str("instructions") == "" {
				add("prompt-instructions", s.Key, "prompt agent %q requires instructions", s.Name)
			}
		case s.isVoiceAgent():
			if strings.EqualFold(s.str("modelType"), "hosted_agent") {
				add("voice-modelType", s.get("modelType"), "voice agent %q uses unsupported modelType hosted_agent", s.Name)
			}
			if has(s.Node, "targetAgent") {
				add("voice-targetAgent", s.get("targetAgent"), "voice agent %q uses unsupported targetAgent", s.Name)
			}
		}
	case hostProject:
		for _, k := range []string{"project", "runtime", "docker", "image", "config"} {
			if has(s.Node, k) {
				add("project-forbidden:"+k, s.get(k), "azure.ai.project service %q must not set %s", s.Name, k)
			}
		}
	}
	return fs
}

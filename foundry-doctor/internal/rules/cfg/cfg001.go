package cfg

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Verified minimum extension versions (catalogue FND-CFG-001 sources).
var cfg001MinVersions = map[string]string{
	"azure.ai.agents":   "1.0.0-beta.18",
	"azure.ai.projects": "1.0.0-beta.13",
}

var verRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?`)

// parseVer extracts major.minor.patch and beta number (-1 for a release).
func parseVer(s string) ([4]int, bool) {
	m := verRe.FindStringSubmatch(s)
	if m == nil {
		return [4]int{}, false
	}
	var v [4]int
	for i := 0; i < 3; i++ {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	v[3] = 1 << 30
	if m[4] != "" {
		v[3], _ = strconv.Atoi(m[4])
	}
	return v, true
}

func verLess(a, b [4]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// requiredVersionsBelow returns findings-ready entries (extension, node,
// constraint) whose constraint is parseable and below min.
func requiredVersionsBelow(root *yaml.Node, mins map[string]string) []nameAt {
	ext := child(child(root, "requiredVersions"), "extensions")
	var out []nameAt
	for _, p := range pairs(ext) {
		min, ok := mins[p.Key.Value]
		if !ok {
			continue
		}
		got, ok1 := parseVer(scalar(p.Val))
		want, ok2 := parseVer(min)
		if ok1 && ok2 && verLess(got, want) {
			out = append(out, nameAt{p.Key.Value, p.Val})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

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
	svcNode := child(d.Root(), "services")
	svcs := serviceList(d)
	if len(svcs) == 0 {
		n := svcNode
		fs = append(fs, sdk.Finding{Location: loc(d, n), Evidence: "services must declare at least one service"})
	}
	for _, s := range svcs {
		fs = append(fs, cfg001Service(d, s)...)
	}
	for _, e := range requiredVersionsBelow(d.Root(), cfg001MinVersions) {
		fs = append(fs, sdk.Finding{Location: loc(d, e.Node),
			Evidence: fmt.Sprintf("requiredVersions.extensions.%s %q is older than verified %s", e.Name, scalar(e.Node), cfg001MinVersions[e.Name])})
	}
	return ok(fs)
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

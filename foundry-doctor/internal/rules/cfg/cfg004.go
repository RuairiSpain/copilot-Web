package cfg

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Documented bounds (catalogue FND-CFG-004 sources).
const (
	cpuMin, cpuMax = 0.25, 4.0
	memMin, memMax = 0.5, 8.0
)

func parseGi(s string) (float64, bool) {
	t := strings.TrimSuffix(strings.TrimSpace(s), "Gi")
	v, err := strconv.ParseFloat(t, 64)
	return v, err == nil && strings.HasSuffix(strings.TrimSpace(s), "Gi")
}

func evalCFG004(in *sdk.Input, p Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	svcs := serviceList(d)
	all := byName(svcs)
	var fs []sdk.Finding
	for _, s := range svcs {
		if s.Host != hostAgent {
			continue
		}
		fs = append(fs, cfg004Agent(d, s, all, p)...)
	}
	return ok(fs)
}

func cfg004Agent(d document, s service, all map[string]service, p Providers) []sdk.Finding {
	var fs []sdk.Finding
	add := func(n *yaml.Node, f string, a ...any) { fs = append(fs, finding(d, s, n, fmt.Sprintf(f, a...))) }
	switch {
	case s.isPromptAgent():
		model := s.str("model")
		if model == "" {
			add(s.Key, "prompt agent %q has blank model", s.Name)
		}
		if s.str("instructions") == "" {
			add(s.Key, "prompt agent %q has blank instructions", s.Name)
		}
		if pr, found := projectOf(s, all); found && model != "" && !containsRef(model) {
			if names, known := deploymentNames(pr); known && !names[model] {
				add(s.get("model"), "model %q is not a deployment of project %q", model, pr.Name)
			}
		}
	case s.isVoiceAgent():
		if s.str("model") == "" && !has(s.Node, "conversationEngine") {
			add(s.Key, "voice agent %q needs model or conversationEngine", s.Name)
		}
	default:
		fs = append(fs, cfg004Hosted(d, s, p)...)
	}
	return fs
}

func cfg004Hosted(d document, s service, p Providers) []sdk.Finding {
	var fs []sdk.Finding
	add := func(n *yaml.Node, f string, a ...any) { fs = append(fs, finding(d, s, n, fmt.Sprintf(f, a...))) }
	proj := s.str("project")
	cc := s.get("codeConfiguration")
	img := s.str("image")
	if proj == "" {
		add(s.Key, "hosted agent %q requires project path", s.Name)
	} else if p.Layout != nil && !containsRef(proj) {
		if !p.Layout.DirExists(proj) {
			add(s.get("project"), "project directory %q does not exist", proj)
		} else if img == "" && cc == nil && !p.Layout.FileExists(path.Join(proj, "Dockerfile")) {
			add(s.get("project"), "no Dockerfile under %q and no image or codeConfiguration", proj)
		}
	}
	if cc != nil {
		if img != "" {
			add(cc, "codeConfiguration and image are both set")
		}
		for _, k := range []string{"runtime", "entryPoint"} {
			if str(cc, k) == "" {
				add(cc, "codeConfiguration is missing %s", k)
			}
		}
	}
	if n := s.get("cpu"); n != nil && !containsRef(scalar(n)) {
		if v, err := strconv.ParseFloat(scalar(n), 64); err != nil || v < cpuMin || v > cpuMax {
			add(n, "cpu %q is outside %v-%v", scalar(n), cpuMin, cpuMax)
		}
	}
	if n := s.get("memory"); n != nil && !containsRef(scalar(n)) {
		if v, okv := parseGi(scalar(n)); !okv || v < memMin || v > memMax {
			add(n, "memory %q is outside %vGi-%vGi", scalar(n), memMin, memMax)
		}
	}
	return fs
}

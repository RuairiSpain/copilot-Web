package cfg

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Documented bounds (catalogue FND-CFG-004 sources). cpu is in cores; memory is
// in GiB after unit normalisation.
const (
	cpuMin, cpuMax = 0.25, 4.0
	memMin, memMax = 0.5, 8.0
)

var (
	cpuRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(m?)$`)
	memRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$`)
)

// memUnitBytes maps the quantity suffixes the agent schema allows
// (^[0-9]+(\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$) to bytes.
var memUnitBytes = map[string]float64{
	"": 1, "Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50, "Ei": 1 << 60,
	"k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
}

// parseCPU returns cores for "2", "0.5" and "500m".
func parseCPU(s string) (float64, bool) {
	m := cpuRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	if m[2] == "m" {
		v /= 1000
	}
	return v, true
}

// parseGiB returns GiB for a memory quantity such as "512Mi", "1Gi" or "2G".
func parseGiB(s string) (float64, bool) {
	m := memRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v * memUnitBytes[m[2]] / (1 << 30), true
}

func evalCFG004(in *sdk.Input, p Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	svcs := serviceList(d)
	all := byName(svcs)
	var fs []sdk.Finding
	var notes []string
	for _, s := range svcs {
		if s.Host != hostAgent {
			continue
		}
		fs = append(fs, cfg004Agent(d, s, all, p, &notes)...)
	}
	return withNotes(fs, notes)
}

func cfg004Agent(d document, s service, all map[string]service, p Providers, notes *[]string) []sdk.Finding {
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
			names, known := deploymentNames(pr)
			switch {
			case !known:
				*notes = append(*notes, fmt.Sprintf("%s: model %q cannot be matched because deployments of project %q are not declared in azure.yaml", s.Name, model, pr.Name))
			case !names[model]:
				add(s.get("model"), "model %q is not a deployment of project %q", model, pr.Name)
			}
		}
	case s.isVoiceAgent():
		if s.str("model") == "" && !has(s.Node, "conversationEngine") {
			add(s.Key, "voice agent %q needs model or conversationEngine", s.Name)
		}
	default:
		fs = append(fs, cfg004Hosted(d, s, p, notes)...)
	}
	return fs
}

// Service-level keys that give a hosted agent a source other than a
// Dockerfile or image. The lead review's reference example uses runtime and
// startupCommand; skill is accepted for the same reason. Their semantics are
// not verified here, so they only suppress the Dockerfile finding.
var alternateSourceKeys = []string{"runtime", "startupCommand", "skill"}

func hasAlternateSource(s service) bool {
	for _, k := range alternateSourceKeys {
		if has(s.Node, k) {
			return true
		}
	}
	return false
}

func cfg004Hosted(d document, s service, p Providers, notes *[]string) []sdk.Finding {
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
		} else if img == "" && cc == nil && !p.Layout.FileExists(path.Join(proj, "Dockerfile")) && !hasAlternateSource(s) {
			*notes = append(*notes, fmt.Sprintf("%s: no Dockerfile under %q and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear", s.Name, proj))
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
	res := child(child(s.Node, "container"), "resources")
	if n := child(res, "cpu"); n != nil && n.Kind == yaml.ScalarNode && !containsRef(n.Value) {
		if v, okv := parseCPU(n.Value); !okv || v < cpuMin || v > cpuMax {
			add(n, "container.resources.cpu %q is outside %v-%v cores", n.Value, cpuMin, cpuMax)
		}
	}
	if n := child(res, "memory"); n != nil && n.Kind == yaml.ScalarNode && !containsRef(n.Value) {
		if v, okv := parseGiB(n.Value); !okv || v < memMin || v > memMax {
			add(n, "container.resources.memory %q is outside %vGi-%vGi", n.Value, memMin, memMax)
		}
	}
	return fs
}

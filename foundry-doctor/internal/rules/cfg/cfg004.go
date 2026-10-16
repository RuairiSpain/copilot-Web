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

// binaryUnitBytes lists the only memory suffixes that are range-checked: the
// binary forms Ki, Mi, Gi and Ti, which the documented bounds (GiB) cover
// unambiguously. Decimal suffixes (k, M, G, T, P, E), Pi/Ei and bare numbers
// match the schema pattern, but how the service reads them is not verified, so
// they are reported as uncertain, never as out of range.
var binaryUnitBytes = map[string]float64{
	"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40,
}

// quantity says how far a resource value could be judged.
type quantity int

const (
	quantityInvalid   quantity = iota // does not match the schema pattern
	quantityChecked                   // a form the bounds clearly cover
	quantityUncertain                 // matches the pattern, bounds do not clearly cover it
)

// parseCPU returns cores for a plain decimal such as "2" or "0.5". The "m"
// suffix matches the schema pattern but the documented bounds are stated in
// cores, so it is uncertain.
func parseCPU(s string) (float64, quantity) {
	m := cpuRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, quantityInvalid
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, quantityInvalid
	}
	if m[2] == "m" {
		return v / 1000, quantityUncertain
	}
	return v, quantityChecked
}

// parseGiB returns GiB for a binary-suffixed quantity such as "512Mi" or "1Gi".
func parseGiB(s string) (float64, quantity) {
	m := memRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, quantityInvalid
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, quantityInvalid
	}
	unit, binary := binaryUnitBytes[m[2]]
	if !binary {
		return 0, quantityUncertain
	}
	return v * unit / (1 << 30), quantityChecked
}

func evalCFG004(in *sdk.Input, p Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
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
	add := func(key string, n *yaml.Node, f string, a ...any) {
		fs = append(fs, finding(d, s, n, key, fmt.Sprintf(f, a...)))
	}
	switch {
	case s.isPromptAgent():
		model := s.str("model")
		if model == "" {
			add("prompt-model-blank", s.Key, "prompt agent %q has blank model", s.Name)
		}
		if s.str("instructions") == "" {
			add("prompt-instructions-blank", s.Key, "prompt agent %q has blank instructions", s.Name)
		}
		if pr, found := projectOf(s, all); found && model != "" && !containsRef(model) {
			names, known := deploymentNames(pr)
			switch {
			case !known:
				*notes = append(*notes, fmt.Sprintf("%s: model %q cannot be matched because deployments of project %q are not declared in azure.yaml", s.Name, model, pr.Name))
			case !names[model]:
				add("prompt-model-not-deployed", s.get("model"), "model %q is not a deployment of project %q", model, pr.Name)
			}
		}
	case s.isVoiceAgent():
		if s.str("model") == "" && !has(s.Node, "conversationEngine") {
			add("voice-model-or-engine", s.Key, "voice agent %q needs model or conversationEngine", s.Name)
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
	add := func(key string, n *yaml.Node, f string, a ...any) {
		fs = append(fs, finding(d, s, n, key, fmt.Sprintf(f, a...)))
	}
	proj := s.str("project")
	cc := s.get("codeConfiguration")
	img := s.str("image")
	if proj == "" {
		add("hosted-project-path", s.Key, "hosted agent %q requires project path", s.Name)
	} else if p.Layout != nil && !containsRef(proj) {
		if !p.Layout.DirExists(proj) {
			add("hosted-project-dir-missing", s.get("project"), "project directory %q does not exist", proj)
		} else if img == "" && cc == nil && !p.Layout.FileExists(path.Join(proj, "Dockerfile")) && !hasAlternateSource(s) {
			*notes = append(*notes, fmt.Sprintf("%s: no Dockerfile under %q and no image, codeConfiguration, runtime, startupCommand or skill; the agent source is unclear", s.Name, proj))
		}
	}
	if cc != nil {
		if img != "" {
			add("code-and-image", cc, "codeConfiguration and image are both set")
		}
		for _, k := range []string{"runtime", "entryPoint"} {
			if str(cc, k) == "" {
				add("codeconfiguration-missing:"+k, cc, "codeConfiguration is missing %s", k)
			}
		}
	}
	res := child(child(s.Node, "container"), "resources")
	if n := child(res, "cpu"); n != nil && n.Kind == yaml.ScalarNode && !containsRef(n.Value) {
		switch v, q := parseCPU(n.Value); {
		case q == quantityInvalid:
			add("cpu-pattern", n, "container.resources.cpu %q does not match the schema pattern for a quantity", n.Value)
		case q == quantityUncertain:
			*notes = append(*notes, fmt.Sprintf("%s: container.resources.cpu %q uses a form the documented core bounds do not clearly cover", s.Name, n.Value))
		case v < cpuMin || v > cpuMax:
			add("cpu-range", n, "container.resources.cpu %q is outside %v-%v cores", n.Value, cpuMin, cpuMax)
		}
	}
	if n := child(res, "memory"); n != nil && n.Kind == yaml.ScalarNode && !containsRef(n.Value) {
		switch v, q := parseGiB(n.Value); {
		case q == quantityInvalid:
			add("memory-pattern", n, "container.resources.memory %q does not match the schema pattern for a quantity", n.Value)
		case q == quantityUncertain:
			*notes = append(*notes, fmt.Sprintf("%s: container.resources.memory %q is not a Ki, Mi, Gi or Ti quantity; how the service reads it is not verified", s.Name, n.Value))
		case v < memMin || v > memMax:
			add("memory-range", n, "container.resources.memory %q is outside %vGi-%vGi", n.Value, memMin, memMax)
		}
	}
	return fs
}

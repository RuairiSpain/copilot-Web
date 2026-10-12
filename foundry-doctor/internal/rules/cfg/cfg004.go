package cfg

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule004 is FND-CFG-004: a prompt agent has a model that a project deployment provides, and a hosted
// agent has a coherent source or image and container resources inside the documented range.
type rule004 struct{}

func (rule004) ID() string   { return "FND-CFG-004" }
func (rule004) Version() int { return 1 }

const (
	rec004 = "Set model to the name of a deployment declared on the project, provide instructions for prompt agents, and for hosted agents provide either source with a Dockerfile, a codeConfiguration, or a prebuilt image."
	fix004 = "services:\n  ai-project:\n    host: azure.ai.project\n    deployments:\n      - name: gpt-5.4-mini\n        model: {format: OpenAI, name: gpt-5.4-mini, version: \"2026-03-17\"}\n        sku: {name: GlobalStandard, capacity: 10}\n  helper:\n    host: azure.ai.agent\n    project: src/helper\n    uses: [ai-project]\n    kind: prompt\n    name: helper\n    model: gpt-5.4-mini\n    instructions: Answer briefly.\n"
)

// Documented container bounds: cpu 0.25 to 4.0, memory 0.5Gi to 8.0Gi.
const (
	cpuMin, cpuMax = 0.25, 4.0
	memMin, memMax = 0.5 * (1 << 30), 8.0 * (1 << 30)
)

func (r rule004) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	by := serviceByName(in.AzureYAML)
	for _, s := range sortedServices(in.AzureYAML) {
		if s.Host != hostAgent {
			continue
		}
		switch kindOf(s) {
		case "prompt":
			r.prompt(x, by, s)
		case "voice", "prompt-voice":
			r.voice(x, s)
		case "hosted":
			r.hosted(x, s)
		}
	}
	return x.done(), nil
}

func (r rule004) bad(x *run, s model.Service, n *model.Node, key, ev string) {
	x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), key+":"+s.Name, sdk.ConfidenceCertain, ev, rec004, fix004)
}

func (r rule004) prompt(x *run, by map[string]model.Service, s model.Service) {
	if hasRefFile(s) && !hasField(s, "model") {
		x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
		return
	}
	modelName, mn := field(s, "model")
	switch {
	case mn == nil || (mn.Kind == model.NodeScalar && blank(modelName)):
		r.bad(x, s, mn, "prompt-model", fmt.Sprintf("prompt agent %s has no model deployment name", s.Name))
	case mn.Kind == model.NodeScalar && !hasRef(modelName):
		names, why := deployments(by, s)
		switch {
		case names[modelName]:
		case why != "":
			x.skip(rules.ReasonUnresolvedValue(why), "project deployment list", s.Name)
		default:
			r.bad(x, s, mn, "prompt-model-deployment", fmt.Sprintf("model of prompt agent %s is not a deployment of the project it uses", s.Name))
		}
	}
	if v, n := field(s, "instructions"); n == nil || (n.Kind == model.NodeScalar && blank(v)) {
		r.bad(x, s, n, "prompt-instructions", fmt.Sprintf("prompt agent %s has no instructions", s.Name))
	}
}

func (r rule004) voice(x *run, s model.Service) {
	if hasField(s, "conversationEngine") || hasRefFile(s) {
		return
	}
	if v, n := field(s, "model"); n == nil || (n.Kind == model.NodeScalar && blank(v)) {
		r.bad(x, s, n, "voice-model", fmt.Sprintf("voice agent %s has no model and no conversationEngine", s.Name))
	}
}

func (r rule004) hosted(x *run, s model.Service) {
	if hasRefFile(s) {
		x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
	}
	project, pn := field(s, "project")
	image, imageNode := field(s, "image")
	hasImage, hasCode := imageNode != nil && !blank(image), hasField(s, "codeConfiguration")
	if !hasRefFile(s) && (pn == nil || blank(project)) && !hasImage {
		r.bad(x, s, nil, "hosted-source", fmt.Sprintf("hosted agent %s has neither a project path nor an image", s.Name))
	}
	if hasImage && hasCode {
		r.bad(x, s, s.Node.Lookup("codeConfiguration"), "code-and-image", fmt.Sprintf("hosted agent %s sets both codeConfiguration and image", s.Name))
	}
	if hasCode {
		for _, f := range []string{"runtime", "entryPoint"} {
			v, n := field(s, "codeConfiguration", f)
			if n == nil || (n.Kind == model.NodeScalar && blank(v)) {
				r.bad(x, s, s.Node.Lookup("codeConfiguration"), "code-"+f, fmt.Sprintf("codeConfiguration of agent %s has no %s", s.Name, f))
			}
		}
	}
	if cpu, n := field(s, "container", "resources", "cpu"); n != nil && !hasRef(cpu) {
		if v, ok := parseCPU(cpu); !ok {
			r.bad(x, s, n, "cpu-parse", fmt.Sprintf("container cpu of agent %s is not a number", s.Name))
		} else if v < cpuMin || v > cpuMax {
			r.bad(x, s, n, "cpu-range", fmt.Sprintf("container cpu of agent %s is outside the documented range 0.25 to 4.0", s.Name))
		}
	}
	if mem, n := field(s, "container", "resources", "memory"); n != nil && !hasRef(mem) {
		if v, ok := parseMemory(mem); !ok {
			r.bad(x, s, n, "memory-parse", fmt.Sprintf("container memory of agent %s is not a quantity", s.Name))
		} else if v < memMin || v > memMax {
			r.bad(x, s, n, "memory-range", fmt.Sprintf("container memory of agent %s is outside the documented range 0.5Gi to 8.0Gi", s.Name))
		}
	}
	// The project directory and a Dockerfile are file-system facts; Input carries no file tree.
	if !hasImage && !hasCode && pn != nil && !blank(project) {
		x.skip(rules.ReasonUnresolvedValue("file-tree-unavailable"), capFileTree, s.Name)
	}
}

func parseCPU(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	div := 1.0
	if t, ok := strings.CutSuffix(s, "m"); ok {
		s, div = t, 1000
	}
	v, err := strconv.ParseFloat(s, 64)
	return v / div, err == nil && v >= 0
}

var memRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$`)

var memUnits = map[string]float64{
	"": 1, "Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50, "Ei": 1 << 60,
	"k": 1e3, "M": 1e6, "G": 1e9, "T": 1e12, "P": 1e15, "E": 1e18,
}

func parseMemory(s string) (float64, bool) {
	m := memRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v * memUnits[m[2]], err == nil
}

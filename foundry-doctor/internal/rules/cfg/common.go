// Package cfg implements the Phase 1 FND-CFG-* rules. All checks are static
// over azure.yaml (and optional, injected project/environment views); none
// call Azure.
package cfg

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Hosts used by the Foundry azd extensions.
const (
	hostAgent      = "azure.ai.agent"
	hostProject    = "azure.ai.project"
	hostConnection = "azure.ai.connection"
	hostToolbox    = "azure.ai.toolbox"
)

// Layout answers questions about the project directory. It is optional:
// rules that need it skip the dependent checks when it is nil.
type Layout interface {
	DirExists(rel string) bool
	FileExists(rel string) bool
}

// EnvView exposes the selected azd environment's variables. Values must never
// be logged or placed in evidence.
type EnvView interface {
	// Selected reports the selected environment name; ok is false when none.
	Selected() (name string, ok bool)
	// Values returns the .env key/value pairs of the selected environment.
	Values() map[string]string
}

// Providers carries the optional views a coordinator can supply.
type Providers struct {
	Layout Layout
	Env    EnvView
}

// document is the richer view of *azureyaml.Document the rules consume.
type document interface {
	sdk.AzureYAMLView
	Root() *yaml.Node
	Diagnostics() []azureyaml.Diagnostic
}

func skip(reason string) sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: reason}}
}

func loadDoc(in *sdk.Input) (document, bool) {
	if in == nil || in.AzureYAML == nil {
		return nil, false
	}
	d, ok := in.AzureYAML.(document)
	if !ok || d.Root() == nil {
		return nil, false
	}
	return d, true
}

func child(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			found = n.Content[i+1]
		}
	}
	return found
}

func has(n *yaml.Node, key string) bool { return child(n, key) != nil }

func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func str(n *yaml.Node, key string) string { return strings.TrimSpace(scalar(child(n, key))) }

type pair struct{ Key, Val *yaml.Node }

func pairs(n *yaml.Node) []pair {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []pair
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, pair{n.Content[i], n.Content[i+1]})
	}
	return out
}

func items(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

type nameAt struct {
	Name string
	Node *yaml.Node
}

// refNames returns names from a scalar, a sequence of scalars, or a sequence
// of mappings with a name key.
func refNames(n *yaml.Node) []nameAt {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []nameAt{{strings.TrimSpace(n.Value), n}}
	}
	var out []nameAt
	for _, it := range items(n) {
		switch it.Kind {
		case yaml.ScalarNode:
			out = append(out, nameAt{strings.TrimSpace(it.Value), it})
		case yaml.MappingNode:
			if nm := child(it, "name"); nm != nil {
				out = append(out, nameAt{strings.TrimSpace(scalar(nm)), nm})
			}
		}
	}
	return out
}

type service struct {
	Name string
	Node *yaml.Node
	Host string
	Key  *yaml.Node
}

func (s service) get(key string) *yaml.Node { return child(s.Node, key) }
func (s service) str(key string) string     { return str(s.Node, key) }
func (s service) kind() string              { return strings.ToLower(s.str("kind")) }

func (s service) isPromptAgent() bool { return s.Host == hostAgent && s.kind() == "prompt" }
func (s service) isVoiceAgent() bool {
	return s.Host == hostAgent && (s.kind() == "voice" || s.kind() == "prompt-voice")
}
func (s service) isHostedAgent() bool {
	return s.Host == hostAgent && !s.isPromptAgent() && !s.isVoiceAgent()
}

func (s service) uses() []string {
	var out []string
	for _, n := range refNames(s.get("uses")) {
		out = append(out, n.Name)
	}
	return out
}

func (s service) usesName(name string) bool {
	for _, u := range s.uses() {
		if u == name {
			return true
		}
	}
	return false
}

func serviceList(d document) []service {
	var out []service
	for _, p := range pairs(child(d.Root(), "services")) {
		if p.Val.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, service{Name: p.Key.Value, Node: p.Val, Host: str(p.Val, "host"), Key: p.Key})
	}
	return out
}

func byName(svcs []service) map[string]service {
	m := make(map[string]service, len(svcs))
	for _, s := range svcs {
		m[s.Name] = s
	}
	return m
}

// projectOf returns the azure.ai.project service the service uses (or is).
func projectOf(s service, all map[string]service) (service, bool) {
	if s.Host == hostProject {
		return s, true
	}
	for _, u := range s.uses() {
		if p, ok := all[u]; ok && p.Host == hostProject {
			return p, true
		}
	}
	return service{}, false
}

func loc(d document, n *yaml.Node) sdk.Location {
	l := sdk.Location{File: d.Path()}
	if n != nil {
		l.Line, l.Column = n.Line, n.Column
	}
	return l
}

// finding builds a finding anchored at n (or the service key when n is nil).
func finding(d document, s service, n *yaml.Node, evidence string) sdk.Finding {
	if n == nil {
		n = s.Key
	}
	f := sdk.Finding{Location: loc(d, n), Evidence: evidence}
	if s.Name != "" {
		f.Resource = sdk.ResourceRef{Type: s.Host, Name: s.Name}
	}
	return f
}

func ok(fs []sdk.Finding) sdk.Result { return sdk.Result{Findings: fs} }

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// varRefs extracts plain ${VAR} references. It ignores ${{...}} expressions,
// $${VAR} escapes and ${VAR:-default} inline defaults.
func varRefs(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "${{") {
			if end := strings.Index(s[i:], "}}"); end >= 0 {
				i += end + 1
			}
			continue
		}
		if !strings.HasPrefix(s[i:], "${") {
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return out
		}
		name := s[i+2 : i+end]
		i += end
		if varName.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

func containsRef(s string) bool { return strings.Contains(s, "${") }

// isWholeRef reports whether the entire value is a single ${VAR} or ${{...}}.
func isWholeRef(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
		return strings.Count(s, "${{") == 1
	}
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return strings.Count(s, "${") == 1 && strings.Count(s, "}") == 1
	}
	return false
}

// walkScalars calls fn for every scalar with its dotted path and map key.
func walkScalars(n *yaml.Node, path string, fn func(path, key string, v *yaml.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, p := range pairs(n) {
			sub := path + "." + p.Key.Value
			if p.Val.Kind == yaml.ScalarNode {
				fn(sub, p.Key.Value, p.Val)
			} else {
				walkScalars(p.Val, sub, fn)
			}
		}
	case yaml.SequenceNode:
		for i, it := range n.Content {
			sub := path + "[" + strconv.Itoa(i) + "]"
			if it.Kind == yaml.ScalarNode {
				fn(sub, "", it)
			} else {
				walkScalars(it, sub, fn)
			}
		}
	}
}

type rule struct {
	id   string
	eval func(*sdk.Input) sdk.Result
}

func (r rule) ID() string { return r.id }
func (r rule) Evaluate(_ context.Context, in *sdk.Input) (sdk.Result, error) {
	res := r.eval(in)
	if res.Skipped != nil {
		res.Skipped.RuleID = r.id
	}
	return res, nil
}

// Register returns all Phase 1 CFG rules without optional providers. Checks
// that need a layout or environment view are skipped.
func Register() []sdk.Rule { return RegisterWith(Providers{}) }

// RegisterWith returns all Phase 1 CFG rules using the supplied providers.
func RegisterWith(p Providers) []sdk.Rule {
	return []sdk.Rule{
		rule{"FND-CFG-001", evalCFG001},
		rule{"FND-CFG-002", evalCFG002},
		rule{"FND-CFG-003", evalCFG003},
		rule{"FND-CFG-004", func(in *sdk.Input) sdk.Result { return evalCFG004(in, p) }},
		rule{"FND-CFG-005", evalCFG005},
		rule{"FND-CFG-006", func(in *sdk.Input) sdk.Result { return evalCFG006(in, p) }},
		rule{"FND-CFG-007", evalCFG007},
		rule{"FND-CFG-011", evalCFG011},
		rule{"FND-CFG-012", func(in *sdk.Input) sdk.Result { return evalCFG012(in, p) }},
	}
}

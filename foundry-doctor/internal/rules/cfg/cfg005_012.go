package cfg

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var (
	secretAuth = map[string]bool{"apikey": true, "customkeys": true, "pat": true, "accesskey": true,
		"accountkey": true, "sas": true, "usernamepassword": true, "oauth2": true}
	secretKeyRe = regexp.MustCompile(`(?i)(key|secret|token|password|pwd|connection[_-]?string)`)
	// Names that look secret-bearing by substring but describe a quantity,
	// a rotation setting or a file location, not a credential.
	nonSecretNameRe = regexp.MustCompile(`(?i)(limit|count|rotation|size|file)`)
	// Only a signature makes a SAS query string a credential; sv is the
	// signed-version selector.
	sensQuery = map[string]bool{"sig": true, "token": true}
	// Literal secret shapes beyond findings.Redact.
	secretShapeRe = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`)
)

func isSecretKey(k string) bool { return secretKeyRe.MatchString(k) && !nonSecretNameRe.MatchString(k) }

// secretShaped reports whether a literal has the shape of a credential.
func secretShaped(v string) bool {
	return findings.Redact(v) != v || secretShapeRe.MatchString(v)
}

func evalCFG005(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	var fs []sdk.Finding
	var notes []string
	for _, s := range serviceList(d) {
		if s.Host == hostConnection && secretAuth[strings.ToLower(s.str("authType"))] {
			for _, p := range pairs(s.get("credentials")) {
				shape, _ := classifyValue(scalar(p.Val))
				switch shape {
				case shapeLiteral:
					fs = append(fs, finding(d, s, p.Val, fmt.Sprintf("credentials.%s is a literal; use a ${VAR} reference", p.Key.Value)))
				case shapeDefaultLiteral:
					fs = append(fs, finding(d, s, p.Val, fmt.Sprintf("credentials.%s has a literal default; remove the default after :-", p.Key.Value)))
				case shapeMixed:
					notes = append(notes, fmt.Sprintf("%s: credentials.%s mixes literal text and a reference", s.Name, p.Key.Value))
				}
			}
		}
		for _, p := range pairs(s.get("env")) {
			shape, lit := classifyValue(scalar(p.Val))
			named := isSecretKey(p.Key.Value)
			switch {
			case (shape == shapeLiteral || shape == shapeDefaultLiteral) && secretShaped(lit):
				fs = append(fs, finding(d, s, p.Val, fmt.Sprintf("env.%s holds a literal with the shape of a credential", p.Key.Value)))
			case named && shape != shapeEmpty && shape != shapeRef:
				notes = append(notes, fmt.Sprintf("%s: env.%s looks secret-bearing by name only", s.Name, p.Key.Value))
			}
		}
		for _, u := range urlFields(s) {
			if pu, err := url.Parse(scalar(u.Node)); err == nil && !containsRef(scalar(u.Node)) {
				bad := pu.User != nil
				for q := range pu.Query() {
					bad = bad || sensQuery[strings.ToLower(q)]
				}
				if bad {
					fs = append(fs, finding(d, s, u.Node, fmt.Sprintf("%s embeds credentials in the URL", u.Name)))
				}
			}
		}
	}
	return withNotes(fs, notes)
}

// urlFields returns URL-bearing scalar nodes of a service in stable order.
func urlFields(s service) []nameAt {
	var out []nameAt
	for _, k := range []string{"target", "endpoint", "url", "serverUrl"} {
		if n := s.get(k); n != nil && n.Kind == yaml.ScalarNode {
			out = append(out, nameAt{k, n})
		}
	}
	for i, t := range items(s.get("tools")) {
		for _, k := range []string{"endpoint", "url", "serverUrl", "target"} {
			if n := child(t, k); n != nil && n.Kind == yaml.ScalarNode {
				out = append(out, nameAt{fmt.Sprintf("tools[%d].%s", i, k), n})
			}
		}
	}
	for _, p := range pairs(s.get("env")) {
		if v := scalar(p.Val); strings.Contains(v, "://") {
			out = append(out, nameAt{"env." + p.Key.Value, p.Val})
		}
	}
	return out
}

func riskyHost(h string) bool {
	h = strings.ToLower(strings.Trim(h, "[]"))
	switch h {
	case "localhost", "metadata.google.internal", "fd00:ec2::254":
		return true
	}
	if strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate())
}

func evalCFG007(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	var fs []sdk.Finding
	for _, s := range serviceList(d) {
		for _, u := range urlFields(s) {
			v := strings.TrimSpace(scalar(u.Node))
			if v == "" || containsRef(v) || !strings.Contains(v, "://") {
				continue
			}
			pu, err := url.Parse(v)
			if err != nil {
				continue
			}
			if !strings.EqualFold(pu.Scheme, "https") {
				fs = append(fs, finding(d, s, u.Node, fmt.Sprintf("%s uses non-https scheme %q", u.Name, pu.Scheme)))
			}
			if riskyHost(pu.Hostname()) {
				fs = append(fs, finding(d, s, u.Node, fmt.Sprintf("%s targets local/private host %q", u.Name, pu.Hostname())))
			}
		}
	}
	return ok(fs)
}

// Verified minimum extension versions for FND-CFG-011 (catalogue sources).
var cfg011MinVersions = map[string]string{
	"azure.ai.agents":   "1.0.0-beta.8",
	"azure.ai.projects": "1.0.0-beta.4",
}

func evalCFG011(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	var fs []sdk.Finding
	for _, s := range serviceList(d) {
		add := func(n *yaml.Node, m string) { fs = append(fs, finding(d, s, n, m)) }
		if strings.EqualFold(s.Host, "microsoft.foundry") {
			add(s.Key, "host microsoft.foundry is a legacy shape")
		}
		if s.Host != hostAgent {
			continue
		}
		if has(s.Node, "config") {
			add(s.get("config"), "config on azure.ai.agent is a legacy shape")
		}
		if s.kind() == "prompt-voice" {
			add(s.get("kind"), "kind prompt-voice is a legacy shape")
		}
		if s.isHostedAgent() {
			for _, k := range []string{"displayName", "inputSchema", "outputSchema"} {
				if has(s.Node, k) {
					add(s.get(k), k+" on a hosted agent is a legacy shape")
				}
			}
		}
		for _, p := range pairs(s.get("env")) {
			if strings.HasPrefix(p.Key.Value, "FOUNDRY_") || strings.HasPrefix(p.Key.Value, "AGENT_") {
				add(p.Key, "env key "+p.Key.Value+" uses a reserved prefix")
			}
		}
	}
	return withNotes(fs, noteTexts(requiredVersionNotes(d.Resolved(), cfg011MinVersions)))
}

// Variable names azd core defines as environment keys (azd source,
// cli/azd/pkg/environment/environment.go). These are producers by definition.
var standardVars = map[string]bool{
	"AZURE_ENV_NAME": true, "AZURE_LOCATION": true, "AZURE_SUBSCRIPTION_ID": true,
	"AZURE_RESOURCE_GROUP": true, "AZURE_TENANT_ID": true,
	"AZURE_PRINCIPAL_ID": true, "AZURE_PRINCIPAL_TYPE": true,
}

// Names azd also defines in the same source but that only exist when the
// infrastructure or host provides them. They are not known producers; a
// missing one is uncertain, not an error.
var infraDependentVars = map[string]bool{
	"AZURE_CONTAINER_REGISTRY_ENDPOINT":               true,
	"AZURE_CONTAINER_APPS_ENVIRONMENT_DEFAULT_DOMAIN": true,
	"AZURE_APP_SERVICE_DASHBOARD_URI":                 true,
	"AZURE_AKS_CLUSTER_NAME":                          true,
	"AZD_PLATFORM_TYPE":                               true,
}

func evalCFG006(in *sdk.Input, p Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd || p.Env == nil {
		return skip(sdk.SkipInputUnavailable)
	}
	if _, sel := p.Env.Selected(); !sel {
		return skip(sdk.SkipInputUnavailable)
	}
	have := map[string]bool{}
	for k := range p.Env.Values() {
		have[k] = true
	}
	// Bicep/ARM outputs are an additional producer class. Without a compiled
	// model, or with a model that exposes no outputs, the set of outputs is
	// unknown and an unresolved name cannot be called undefined.
	outputsKnown := false
	outputOnly := map[string]bool{}
	if in.ARM != nil {
		if o, isOut := in.ARM.(sdk.ARMOutputs); isOut {
			outputsKnown = true
			for _, out := range o.Outputs() {
				for _, n := range []string{out.Name, strings.ToUpper(out.Name)} {
					if !have[n] {
						outputOnly[n] = true
					}
				}
			}
		}
	}
	for _, s := range serviceList(d) {
		up := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(s.Name))
		for _, sfx := range []string{"_NAME", "_VERSION", "_ENDPOINT"} {
			have["AGENT_"+up+sfx] = true
		}
	}
	var fs []sdk.Finding
	var notes []string
	seen := map[string]bool{}
	walkScalars(d.Resolved(), "", func(path, _ string, v *yaml.Node) {
		for _, name := range varRefs(v.Value) {
			if have[name] || standardVars[name] || seen[name] {
				continue
			}
			seen[name] = true
			switch {
			case outputOnly[name]:
				notes = append(notes, fmt.Sprintf("${%s} is produced by a template output that is not in the environment yet (provision has not written it)", name))
			case infraDependentVars[name]:
				notes = append(notes, fmt.Sprintf("${%s} is an azd-defined name that only exists when the infrastructure provides it", name))
			case !outputsKnown:
				notes = append(notes, fmt.Sprintf("${%s} is not in the environment and no compiled template outputs are available to check", name))
			default:
				fs = append(fs, sdk.Finding{Location: loc(d, v), Evidence: fmt.Sprintf("%s references ${%s} which no producer defines", path, name)})
			}
		}
	})
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Evidence < fs[j].Evidence })
	return withNotes(fs, notes)
}

func evalCFG012(in *sdk.Input, p Providers) sdk.Result {
	if p.Env == nil {
		return skip(sdk.SkipInputUnavailable)
	}
	if _, sel := p.Env.Selected(); !sel {
		return skip(sdk.SkipInputUnavailable)
	}
	v, present := p.Env.Values()["AZURE_LOCATION"]
	if !present || containsRef(v) {
		return skip(sdk.SkipInputUnavailable)
	}
	f := sdk.Finding{}
	switch {
	case strings.TrimSpace(v) == "":
		f.Evidence = "AZURE_LOCATION is empty"
	case v != strings.ToLower(v) || strings.ContainsAny(v, " \t"):
		f.Evidence = "AZURE_LOCATION must be a lowercase region name without spaces (for example eastus)"
	default:
		return ok(nil)
	}
	return ok([]sdk.Finding{f})
}

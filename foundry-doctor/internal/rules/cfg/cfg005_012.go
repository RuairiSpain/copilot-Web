package cfg

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var (
	secretAuth = map[string]bool{"apikey": true, "customkeys": true, "pat": true, "accesskey": true,
		"accountkey": true, "sas": true, "usernamepassword": true, "oauth2": true}
	secretKeyRe = regexp.MustCompile(`(?i)(key|secret|token|password|pwd|connection[_-]?string)`)
	sensQuery   = map[string]bool{"sig": true, "sv": true, "token": true}
)

func isSecretKey(k string) bool { return secretKeyRe.MatchString(k) }

func evalCFG005(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	var fs []sdk.Finding
	for _, s := range serviceList(d) {
		if s.Host == hostConnection && secretAuth[strings.ToLower(s.str("authType"))] {
			for _, p := range pairs(s.get("credentials")) {
				if v := scalar(p.Val); v != "" && !isWholeRef(v) {
					fs = append(fs, finding(d, s, p.Val, fmt.Sprintf("credentials.%s is a literal; use a ${VAR} reference", p.Key.Value)))
				}
			}
		}
		for _, p := range pairs(s.get("env")) {
			if v := scalar(p.Val); v != "" && isSecretKey(p.Key.Value) && !containsRef(v) {
				fs = append(fs, finding(d, s, p.Val, fmt.Sprintf("env.%s looks secret-bearing but is a literal", p.Key.Value)))
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
	return ok(fs)
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
	for _, e := range requiredVersionsBelow(d.Root(), cfg011MinVersions) {
		fs = append(fs, sdk.Finding{Location: loc(d, e.Node),
			Evidence: fmt.Sprintf("requiredVersions.extensions.%s %q is below documented %s", e.Name, scalar(e.Node), cfg011MinVersions[e.Name])})
	}
	return ok(fs)
}

var standardVars = map[string]bool{"AZURE_ENV_NAME": true, "AZURE_LOCATION": true,
	"AZURE_SUBSCRIPTION_ID": true, "AZURE_RESOURCE_GROUP": true}

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
	// Bicep/ARM outputs are an additional producer class; azd maps template
	// outputs to environment values of the same name.
	if o, isOut := in.ARM.(sdk.ARMOutputs); isOut {
		for _, out := range o.Outputs() {
			have[out.Name] = true
			have[strings.ToUpper(out.Name)] = true
		}
	}
	for _, s := range serviceList(d) {
		up := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(s.Name))
		for _, sfx := range []string{"_NAME", "_VERSION", "_ENDPOINT"} {
			have["AGENT_"+up+sfx] = true
		}
	}
	var fs []sdk.Finding
	seen := map[string]bool{}
	walkScalars(d.Root(), "", func(path, _ string, v *yaml.Node) {
		for _, name := range varRefs(v.Value) {
			if !have[name] && !standardVars[name] && !seen[name] {
				seen[name] = true
				fs = append(fs, sdk.Finding{Location: loc(d, v), Evidence: fmt.Sprintf("%s references ${%s} which no producer defines", path, name)})
			}
		}
	})
	for k := range p.Env.Values() {
		if strings.HasPrefix(k, "FOUNDRY_") || strings.HasPrefix(k, "AGENT_") {
			fs = append(fs, sdk.Finding{Evidence: "environment key " + k + " uses a reserved prefix"})
		}
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Evidence < fs[j].Evidence })
	return ok(fs)
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

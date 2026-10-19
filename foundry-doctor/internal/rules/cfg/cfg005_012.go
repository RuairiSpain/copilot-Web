package cfg

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var (
	secretAuth = map[string]bool{"apikey": true, "customkeys": true, "pat": true, "accesskey": true,
		"accountkey": true, "sas": true, "usernamepassword": true, "oauth2": true}
	secretKeyRe = regexp.MustCompile(`(?i)(key|secret|token|password|pwd|connection[_-]?string)`)
	// Names that look secret-bearing by substring but describe a quantity,
	// a rotation setting or a file location, not a credential.
	// Matched as whole `_`-delimited tokens, never as substrings: ACCOUNT
	// contains COUNT and PROFILE contains FILE, and neither is excluded.
	nonSecretTokens = map[string]bool{"LIMIT": true, "LIMITS": true, "COUNT": true, "SIZE": true,
		"FILE": true, "ROTATION": true, "TOKENS": true, "DAYS": true}
	// Only a signature makes a SAS query string a credential; sv is the
	// signed-version selector.
	sensQuery = map[string]bool{"sig": true, "token": true}
	// Literal secret shapes beyond findings.Redact.
	secretShapeRe = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`)
)

func isSecretKey(k string) bool {
	if !secretKeyRe.MatchString(k) {
		return false
	}
	toks := strings.FieldsFunc(strings.ToUpper(k), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	// A quantity or file-location token excludes the name only when it comes
	// after the last credential word (API_KEY_FILE, KEY_ROTATION_DAYS,
	// MAX_TOKENS). FILE_SECRET still names a credential.
	last := -1
	for i, tok := range toks {
		if secretKeyRe.MatchString(tok) || (tok == "STRING" && i > 0 && toks[i-1] == "CONNECTION") {
			last = i
		}
	}
	for i, tok := range toks {
		if nonSecretTokens[tok] && i >= last {
			return false
		}
	}
	return true
}

// secretShaped reports whether a literal has the shape of a credential.
func secretShaped(v string) bool {
	return findings.Redact(v) != v || secretShapeRe.MatchString(v)
}

func evalCFG005(in *sdk.Input, pr Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		// The compiled template can still be scanned without azure.yaml.
		if armFs := cfg005ARM(in); len(armFs) > 0 {
			return ok(armFs)
		}
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	var fs []sdk.Finding
	var notes []string
	for _, s := range serviceList(d) {
		if s.Host == hostConnection && secretAuth[strings.ToLower(s.str("authType"))] {
			for _, p := range pairs(s.get("credentials")) {
				shape, _ := classifyValue(scalar(p.Val))
				switch shape {
				case shapeLiteral:
					fs = append(fs, finding(d, s, p.Val, "credential-literal:"+p.Key.Value, fmt.Sprintf("credentials.%s is a literal; use a ${VAR} reference", p.Key.Value)))
				case shapeDefaultLiteral:
					fs = append(fs, finding(d, s, p.Val, "credential-default-literal:"+p.Key.Value, fmt.Sprintf("credentials.%s has a literal default; remove the default after :-", p.Key.Value)))
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
				fs = append(fs, finding(d, s, p.Val, "env-credential-shape:"+p.Key.Value, fmt.Sprintf("env.%s holds a literal with the shape of a credential", p.Key.Value)))
			case (shape == shapeLiteral || shape == shapeDefaultLiteral) && endsInCredentialWord(p.Key.Value) && plausibleLiteralSecret(lit):
				// A name that ends in a credential word with a literal that is
				// not a placeholder, URL or plain word. Not a pure pattern match,
				// so the finding carries lower confidence.
				f := finding(d, s, p.Val, "env-secret-name-literal:"+p.Key.Value, fmt.Sprintf("env.%s is named like a credential and holds a literal that is not a reference or placeholder", p.Key.Value))
				f.Confidence = sdk.ConfidenceMedium
				fs = append(fs, f)
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
					fs = append(fs, finding(d, s, u.Node, "url-credentials:"+u.Name, fmt.Sprintf("%s embeds credentials in the URL", u.Name)))
				}
			}
		}
	}
	fs = append(fs, cfg005ARM(in)...)
	notes = append(notes, cfg005Env(pr)...)
	return withNotes(fs, notes)
}

// URL-typed keys, from the vendored azd schemas (descriptions say URL or
// endpoint) and the FND-CFG-007 catalogue sources. serviceURLKeys sit on the
// service; toolURLKeys on tools[] entries (tool items allow additional
// properties, so server_url is known from extension source only);
// publishURLKeys are the format: uri fields of activity.publish.
var (
	serviceURLKeys = []string{"target", "endpoint", "url", "serverUrl", "authorizationUrl", "tokenUrl", "refreshUrl"}
	toolURLKeys    = []string{"endpoint", "url", "serverUrl", "server_url", "target"}
	publishURLKeys = []string{"developerWebsiteUrl", "privacyUrl", "termsOfUseUrl"}
)

// urlFields returns URL-bearing scalar nodes of a service in stable order.
func urlFields(s service) []nameAt {
	var out []nameAt
	for _, k := range serviceURLKeys {
		if n := s.get(k); n != nil && n.Kind == yaml.ScalarNode {
			out = append(out, nameAt{k, n})
		}
	}
	for i, t := range items(s.get("tools")) {
		for _, k := range toolURLKeys {
			if n := child(t, k); n != nil && n.Kind == yaml.ScalarNode {
				out = append(out, nameAt{fmt.Sprintf("tools[%d].%s", i, k), n})
			}
		}
	}
	for i, t := range items(s.get("toolConnections")) {
		if n := child(t, "target"); n != nil && n.Kind == yaml.ScalarNode {
			out = append(out, nameAt{fmt.Sprintf("toolConnections[%d].target", i), n})
		}
	}
	pub := child(child(s.Node, "activity"), "publish")
	for _, k := range publishURLKeys {
		if n := child(pub, k); n != nil && n.Kind == yaml.ScalarNode {
			out = append(out, nameAt{"activity.publish." + k, n})
		}
	}
	for _, p := range pairs(s.get("env")) {
		if v := scalar(p.Val); strings.Contains(v, "://") {
			out = append(out, nameAt{"env." + p.Key.Value, p.Val})
		}
	}
	return out
}

// egressInjected reports whether a project declares network.agentSubnet, which
// the azure.ai.project schema documents as injecting the agent runtime into a
// customer subnet (BYO VNet). RFC 1918 targets can then be reachable.
func egressInjected(d document) bool {
	for _, s := range serviceList(d) {
		if s.Host == hostProject && has(child(s.Node, "network"), "agentSubnet") {
			return true
		}
	}
	return false
}

// rfc1918Host reports a private-range IP literal that is neither loopback nor
// link-local (so not the metadata address).
func rfc1918Host(h string) bool {
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
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
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	var fs []sdk.Finding
	injected := egressInjected(d)
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
				fs = append(fs, finding(d, s, u.Node, "non-https:"+u.Name, fmt.Sprintf("%s uses non-https scheme %q", u.Name, pu.Scheme)))
			}
			if riskyHost(pu.Hostname()) && !(injected && rfc1918Host(pu.Hostname())) {
				fs = append(fs, finding(d, s, u.Node, "private-host:"+u.Name, fmt.Sprintf("%s targets local/private host %q", u.Name, pu.Hostname())))
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
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	var fs []sdk.Finding
	for _, s := range serviceList(d) {
		add := func(key string, n *yaml.Node, m string) { fs = append(fs, finding(d, s, n, key, m)) }
		if strings.EqualFold(s.Host, "microsoft.foundry") {
			add("legacy-host", s.Key, "host microsoft.foundry is a legacy shape")
		}
		if s.Host != hostAgent {
			continue
		}
		if has(s.Node, "config") {
			add("legacy-config", s.get("config"), "config on azure.ai.agent is a legacy shape")
		}
		if s.kind() == "prompt-voice" {
			add("legacy-kind", s.get("kind"), "kind prompt-voice is a legacy shape")
		}
		if s.isHostedAgent() {
			for _, k := range []string{"displayName", "inputSchema", "outputSchema"} {
				if has(s.Node, k) {
					add("legacy-field:"+k, s.get(k), k+" on a hosted agent is a legacy shape")
				}
			}
		}
		for _, p := range pairs(s.get("env")) {
			if strings.HasPrefix(p.Key.Value, "FOUNDRY_") || strings.HasPrefix(p.Key.Value, "AGENT_") {
				add("reserved-env-prefix:"+p.Key.Value, p.Key, "env key "+p.Key.Value+" uses a reserved prefix")
			}
		}
	}
	return withNotes(fs, noteTexts(requiredVersionNotes(d.Resolved(), cfg011MinVersions)))
}

// Variable names azd core defines as environment keys (azd source,
// cli/azd/pkg/environment/environment.go). The source defines the names but
// does not state which are always set, so none is a guaranteed producer: a
// name absent from the selected environment's .env is uncertain, except
// AZURE_ENV_NAME when the environment was selected by name (see
// EnvNameSelection).

// Names azd also defines in the same source but that only exist when the
// infrastructure, the host or the user provides them. They are not known
// producers; a missing one is uncertain, not an error.
var infraDependentVars = map[string]bool{
	"AZURE_ENV_NAME":                                  true,
	"AZURE_SUBSCRIPTION_ID":                           true,
	"AZURE_RESOURCE_GROUP":                            true,
	"AZURE_LOCATION":                                  true,
	"AZURE_TENANT_ID":                                 true,
	"AZURE_PRINCIPAL_ID":                              true,
	"AZURE_PRINCIPAL_TYPE":                            true,
	"AZURE_CONTAINER_REGISTRY_ENDPOINT":               true,
	"AZURE_CONTAINER_APPS_ENVIRONMENT_DEFAULT_DOMAIN": true,
	"AZURE_APP_SERVICE_DASHBOARD_URI":                 true,
	"AZURE_AKS_CLUSTER_NAME":                          true,
	"AZD_PLATFORM_TYPE":                               true,
}

// Reasons for a skipped environment-dependent check (R13). They follow
// "input-unavailable:" so `--strict` counts them as a missing input.
const (
	reasonNoEnvironmentSelected      = "no-environment-selected"
	reasonEnvFileMissing             = "env-file-missing"
	reasonDefaultEnvironmentNotFound = "default-environment-not-found"
	reasonEnvironmentNotFound        = "environment-not-found"
	reasonEnvFileUnreadable          = "env-file-unreadable"
	reasonEnvFileUnparseable         = "env-file-unparseable"
	reasonEnvFileOversize            = "env-file-oversize"
)

// EnvSelection is optionally implemented by an EnvView to say why no
// environment is selected. It returns one of no-environment-selected,
// env-file-missing, default-environment-not-found (the default in
// .azure/config.json names no environment) or environment-not-found (an
// explicitly named environment does not exist; the value may be followed by
// ": <name>"). A view without it is reported as no-environment-selected.
type EnvSelection interface {
	SelectionReason() string
}

// EnvNameSelection is optionally implemented by an EnvView to say that the
// environment was selected by name (a flag or configured name), not by the
// default in .azure/config.json. Only then is AZURE_ENV_NAME known to equal
// the selected name even when the .env file does not hold it. A view without
// it, or one that returns false, gets no such assumption.
type EnvNameSelection interface {
	SelectedByName() bool
}

// ExpandedKeys is optionally implemented by an EnvView to name .env keys whose
// raw value used variable expansion ($VAR or ${VAR}). The parsed value of such
// a key depends on keys the parser may not have seen (an undefined variable
// expands to the empty string), so it is not trusted as empty or as set.
type ExpandedKeys interface {
	ExpandedKeys() map[string]bool
}

// envSkip builds the missing-input skip for an unusable environment view, or
// reports false when an environment is selected.
func envSkip(p Providers) (sdk.Result, bool) {
	if p.Env != nil {
		if _, sel := p.Env.Selected(); sel {
			return sdk.Result{}, false
		}
	}
	reason := reasonNoEnvironmentSelected
	if es, isSel := p.Env.(EnvSelection); isSel && p.Env != nil {
		r := es.SelectionReason()
		switch {
		case r == reasonEnvFileMissing, r == reasonDefaultEnvironmentNotFound, r == reasonNoEnvironmentSelected,
			r == reasonEnvFileUnreadable, r == reasonEnvFileUnparseable, r == reasonEnvFileOversize,
			r == reasonEnvironmentNotFound:
			reason = r
		case strings.HasPrefix(r, reasonEnvironmentNotFound+": "):
			reason = reasonEnvironmentNotFound + ": " + safeEnvName(strings.TrimPrefix(r, reasonEnvironmentNotFound+": "))
		}
	}
	return skip(sdk.SkipInputUnavailable + ": " + reason), true
}

// safeEnvName keeps the characters azd environment names use and caps the
// length, so a hostile name cannot inject text into a skip reason.
func safeEnvName(n string) string {
	const limit = 40
	var b strings.Builder
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('?')
		}
		if b.Len() >= limit {
			b.WriteString("...")
			break
		}
	}
	return b.String()
}

// agentNameReplacer mirrors azd's agentServiceKey (azure.ai.agents
// 1.0.0-beta.20, service_target_agent.go): it replaces a space and a hyphen
// with an underscore and then upper-cases. Nothing else is replaced.
var agentNameReplacer = strings.NewReplacer(" ", "_", "-", "_")

// agentNameCertain reports whether every character of a service name is one
// azd's key derivation is known to handle (letters, digits, space, hyphen and
// underscore). Any other character, such as a dot, leaves the derived
// AGENT_ names unverified.
func agentNameCertain(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == ' ':
		default:
			return false
		}
	}
	return name != ""
}

// agentProducers returns the AGENT_<SERVICE>_* names the agent extension is
// known to write for an agent-host service. Other hosts get none, and a name
// with a character outside azd's verified key derivation gets none either, so
// an AGENT_ reference stays uncertain. Suffixes: azure.ai.agents 1.0.0-beta.20
// displayableProtocols.
func agentProducers(s service) []string {
	if s.Host != hostAgent || !agentNameCertain(s.Name) {
		return nil
	}
	up := "AGENT_" + strings.ToUpper(agentNameReplacer.Replace(s.Name))
	out := []string{up + "_NAME", up + "_VERSION", up + "_ENDPOINT"}
	for _, proto := range []string{"RESPONSES", "INVOCATIONS", "A2A", "INVOCATIONS_WS"} {
		out = append(out, up+"_"+proto+"_ENDPOINT")
	}
	return out
}

func evalCFG006(in *sdk.Input, p Providers) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable + ": azure.yaml unavailable")
	}
	if r, bad := envSkip(p); bad {
		return r
	}
	have := map[string]bool{}
	empty := map[string]bool{}
	for k, v := range p.Env.Values() {
		if strings.TrimSpace(v) == "" {
			empty[k] = true // a key with no value is not a producer
			continue
		}
		have[k] = true
	}
	if es, isName := p.Env.(EnvNameSelection); isName && es.SelectedByName() && !empty["AZURE_ENV_NAME"] {
		have["AZURE_ENV_NAME"] = true
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
		for _, n := range agentProducers(s) {
			have[n] = true
		}
	}
	var fs []sdk.Finding
	var notes []string
	seen := map[string]bool{}
	walkScalars(d.Resolved(), "", func(path, _ string, v *yaml.Node) {
		for _, form := range unusualForms(v.Value) {
			if !seen["form:"+form] {
				seen["form:"+form] = true
				notes = append(notes, fmt.Sprintf("${%s...} uses a substitution form that no verified azd source documents for azure.yaml; whether it is defined is undecided", form))
			}
		}
		for _, name := range varRefs(v.Value) {
			if have[name] || seen[name] {
				continue
			}
			seen[name] = true
			switch {
			case empty[name]:
				notes = append(notes, fmt.Sprintf("${%s} is present in the environment with an empty value", name))
			case outputOnly[name]:
				notes = append(notes, fmt.Sprintf("${%s} is produced by a template output that is not in the environment yet (provision has not written it)", name))
			case infraDependentVars[name]:
				notes = append(notes, fmt.Sprintf("${%s} is an azd-defined name that is only set when the environment, infrastructure or host provides it", name))
			case strings.HasPrefix(name, "AGENT_"):
				notes = append(notes, fmt.Sprintf("${%s} is an AGENT_<SERVICE>_* name that is not a verified producer for any agent-host service", name))
			case !outputsKnown:
				notes = append(notes, fmt.Sprintf("${%s} is not in the environment and no compiled template outputs are available to check", name))
			default:
				fs = append(fs, sdk.Finding{Key: "undefined-variable:" + name, Location: loc(d, v),
					Evidence: fmt.Sprintf("%s references ${%s} which no producer defines", path, name)})
			}
		}
	})
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Evidence < fs[j].Evidence })
	return withNotes(fs, notes)
}

const (
	cfg012ShapeRecommendation = "Use a location name from az account list-locations (the name field, such as eastus2); " +
		"Azure location names are lowercase letters and digits only."
	cfg012ShapeFix = "azd env set AZURE_LOCATION eastus2"
)

// regionShapeBad reports a value that is already in the lowercase,
// whitespace-free name form but holds a character other than a lowercase
// letter or digit. Every location name in the bundled lists is letters and
// digits only; a display name (upper case or spaces) is never judged here.
func regionShapeBad(v string) bool {
	if v == "" || v != strings.ToLower(v) || strings.ContainsAny(v, " \t") {
		return false
	}
	return !regionNameShape.MatchString(v)
}

// cfg012ARM judges literal resource locations of the compiled template. Only
// a name-form value with a character outside [a-z0-9] is a finding; template
// expressions, "global" and display names are not judged.
func cfg012ARM(in *sdk.Input) []sdk.Finding {
	if in == nil || in.ARM == nil {
		return nil
	}
	var fs []sdk.Finding
	for _, r := range in.ARM.Resources() {
		v := strings.TrimSpace(r.Region)
		if v == "" || strings.HasPrefix(v, "[") || containsRef(v) || v == "global" || !regionShapeBad(v) {
			continue
		}
		fs = append(fs, sdk.Finding{
			Key:            "arm-location-not-region-name:" + r.Type + "/" + armexpr.StripScope(r.Name),
			Resource:       sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
			Location:       r.Location,
			Evidence:       "resource location " + strconv.Quote(clip(findings.Redact(v))) + " is not an Azure location name",
			Recommendation: cfg012ShapeRecommendation + suggestion(v),
			Fix:            cfg012ShapeFix,
			Confidence:     sdk.ConfidenceLow,
		})
	}
	return fs
}

func evalCFG012(in *sdk.Input, p Providers) sdk.Result {
	armFs := cfg012ARM(in)
	if r, bad := envSkip(p); bad {
		if len(armFs) > 0 {
			return ok(armFs)
		}
		return r
	}
	v, present := p.Env.Values()["AZURE_LOCATION"]
	if !present {
		if len(armFs) > 0 {
			return ok(armFs)
		}
		return skip(sdk.SkipInputUnavailable + ": AZURE_LOCATION is not set in the selected environment")
	}
	if containsRef(v) {
		// A single-quoted .env value keeps "${" literally; whether azd or a
		// consumer expands it later is not decidable here.
		return withEnvNote(armFs, uncertainPrefix+"AZURE_LOCATION is set but its value contains a variable reference (${...}); it was not judged")
	}
	if xk, isX := p.Env.(ExpandedKeys); isX && xk.ExpandedKeys()["AZURE_LOCATION"] {
		// godotenv expands an undefined variable to the empty string; the parsed
		// value does not show what the author or azd intends.
		return withEnvNote(armFs, uncertainPrefix+"AZURE_LOCATION is defined by variable expansion in the .env file; its value cannot be judged statically")
	}
	key := regionKey(v)
	switch {
	case strings.TrimSpace(v) == "":
		return ok(append(armFs, sdk.Finding{Key: "location-empty", Evidence: "AZURE_LOCATION is empty"}))
	case regionShapeBad(strings.TrimSpace(v)):
		return ok(append(armFs, sdk.Finding{
			Key:            "location-not-region-name",
			Evidence:       "AZURE_LOCATION " + strconv.Quote(clip(findings.Redact(strings.TrimSpace(v)))) + " is not an Azure location name (letters and digits only)",
			Recommendation: cfg012ShapeRecommendation + suggestion(key),
			Fix:            cfg012ShapeFix,
			Confidence:     sdk.ConfidenceLow,
		}))
	case v != strings.ToLower(v) || strings.ContainsAny(v, " \t"):
		// No primary source says ARM or azd rejects a display name such as
		// "East US", so this is inconclusive in every profile.
		msg := uncertainPrefix + "AZURE_LOCATION is not in the lowercase region-name form azd documents (for example eastus); no source says it is rejected"
		if isKnownRegion(key) {
			msg += "; it names the region " + key
		} else {
			msg += suggestion(key)
		}
		return withEnvNote(armFs, msg)
	case !isKnownRegion(key):
		// The bundled lists are point-in-time subsets (see azureRegionNames),
		// so an unlisted alphanumeric name may be a new region or a typo.
		return withEnvNote(armFs, uncertainPrefix+"AZURE_LOCATION "+strconv.Quote(clip(findings.Redact(key)))+" is not in the bundled list of Azure region names, which can lag new regions; compare it with az account list-locations."+suggestion(key))
	}
	return ok(armFs)
}

// withEnvNote returns the ARM findings with one uncertain skip, or a
// rule-level uncertain skip when there are no findings.
func withEnvNote(fs []sdk.Finding, msg string) sdk.Result {
	if len(fs) == 0 {
		return skip(msg)
	}
	return sdk.Result{Findings: fs, Skips: []sdk.Skip{{Reason: msg}}}
}

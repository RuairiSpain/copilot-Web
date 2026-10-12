package cfg

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule005 is FND-CFG-005: no raw secrets in azure.yaml. Only the native half runs here. The Bicep and ARM
// half is mapped from the Bicep linter, PSRule and Checkov results, so without those tools it is skipped.
// Findings never carry a value, only the field path.
type rule005 struct{}

func (rule005) ID() string   { return "FND-CFG-005" }
func (rule005) Version() int { return 1 }

const (
	rec005 = "Store the secret in the azd environment as a secret (azd env set-secret) or in Key Vault, and reference it with ${VAR} in azure.yaml. In Bicep, mark secret parameters @secure() with no default and never output them. Prefer Entra ID or managed identity authentication where the target supports it."
	fix005 = "services:\n  search-conn:\n    host: azure.ai.connection\n    uses: [ai-project]\n    category: CognitiveSearch\n    target: https://my-search.search.windows.net\n    authType: ApiKey\n    credentials:\n      key: ${SEARCH_API_KEY}   # set with: azd env set-secret SEARCH_API_KEY\n"
)

// secretAuthTypes are the connection authTypes whose credentials hold secrets (ARM specification
// 2026-09-01, fact-check item 12). ManagedIdentity, AAD and None carry none.
var secretAuthTypes = []string{"ApiKey", "CustomKeys", "PAT", "AccessKey", "AccountKey", "SAS", "UsernamePassword", "ServicePrincipal", "OAuth2"}

// secretCredentialKeys are the ARM password-typed credential fields. CustomKeys keys are free-form, so
// every CustomKeys value counts.
var secretCredentialKeys = []string{"key", "keys", "pat", "sas", "secretaccesskey", "password", "securitytoken", "clientsecret", "developertoken", "refreshtoken"}

// secretLiteral are token shapes that raise confidence to certain. They are Foundry Doctor heuristics.
var secretLiteral = regexp.MustCompile(`^(sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|xox[abprs]-[A-Za-z0-9-]{10,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})$`)

var wordSplit = regexp.MustCompile(`[A-Z]+[a-z0-9]*|[a-z0-9]+`)

// secretName reports whether an env key is named like a secret.
func secretName(key string) bool {
	var words []string
	for _, w := range wordSplit.FindAllString(key, -1) {
		words = append(words, strings.ToLower(w))
	}
	if len(words) == 0 {
		return false
	}
	switch words[len(words)-1] {
	case "name", "url", "uri", "endpoint", "id", "vault", "path", "version", "enabled":
		return false
	}
	for i, w := range words {
		switch w {
		case "key", "apikey", "secret", "token", "password", "pwd", "passwd", "connectionstring":
			return true
		case "connection":
			if i+1 < len(words) && words[i+1] == "string" {
				return true
			}
		}
	}
	return false
}

// literal reports whether v is a non-empty value with no ${...} reference.
func literal(v string) bool { return !blank(v) && !hasRef(v) }

func confidenceFor(v string) sdk.Confidence {
	if secretLiteral.MatchString(strings.TrimSpace(v)) {
		return sdk.ConfidenceCertain
	}
	return sdk.ConfidenceLikely
}

func (r rule005) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	for _, s := range sortedServices(in.AzureYAML) {
		if s.Host == hostConnection {
			r.credentials(x, s, s.Node, "credentials")
		}
		if s.Host == hostAgent {
			for i, tc := range seqItems(s.Node.Lookup("toolConnections")) {
				r.credentials(x, s, tc, fmt.Sprintf("toolConnections[%d].credentials", i))
			}
		}
		r.env(x, s)
	}
	r.urls(x, in.AzureYAML)

	// Which .env files are tracked by git is not available; Discovery lists the capability as unavailable.
	x.skip(rules.ReasonUnresolvedValue("git-tracked-files-unavailable"), project.CapabilityTrackedFiles, ".env")
	r.armHalf(x, in)
	return x.done(), nil
}

func (rule005) credentials(x *run, s model.Service, owner *model.Node, label string) {
	authType, _ := scalar(owner.Lookup("authType"))
	ok := authType != ""
	creds := owner.Lookup("credentials")
	if creds == nil || creds.Kind != model.NodeMapping || !ok {
		return
	}
	if hasRef(authType) {
		x.skip(rules.ReasonUnresolvedValue("auth-type-reference"), "expanded ${VAR} values", s.Name)
		return
	}
	if !slices.Contains(secretAuthTypes, authType) {
		return
	}
	for _, c := range creds.Children {
		v, isScalar := scalar(c)
		if !isScalar || !literal(v) {
			continue
		}
		if authType != "CustomKeys" && !slices.Contains(secretCredentialKeys, strings.ToLower(c.Key)) {
			continue
		}
		path := label + "." + c.Key
		x.fail(svcRef(s), x.loc(posOf(c, s.Pos)), "credential:"+s.Name+":"+path, confidenceFor(v),
			fmt.Sprintf("%s of service %s holds a literal value for authType %s; the value is not shown", path, s.Name, authType), rec005, fix005)
	}
}

func (rule005) env(x *run, s model.Service) {
	env := s.Node.Lookup("env")
	if env == nil || env.Kind != model.NodeMapping {
		return
	}
	for _, c := range env.Children {
		v, ok := scalar(c)
		if !ok || !literal(v) || !secretName(c.Key) || v == "true" || v == "false" {
			continue
		}
		x.fail(svcRef(s), x.loc(posOf(c, s.Pos)), "env:"+s.Name+":"+c.Key, confidenceFor(v),
			fmt.Sprintf("env.%s of service %s is named like a secret and holds a literal value; the value is not shown", c.Key, s.Name), rec005, fix005)
	}
}

var sasKeys = []string{"sig", "sv", "token"}

func (rule005) urls(x *run, ay *model.AzureYAML) {
	for _, f := range append(urlFields(ay), envURLFields(ay)...) {
		u, ok := parseURL(f.value)
		if !ok {
			continue
		}
		if userinfo, has := rawUserinfo(f.value); has && !hasRef(userinfo) {
			x.fail(svcRef(f.svc), x.loc(f.pos), "url-userinfo:"+f.svc.Name+":"+f.name, sdk.ConfidenceCertain,
				fmt.Sprintf("%s of service %s embeds credentials in the URL; the value is not shown", f.name, f.svc.Name), rec005, fix005)
		}
		q := u.Query()
		for _, k := range sasKeys {
			if vals, has := q[k]; has && len(vals) > 0 && vals[0] != "" && vals[0] != refToken {
				x.fail(svcRef(f.svc), x.loc(f.pos), "url-sas:"+f.svc.Name+":"+f.name+":"+k, sdk.ConfidenceLikely,
					fmt.Sprintf("%s of service %s has a literal %q query parameter; the value is not shown", f.name, f.svc.Name, k), rec005, fix005)
			}
		}
	}
}

// armHalf records the mapped half of the rule: Bicep, ARM and parameter-file secrets come from the Bicep
// linter, PSRule and Checkov, so a missing tool is a skipped check.
func (rule005) armHalf(x *run, in *model.Input) {
	switch in.Project.IaC {
	case model.IaCSynthetic:
		x.skip(rules.ReasonSyntheticInfrastructure, "compiled ARM template", "bicep-arm")
		return
	case model.IaCBicep, model.IaCARMJSON:
	default:
		return
	}
	for _, name := range []string{"bicep", "psrule", "checkov"} {
		available := false
		for _, t := range in.Tools {
			if t.Name == name && t.State == sdk.ToolAvailable {
				available = true
			}
		}
		if !available {
			x.skip(rules.ReasonToolMissing(name), name+" results for Bicep and ARM secrets", name)
		}
	}
}

// rawUserinfo returns the userinfo part of the authority of a URL, from the unmasked text.
func rawUserinfo(v string) (string, bool) {
	_, after, ok := strings.Cut(v, "://")
	if !ok {
		return "", false
	}
	if i := strings.IndexAny(after, "/?#"); i >= 0 {
		after = after[:i]
	}
	if at := strings.LastIndex(after, "@"); at >= 0 {
		return after[:at], true
	}
	return "", false
}

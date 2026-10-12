package idn

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// idn006 implements FND-IDN-006: federated credentials use an HTTPS issuer and a specific subject.
type idn006 struct{ meta }

func newIDN006() rules.Rule { return idn006{meta{"FND-IDN-006", 1}} }

// Issuer hosts documented in the catalogue sources.
const (
	githubHost = "token.actions.githubusercontent.com"
	aksSuffix  = ".oic.prod-aks.azure.com"
)

// knownIssuerHosts are issuers whose subject format the rule does not check further: GitLab, Terraform
// Cloud, Azure DevOps and Google (catalogue sources). Any other issuer gets an uncertain result.
var knownIssuerHosts = map[string]bool{
	"gitlab.com":                true,
	"app.terraform.io":          true,
	"app.eu.terraform.io":       true,
	"vstoken.dev.azure.com":     true,
	"login.microsoftonline.com": true,
	"accounts.google.com":       true,
}

func (r idn006) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, stop := armGuard(r, in); stop {
		return res, nil
	}
	var out []rules.Result
	for _, res := range in.ARM.Resources {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("FND-IDN-006: %w", err)
		}
		if !strings.HasSuffix(strings.ToLower(res.Type), "/federatedidentitycredentials") {
			continue
		}
		if !isType(res, typeFIC) {
			out = append(out, skipRes(r, res, rules.ReasonNotApplicable,
				"a federated credential on an Entra application registration is a Microsoft Graph object; it is checked by a later live check", "Microsoft Graph access"))
			continue
		}
		out = append(out, r.check(res)...)
	}
	return out, nil
}

// text reads a string property; state tells absent, unresolved (with the parameter name if any) or literal.
func (r idn006) text(res model.Resource, prop string) (val string, st model.ValueState, detail string) {
	v := res.Get("properties." + prop)
	switch v.State {
	case model.Absent:
		return "", model.Absent, ""
	case model.Unresolved:
		raw, _ := rawString(res, "properties", prop)
		d := prop + " is an expression"
		if e, ok := inner(raw); ok {
			if fn, args, rest, ok := parseCall(e); ok && strings.EqualFold(fn, "parameters") && rest == "" && len(args) == 1 {
				d = fmt.Sprintf("%s is the parameter %s", prop, argText(args[0]))
			}
		}
		return "", model.Unresolved, d
	}
	s, ok := v.String()
	if !ok {
		return "", model.Unresolved, prop + " is not a string"
	}
	return s, model.Literal, ""
}

func (r idn006) check(res model.Resource) []rules.Result {
	var out []rules.Result
	bad := func(key, prop, why string) {
		out = append(out, fail(r, res, key, "/properties/"+prop, fmt.Sprintf("federated credential %s: %s", res.Name, why), rec006, fix006, sdk.ConfidenceLikely))
	}
	skipped := false
	skip := func(detail string) {
		skipped = true
		out = append(out, skipRes(r, res, rules.ReasonUnresolvedValue("parameter"), detail, "resolved parameter value"))
	}

	issuer, ist, idetail := r.text(res, "issuer")
	subject, sst, sdetail := r.text(res, "subject")
	host := ""
	switch ist {
	case model.Absent:
		bad("issuer-missing", "issuer", "properties.issuer is not set")
	case model.Unresolved:
		skip(idetail)
	default:
		u, err := url.Parse(strings.TrimSpace(issuer))
		switch {
		case strings.TrimSpace(issuer) == "":
			bad("issuer-empty", "issuer", "issuer is empty")
		case err != nil || !u.IsAbs():
			bad("issuer-not-url", "issuer", "issuer is not an absolute URL")
		case !strings.EqualFold(u.Scheme, "https"):
			bad("issuer-not-https", "issuer", fmt.Sprintf("issuer %q does not use the https scheme", issuer))
		case u.Hostname() == "":
			bad("issuer-no-host", "issuer", "issuer has no host")
		default:
			host = strings.ToLower(u.Hostname())
		}
	}

	switch sst {
	case model.Absent:
		bad("subject-missing", "subject", "properties.subject is not set")
	case model.Unresolved:
		skip(sdetail)
	default:
		switch {
		case strings.TrimSpace(subject) == "":
			bad("subject-empty", "subject", "subject is empty")
		case strings.ContainsAny(subject, "*?"):
			bad("subject-wildcard", "subject", fmt.Sprintf("subject %q contains a wildcard; a standard federated credential matches the subject exactly, so it would match no workload", subject))
		case host != "":
			out = append(out, r.subjectFor(res, host, subject)...)
		}
	}

	if len(out) == 0 && !skipped {
		return []rules.Result{rules.Pass()}
	}
	return out
}

// subjectFor applies the issuer-specific subject checks. Only GitHub and AKS formats are verified.
func (r idn006) subjectFor(res model.Resource, host, subject string) []rules.Result {
	badSub := func(key, why string) []rules.Result {
		return []rules.Result{fail(r, res, key, "/properties/subject", fmt.Sprintf("federated credential %s: %s", res.Name, why), rec006, fix006, sdk.ConfidenceLikely)}
	}
	switch {
	case host == githubHost:
		if why := githubSubjectProblem(subject); why != "" {
			return badSub("github-subject-invalid", fmt.Sprintf("GitHub subject %q is invalid: %s", subject, why))
		}
	case strings.HasSuffix(host, aksSuffix):
		if why := aksSubjectProblem(subject); why != "" {
			return badSub("aks-subject-invalid", fmt.Sprintf("AKS subject %q is invalid: %s", subject, why))
		}
	case !knownIssuerHosts[host]:
		return []rules.Result{uncertain(r, res, "subject-format-unknown", "/properties/subject",
			fmt.Sprintf("federated credential %s: the issuer host %s is not one whose subject format is documented, so only the non-empty and no-wildcard checks ran", res.Name, host), rec006, fix006)}
	}
	return nil
}

// githubSubjectProblem checks repo:{owner}/{repo}:{environment:X|ref:X|pull_request}. The immutable form
// repo:{owner}@{id}/{repo}@{id}:... is accepted.
func githubSubjectProblem(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) < 3 || parts[0] != "repo" {
		return "expected repo:{owner}/{repo}:{entity}"
	}
	or := strings.Split(parts[1], "/")
	if len(or) != 2 || or[0] == "" || or[1] == "" {
		return "repository must be {owner}/{repo}"
	}
	switch entity := parts[2]; entity {
	case "environment", "ref":
		if len(parts) < 4 || strings.Join(parts[3:], ":") == "" {
			return entity + " entity needs a concrete value"
		}
	case "pull_request":
		if len(parts) != 3 {
			return "pull_request takes no value"
		}
	default:
		return "entity must be environment, ref or pull_request"
	}
	return ""
}

// aksSubjectProblem checks system:serviceaccount:{namespace}:{serviceaccount}.
func aksSubjectProblem(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" || parts[2] == "" || parts[3] == "" {
		return "expected system:serviceaccount:{namespace}:{serviceaccount} with both parts concrete"
	}
	return ""
}

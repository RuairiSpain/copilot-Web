package env

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Keys read by the rules. They are azd names (fact-check item 2), safe to print.
const (
	keyEnvName       = "AZURE_ENV_NAME"
	keySubscription  = "AZURE_SUBSCRIPTION_ID"
	keyResourceGroup = "AZURE_RESOURCE_GROUP"
)

// envFile is the project-relative location of an environment's .env file.
func envFile(name string) string { return ".azure/" + name + "/.env" }

func envLocation(name string) sdk.Location { return sdk.Location{File: envFile(name)} }

// value returns the value of key, or "" when unset. Callers use it to compare, never to print.
func value(e model.Environment, key string) string {
	v, ok := e.Get(key)
	if !ok {
		return ""
	}
	return v.Reveal()
}

// tierName is the AZURE_ENV_NAME of an environment, falling back to its directory name.
func tierName(e model.Environment) string {
	if v := strings.TrimSpace(value(e, keyEnvName)); v != "" {
		return v
	}
	return e.Name
}

// skipSingle builds the skip for fewer than two visible environments.
func skipSingle(r rules.Rule, in *model.Input, visibleCount int) rules.Result {
	if len(in.Project.Environments) > visibleCount && visibleCount < 2 {
		return rules.SkipFor(r, rules.ReasonNotApplicable,
			"env-files-not-in-workspace: azd lists more environments than have a readable .azure/<env>/.env file",
			"a second readable .azure/<env>/.env file", sdk.ResourceRef{Kind: "project", Name: in.Project.Name})
	}
	return rules.SkipFor(r, rules.ReasonNotApplicable,
		"single-environment-visible: at least two environments are needed to compare",
		"a second azd environment", sdk.ResourceRef{Kind: "project", Name: in.Project.Name})
}

// results turns findings into results; no findings is a pass.
func results(fs []rules.Result) []rules.Result {
	if len(fs) == 0 {
		return []rules.Result{rules.Pass()}
	}
	return fs
}

var refRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?-)([^}]*))?\}`)

// hasRef reports whether s contains a ${...} reference.
func hasRef(s string) bool { return strings.Contains(s, "${") }

// expand substitutes ${NAME}, ${NAME:-default} and ${NAME-default} from e. AZURE_ENV_NAME falls
// back to the environment name. An unset variable expands to "" (azd treats a blank group as unset).
// Other reference forms expand to "".
func expand(s string, e model.Environment) string {
	return refRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := refRE.FindStringSubmatch(m)
		name, op, def := sub[1], sub[2], sub[3]
		v, ok := e.Get(name)
		raw := ""
		if ok {
			raw = v.Reveal()
		} else if name == keyEnvName {
			raw, ok = e.Name, true
		}
		switch {
		case op == ":-" && raw == "":
			return def
		case op == "-" && !ok:
			return def
		}
		return raw
	})
}

// scalar returns the text of a scalar node, and false for anything else.
func scalar(n *model.Node) (string, bool) {
	if n == nil || n.Kind != model.NodeScalar {
		return "", false
	}
	return n.Value, true
}

// rgSource is one azure.yaml resourceGroup setting.
type rgSource struct {
	ID   string // "project" or "service:<name>"
	Kind string // resource kind for findings
	Name string
	Raw  string // unexpanded text; not printed
	Pos  model.Pos
}

// resourceGroupSettings lists the project-level and service-level resourceGroup scalars of azure.yaml.
func resourceGroupSettings(y *model.AzureYAML) []rgSource {
	if y == nil {
		return nil
	}
	var out []rgSource
	if s, ok := scalar(y.Root.Lookup("resourceGroup")); ok {
		out = append(out, rgSource{ID: "project", Kind: "project", Name: y.Name, Raw: s, Pos: y.Root.Lookup("resourceGroup").Pos})
	}
	for _, svc := range y.Services {
		n := svc.Node.Lookup("resourceGroup")
		if s, ok := scalar(n); ok {
			out = append(out, rgSource{ID: "service:" + svc.Name, Kind: "service", Name: svc.Name, Raw: s, Pos: n.Pos})
		}
	}
	return out
}

func yamlLocation(y *model.AzureYAML, p model.Pos) sdk.Location {
	file := "azure.yaml"
	if y != nil && y.File != "" {
		file = y.File
	}
	return sdk.Location{File: file, Line: p.Line, Column: p.Column}
}

// identities extracts the resource identities a value refers to: the normalised ARM resource ID and
// its resource-group scope, an endpoint host, nothing otherwise. The strings are compared in
// process and never printed.
func identities(v string) []string {
	v = strings.TrimSpace(v)
	switch {
	case strings.HasPrefix(strings.ToLower(v), "/subscriptions/"):
		id := strings.ToLower(strings.TrimRight(v, "/"))
		out := []string{"id:" + id}
		parts := strings.Split(id, "/")
		if len(parts) >= 5 && parts[3] == "resourcegroups" && parts[2] != "" && parts[4] != "" {
			out = append(out, "rg:"+parts[2]+"/"+parts[4])
		}
		return out
	case strings.Contains(v, "://"):
		u, err := url.Parse(v)
		if err != nil || u.Hostname() == "" {
			return nil
		}
		return []string{"host:" + strings.ToLower(u.Hostname())}
	}
	return nil
}

// envIdentities returns every identity an environment refers to, including its own scope.
func envIdentities(e model.Environment) map[string]bool {
	set := map[string]bool{}
	for _, k := range e.Keys() {
		v := value(e, k)
		for _, id := range identities(v) {
			set[id] = true
		}
	}
	if sub, rg := strings.ToLower(strings.TrimSpace(value(e, keySubscription))), strings.ToLower(strings.TrimSpace(value(e, keyResourceGroup))); sub != "" && rg != "" {
		set["rg:"+sub+"/"+rg] = true
	}
	return set
}

// nameList renders at most limit names, then a count of the rest.
func nameList(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:limit], ", ") + " and " + strconv.Itoa(len(names)-limit) + " more"
}

// inList reports whether name is in list.
func inList(list []string, name string) bool { return slices.Contains(list, name) }

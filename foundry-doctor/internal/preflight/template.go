package preflight

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// deployScope is the ARM deployment scope of the compiled template.
type deployScope int

const (
	scopeUnknown deployScope = iota
	scopeResourceGroup
	scopeSubscription
	scopeManagementGroup
	scopeTenant
)

func (s deployScope) String() string {
	switch s {
	case scopeResourceGroup:
		return "resourceGroup"
	case scopeSubscription:
		return "subscription"
	case scopeManagementGroup:
		return "managementGroup"
	case scopeTenant:
		return "tenant"
	}
	return "unknown"
}

// tmplParam is one declared template parameter (no values).
type tmplParam struct {
	name       string
	typ        string // lower-case ARM type
	hasDefault bool
}

func (p tmplParam) secure() bool { return p.typ == "securestring" || p.typ == "secureobject" }

// templateFacts is what preflight needs from the compiled ARM JSON: its
// deployment scope (from "$schema") and its declared parameters. It reads ARM
// JSON only; there is no Bicep parsing.
type templateFacts struct {
	scope  deployScope
	schema string
	params []tmplParam
	err    error
}

// parseTemplate reads $schema and parameters from the compiled ARM template.
//
// The schema file names are documented in Learn (azure-resource-manager/
// templates/syntax.md, "$schema"): deploymentTemplate.json for resource group
// deployments, subscriptionDeploymentTemplate.json,
// managementGroupDeploymentTemplate.json and tenantDeploymentTemplate.json.
func parseTemplate(raw []byte) templateFacts {
	var doc struct {
		Schema     string                     `json:"$schema"`
		Parameters map[string]json.RawMessage `json:"parameters"`
	}
	if len(raw) == 0 {
		return templateFacts{err: fmt.Errorf("no template")}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return templateFacts{err: fmt.Errorf("template is not valid JSON")}
	}
	tf := templateFacts{schema: doc.Schema}
	s := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(doc.Schema), "#"))
	switch {
	case strings.HasSuffix(s, "/managementgroupdeploymenttemplate.json"):
		tf.scope = scopeManagementGroup
	case strings.HasSuffix(s, "/tenantdeploymenttemplate.json"):
		tf.scope = scopeTenant
	case strings.HasSuffix(s, "/subscriptiondeploymenttemplate.json"):
		tf.scope = scopeSubscription
	case strings.HasSuffix(s, "/deploymenttemplate.json"):
		tf.scope = scopeResourceGroup
	}
	names := make([]string, 0, len(doc.Parameters))
	for n := range doc.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		var p struct {
			Type         string          `json:"type"`
			DefaultValue json.RawMessage `json:"defaultValue"`
		}
		if err := json.Unmarshal(doc.Parameters[n], &p); err != nil {
			// A parameter we cannot read cannot be given a value: unresolved.
			tf.params = append(tf.params, tmplParam{name: n})
			continue
		}
		tf.params = append(tf.params, tmplParam{name: n, typ: strings.ToLower(p.Type), hasDefault: len(p.DefaultValue) > 0})
	}
	return tf
}

// secretParamName flags string parameters that probably hold a secret even
// when they are not declared securestring. Their file value is never sent.
var secretParamName = regexp.MustCompile(`(?i)(secret|passw|pwd|token|credential|connectionstring|accesskey|accountkey|apikey|api_key|primarykey|secondarykey|sharedkey|privatekey|sastoken|sasurl|certificate)`)

// resolveParameters decides the "parameters" object of a what-if request. It
// returns the non-secret values to send and the names that cannot be resolved.
//
//   - A secure parameter is never sent. Without a file value and with a
//     template default, ARM uses the default (same as a real deployment);
//     otherwise it is unresolved.
//   - A value set in the parameter file that the loader could not resolve (nil:
//     unresolved ${VAR}, object, array, Key Vault reference) is unresolved even
//     when the template has a default, because the real deployment would use
//     the file value, not the default.
//   - A string value that starts with "[" would be evaluated as an ARM
//     expression by the service and is unresolved. A string parameter whose
//     name looks like a secret holder is unresolved as well.
//   - A parameter absent from the file keeps its template default; without a
//     default it is unresolved (azd would prompt for it).
func resolveParameters(params []tmplParam, file map[string]any) (values map[string]any, unresolved []string) {
	values = map[string]any{}
	given := make(map[string]any, len(file))
	for k, v := range file {
		given[strings.ToLower(k)] = v
	}
	for _, p := range params {
		v, set := given[strings.ToLower(p.name)]
		switch {
		case p.secure():
			// Never sent. A value in the file means the real deployment would use
			// it, so the template default is not an equivalent stand-in.
			if set || !p.hasDefault {
				unresolved = append(unresolved, p.name)
			}
		case set:
			cv, ok := coerceParam(p, v)
			if !ok {
				unresolved = append(unresolved, p.name)
				continue
			}
			values[p.name] = cv
		case p.hasDefault:
			// The template default applies; nothing is sent.
		default:
			unresolved = append(unresolved, p.name)
		}
	}
	sort.Strings(unresolved)
	return values, unresolved
}

// coerceParam converts a parameter-file scalar to the declared ARM type.
func coerceParam(p tmplParam, v any) (any, bool) {
	if v == nil {
		return nil, false
	}
	switch p.typ {
	case "string":
		s, ok := v.(string)
		if !ok || strings.HasPrefix(s, "[") || secretParamName.MatchString(p.name) {
			return nil, false
		}
		return s, true
	case "bool":
		switch x := v.(type) {
		case bool:
			return x, true
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
		}
	case "int":
		switch x := v.(type) {
		case json.Number:
			if n, err := x.Int64(); err == nil {
				return n, true
			}
		case float64:
			if x == float64(int64(x)) {
				return int64(x), true
			}
		case int:
			return int64(x), true
		case int64:
			return x, true
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
				return n, true
			}
		}
	}
	return nil, false
}

// capNames renders at most n names for a reason text.
func capNames(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + fmt.Sprintf(" and %d more", len(names)-n)
}

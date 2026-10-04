// Package azurenames holds Azure resource-name constraints, shared by validation
// and name generation. Source: "Naming rules and restrictions for Azure resources".
package azurenames

import (
	"fmt"
	"regexp"
	"strings"
)

// Rule describes what a resource provider accepts.
type Rule struct {
	Kind                    string
	Min, Max                int
	pattern                 *regexp.Regexp
	Description             string
	ForbidConsecutiveHyphen bool
}

func rule(kind string, min, max int, pattern, description string, noDouble bool) Rule {
	return Rule{kind, min, max, regexp.MustCompile("^(?:" + pattern + ")$"), description, noDouble}
}

const (
	letterStart = "must start with a letter, end with a letter or digit and contain only letters, digits and hyphens"
	lowerStart  = "may only contain lowercase letters, digits and hyphens, must start with a letter and end with a letter or digit"
)

// Rules by resource kind.
var Rules = func() map[string]Rule {
	rules := []Rule{
		rule("storage", 3, 24, `[a-z0-9]+`, "may only contain lowercase letters and digits", false),
		rule("key-vault", 3, 24, `[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]`, letterStart, true),
		rule("search", 2, 60, `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`, "may only contain lowercase letters, digits and hyphens, and cannot start or end with a hyphen", true),
		rule("redis", 1, 63, `[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?`, "may only contain letters, digits and hyphens, and cannot start or end with a hyphen", true),
		rule("cosmos", 3, 44, `[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`, "may only contain lowercase letters, digits and hyphens, and cannot start or end with a hyphen", false),
		rule("apim", 1, 50, `[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]|[A-Za-z]`, letterStart, false),
		rule("container-app", 2, 32, `[a-z](?:[a-z0-9-]*[a-z0-9])?`, lowerStart, true),
		rule("storage-container", 3, 63, `[a-z0-9]+(?:-[a-z0-9]+)*`, "may only contain lowercase letters, digits and single hyphens between letters or digits", false),
		rule("registry", 5, 50, `[A-Za-z0-9]+`, "may only contain letters and digits", false),
		rule("service-bus", 6, 50, `[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]`, letterStart, false),
		rule("managed-identity", 3, 128, `[A-Za-z0-9][A-Za-z0-9_-]*`, "must start with a letter or digit and contain only letters, digits, hyphens and underscores", false),
		rule("resource-group", 1, 90, `[\w.()-]*[\w()-]`, "may contain letters, digits, underscores, hyphens, periods and parentheses, and cannot end with a period", false),
	}
	m := make(map[string]Rule, len(rules))
	for _, r := range rules {
		m[r.Kind] = r
	}
	return m
}()

// Problems returns why name is invalid for the rule (empty when valid).
func (r Rule) Problems(name string) []string {
	var found []string
	if n := len(name); n < r.Min || n > r.Max {
		found = append(found, fmt.Sprintf("must be %d-%d characters long", r.Min, r.Max))
	}
	if !r.pattern.MatchString(name) {
		found = append(found, r.Description)
	}
	if r.ForbidConsecutiveHyphen && strings.Contains(name, "--") {
		found = append(found, "must not contain consecutive hyphens")
	}
	return found
}

// Problems returns why name is invalid for kind. It panics on an unknown kind.
func Problems(kind, name string) []string {
	r, ok := Rules[kind]
	if !ok {
		panic("azurenames: unknown kind " + kind)
	}
	return r.Problems(name)
}

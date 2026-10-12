package ops

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// nameConstraint is one row of the dated table in the FND-OPS-005 catalogue entry.
type nameConstraint struct {
	typ      string
	min, max int
	// hardMax, when above max, is the length at which every source rejects the name; names between
	// max and hardMax are reported as uncertain (Cosmos DB: Learn says 44, the specification 50).
	hardMax int
	check   func(name string) []string
}

func isLower(c rune) bool { return c >= 'a' && c <= 'z' }
func isUpper(c rune) bool { return c >= 'A' && c <= 'Z' }
func isDigit(c rune) bool { return c >= '0' && c <= '9' }
func isAlnum(c rune) bool { return isLower(c) || isUpper(c) || isDigit(c) }

// allIn reports the first character outside the allowed set, or ok.
func allIn(name string, allowed func(rune) bool) (rune, bool) {
	for _, c := range name {
		if !allowed(c) {
			return c, false
		}
	}
	return 0, true
}

func first(name string) rune { r, _ := utf8.DecodeRuneInString(name); return r }
func last(name string) rune  { r, _ := utf8.DecodeLastRuneInString(name); return r }

func charsetViolation(name, set string, allowed func(rune) bool) []string {
	if _, ok := allIn(name, allowed); !ok {
		return []string{"contains characters outside " + set}
	}
	return nil
}

// foundryName is shared by accounts and projects: the violations rejected by both the naming-rules
// reference and the specification pattern ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$.
func foundryName(name string) []string {
	var v []string
	if !isAlnum(first(name)) {
		v = append(v, "does not start with a letter or digit")
	}
	return append(v, charsetViolation(name, "letters, digits, underscore, period and hyphen", func(c rune) bool {
		return isAlnum(c) || c == '_' || c == '.' || c == '-'
	})...)
}

func alnumHyphen(c rune) bool { return isAlnum(c) || c == '-' }

// nameTable is immutable metadata: Microsoft Learn resource-name-rules (ms.date 08/05/2026), Azure AI
// Search portal quickstart, and the Microsoft.CognitiveServices 2026-09-01 specification.
var nameTable = []nameConstraint{
	{typ: typeAccount, min: 2, max: 64, check: foundryName},
	{typ: typeProject, min: 2, max: 64, check: foundryName},
	{typ: typeAPIM, min: 1, max: 50, check: func(n string) []string {
		var v []string
		if !isAlnum(first(n)) || isDigit(first(n)) {
			v = append(v, "does not start with a letter")
		}
		if !isAlnum(last(n)) {
			v = append(v, "does not end with a letter or digit")
		}
		return append(v, charsetViolation(n, "letters, digits and hyphens", alnumHyphen)...)
	}},
	{typ: typeStorage, min: 3, max: 24, check: func(n string) []string {
		return charsetViolation(n, "lowercase letters and digits", func(c rune) bool { return isLower(c) || isDigit(c) })
	}},
	{typ: typeCosmos, min: 3, max: 44, hardMax: 50, check: func(n string) []string {
		var v []string
		if !isLower(first(n)) && !isDigit(first(n)) {
			v = append(v, "does not start with a lowercase letter or digit")
		}
		return append(v, charsetViolation(n, "lowercase letters, digits and hyphens", func(c rune) bool {
			return isLower(c) || isDigit(c) || c == '-'
		})...)
	}},
	{typ: "Microsoft.KeyVault/vaults", min: 3, max: 24, check: func(n string) []string {
		var v []string
		if !isUpper(first(n)) && !isLower(first(n)) {
			v = append(v, "does not start with a letter")
		}
		if !isAlnum(last(n)) {
			v = append(v, "does not end with a letter or digit")
		}
		if strings.Contains(n, "--") {
			v = append(v, "contains consecutive hyphens")
		}
		return append(v, charsetViolation(n, "letters, digits and hyphens", alnumHyphen)...)
	}},
	{typ: "Microsoft.ContainerRegistry/registries", min: 5, max: 50, check: func(n string) []string {
		return charsetViolation(n, "letters and digits", isAlnum)
	}},
	{typ: typeWorkspace, min: 4, max: 63, check: func(n string) []string {
		var v []string
		if !isAlnum(first(n)) || !isAlnum(last(n)) {
			v = append(v, "does not start and end with a letter or digit")
		}
		return append(v, charsetViolation(n, "letters, digits and hyphens", alnumHyphen)...)
	}},
	{typ: typeAppInsights, min: 1, max: 260, check: func(n string) []string {
		var v []string
		if strings.ContainsAny(n, `%&\?/`) || strings.IndexFunc(n, func(c rune) bool { return c < 0x20 || c == 0x7f }) >= 0 {
			v = append(v, `contains one of % & \ ? / or a control character`)
		}
		if strings.HasSuffix(n, " ") || strings.HasSuffix(n, ".") {
			v = append(v, "ends with a space or period")
		}
		return v
	}},
	{typ: "Microsoft.App/containerApps", min: 2, max: 32, check: func(n string) []string {
		var v []string
		if !isLower(first(n)) {
			v = append(v, "does not start with a lowercase letter")
		}
		if !isLower(last(n)) && !isDigit(last(n)) {
			v = append(v, "does not end with a lowercase letter or digit")
		}
		return append(v, charsetViolation(n, "lowercase letters, digits and hyphens", func(c rune) bool {
			return isLower(c) || isDigit(c) || c == '-'
		})...)
	}},
	{typ: typeSearch, min: 2, max: 60, check: func(n string) []string {
		var v []string
		if strings.HasPrefix(n, "-") || len(n) > 1 && n[1] == '-' {
			v = append(v, "has a dash as one of its first two characters")
		}
		if strings.HasSuffix(n, "-") {
			v = append(v, "ends with a dash")
		}
		if strings.Contains(n, "--") {
			v = append(v, "contains consecutive dashes")
		}
		return append(v, charsetViolation(n, "lowercase letters, digits and dashes", func(c rune) bool {
			return isLower(c) || isDigit(c) || c == '-'
		})...)
	}},
}

type ops005 struct{}

func (ops005) ID() string   { return "FND-OPS-005" }
func (ops005) Version() int { return 1 }

func (r ops005) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if s, ok := needARM(r, in, "resource names"); ok {
		return []rules.Result{s}, nil
	}
	var out []rules.Result
	checked := false
	for _, res := range in.ARM.Resources {
		row, found := rowFor(res.Type)
		if !found {
			if strings.EqualFold(res.Type, typeDeployment) {
				out = append(out, rules.SkipFor(r, rules.ReasonNotApplicable,
					"no verified naming constraint for "+res.Type+": the specification defines no pattern or length for deployment names", "", armRef(res)))
			}
			continue
		}
		name, fromEnv, skip := resolveName(res, in.Environment)
		if skip != "" {
			out = append(out, rules.SkipFor(r, rules.ReasonUnresolvedValue(skip),
				"the resource name cannot be resolved offline, so its length and characters are not checked", "resolved resource name", armRef(res)))
			continue
		}
		checked = true
		if got := r.judge(res, row, name, fromEnv); got != nil {
			out = append(out, *got)
		}
	}
	if len(out) == 0 && checked {
		out = append(out, rules.Pass())
	}
	return out, nil
}

func rowFor(typ string) (nameConstraint, bool) {
	for _, row := range nameTable {
		if strings.EqualFold(row.typ, typ) {
			return row, true
		}
	}
	return nameConstraint{}, false
}

// resolveName returns the resolved name, whether it came from an azd environment value, or the reason
// it cannot be resolved.
func resolveName(res model.Resource, env *model.Environment) (name string, fromEnv bool, skipReason string) {
	name = res.Name
	if model.IsARMExpression(name) {
		v, fe, err := evalARM(name, env)
		if err != nil {
			return "", false, reasonOf(err)
		}
		s, ok := v.(string)
		if !ok {
			return "", false, reasonUnsupported
		}
		name, fromEnv = s, fe
	}
	if strings.EqualFold(res.Type, typeProject) {
		name = lastSegment(name)
	}
	return name, fromEnv, ""
}

func (r ops005) judge(res model.Resource, row nameConstraint, name string, fromEnv bool) *rules.Result {
	var problems []string
	uncertain := false
	n := utf8.RuneCountInString(name)
	switch {
	case n < row.min:
		problems = append(problems, fmt.Sprintf("length %d is below the minimum %d", n, row.min))
	case n > row.max && row.hardMax > row.max && n <= row.hardMax:
		problems = append(problems, fmt.Sprintf("length %d exceeds %d (the naming-rules reference), but the specification allows up to %d", n, row.max, row.hardMax))
		uncertain = true
	case n > row.max && row.hardMax > row.max:
		problems = append(problems, fmt.Sprintf("length %d exceeds %d, the maximum of every source", n, row.hardMax))
	case n > row.max:
		problems = append(problems, fmt.Sprintf("length %d exceeds the maximum %d", n, row.max))
	}
	problems = append(problems, row.check(name)...)
	if len(problems) == 0 {
		return nil
	}
	shown := fmt.Sprintf("name %q", name)
	if fromEnv {
		shown = fmt.Sprintf("name resolved from the azd environment (%d characters)", n)
	}
	ev := fmt.Sprintf("%s %s violates the provider constraints: %s.", res.Type, shown, strings.Join(problems, "; "))
	f := rules.NewFinding(r, armRef(res), ev,
		"Choose a name that satisfies the provider limits for every environment name azd can produce, and constrain name parameters with @minLength and @maxLength.")
	f.Key = "name"
	f.Location = armLoc(res)
	f.Fix = fixOPS005
	f.Confidence = lowered(sdk.ConfidenceCertain, res.Meta)
	if uncertain {
		f.Confidence = sdk.ConfidenceUncertain
		x := rules.Uncertain(f)
		return &x
	}
	x := rules.Fail(f)
	return &x
}

const fixOPS005 = `@minLength(3)
@maxLength(24)
param storageName string

resource sa 'Microsoft.Storage/storageAccounts@2024-01-01' = {
  name: toLower(replace(storageName, '-', ''))
  location: location
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
}`

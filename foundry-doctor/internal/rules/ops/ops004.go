package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Tag limits (Azure Resource Manager tag-resources article): 50 pairs, name 512, value 256, and 128 for
// tag names on storage accounts.
const (
	maxTagPairs        = 50
	maxTagName         = 512
	maxTagNameStorage  = 128
	maxTagValue        = 256
	tagsFix            = "@description('Tags required by the organisation.')\nparam tags requiredTags\ntype requiredTags = { Env: string, CostCode: string }\n\nresource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {\n  name: accountName\n  location: location\n  kind: 'AIServices'\n  sku: { name: 'S0' }\n  tags: tags\n}"
	tagsRecommendation = "Define the required tags once (for example in a Bicep user-defined type), pass them to every resource, and enforce them at runtime with Azure Policy (Require a tag on resources). Foundry Doctor reports where the template omits them."
)

// taggableTypes is the rule's own list of Foundry-stack types known to carry tags (ADR-007 baseline).
var taggableTypes = []string{
	typeAccount, typeProject, typeDeployment, typeSearch, typeAPIM, typeStorage, typeCosmos,
	typeAppInsights, typeWorkspace, typeResGroup,
}

type ops004 struct{}

func (ops004) ID() string   { return "FND-OPS-004" }
func (ops004) Version() int { return 1 }

func inList(list []string, typ string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, typ) })
}

func (r ops004) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if s, ok := needARM(r, in, "resource tags"); ok {
		return []rules.Result{s}, nil
	}
	if len(in.Policy.Tags.Required) == 0 {
		detail := "policy.tags.required is not set"
		if in.Policy.Tags.Required != nil {
			detail = "policy.tags.required is empty"
		}
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyTagsRequired), detail,
			"policy.tags.required in the profile", projectRef(in))}, nil
	}
	scope := taggableTypes
	if in.Policy.Tags.ResourceTypes != nil {
		scope = in.Policy.Tags.ResourceTypes
	}
	formats := map[string]*regexp.Regexp{}
	var out []rules.Result
	for _, t := range in.Policy.Tags.Required {
		if t.Format == "" {
			continue
		}
		re, err := regexp.Compile(t.Format)
		if err != nil {
			return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("invalid-tag-format"),
				fmt.Sprintf("the format of required tag %q is not a valid regular expression", t.Name), "valid policy.tags.required format", projectRef(in))}, nil
		}
		formats[strings.ToLower(t.Name)] = re
	}
	for _, res := range in.ARM.Resources {
		if !inList(scope, res.Type) {
			continue
		}
		if !inList(taggableTypes, res.Type) {
			out = append(out, rules.SkipFor(r, rules.ReasonNotApplicable,
				"tag support of "+res.Type+" is not verified, so the type is not checked", "", armRef(res)))
			continue
		}
		out = append(out, r.checkResource(in, res, formats)...)
	}
	return out, nil
}

// tagSet is the tags object of one resource as far as it is known.
type tagSet struct {
	values        map[string]any // name -> value (string, or other literal)
	unresolvedKey bool           // a tag name is an expression: any required tag may be supplied by it
	unresolvedVal map[string]bool
}

func (r ops004) readTags(res model.Resource, env *model.Environment) (ts tagSet, skip string) {
	v := res.Get("tags")
	var m map[string]any
	switch v.State {
	case model.Absent:
		return tagSet{values: map[string]any{}}, ""
	case model.Unresolved:
		raw, _ := res.Body["tags"].(string)
		ev, _, err := evalARM(raw, env)
		if err != nil {
			return tagSet{}, reasonOf(err)
		}
		mm, ok := ev.(map[string]any)
		if !ok {
			return tagSet{}, reasonUnsupported
		}
		m = mm
	default:
		mm, ok := v.Lit.(map[string]any)
		if !ok {
			return tagSet{}, "not-an-object"
		}
		m = mm
	}
	ts = tagSet{values: map[string]any{}, unresolvedVal: map[string]bool{}}
	for k, val := range m {
		if model.IsARMExpression(k) {
			ts.unresolvedKey = true
			continue
		}
		ts.values[k] = val
		if s, ok := val.(string); ok && model.IsARMExpression(s) {
			ts.unresolvedVal[k] = true
		}
	}
	return ts, ""
}

func (r ops004) checkResource(in *model.Input, res model.Resource, formats map[string]*regexp.Regexp) []rules.Result {
	ts, skip := r.readTags(res, in.Environment)
	if skip != "" {
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("tags-"+skip),
			"the tags of the resource depend on a value that is not known offline (a parameter, or tags supplied by Azure Policy), so they are not reported as missing", "resolved tags object", armRef(res))}
	}
	conf := lowered(sdk.ConfidenceLikely, res.Meta)
	var out []rules.Result
	fail := func(key, evidence string) {
		f := rules.NewFinding(r, armRef(res), evidence, tagsRecommendation)
		f.Key = key
		f.Location = armLoc(res)
		f.Fix = tagsFix
		f.Confidence = conf
		out = append(out, rules.Fail(f))
	}
	skipRes := func(why, detail string) {
		out = append(out, rules.SkipFor(r, rules.ReasonUnresolvedValue(why), detail, "resolved tags object", armRef(res)))
	}
	clean := true
	for _, req := range in.Policy.Tags.Required {
		name, value, found := lookupTag(ts, req.Name)
		key := strings.ToLower(req.Name)
		if !found {
			if ts.unresolvedKey {
				skipRes("tag-name-expression", "a tag name in the resource is an expression, so required tag "+req.Name+" may be supplied by it")
			} else {
				fail("missing-tag:"+key, fmt.Sprintf("%s %q has no tag %q (tag names compare case-insensitively).", res.Type, res.Name, req.Name))
			}
			clean = false
			continue
		}
		re := formats[key]
		if re == nil {
			continue
		}
		s, isStr := value.(string)
		switch {
		case ts.unresolvedVal[name] || (isStr && model.IsARMExpression(s)):
			skipRes("tag-value-expression", "the value of tag "+req.Name+" is not known offline, so its format is not checked")
			clean = false
		case !re.MatchString(fmt.Sprint(value)):
			fail("format:"+key, fmt.Sprintf("%s %q: the value of tag %q does not match the format required by the profile.", res.Type, res.Name, req.Name))
			clean = false
		}
	}
	nameLimit := maxTagName
	if strings.EqualFold(res.Type, typeStorage) {
		nameLimit = maxTagNameStorage
	}
	if len(ts.values) > maxTagPairs {
		fail("limit:count", fmt.Sprintf("%s %q has %d tags; the limit is %d.", res.Type, res.Name, len(ts.values), maxTagPairs))
		clean = false
	}
	names := make([]string, 0, len(ts.values))
	for k := range ts.values {
		names = append(names, k)
	}
	slices.Sort(names)
	tooLongName, tooLongValue := 0, 0
	for _, k := range names {
		if utf8.RuneCountInString(k) > nameLimit {
			tooLongName++
		}
		if s, ok := ts.values[k].(string); ok && !model.IsARMExpression(s) && utf8.RuneCountInString(s) > maxTagValue {
			tooLongValue++
		}
	}
	if tooLongName > 0 {
		fail("limit:name-length", fmt.Sprintf("%s %q has %d tag name(s) longer than %d characters.", res.Type, res.Name, tooLongName, nameLimit))
		clean = false
	}
	if tooLongValue > 0 {
		fail("limit:value-length", fmt.Sprintf("%s %q has %d tag value(s) longer than %d characters.", res.Type, res.Name, tooLongValue, maxTagValue))
		clean = false
	}
	if clean {
		out = append(out, rules.Pass())
	}
	return out
}

// lookupTag finds a tag by name, ignoring case, and returns the name as written. When several names
// differ only by case the first in sorted order wins, for determinism.
func lookupTag(ts tagSet, want string) (name string, value any, found bool) {
	keys := make([]string, 0, len(ts.values))
	for k := range ts.values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if strings.EqualFold(k, want) {
			return k, ts.values[k], true
		}
	}
	return "", nil, false
}

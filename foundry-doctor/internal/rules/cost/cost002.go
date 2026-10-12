package cost

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// devFixedSKUs is FND-COST-002.
type devFixedSKUs struct{}

func (devFixedSKUs) ID() string   { return "FND-COST-002" }
func (devFixedSKUs) Version() int { return 1 }

const (
	recommendation = "In dev use pay-per-token deployments (GlobalStandard or Standard), the Search free or basic tier with one replica and " +
		"one partition, an API Management Developer or consumption-style tier, and the lowest Cosmos DB throughput that the Foundry Agent Service " +
		"setup allows. Reserve production-sized capacity for test and production environments."
	fix = "# azure.yaml - dev environment values\ndeployments:\n  - name: chat\n    model: { format: OpenAI, name: gpt-5.4-mini, version: \"2026-03-17\" }\n    sku: { name: GlobalStandard, capacity: 10 }\n"

	// cosmosAgentMinimum is the platform minimum of the Foundry Agent Service standard setup (RU/s):
	// throughput at or below it is never reported (catalogue evidence).
	cosmosAgentMinimum = 3000
)

var provisionedDeployment = []string{"GlobalProvisionedManaged", "DataZoneProvisionedManaged", "ProvisionedManaged"}
var searchSKUs = []string{"standard2", "standard3", "storage_optimized_l1", "storage_optimized_l2"}
var apimSKUs = []string{"Premium", "Isolated"}

// Evaluate checks the dev environment for production-sized fixed-capacity SKUs. The environment tier
// comes from policy.environments.development only (ADR-007); it is never guessed from the name.
// SKU comparison is case-sensitive as typed in the sources (catalogue notes). A value that is an
// unresolved parameter or expression is skipped, not judged.
func (r devFixedSKUs) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	proj := sdk.ResourceRef{Kind: "project", Name: in.Project.Name}
	if in.Policy.Environments.Development == nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(model.KeyEnvironmentsDevelopment),
			"the development environments are not listed; the tier is never guessed from the environment name",
			"profile key "+model.KeyEnvironmentsDevelopment, proj)}, nil
	}
	name := in.Project.CurrentEnvironment
	if in.Environment != nil {
		name = in.Environment.Name
	}
	switch {
	case name == "":
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable, "no azd environment is selected, so it cannot be classified as dev",
			"a selected azd environment", proj)}, nil
	case !slices.Contains(in.Policy.Environments.Development, name):
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"environment-not-classified-as-dev: the selected environment is not listed in policy.environments.development; only dev environments are evaluated",
			"", proj)}, nil
	}

	c := &checker{r: r, in: in, exempt: in.Policy.Cost.ProductionSizedSkuExemptions}
	c.yamlDeployments()
	if !in.Project.HasARM() || in.ARM == nil {
		detail := "iac is " + string(in.Project.IaC) + ": no compiled ARM template; Search, API Management and Cosmos DB sub-checks were not run"
		c.out = append(c.out, rules.SkipFor(r, rules.ReasonSyntheticInfrastructure, detail, "compiled ARM template", proj))
		return finish(c.out), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", r.ID(), err)
	}
	for _, res := range in.ARM.Resources {
		switch typ := strings.ToLower(res.Type); {
		case typ == "microsoft.cognitiveservices/accounts/deployments":
			c.skuName(res, "sku.name", provisionedDeployment, "ptu-deployment", "reserved provisioned-throughput (PTU) capacity")
		case typ == "microsoft.search/searchservices":
			c.skuName(res, "sku.name", searchSKUs, "search-sku", "a production-sized Search tier")
			c.countAbove1(res, "properties.replicaCount", "replica-count", "Search replicas")
			c.countAbove1(res, "properties.partitionCount", "partition-count", "Search partitions")
		case typ == "microsoft.apimanagement/service":
			c.skuName(res, "sku.name", apimSKUs, "apim-sku", "an API Management Premium or Isolated tier")
			c.countAbove1(res, "sku.capacity", "apim-capacity", "API Management units")
		case strings.HasPrefix(typ, "microsoft.documentdb/") && strings.HasSuffix(typ, "/throughputsettings"):
			c.cosmos(res)
		}
	}
	return finish(c.out), nil
}

func finish(out []rules.Result) []rules.Result {
	if len(out) == 0 {
		return []rules.Result{rules.Pass()}
	}
	return out
}

type checker struct {
	r      rules.Rule
	in     *model.Input
	exempt []string
	out    []rules.Result
}

func (c *checker) exempted(res model.Resource) bool {
	return slices.Contains(c.exempt, res.Name) || (res.Symbolic != "" && slices.Contains(c.exempt, res.Symbolic))
}

func ref(res model.Resource, path string) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "arm-resource", Type: res.Type, Name: res.Name, Pointer: res.Meta.Pointer + "/" + strings.ReplaceAll(path, ".", "/")}
}

func (c *checker) skipUnresolved(res model.Resource, path string, v model.Value) {
	c.out = append(c.out, rules.SkipFor(c.r, rules.ReasonUnresolvedValue(v.Reason),
		fmt.Sprintf("%s of %s is not a literal in the compiled template", path, res.Type), "a literal or supplied parameter value", ref(res, path)))
}

func (c *checker) fail(res model.Resource, path, key, evidence string) {
	f := rules.NewFinding(c.r, ref(res, path), evidence, recommendation)
	f.Key = key
	f.Fix = fix
	f.Location = sdk.Location{File: res.File, Line: res.Location.Line, Column: res.Location.Column, Pointer: res.Meta.Pointer}
	f.Confidence = sdk.ConfidenceLikely
	if res.Meta.MayNotDeploy || res.Meta.MultiInstance {
		f.Confidence = sdk.ConfidenceUncertain
		c.out = append(c.out, rules.Uncertain(f))
		return
	}
	c.out = append(c.out, rules.Fail(f))
}

// skuName flags a literal sku name from bad.
func (c *checker) skuName(res model.Resource, path string, bad []string, key, what string) {
	if c.exempted(res) {
		return
	}
	v := res.Get(path)
	switch {
	case v.State == model.Unresolved:
		c.skipUnresolved(res, path, v)
	case v.State == model.Literal:
		if s, ok := v.String(); ok && slices.Contains(bad, s) {
			c.fail(res, path, key, fmt.Sprintf("%s %q in the dev environment uses %s (%s is a fixed-capacity SKU name). The dev environment is listed in policy.environments.development.", res.Type, res.Name, what, path))
		}
	}
}

// countAbove1 flags a literal numeric value greater than 1.
func (c *checker) countAbove1(res model.Resource, path, key, what string) {
	if c.exempted(res) {
		return
	}
	v := res.Get(path)
	if v.State == model.Unresolved {
		c.skipUnresolved(res, path, v)
		return
	}
	if n, ok := v.Number(); ok && n > 1 {
		c.fail(res, path, key, fmt.Sprintf("%s %q in the dev environment has more than one of %s (%s is greater than 1).", res.Type, res.Name, what, path))
	}
}

// cosmos checks provisioned and autoscale throughput against policy.cost.devMaxCosmosThroughput.
func (c *checker) cosmos(res model.Resource) {
	if c.exempted(res) {
		return
	}
	limit := c.in.Policy.Cost.DevMaxCosmosThroughput
	if limit == nil {
		c.out = append(c.out, rules.SkipFor(c.r, rules.ReasonProfileKeyMissing(model.KeyCostDevMaxCosmosThroughput),
			"the Cosmos DB throughput sub-check needs a profile limit; no default is shipped",
			"profile key "+model.KeyCostDevMaxCosmosThroughput, ref(res, "properties.resource")))
		return
	}
	for _, p := range []struct{ path, key string }{
		{"properties.resource.throughput", "cosmos-throughput"},
		{"properties.resource.autoscaleSettings.maxThroughput", "cosmos-autoscale-max"},
	} {
		v := res.Get(p.path)
		if v.State == model.Unresolved {
			c.skipUnresolved(res, p.path, v)
			continue
		}
		n, ok := v.Number()
		if !ok || int(n) <= *limit || int(n) <= cosmosAgentMinimum {
			continue
		}
		c.fail(res, p.path, p.key, fmt.Sprintf("%s %q in the dev environment sets %s above the profile limit of %s RU/s (the Foundry Agent Service minimum of %d RU/s is never reported).",
			res.Type, res.Name, p.path, strconv.Itoa(*limit), cosmosAgentMinimum))
	}
}

// yamlDeployments checks services.*.deployments[].sku.name in azure.yaml (shape from the vendored
// azure.ai.agents Deployment schema) for provisioned deployment SKUs.
func (c *checker) yamlDeployments() {
	y := c.in.AzureYAML
	if y == nil {
		return
	}
	for _, svc := range y.Services {
		deps := svc.Node.Lookup("deployments")
		if deps == nil || deps.Kind != model.NodeSequence {
			continue
		}
		for i, d := range deps.Children {
			n := d.Lookup("sku", "name")
			s, ok := scalar(n)
			if !ok || !slices.Contains(provisionedDeployment, s) {
				continue
			}
			f := rules.NewFinding(c.r, sdk.ResourceRef{Kind: "service", Name: svc.Name, Pointer: "/deployments/" + strconv.Itoa(i) + "/sku/name"},
				fmt.Sprintf("Service %s deployments[%d] uses a provisioned-throughput (PTU) SKU in the dev environment.", svc.Name, i), recommendation)
			f.Key = "ptu-deployment:" + svc.Name + ":" + strconv.Itoa(i)
			f.Fix = fix
			f.Location = sdk.Location{File: y.File, Line: n.Pos.Line, Column: n.Pos.Column}
			c.out = append(c.out, rules.Fail(f))
		}
	}
}

func scalar(n *model.Node) (string, bool) {
	if n == nil || n.Kind != model.NodeScalar {
		return "", false
	}
	return n.Value, true
}

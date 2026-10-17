package preflight

import (
	"context"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// whatIfOutcome is the shared result of the single what-if of a run.
type whatIfOutcome struct {
	res   azure.WhatIfResult
	skip  string
	err   error
	scope deployScope
}

// facts returns the scope and parameter declarations of the compiled template
// (read once per run).
func (e *env) facts(in *sdk.Input) templateFacts {
	e.tfOnce.Do(func() { e.tf = parseTemplate(e.rawTemplate(in)) })
	return e.tf
}

// deploymentScope returns the template's deployment scope. unknownReason is
// non-empty, naming why, when the scope could not be determined.
func (e *env) deploymentScope(in *sdk.Input) (deployScope, string) {
	tf := e.facts(in)
	switch {
	case tf.err != nil:
		return scopeUnknown, "the compiled ARM template is not available"
	case tf.scope == scopeUnknown:
		return scopeUnknown, "the template $schema is missing or not a known deployment template schema"
	}
	return tf.scope, ""
}

// whatIf runs the single shared what-if (once) and returns its outcome. It
// never runs without the explicit opt-in, and it is skipped, with a distinct
// reason, when
//
//   - the template targets a management group or the tenant (not supported),
//   - the template scope cannot be determined,
//   - a resource-group-scope template targets a group that does not exist,
//   - a template parameter has no resolvable non-secret value ("template
//     parameters unresolved: <names>"): sending a guess would make the
//     prediction describe a different deployment, so it is never a pass.
func (e *env) whatIf(ctx context.Context, in *sdk.Input) whatIfOutcome {
	e.wiOnce.Do(func() {
		switch {
		case !e.d.WhatIfOptIn:
			e.wi.skip = sdk.SkipInputUnavailable + ": what-if was not requested; re-run with --what-if (or --preflight)"
		case e.d.WhatIf == nil:
			e.wi.skip = missingCap("Microsoft.Resources/deployments/whatIf", "Microsoft.Resources/deployments/whatIf/action")
		default:
			e.runWhatIf(ctx, in)
		}
	})
	return e.wi
}

func (e *env) runWhatIf(ctx context.Context, in *sdk.Input) {
	tpl := e.rawTemplate(in)
	if len(tpl) == 0 {
		e.wi.skip = sdk.SkipInputUnavailable + ": compiled ARM template is not available for what-if"
		return
	}
	tf := e.facts(in)
	if tf.err != nil {
		e.wi.skip = sdk.SkipInputUnavailable + ": compiled ARM template could not be read for what-if"
		return
	}
	e.wi.scope = tf.scope
	var scope, location string
	switch tf.scope {
	case scopeManagementGroup, scopeTenant:
		e.wi.skip = sdk.SkipInputUnavailable + ": what-if for a " + tf.scope.String() + "-scope template is not supported (resource-group and subscription scope only)"
		return
	case scopeSubscription:
		if m := needTarget(false, e.d.Target); m != "" {
			e.wi.skip = m
			return
		}
		if e.d.Target.Location == "" {
			e.wi.skip = sdk.SkipInputUnavailable + ": target location is not set (use --location or AZURE_LOCATION); a subscription-scope what-if needs one"
			return
		}
		scope, location = e.subScope(), e.d.Target.Location
	case scopeResourceGroup:
		if m := needTarget(true, e.d.Target); m != "" {
			e.wi.skip = m
			return
		}
		scope = e.rgScope()
		if e.d.Inventory != nil {
			rg, err := e.d.Inventory.ResourceGroup(ctx, e.sub(), e.d.Target.ResourceGroup)
			if err != nil {
				e.wi.err = err
				e.wi.skip = azureReason(err)
				return
			}
			if !rg.Exists {
				e.wi.skip = sdk.SkipInputUnavailable + ": target resource group " + e.d.Target.ResourceGroup +
					" does not exist, so a resource-group-scope what-if cannot run (create it first, or use a subscription-scope template)"
				return
			}
		}
	default:
		e.wi.skip = sdk.SkipInputUnavailable + ": the template deployment scope could not be determined from its $schema, so the what-if scope is unknown"
		return
	}
	values, unresolved := resolveParameters(tf.params, e.d.Parameters)
	if len(unresolved) > 0 {
		e.wi.skip = sdk.SkipInputUnavailable + ": template parameters unresolved: " + capNames(unresolved, 10) +
			" (no non-secret value is available from the parameter file or a template default; what-if was not run)"
		return
	}
	res, err := e.d.WhatIf.Run(ctx, azure.WhatIfRequest{OptIn: true, Scope: scope, Location: location, Template: tpl, Parameters: values})
	if err != nil {
		e.wi.err = err
		e.wi.skip = azureReason(err)
		return
	}
	e.wi.res = res
}

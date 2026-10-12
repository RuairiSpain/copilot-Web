package net

import (
	"context"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// meta is the identity every rule of the package embeds. ID and version equal the catalogue entry.
type meta struct {
	id  string
	ver int
}

func (m meta) ID() string   { return m.id }
func (m meta) Version() int { return m.ver }

// guard returns the skip every ARM-only rule emits when no compiled template exists. The engine
// short-circuits before Evaluate; this keeps the rule safe when it is called directly.
func guard(r rules.Rule, in *model.Input) ([]rules.Result, bool) {
	if in == nil || in.ARM == nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonSyntheticInfrastructure,
			"no compiled ARM template is available", "compiled ARM template", sdk.ResourceRef{})}, true
	}
	return nil, false
}

// run is the shared Evaluate prologue: it checks the context, applies the guard and builds the index.
func run(ctx context.Context, r rules.Rule, in *model.Input) (*topo, []rules.Result, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	if res, stop := guard(r, in); stop {
		return nil, res, true, nil
	}
	return newTopo(in), nil, false, nil
}

func resRef(r model.Resource) sdk.ResourceRef {
	return sdk.ResourceRef{Kind: "arm-resource", Type: r.Type, Name: r.Name, Pointer: r.Meta.Pointer}
}

// loc locates a finding. prop is a JSON pointer inside the resource, for example /properties/networkAcls.
func loc(r model.Resource, prop string) sdk.Location {
	return sdk.Location{File: r.File, Line: r.Location.Line, Column: r.Location.Column, Pointer: r.Meta.Pointer + prop}
}

func rank(c sdk.Confidence) int {
	switch c {
	case sdk.ConfidenceCertain:
		return 3
	case sdk.ConfidenceLikely:
		return 2
	}
	return 1
}

// confidence is the lowest of the catalogue confidence, the compiler's confidence in the resource
// location, and likely when a resource may not deploy or has an unknown instance count.
func confidence(base sdk.Confidence, rs ...model.Resource) sdk.Confidence {
	out := base
	lower := func(c sdk.Confidence) {
		if rank(c) < rank(out) {
			out = c
		}
	}
	for _, r := range rs {
		if c := sdk.Confidence(r.Meta.Confidence); c != "" {
			lower(c)
		}
		if r.Meta.MayNotDeploy || r.Meta.MultiInstance {
			lower(sdk.ConfidenceLikely)
		}
	}
	return out
}

// text carries the fixed wording of a finding.
type text struct{ evidence, recommendation, fix string }

func failure(r rules.Rule, res model.Resource, prop, key string, tx text, base sdk.Confidence, others ...model.Resource) rules.Result {
	f := rules.NewFinding(r, resRef(res), tx.evidence, tx.recommendation)
	f.Fix, f.Key, f.Location = tx.fix, key, loc(res, prop)
	f.Confidence = confidence(base, append([]model.Resource{res}, others...)...)
	return rules.Fail(f)
}

func doubt(r rules.Rule, res model.Resource, prop, key string, tx text) rules.Result {
	f := rules.NewFinding(r, resRef(res), tx.evidence, tx.recommendation)
	f.Fix, f.Key, f.Location = tx.fix, key, loc(res, prop)
	f.Confidence = sdk.ConfidenceUncertain
	return rules.Uncertain(f)
}

func skipped(r rules.Rule, res model.Resource, reason, detail, missing string) rules.Result {
	return rules.SkipFor(r, reason, detail, missing, resRef(res))
}

func unresolvedSkip(r rules.Rule, res model.Resource, what string) rules.Result {
	return skipped(r, res, rules.ReasonUnresolvedValue(what), what+" is an expression or parameter that cannot be read from the template", "resolved template value")
}

// accounts returns the AI Services accounts. An account whose kind is not a literal yields a skip.
func accounts(r rules.Rule, t *topo) ([]int, []rules.Result) {
	var out []int
	var skips []rules.Result
	for _, i := range t.ofType(typeAccount) {
		v := t.res[i].Get("kind")
		if v.State == model.Unresolved {
			skips = append(skips, unresolvedSkip(r, t.res[i], "kind"))
			continue
		}
		if s, ok := v.String(); ok && strings.EqualFold(s, kindAIServices) {
			out = append(out, i)
		}
	}
	return out, skips
}

// strProp reads a property as a string. state is Absent, Literal (s set) or Unresolved. A literal
// that is not a string is reported Unresolved so the caller never treats it as a known value.
func strProp(r model.Resource, path string) (s string, state model.ValueState) {
	v := r.Get(path)
	switch v.State {
	case model.Absent:
		return "", model.Absent
	case model.Literal:
		if str, ok := v.String(); ok {
			return str, model.Literal
		}
	}
	return "", model.Unresolved
}

// arrayProp returns the literal length of an array property. state is Absent, Literal or Unresolved.
func arrayProp(r model.Resource, path string) (n int, state model.ValueState) {
	v := r.Get(path)
	switch v.State {
	case model.Absent:
		return 0, model.Absent
	case model.Literal:
		if a, ok := v.Lit.([]any); ok {
			return len(a), model.Literal
		}
	}
	return 0, model.Unresolved
}

// isPrivate reports whether an account is in a private mode: public access Disabled or an agent
// network injection. known is false when a deciding value is unresolved.
func isPrivate(r model.Resource) (private, known bool) {
	pna, st := strProp(r, "properties.publicNetworkAccess")
	if st == model.Unresolved {
		return false, false
	}
	if st == model.Literal && strings.EqualFold(pna, "Disabled") {
		return true, true
	}
	inj, ok := agentInjections(r)
	if !ok {
		return false, false
	}
	return len(inj) > 0, true
}

// injection is one properties.networkInjections entry with scenario agent.
type injection struct {
	index          int
	managed        bool
	managedKnown   bool
	subnetRaw      any
	subnetPresent  bool
	subnetPathBase string
}

// agentInjections returns the networkInjections entries whose scenario is agent. ok is false when the
// array or a scenario cannot be read.
func agentInjections(r model.Resource) ([]injection, bool) {
	n, st := arrayProp(r, "properties.networkInjections")
	switch st {
	case model.Absent:
		return nil, true
	case model.Unresolved:
		return nil, false
	}
	var out []injection
	ok := true
	for i := 0; i < n; i++ {
		base := "properties.networkInjections[" + strconv.Itoa(i) + "]"
		sc, st := strProp(r, base+".scenario")
		if st != model.Literal {
			ok = false
			continue
		}
		if !strings.EqualFold(sc, "agent") {
			continue
		}
		in := injection{index: i, subnetPathBase: base}
		raw := rawAt(r.Body, "properties", "networkInjections", i, "subnetArmId")
		in.subnetRaw, in.subnetPresent = raw, raw != nil
		if v := r.Get(base + ".useMicrosoftManagedNetwork"); v.State == model.Literal {
			if b, isBool := v.Bool(); isBool {
				in.managed, in.managedKnown = b, true
			}
		} else if v.State == model.Absent {
			in.managedKnown = true
		}
		out = append(out, in)
	}
	return out, ok
}

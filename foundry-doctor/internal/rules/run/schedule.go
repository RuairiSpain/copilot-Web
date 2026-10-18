package run

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Schedule evaluates the selected runtime rules through runtime.Scheduler, so
// that Options.Parallelism bounds the number of rules in flight and
// Options.Timeout bounds each probe, then returns rules that replay the stored
// outcomes in the engine's sequential pass. A probe that exceeds its timeout is
// reported as an "input-unavailable" skip naming --timeout; a cancelled run
// returns the context error.
func Schedule(ctx context.Context, d Deps, in *sdk.Input, selected []string) []sdk.Rule {
	return schedule(ctx, Register(d), d.Options, in, selected)
}

type outcome struct {
	res sdk.Result
	err error
}

func schedule(ctx context.Context, rules []sdk.Rule, opts runtime.Options, in *sdk.Input, selected []string) []sdk.Rule {
	want := map[string]bool{}
	for _, id := range selected {
		want[id] = true
	}
	var (
		mu       sync.Mutex
		outcomes = map[string]outcome{}
		probes   []runtime.Probe
	)
	store := func(id string, o outcome) {
		mu.Lock()
		outcomes[id] = o
		mu.Unlock()
	}
	for _, r := range rules {
		if want[r.ID()] {
			probes = append(probes, probe{id: r.ID(), class: classes[r.ID()], timeout: opts.Timeout, rule: r, in: in, store: store})
		}
	}
	_, _ = runtime.Scheduler{Parallelism: opts.Parallelism}.Run(ctx, probes)
	out := make([]sdk.Rule, 0, len(rules))
	for _, r := range rules {
		id := r.ID()
		if !want[id] {
			out = append(out, r) // not pre-computed; evaluated in the engine's pass
			continue
		}
		mu.Lock()
		o, ok := outcomes[id]
		mu.Unlock()
		if !ok {
			// The scheduler never started the probe: the run was cancelled first.
			o = outcome{err: ctx.Err()}
			if o.err == nil {
				o.err = context.Canceled
			}
		}
		out = append(out, replay{id: id, o: o})
	}
	return out
}

type replay struct {
	id string
	o  outcome
}

func (r replay) ID() string { return r.id }
func (r replay) Evaluate(context.Context, *sdk.Input) (sdk.Result, error) {
	return r.o.res, r.o.err
}

// probe adapts a rule to runtime.Probe. It always returns a nil error to the
// scheduler: the rule's own outcome (including a skip) is stored separately.
type probe struct {
	id      string
	class   runtime.Class
	timeout time.Duration
	rule    sdk.Rule
	in      *sdk.Input
	store   func(string, outcome)
}

func (p probe) ID() string           { return p.id }
func (p probe) Class() runtime.Class { return p.class }
func (p probe) Timeout() time.Duration {
	return runtime.Timeout(p.timeout, 30*time.Second)
}
func (p probe) Run(ctx context.Context) (runtime.Result, error) {
	res, err := p.rule.Evaluate(ctx, p.in)
	p.store(p.id, outcome{res: res, err: err})
	return runtime.Result{State: stateOf(res, err), Class: p.class}, nil
}

func stateOf(res sdk.Result, err error) runtime.State {
	switch {
	case err != nil:
		return runtime.StateSkipped
	case len(res.Findings) > 0:
		return runtime.StateFail
	case res.Skipped != nil && strings.HasPrefix(res.Skipped.Reason, runtime.UncertainPrefix):
		return runtime.StateUncertain
	case res.Skipped != nil:
		return runtime.StateSkipped
	default:
		return runtime.StatePass
	}
}

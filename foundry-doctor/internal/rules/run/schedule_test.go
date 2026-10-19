package run

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type funcRule struct {
	id string
	fn func(context.Context) (sdk.Result, error)
}

func (f funcRule) ID() string { return f.id }
func (f funcRule) Evaluate(ctx context.Context, _ *sdk.Input) (sdk.Result, error) {
	return f.fn(ctx)
}

func TestScheduleBoundsConcurrencyAndReplaysResults(t *testing.T) {
	var inflight, peak atomic.Int32
	mk := func(id string, finding bool) sdk.Rule {
		return funcRule{id: id, fn: func(context.Context) (sdk.Result, error) {
			n := inflight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(15 * time.Millisecond)
			inflight.Add(-1)
			if finding {
				return sdk.Result{Findings: []sdk.Finding{{Evidence: id}}}, nil
			}
			return sdk.Result{}, nil
		}}
	}
	rules := []sdk.Rule{mk("FND-RUN-001", true), mk("FND-RUN-002", false), mk("FND-RUN-003", false), mk("FND-RUN-004", true), mk("FND-RUN-005", false), mk("FND-RUN-006", false)}
	sel := []string{"FND-RUN-001", "FND-RUN-002", "FND-RUN-003", "FND-RUN-004", "FND-RUN-005"}
	out := schedule(context.Background(), rules, rt.Options{Parallelism: 2, Timeout: time.Second}, &sdk.Input{}, sel)
	if peak.Load() > 2 || peak.Load() < 2 {
		t.Errorf("peak concurrency = %d, want exactly the bound 2", peak.Load())
	}
	if len(out) != 6 {
		t.Fatalf("rules = %d", len(out))
	}
	for i, r := range out {
		if r.ID() != rules[i].ID() {
			t.Errorf("order changed: %s at %d", r.ID(), i)
		}
	}
	// Results are replayed without running the rule again.
	before := inflight.Load()
	res, err := out[0].Evaluate(context.Background(), &sdk.Input{})
	if err != nil || len(res.Findings) != 1 || res.Findings[0].Evidence != "FND-RUN-001" || inflight.Load() != before {
		t.Fatalf("replay = %+v err=%v", res, err)
	}
	// The unselected rule is returned as is (the engine decides whether to run it).
	if _, isReplay := out[5].(replay); isReplay {
		t.Error("an unselected rule must not be pre-computed")
	}
}

func TestSchedulePerProbeTimeoutBecomesInputUnavailable(t *testing.T) {
	slow := rule{id: "FND-RUN-004", fn: func(ctx context.Context, _ *sdk.Input) (sdk.Result, error) {
		<-ctx.Done()
		return sdk.Result{}, ctx.Err()
	}}
	fast := funcRule{id: "FND-RUN-001", fn: func(context.Context) (sdk.Result, error) { return sdk.Result{}, nil }}
	start := time.Now()
	out := schedule(context.Background(), []sdk.Rule{fast, slow}, rt.Options{Parallelism: 2, Timeout: 30 * time.Millisecond}, &sdk.Input{}, []string{"FND-RUN-001", "FND-RUN-004"})
	if time.Since(start) > 5*time.Second {
		t.Fatal("the per-probe timeout did not apply")
	}
	res, err := out[1].Evaluate(context.Background(), &sdk.Input{})
	if err != nil || res.Skipped == nil || !strings.HasPrefix(res.Skipped.Reason, "input-unavailable") || !strings.Contains(res.Skipped.Reason, "--timeout") {
		t.Fatalf("timed-out probe = %+v err=%v", res, err)
	}
	if res, err := out[0].Evaluate(context.Background(), &sdk.Input{}); err != nil || res.Skipped != nil {
		t.Fatalf("fast probe = %+v err=%v", res, err)
	}
}

func TestScheduleCancelledRunReturnsTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := funcRule{id: "FND-RUN-001", fn: func(c context.Context) (sdk.Result, error) { return sdk.Result{}, c.Err() }}
	out := schedule(ctx, []sdk.Rule{r}, rt.Options{Parallelism: 1, Timeout: time.Second}, &sdk.Input{}, []string{"FND-RUN-001"})
	if _, err := out[0].Evaluate(context.Background(), &sdk.Input{}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestScheduleWithTheRealRulesAndStates(t *testing.T) {
	d := baseDeps()
	d.RBAC = allGranted()
	d.Options.Parallelism = 3
	out := Schedule(context.Background(), d, baseInput(), RuleIDs())
	if len(out) != 8 {
		t.Fatalf("rules = %d", len(out))
	}
	if res, err := out[0].Evaluate(context.Background(), baseInput()); err != nil || res.Skipped != nil || len(res.Findings) != 0 {
		t.Fatalf("RUN-001 = %+v err=%v", res, err)
	}
	if stateOf(sdk.Result{Findings: []sdk.Finding{{}}}, nil) != rt.StateFail || stateOf(sdk.Result{}, errors.New("x")) != rt.StateSkipped ||
		stateOf(sdk.Result{Skipped: &sdk.Skip{Reason: rt.UncertainPrefix + "x"}}, nil) != rt.StateUncertain ||
		stateOf(sdk.Result{Skipped: &sdk.Skip{Reason: "input-unavailable: x"}}, nil) != rt.StateSkipped || stateOf(sdk.Result{}, nil) != rt.StatePass {
		t.Error("stateOf")
	}
	for _, id := range RuleIDs() {
		if classes[id] == "" {
			t.Errorf("%s has no probe class", id)
		}
	}
}

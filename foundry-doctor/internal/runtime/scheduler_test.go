package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProbe struct {
	id      string
	class   Class
	timeout time.Duration
	run     func(context.Context) (Result, error)
}

func (f fakeProbe) ID() string                              { return f.id }
func (f fakeProbe) Class() Class                            { return f.class }
func (f fakeProbe) Timeout() time.Duration                  { return f.timeout }
func (f fakeProbe) Run(ctx context.Context) (Result, error) { return f.run(ctx) }

func TestSchedulerRun(t *testing.T) {
	s := Scheduler{Parallelism: 2}
	res, err := s.Run(context.Background(), []Probe{
		fakeProbe{id: "b", class: ClassDataPlane, run: func(context.Context) (Result, error) {
			return Result{State: StatePass}, nil
		}},
		fakeProbe{id: "a", class: ClassControlPlane, timeout: 10 * time.Millisecond, run: func(ctx context.Context) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		}},
		fakeProbe{id: "c", class: ClassSafeLocal, run: func(context.Context) (Result, error) {
			return Result{}, errors.New("boom")
		}},
	})
	if err == nil {
		t.Fatal("expected joined error")
	}
	if len(res) != 1 || res["b"].Class != ClassDataPlane {
		t.Fatalf("results = %+v", res)
	}
}

func TestSchedulerEmptyAndDefaultParallelism(t *testing.T) {
	s := Scheduler{}
	res, err := s.Run(context.Background(), nil)
	if err != nil || len(res) != 0 {
		t.Fatalf("res=%v err=%v", res, err)
	}
	res, err = s.Run(context.Background(), []Probe{
		fakeProbe{id: "b", class: ClassSafeLocal, run: func(context.Context) (Result, error) { return Result{}, nil }},
		fakeProbe{id: "a", class: ClassControlPlane, run: func(context.Context) (Result, error) { return Result{}, nil }},
	})
	if err != nil || res["a"].Class != ClassControlPlane || res["b"].Class != ClassSafeLocal {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestResultRedactionHelpers(t *testing.T) {
	r := Result{Message: "token=abcd12345678", Details: []string{"bearer abcdefghijk"}}
	if got := r.RedactedMessage(); got == r.Message || got == "" {
		t.Fatalf("message not redacted: %q", got)
	}
	if len(r.RedactedDetails()) != 1 || r.RedactedDetails()[0] == r.Details[0] {
		t.Fatalf("details not redacted: %v", r.RedactedDetails())
	}
	if Timeout(0, time.Second) != time.Second {
		t.Fatal("fallback timeout not used")
	}
}

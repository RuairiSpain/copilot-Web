package network

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

type fakeResolver struct {
	answers []string
	err     error
}

func (f fakeResolver) LookupHost(context.Context, string) ([]string, error) { return f.answers, f.err }

func TestResolvePrivate(t *testing.T) {
	if got := ResolvePrivate(context.Background(), fakeResolver{}, runtime.VantageNone, "x"); got.State != runtime.StateSkipped {
		t.Fatalf("non-vnet state = %s", got.State)
	}
	if got := ResolvePrivate(context.Background(), fakeResolver{answers: []string{"10.0.0.4"}}, runtime.VantageVNet, "x"); got.State != runtime.StatePass {
		t.Fatalf("private state = %s", got.State)
	}
	if got := ResolvePrivate(context.Background(), fakeResolver{answers: []string{"52.1.1.1"}}, runtime.VantageVNet, "x"); got.State != runtime.StateFail {
		t.Fatalf("public state = %s", got.State)
	}
	if got := ResolvePrivate(context.Background(), fakeResolver{err: errors.New("boom")}, runtime.VantageVNet, "x"); got.State != runtime.StateSkipped {
		t.Fatalf("error state = %s", got.State)
	}
	if got := ResolvePrivate(context.Background(), fakeResolver{answers: nil}, runtime.VantageVNet, "x"); got.State != runtime.StateFail {
		t.Fatalf("empty state = %s", got.State)
	}
}

type fakeLinks struct {
	links []Link
	err   error
}

func (f fakeLinks) ListLinks(context.Context, string) ([]Link, error) { return f.links, f.err }

// ctxResolver returns the context error as soon as the context is done. With
// block set it waits for that; otherwise it checks once and answers.
type ctxResolver struct{ block bool }

func (c ctxResolver) LookupHost(ctx context.Context, _ string) ([]string, error) {
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []string{"10.0.0.4"}, nil
}

func TestLinkStateAndTimeoutResolver(t *testing.T) {
	if got := LinkState(context.Background(), fakeLinks{err: errors.New("boom")}, "zone"); got.State != runtime.StateSkipped {
		t.Fatalf("link error state = %+v", got)
	}
	if got := LinkState(context.Background(), fakeLinks{}, "zone"); got.State != runtime.StateUncertain {
		t.Fatalf("link empty state = %+v", got)
	}
	if got := LinkState(context.Background(), fakeLinks{links: []Link{{Name: "x", State: "Completed"}}}, "zone"); got.State != runtime.StatePass {
		t.Fatalf("link state = %+v", got)
	}
	if got := LinkState(context.Background(), fakeLinks{links: []Link{{Name: "x", State: "InProgress"}}}, "zone"); got.State != runtime.StateUncertain {
		t.Fatalf("link state = %+v", got)
	}
	// A real resolver may answer "localhost" from /etc/hosts without looking at
	// the context, so cancellation is tested with an inner resolver that does.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (TimeoutResolver{Inner: ctxResolver{}, Timeout: time.Millisecond}).LookupHost(ctx, "localhost"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup error = %v, want context.Canceled", err)
	}
	if _, err := (TimeoutResolver{Inner: ctxResolver{block: true}, Timeout: time.Millisecond}).LookupHost(context.Background(), "localhost"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out lookup error = %v, want context.DeadlineExceeded", err)
	}
	if got, err := (TimeoutResolver{Inner: fakeResolver{answers: []string{"10.0.0.4"}}, Timeout: time.Second}).LookupHost(context.Background(), "x"); err != nil || len(got) != 1 {
		t.Fatalf("fast lookup = %v, %v", got, err)
	}
	if _, err := (TimeoutResolver{Inner: NetResolver{Resolver: net.DefaultResolver}, Timeout: time.Second}).LookupHost(ctx, "localhost"); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("net resolver with cancelled context: %v", err)
	}
}

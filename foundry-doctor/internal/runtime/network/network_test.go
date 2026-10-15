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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (TimeoutResolver{Inner: NetResolver{Resolver: net.DefaultResolver}, Timeout: time.Millisecond}).LookupHost(ctx, "localhost"); err == nil {
		t.Fatal("expected cancelled lookup")
	}
}

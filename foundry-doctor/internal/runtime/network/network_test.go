package network

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/network/dnsstub"
)

type fakeResolver struct {
	answers []string
	err     error
}

func (f fakeResolver) LookupHost(context.Context, string) ([]string, error) { return f.answers, f.err }

func TestResolvePrivate(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		r        fakeResolver
		vantage  runtime.Vantage
		peBacked bool
		want     runtime.State
		contains string
	}{
		{"non-vnet vantage skips", fakeResolver{}, runtime.VantageNone, true, runtime.StateSkipped, "asserted by you"},
		{"local vantage skips", fakeResolver{answers: []string{"52.1.1.1"}}, runtime.VantageLocal, true, runtime.StateSkipped, "--vantage vnet"},
		{"private answer passes", fakeResolver{answers: []string{"10.0.0.4"}}, runtime.VantageVNet, true, runtime.StatePass, ""},
		{"private answer passes without proof", fakeResolver{answers: []string{"10.0.0.4"}}, runtime.VantageVNet, false, runtime.StatePass, ""},
		{"public answer on a private-endpoint dependency fails", fakeResolver{answers: []string{"52.1.1.1"}}, runtime.VantageVNet, true, runtime.StateFail, "private-endpoint backed"},
		{"public answer without proof is uncertain", fakeResolver{answers: []string{"52.1.1.1"}}, runtime.VantageVNet, false, runtime.StateUncertain, "not proven"},
		{"mixed answers on a private-endpoint dependency fail", fakeResolver{answers: []string{"10.0.0.4", "52.1.1.1"}}, runtime.VantageVNet, true, runtime.StateFail, ""},
		{"unparseable answer is uncertain, not a failure", fakeResolver{answers: []string{"not-an-ip"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither a private nor a clearly public"},
		{"CGNAT 100.64.0.0/10 is not clearly public", fakeResolver{answers: []string{"100.64.1.1"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither a private nor a clearly public"},
		{"loopback is not clearly public", fakeResolver{answers: []string{"127.0.0.1"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"link-local is not clearly public", fakeResolver{answers: []string{"169.254.10.4"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"IPv6 loopback is not clearly public", fakeResolver{answers: []string{"::1"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"IPv6 link-local is not clearly public", fakeResolver{answers: []string{"fe80::1"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"documentation range is not clearly public", fakeResolver{answers: []string{"203.0.113.7"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"IPv6 ULA is private", fakeResolver{answers: []string{"fd00::4"}}, runtime.VantageVNet, true, runtime.StatePass, ""},
		{"172.16/12 is private", fakeResolver{answers: []string{"172.20.0.4"}}, runtime.VantageVNet, true, runtime.StatePass, ""},
		{"public IPv6 on a private-endpoint dependency fails", fakeResolver{answers: []string{"2603:1030::1"}}, runtime.VantageVNet, true, runtime.StateFail, "public IP"},
		{"private plus CGNAT is uncertain", fakeResolver{answers: []string{"10.0.0.4", "100.100.0.1"}}, runtime.VantageVNet, true, runtime.StateUncertain, "neither"},
		{"CGNAT without proof is uncertain too", fakeResolver{answers: []string{"100.64.1.1"}}, runtime.VantageVNet, false, runtime.StateUncertain, "neither"},
		{"NXDOMAIN with a private endpoint fails", fakeResolver{err: ErrNXDomain}, runtime.VantageVNet, true, runtime.StateFail, "NXDOMAIN"},
		{"NXDOMAIN without proof is uncertain", fakeResolver{err: ErrNXDomain}, runtime.VantageVNet, false, runtime.StateUncertain, "NXDOMAIN"},
		{"wrapped NXDOMAIN is recognised", fakeResolver{err: errors.Join(errors.New("ctx"), ErrNXDomain)}, runtime.VantageVNet, true, runtime.StateFail, "NXDOMAIN"},
		{"timeout is skipped, never a failure", fakeResolver{err: context.DeadlineExceeded}, runtime.VantageVNet, true, runtime.StateSkipped, "timeout"},
		{"other resolver error is skipped", fakeResolver{err: errors.New("servfail")}, runtime.VantageVNet, true, runtime.StateSkipped, "not evidence"},
		{"no answers with a private endpoint fails", fakeResolver{}, runtime.VantageVNet, true, runtime.StateFail, "no addresses"},
		{"no answers without proof is uncertain", fakeResolver{}, runtime.VantageVNet, false, runtime.StateUncertain, "no addresses"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolvePrivate(ctx, c.r, c.vantage, "x.example", c.peBacked)
			if got.State != c.want {
				t.Fatalf("state = %s (%s), want %s", got.State, got.Reason, c.want)
			}
			if !strings.Contains(got.Reason, c.contains) {
				t.Errorf("reason %q lacks %q", got.Reason, c.contains)
			}
			if c.vantage == runtime.VantageVNet && got.State != runtime.StatePass && !strings.Contains(got.Reason, "asserted with --vantage vnet") {
				t.Errorf("reason does not state the assumed vantage: %q", got.Reason)
			}
		})
	}
}

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

func TestTimeoutResolver(t *testing.T) {
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

func TestMapLookupError(t *testing.T) {
	if err := mapLookupError(&net.DNSError{Err: "no such host", IsNotFound: true}); !errors.Is(err, ErrNXDomain) {
		t.Errorf("not found -> %v", err)
	}
	if err := mapLookupError(&net.DNSError{Err: "i/o timeout", IsTimeout: true}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout -> %v", err)
	}
	other := &net.DNSError{Err: "server misbehaving"}
	if err := mapLookupError(other); errors.Is(err, ErrNXDomain) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("other -> %v", err)
	}
	plain := errors.New("x")
	if mapLookupError(plain) != plain {
		t.Error("non-DNS errors must pass through")
	}
}

// The production resolver must turn real resolver errors into the states the
// rules rely on: NXDOMAIN -> ErrNXDomain, a lookup that runs out of time ->
// context.DeadlineExceeded. The errors come from the Go resolver talking to a
// loopback DNS stub, not from hand-built values.
func TestNetResolverMapsRealDNSErrors(t *testing.T) {
	t.Run("NXDOMAIN", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := NetResolver{Resolver: dnsstub.NXDomain(t)}.LookupHost(ctx, "missing.example.test.")
		if !errors.Is(err, ErrNXDomain) {
			t.Fatalf("err = %v, want ErrNXDomain", err)
		}
		got := ResolvePrivate(ctx, NetResolver{Resolver: dnsstub.NXDomain(t)}, runtime.VantageVNet, "missing.example.test.", true)
		if got.State != runtime.StateFail {
			t.Fatalf("NXDOMAIN on a PE-backed dependency: %+v", got)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		_, err := NetResolver{Resolver: dnsstub.Silent(t)}.LookupHost(ctx, "slow.example.test.")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
		if errors.Is(err, ErrNXDomain) {
			t.Fatal("a timeout must never read as NXDOMAIN")
		}
		got := ResolvePrivate(context.Background(), fakeResolver{err: err}, runtime.VantageVNet, "slow.example.test.", true)
		if got.State != runtime.StateSkipped {
			t.Fatalf("timeout must be skipped: %+v", got)
		}
	})
}

// Package network contains the DNS probes used by the runtime rules.
//
// The vantage point is asserted by the user (--vantage); the doctor cannot
// verify it. Every result and message therefore names the assumed vantage.
package network

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// ErrNXDomain marks a resolver answer "no such host". Other resolver errors
// (timeouts, SERVFAIL, refused) are not NXDOMAIN and prove nothing.
var ErrNXDomain = errors.New("network: name does not exist (NXDOMAIN)")

// Resolver resolves a host name without fetching content.
type Resolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

// DNSResult is the metadata-only outcome of a DNS probe.
type DNSResult struct {
	State   runtime.State
	Answers []string
	Reason  string
}

// VantageNote states the assumed vantage for messages.
func VantageNote(v runtime.Vantage) string {
	return "from the vantage point asserted with --vantage " + string(v) + " (not verified by the doctor)"
}

// ResolvePrivate checks that host resolves only to private IPs from a VNet
// vantage point. peBacked says whether the dependency is proven to be
// private-endpoint backed (public network access disabled, an active managed
// private endpoint or a private endpoint connection). A public answer or an
// NXDOMAIN is a failure only when peBacked is true; without that proof the
// result is uncertain, because a public address is legitimate for a dependency
// that is not private. A timeout or any other resolver error is skipped.
func ResolvePrivate(ctx context.Context, r Resolver, vantage runtime.Vantage, host string, peBacked bool) DNSResult {
	if vantage != runtime.VantageVNet {
		return DNSResult{State: runtime.StateSkipped, Reason: "private DNS resolves only from inside the VNet; re-run with --vantage vnet from a machine inside the VNet (the vantage is asserted by you and not verified)"}
	}
	answers, err := r.LookupHost(ctx, host)
	if err != nil {
		if errors.Is(err, ErrNXDomain) {
			if peBacked {
				return DNSResult{State: runtime.StateFail, Reason: "name does not resolve (NXDOMAIN) " + VantageNote(vantage) + " although the dependency is private-endpoint backed"}
			}
			return DNSResult{State: runtime.StateUncertain, Reason: "name does not resolve (NXDOMAIN) " + VantageNote(vantage) + "; private-endpoint backing is not proven"}
		}
		return DNSResult{State: runtime.StateSkipped, Reason: "resolver error or timeout " + VantageNote(vantage) + "; this is not evidence about the private endpoint"}
	}
	if len(answers) == 0 {
		if peBacked {
			return DNSResult{State: runtime.StateFail, Reason: "name resolves to no addresses " + VantageNote(vantage)}
		}
		return DNSResult{State: runtime.StateUncertain, Reason: "name resolves to no addresses " + VantageNote(vantage) + "; private-endpoint backing is not proven"}
	}
	allPrivate := true
	for _, ans := range answers {
		ip := net.ParseIP(ans)
		if ip == nil || !ip.IsPrivate() {
			allPrivate = false
			break
		}
	}
	switch {
	case allPrivate:
		return DNSResult{State: runtime.StatePass, Answers: answers}
	case peBacked:
		return DNSResult{State: runtime.StateFail, Answers: answers, Reason: "one or more answers are not private IP addresses " + VantageNote(vantage) + " although the dependency is private-endpoint backed"}
	default:
		return DNSResult{State: runtime.StateUncertain, Answers: answers, Reason: "an answer is a public IP address " + VantageNote(vantage) + "; private-endpoint backing is not proven, so this is not treated as a failure"}
	}
}

// NetResolver adapts net.Resolver to Resolver.
type NetResolver struct{ Resolver *net.Resolver }

func (n NetResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	res := n.Resolver
	if res == nil {
		res = net.DefaultResolver
	}
	ips, err := res.LookupHost(ctx, host)
	if err != nil {
		return nil, mapLookupError(err)
	}
	return ips, nil
}

// mapLookupError turns a not-found DNS error into ErrNXDomain and a DNS
// timeout into context.DeadlineExceeded; anything else is returned unchanged.
func mapLookupError(err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		switch {
		case dns.IsNotFound:
			return ErrNXDomain
		case dns.IsTimeout:
			return context.DeadlineExceeded
		}
	}
	return err
}

// TimeoutResolver wraps a resolver with a fixed timeout.
type TimeoutResolver struct {
	Inner   Resolver
	Timeout time.Duration
}

func (t TimeoutResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	inner := t.Inner
	if inner == nil {
		inner = NetResolver{}
	}
	cctx, cancel := context.WithTimeout(ctx, runtime.Timeout(t.Timeout, 5*time.Second))
	defer cancel()
	return inner.LookupHost(cctx, host)
}

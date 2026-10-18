// Package network contains the DNS probes used by the runtime rules.
//
// The vantage point is asserted by the user (--vantage); the doctor cannot
// verify it. Every result and message therefore names the assumed vantage.
package network

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
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
	allPrivate, anyPublic := true, false
	for _, ans := range answers {
		switch classify(ans) {
		case addrPrivate:
		case addrPublic:
			allPrivate, anyPublic = false, true
		default:
			allPrivate = false
		}
	}
	switch {
	case allPrivate:
		return DNSResult{State: runtime.StatePass, Answers: answers}
	case anyPublic && peBacked:
		return DNSResult{State: runtime.StateFail, Answers: answers, Reason: "one or more answers are public IP addresses " + VantageNote(vantage) + " although the dependency is private-endpoint backed"}
	case anyPublic:
		return DNSResult{State: runtime.StateUncertain, Answers: answers, Reason: "an answer is a public IP address " + VantageNote(vantage) + "; private-endpoint backing is not proven, so this is not treated as a failure"}
	default:
		return DNSResult{State: runtime.StateUncertain, Answers: answers, Reason: "an answer is neither a private nor a clearly public IP address (for example shared 100.64.0.0/10, loopback, link-local, reserved or unparseable) " + VantageNote(vantage) + "; this is not treated as a failure"}
	}
}

type addrClass int

const (
	addrOther   addrClass = iota // not private, not clearly public: never a failure
	addrPrivate                  // RFC 1918 or IPv6 ULA (fc00::/7)
	addrPublic                   // clearly public, global unicast
)

// reservedPrefixes are non-private ranges that are not publicly routable and so
// are never evidence of a public endpoint. IANA special-purpose registries.
var reservedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// classify says whether ans is private, clearly public, or neither.
func classify(ans string) addrClass {
	addr, err := netip.ParseAddr(strings.TrimSpace(ans))
	if err != nil {
		return addrOther
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsPrivate() {
		return addrPrivate
	}
	if !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return addrOther
	}
	for _, p := range reservedPrefixes {
		if p.Contains(addr) {
			return addrOther
		}
	}
	return addrPublic
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

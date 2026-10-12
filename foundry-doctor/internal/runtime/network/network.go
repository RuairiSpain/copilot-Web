// Package network contains the safe local/VNet-only DNS probes used by the
// runtime rules.
package network

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// Resolver resolves a host name without fetching content.
type Resolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

// Link describes one private DNS VNet link.
type Link struct {
	Name  string
	State string
}

// LinkReader reads private DNS links for a zone.
type LinkReader interface {
	ListLinks(context.Context, string) ([]Link, error)
}

// DNSResult is the metadata-only outcome of a DNS probe.
type DNSResult struct {
	State   runtime.State
	Answers []string
	Reason  string
}

// ResolvePrivate checks that host resolves to only private IPs from a VNet vantage point.
func ResolvePrivate(ctx context.Context, r Resolver, vantage runtime.Vantage, host string) DNSResult {
	if vantage != runtime.VantageVNet {
		return DNSResult{State: runtime.StateSkipped, Reason: "private DNS resolves only from inside the VNet; re-run with --vantage vnet from a machine inside the VNet"}
	}
	answers, err := r.LookupHost(ctx, host)
	if err != nil {
		return DNSResult{State: runtime.StateSkipped, Reason: "resolver unavailable or timed out"}
	}
	if len(answers) == 0 {
		return DNSResult{State: runtime.StateFail, Reason: "name resolves to no addresses"}
	}
	allPrivate := true
	for _, ans := range answers {
		ip := net.ParseIP(strings.TrimSpace(ans))
		if ip == nil || !ip.IsPrivate() {
			allPrivate = false
			break
		}
	}
	if allPrivate {
		return DNSResult{State: runtime.StatePass, Answers: answers}
	}
	return DNSResult{State: runtime.StateFail, Answers: answers, Reason: "one or more answers are not private IP addresses"}
}

// LinkState returns supporting context for a private DNS zone.
func LinkState(ctx context.Context, r LinkReader, zoneID string) runtime.Result {
	links, err := r.ListLinks(ctx, zoneID)
	if err != nil {
		return runtime.Result{State: runtime.StateSkipped, Class: runtime.ClassControlPlane, Message: err.Error()}
	}
	if len(links) == 0 {
		return runtime.Result{State: runtime.StateUncertain, Class: runtime.ClassControlPlane, Message: "private DNS zone has no virtual network links"}
	}
	for _, l := range links {
		if !strings.EqualFold(l.State, "Completed") {
			return runtime.Result{State: runtime.StateUncertain, Class: runtime.ClassControlPlane, Message: "private DNS link is not completed", Details: []string{l.Name + ": " + l.State}}
		}
	}
	return runtime.Result{State: runtime.StatePass, Class: runtime.ClassControlPlane}
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
		var dns *net.DNSError
		if errors.As(err, &dns) && dns.IsTimeout {
			return nil, context.DeadlineExceeded
		}
		return nil, err
	}
	return ips, nil
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

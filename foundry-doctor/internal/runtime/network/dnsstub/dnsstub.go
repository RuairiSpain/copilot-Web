// Package dnsstub provides in-process DNS servers for tests, so that the
// production resolver path can be exercised with real *net.DNSError values
// (IsNotFound, IsTimeout) instead of hand-built ones. It listens on loopback
// only and is meant for tests.
package dnsstub

import (
	"context"
	"net"
	"testing"
)

// NXDomain returns a resolver whose every query is answered with NXDOMAIN.
func NXDomain(t testing.TB) *net.Resolver { return start(t, true) }

// Silent returns a resolver whose server never answers, so a lookup ends when
// the caller's context does.
func Silent(t testing.TB) *net.Resolver { return start(t, false) }

func start(t testing.TB, answer bool) *net.Resolver {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback UDP: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if !answer {
				continue
			}
			if resp := nxdomain(buf[:n]); resp != nil {
				_, _ = pc.WriteTo(resp, from)
			}
		}
	}()
	addr := pc.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

// nxdomain builds an NXDOMAIN response to the query: same ID and question,
// QR, RD, RA set, RCODE 3, no other sections.
func nxdomain(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	i := 12
	for i < len(q) && q[i] != 0 {
		i += int(q[i]) + 1
	}
	end := i + 1 + 4 // root label, QTYPE, QCLASS
	if end > len(q) {
		return nil
	}
	resp := make([]byte, 0, end)
	resp = append(resp, q[0], q[1], 0x81, 0x83, 0, 1, 0, 0, 0, 0, 0, 0)
	return append(resp, q[12:end]...)
}

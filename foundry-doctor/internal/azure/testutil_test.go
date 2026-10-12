package azure

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testSub   = "11111111-1111-1111-1111-111111111111"
	testOID   = "22222222-2222-2222-2222-222222222222"
	testTID   = "33333333-3333-3333-3333-333333333333"
	testRG    = "/subscriptions/" + testSub + "/resourceGroups/rg-demo"
	testScope = "/subscriptions/" + testSub
	roleContr = testScope + "/providers/Microsoft.Authorization/roleDefinitions/b24988ac-6180-42a0-ab88-20f7382dd24c"
	roleRead  = testScope + "/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7"
)

type reply struct {
	status int
	header http.Header
	body   string
	delay  time.Duration
}

// fakeARM is a no-network RoundTripper that replays recorded responses and
// records every request. It fails the test if any request is not allow-listed.
type fakeARM struct {
	t        *testing.T
	mu       sync.Mutex
	routes   map[string][]reply
	calls    []string
	bodies   map[string][]string
	inflight int32
	maxSeen  int32
}

func newFake(t *testing.T) *fakeARM {
	return &fakeARM{t: t, routes: map[string][]reply{}, bodies: map[string][]string{}}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(b)
}

func (f *fakeARM) on(method, path string, rs ...reply) *fakeARM {
	f.routes[method+" "+path] = rs
	return f
}

func (f *fakeARM) onFile(method, path, file string) *fakeARM {
	return f.on(method, path, reply{status: 200, body: fixture(f.t, file)})
}

func (f *fakeARM) on403(method, path string) *fakeARM {
	return f.on(method, path, reply{status: 403, body: `{"error":{"code":"AuthorizationFailed","message":"secret-free text"}}`})
}

func (f *fakeARM) RoundTrip(r *http.Request) (*http.Response, error) {
	cur := atomic.AddInt32(&f.inflight, 1)
	defer atomic.AddInt32(&f.inflight, -1)
	for {
		m := atomic.LoadInt32(&f.maxSeen)
		if cur <= m || atomic.CompareAndSwapInt32(&f.maxSeen, m, cur) {
			break
		}
	}
	var body string
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}
	// Replay test: every recorded request must be on the ADR-006 allow-list.
	if !IsAllowed(r.Method, r.URL.Path) {
		f.t.Errorf("request outside allow-list: %s %s", r.Method, r.URL.Path)
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		f.t.Errorf("request without bearer token")
	}
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.calls = append(f.calls, key+"?"+r.URL.RawQuery)
	f.bodies[key] = append(f.bodies[key], body)
	rs, ok := f.routes[key]
	var rep reply
	if ok {
		rep = rs[0]
		if len(rs) > 1 {
			f.routes[key] = rs[1:]
		}
	}
	f.mu.Unlock()
	if !ok {
		rep = reply{status: 404, body: `{"error":{"code":"ResourceNotFound"}}`}
	}
	if rep.delay > 0 {
		select {
		case <-time.After(rep.delay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	h := http.Header{"Content-Type": {"application/json"}}
	for k, v := range rep.header {
		h[k] = v
	}
	return &http.Response{StatusCode: rep.status, Header: h, Body: io.NopCloser(strings.NewReader(rep.body)), Request: r}, nil
}

func (f *fakeARM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type staticCred struct {
	token string
	err   error
}

func (c staticCred) Token(context.Context, string) (AccessToken, error) {
	return AccessToken{Token: c.token, ExpiresOn: time.Now().Add(time.Hour)}, c.err
}

// fakeJWT builds a JWT-shaped string at runtime (never a literal secret).
func fakeJWT(oid, idtyp string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	payload := fmt.Sprintf(`{"oid":%q,"tid":%q,"idtyp":%q}`, oid, testTID, idtyp)
	return enc(`{"alg":"none"}`) + "." + enc(payload) + "." + enc("sig")
}

type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) fn(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
	return nil
}

func newAdapter(t *testing.T, f *fakeARM, mod ...func(*Options)) (*Adapter, *sleeps) {
	t.Helper()
	sl := &sleeps{}
	o := Options{
		Credential: staticCred{token: fakeJWT(testOID, "")},
		Endpoint:   "https://management.test",
		HTTPClient: &http.Client{Transport: f},
		Sleep:      sl.fn,
	}
	for _, m := range mod {
		m(&o)
	}
	a, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return a, sl
}

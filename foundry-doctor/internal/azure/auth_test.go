package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type scriptRunner struct {
	calls []string
	fn    func(name string) ([]byte, error)
}

func (s *scriptRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, name+" "+strings.Join(args, " "))
	return s.fn(name)
}

const scope = "https://management.azure.com/.default"

func azdOut(exp time.Time) []byte {
	return []byte(fmt.Sprintf(`{"token":"tok-azd","expiresOn":%q}`, exp.UTC().Format(time.RFC3339)))
}

func TestChainAzdFirstAndCache(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &scriptRunner{fn: func(string) ([]byte, error) { return azdOut(now.Add(time.Hour)), nil }}
	c := newChain(r.run, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		tok, err := c.Token(context.Background(), scope)
		if err != nil || tok.Token != "tok-azd" {
			t.Fatalf("token: %v", err)
		}
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], "azd auth token") {
		t.Errorf("calls = %v", r.calls)
	}
	now = now.Add(59 * time.Minute) // inside the 2-minute skew: refresh
	if _, err := c.Token(context.Background(), scope); err != nil || len(r.calls) != 2 {
		t.Errorf("expected refresh, calls = %v err=%v", r.calls, err)
	}
}

func TestChainFallsBackToAz(t *testing.T) {
	r := &scriptRunner{fn: func(name string) ([]byte, error) {
		if name == "azd" {
			return nil, errors.New("not logged in; stderr secret-ish")
		}
		return []byte(`{"accessToken":"tok-az","expires_on":4102444800}`), nil
	}}
	c := newChain(r.run, time.Now)
	tok, err := c.Token(context.Background(), scope)
	if err != nil || tok.Token != "tok-az" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(r.calls[1], "--resource https://management.azure.com") || strings.Contains(r.calls[1], ".default") {
		t.Errorf("az args = %s", r.calls[1])
	}
}

func TestChainNoCredential(t *testing.T) {
	r := &scriptRunner{fn: func(string) ([]byte, error) { return nil, errors.New("boom-detail") }}
	_, err := newChain(r.run, time.Now).Token(context.Background(), scope)
	if !errors.Is(err, ErrNoCredential) || strings.Contains(err.Error(), "boom-detail") {
		t.Errorf("err = %v", err)
	}
	bad := &scriptRunner{fn: func(string) ([]byte, error) { return []byte(`not json tok-leak`), nil }}
	_, err = newChain(bad.run, time.Now).Token(context.Background(), scope)
	if !errors.Is(err, ErrNoCredential) || strings.Contains(err.Error(), "tok-leak") {
		t.Errorf("malformed output err = %v", err)
	}
}

func TestChainUnknownExpiryNotCached(t *testing.T) {
	r := &scriptRunner{fn: func(string) ([]byte, error) { return []byte(`{"token":"t","expiresOn":"garbage"}`), nil }}
	c := newChain(r.run, time.Now)
	for i := 0; i < 2; i++ {
		if _, err := c.Token(context.Background(), scope); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.calls) != 2 {
		t.Errorf("calls = %d", len(r.calls))
	}
}

func TestChainContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &scriptRunner{fn: func(string) ([]byte, error) { cancel(); return nil, errors.New("x") }}
	if _, err := newChain(r.run, time.Now).Token(ctx, scope); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if len(r.calls) != 1 {
		t.Errorf("chain continued after cancel: %v", r.calls)
	}
}

func TestTokenRedaction(t *testing.T) {
	tok := AccessToken{Token: "super-sensitive-value"}
	for _, s := range []string{fmt.Sprintf("%v", tok), fmt.Sprintf("%+v", tok), fmt.Sprintf("%#v", tok), tok.String()} {
		if strings.Contains(s, "super-sensitive") {
			t.Errorf("token leaked: %s", s)
		}
	}
}

func TestParseClaims(t *testing.T) {
	c, err := ParseClaims(fakeJWT(testOID, "app"))
	if err != nil || c.ObjectID != testOID || c.principalType() != "ServicePrincipal" {
		t.Errorf("got %+v, %v", c, err)
	}
	c, _ = ParseClaims(fakeJWT(testOID, ""))
	if c.principalType() != "User" {
		t.Errorf("default type = %s", c.principalType())
	}
	for _, bad := range []string{"", "a.b", "a.!!!.c", "a.e30.c.d"} {
		if _, err := ParseClaims(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestExecRunnerMissingBinary(t *testing.T) {
	if _, err := ExecRunner(context.Background(), "definitely-not-a-real-binary-xyz"); err == nil {
		t.Error("expected error")
	}
}

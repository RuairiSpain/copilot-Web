package azure

import (
	"context"
	"testing"
	"time"
)

func TestCLICredentialIgnoresEnvironmentToken(t *testing.T) {
	t.Setenv("AZURE_ACCESS_TOKEN", "arm-env-token")
	r := &scriptRunner{fn: func(name string) ([]byte, error) {
		return azdOut(time.Now().Add(time.Hour)), nil
	}}
	tok, err := NewCLICredentialForTenant(r.run, "").Token(context.Background(), "https://ai.azure.com/.default")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "tok-azd" {
		t.Fatalf("expected azd token, got a different source")
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls=%v", r.calls)
	}
}

package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

func TestGraphHelpListsFlags(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--help"}, &out, &errb, noBicep(io.Discard)); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"--source", "--deployed", "--combined", "--findings-report", "--subscription", "--resource-group", "--redact-ids", "read-only Azure inventory"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestGraphValidation(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"graph", "--deployed", "--dir", sample("good")}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
	if code := run(context.Background(), []string{"graph", "--source", "--deployed", "--dir", sample("good")}, &out, &errb, noBicep(io.Discard)); code != app.ExitUnavailable {
		t.Fatalf("exit %d, want %d", code, app.ExitUnavailable)
	}
}

package azure

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// A child whose grandchild keeps the output pipe open must not block the
// runner past the context deadline plus WaitDelay.
func TestExecRunnerHungChildDoesNotBlockCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecRunner(ctx, "sh", "-c", "sleep 20 & wait")
	if err == nil {
		t.Fatal("expected an error from the cancelled command")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("runner blocked for %v after cancellation (WaitDelay not set?)", elapsed)
	}
}

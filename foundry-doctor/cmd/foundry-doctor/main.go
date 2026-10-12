// Command foundry-doctor is the standalone Foundry Doctor binary. This file only wires the process to the
// cobra command tree in root.go; the work is done in internal/app.
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr, osDeps())
	stop()
	os.Exit(code)
}

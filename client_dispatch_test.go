package domi

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestClientDispatch checks clone dispatch and version tracking under jsdom.
func TestClientDispatch(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven client dispatch test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "testdata/dispatch_runner.mjs")
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client dispatch: %v (context: %v)\n%s", err, ctx.Err(), out)
	}
}

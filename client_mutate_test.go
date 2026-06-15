package domi

import (
	"os/exec"
	"strings"
	"testing"
)

// TestClientApplyMove drives client.js's optimistic move applier under
// jsdom (see testdata/mutate_runner.mjs): reorder, append, cross-container
// move, and destination key collision. The runner exits non-zero with a
// message on the first failed check. It needs bun; the test skips where
// bun is absent, so the coverage rides CI.
func TestClientApplyMove(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven client move test")
	}
	out, err := exec.Command("bun", "testdata/mutate_runner.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("client move applier failed:\n%s", out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("runner did not report success:\n%s", out)
	}
}

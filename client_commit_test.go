package domi

import (
	"os/exec"
	"strings"
	"testing"
)

// TestClientCommit drives client.js's form-control commit logic under
// jsdom (see testdata/commit_runner.mjs): the mutation ops and attribute
// writes reporting a control's committed state, the local revert that
// converges unhandled controls, and the handler test that decides
// between them — across inputs, textareas, checkboxes, radio groups,
// selects, and the file-input and app-owned exemptions. The runner
// exits non-zero with a message on the first failed check. It needs
// bun; the test skips where bun is absent, so the coverage rides CI.
func TestClientCommit(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven client commit test")
	}
	out, err := exec.Command("bun", "testdata/commit_runner.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("client commit logic failed:\n%s", out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("runner did not report success:\n%s", out)
	}
}

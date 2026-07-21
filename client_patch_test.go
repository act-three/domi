package domi

import (
	"os/exec"
	"strings"
	"testing"
)

// TestClientApplyPatchFormState drives client.js's patch applier against
// form controls carrying user state under jsdom (see
// testdata/patch_runner.mjs): dirty inputs, checkboxes, options, and
// textareas whose properties no longer reflect their attributes, plus
// the focused-element guard that protects in-flight typing. The runner
// exits non-zero with a message on the first failed check. It needs
// bun; the test skips where bun is absent, so the coverage rides CI.
func TestClientApplyPatchFormState(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven client patch test")
	}
	out, err := exec.Command("bun", "testdata/patch_runner.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("client patch applier failed:\n%s", out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("runner did not report success:\n%s", out)
	}
}

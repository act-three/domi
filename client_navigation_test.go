package domi

import (
	"os/exec"
	"strings"
	"testing"
)

// TestClientAnchorNavigation drives the client-side anchor policy and URL
// normalization under jsdom. The browser remains the authority on origin;
// eligible same-origin targets become relative server requests and eligible
// cross-origin targets become absolute ones.
func TestClientAnchorNavigation(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven client navigation test")
	}
	out, err := exec.Command("bun", "testdata/navigation_runner.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("client anchor navigation failed:\n%s", out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("runner did not report success:\n%s", out)
	}
}

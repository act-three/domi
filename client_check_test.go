package domi

import (
	"os/exec"
	"strings"
	"testing"
)

// undefinedNameCodes are the TypeScript diagnostics for an identifier
// with no binding in scope — the class of bug where a renamed parameter
// leaves a dangling reference behind. The check fails on these and
// ignores tsc's other complaints about the untyped client.js.
var undefinedNameCodes = []string{"error TS2304", "error TS2552", "error TS2448"}

// TestClientJSNoUndefinedNames type-checks client.js with tsc and fails
// if any identifier resolves to nothing. tsc runs in CI only; the test
// skips where it is absent.
func TestClientJSNoUndefinedNames(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not installed; skipping client.js undefined-name check")
	}
	out, _ := exec.Command(tsc,
		"--allowJs", "--checkJs", "--noEmit", "--pretty", "false",
		"--target", "es2022", "--module", "esnext", "--moduleResolution", "bundler",
		"--lib", "es2022,dom",
		"client.js",
	).CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		for _, code := range undefinedNameCodes {
			if strings.Contains(line, code) {
				t.Errorf("client.js: %s", strings.TrimSpace(line))
			}
		}
	}
}

// TestClientJSNoShadowedVars lints client.js for a helper parameter that
// shadows an outer binding and the self-assignment it causes — like
// restoreSnapshot(ver) hiding the session's ver. eslint runs in CI only;
// the test skips where it is absent.
func TestClientJSNoShadowedVars(t *testing.T) {
	eslint, err := exec.LookPath("eslint")
	if err != nil {
		t.Skip("eslint not installed; skipping client.js shadowing check")
	}
	if out, err := exec.Command(eslint,
		"--no-config-lookup",
		"--config", "testdata/eslint.config.mjs",
		"client.js",
	).CombinedOutput(); err != nil {
		t.Errorf("client.js fails eslint:\n%s", out)
	}
}

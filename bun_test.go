package domi

import (
	"bufio"
	"crypto/rand"
	"encoding/json/v2"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// bunApplier drives the production patch applier (static/domi.js)
// inside jsdom under a long-running bun subprocess. Each call to apply
// sends one JSON-line request and reads one JSON-line response over
// stdin/stdout; reusing the process avoids paying spawn cost per
// iteration. Constructed via startBunApplier(t); torn down through
// t.Cleanup. The applier returns the resulting tree as serialized HTML
// — canonicalization is the caller's job, using the same parser they'd
// use on render(next).
type bunApplier struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	stderrMu sync.Mutex
	stderr   []byte
}

// startBunApplier launches the bun subprocess that owns the jsdom
// instance and registers cleanup so the process exits when the test
// finishes. Skips the calling test if bun isn't on PATH.
func startBunApplier(t *testing.T) *bunApplier {
	t.Helper()
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not installed; skipping jsdom-driven property test")
	}
	cmd := exec.Command("bun", "testdata/applier_runner.mjs")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("bun stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("bun stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("bun stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("bun start: %v", err)
	}

	a := &bunApplier{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}

	// Drain stderr so it can be surfaced in error messages if a round-
	// trip fails. The goroutine exits on EOF when the process exits.
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		buf, _ := io.ReadAll(stderr)
		a.stderrMu.Lock()
		a.stderr = append(a.stderr, buf...)
		a.stderrMu.Unlock()
	}()

	t.Cleanup(func() {
		// Closing stdin lets bun's readline loop exit cleanly; if it
		// doesn't exit promptly (stuck import, infinite loop), kill.
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		<-stderrDone
	})

	return a
}

// bunResp is the one-line response the bun runner emits per request.
// Tag echoes the request's tag and is verified by the caller; exactly
// one of HTML or Err is set.
type bunResp struct {
	Tag  string `json:"tag"`
	HTML string `json:"html,omitempty"`
	Err  string `json:"err,omitempty"`
}

// apply sends one (initial HTML, patches) request to the bun runner and
// returns the serialized HTML of the resulting tree. Each request
// carries a fresh random tag that the runner echoes back; a mismatch
// means stdin/stdout got out of sync — the test harness is broken and
// any further results would be meaningless, so we panic rather than
// return an error.
func (a *bunApplier) apply(initial string, patches []patch) (string, error) {
	tag := rand.Text()
	req := struct {
		Tag     string  `json:"tag"`
		Initial string  `json:"initial"`
		Patches []patch `json:"patches"`
	}{tag, initial, patches}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	if _, err := a.stdin.Write(append(body, '\n')); err != nil {
		return "", fmt.Errorf("write: %w (stderr=%s)", err, a.stderrSnapshot())
	}
	line, err := a.stdout.ReadBytes('\n')
	if err != nil {
		return "", fmt.Errorf("read: %w (stderr=%s)", err, a.stderrSnapshot())
	}
	var resp bunResp
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", fmt.Errorf("decode: %w (line=%s)", err, string(line))
	}
	if resp.Tag != tag {
		panic(fmt.Sprintf("bunApplier out of sync: sent tag %q, got %q (stderr=%s)", tag, resp.Tag, a.stderrSnapshot()))
	}
	if resp.Err != "" {
		return "", fmt.Errorf("bun: %s", resp.Err)
	}
	return resp.HTML, nil
}

func (a *bunApplier) stderrSnapshot() string {
	a.stderrMu.Lock()
	defer a.stderrMu.Unlock()
	return string(a.stderr)
}

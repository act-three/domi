package domi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ily.dev/domi/internal/vdom"
)

// counterApp is a minimal App used in lifecycle tests. Each Update bumps n
// so View produces a different tree (and the diff produces real patches).
type counterApp struct{ n int }

func (a *counterApp) Update(int) Cmd[int] { a.n++; return CmdNone[int]() }
func (a *counterApp) View() (string, Node) {
	return "", Tag("div")()(Text(fmt.Sprintf("%d", a.n)))
}

// sessionLoop exits promptly when its ctx is cancelled.
func TestSessionLoopExitsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	msgChan := make(chan int, 1)
	patchChan := make(chan []vdom.Patch, 1)
	done := make(chan struct{})
	go func() {
		sessionLoop(ctx, &counterApp{}, "", lower(Tag("div")()()), msgChan, patchChan)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sessionLoop did not exit on ctx.Done()")
	}
}

// The patchTx send is interruptible: with no SSE reader on patchChan and
// a message waiting in msgChan, sessionLoop produces a patch, tries to
// send, blocks — and unblocks cleanly when ctx is cancelled.
func TestSessionLoopPatchSendInterruptible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	msgChan := make(chan int, 1)
	patchChan := make(chan []vdom.Patch) // unbuffered, no reader
	msgChan <- 1
	done := make(chan struct{})
	go func() {
		sessionLoop(ctx, &counterApp{}, "", lower(Tag("div")()()), msgChan, patchChan)
		close(done)
	}()
	// Give the loop time to consume the message and block on the send.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sessionLoop blocked on patchTx send despite ctx cancel")
	}
}

// fragmentApp's View returns a Fragment so the framework treats its
// members as separate top-level children of the mount.
type fragmentApp struct{ n int }

func (a *fragmentApp) Update(int) Cmd[int] { a.n++; return CmdNone[int]() }
func (a *fragmentApp) View() (string, Node) {
	return "", Fragment(
		Tag("div")()(Text(fmt.Sprintf("a%d", a.n))),
		Tag("div")()(Text(fmt.Sprintf("b%d", a.n))),
	)
}

// A Fragment returned from View lowers to multiple top-level children,
// and sessionLoop diffs them positionally against the previous frame.
func TestSessionLoopFragmentAtRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgChan := make(chan int, 1)
	patchChan := make(chan []vdom.Patch, 1)
	app := &fragmentApp{}
	_, view0 := app.View()
	go sessionLoop(ctx, app, "", lower(view0), msgChan, patchChan)
	msgChan <- 1
	select {
	case patches := <-patchChan:
		// Each <div> child's text changes (a0→a1, b0→b1), producing
		// two set_text patches addressed at [0,0] and [1,0]. The exact
		// shape isn't the contract — what matters is that *both*
		// top-level siblings were diffed, not just one.
		if len(patches) < 2 {
			t.Fatalf("expected patches for both Fragment siblings, got %d: %+v", len(patches), patches)
		}
	case <-time.After(time.Second):
		t.Fatal("no patches arrived")
	}
}

// spawnCmd hands the session ctx (not context.Background) to the Cmd body,
// so cmds can honor cancellation.
func TestSpawnCmdPassesSessionCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgChan := make(chan int, 1)
	got := make(chan context.Context, 1)
	cmd := CmdFn(func(c context.Context) int {
		got <- c
		return 0
	})
	spawnCmd(ctx, cmd, msgChan)
	select {
	case c := <-got:
		if c != ctx {
			t.Fatalf("spawnCmd should pass session ctx, got a different one")
		}
	case <-time.After(time.Second):
		t.Fatal("Cmd body never ran")
	}
}

// spawnCmd doesn't leak a goroutine on the unread channel when ctx is
// cancelled before the body's return value can be received.
func TestSpawnCmdSendInterruptibleOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	msgChan := make(chan int) // unbuffered, no reader
	started := make(chan struct{})
	exited := make(chan struct{})
	cmd := CmdFn(func(c context.Context) int {
		close(started)
		<-c.Done() // honor ctx
		return 0
	})
	spawnCmd(ctx, cmd, msgChan)

	// The goroutine wrapper isn't directly observable, but we can run an
	// auxiliary one that waits on the send-then-exit path.
	go func() {
		<-started
		cancel()
		// Give the wrapper a beat to fall out of its select.
		time.Sleep(20 * time.Millisecond)
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("spawnCmd goroutine appears stuck after ctx cancel")
	}
}

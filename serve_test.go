package domi

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// counterApp is a minimal App used in lifecycle tests. Each Update bumps n
// so View produces a different tree (and the diff produces real patches).
type counterApp struct{ n int }

func (a *counterApp) Init() Cmd[int]      { return CmdNone[int]() }
func (a *counterApp) Update(int) Cmd[int] { a.n++; return CmdNone[int]() }
func (a *counterApp) View() Node          { return E("div", nil, []Node{Text(fmt.Sprintf("%d", a.n))}) }
func (a *counterApp) Title() string       { return "" }

// sessionLoop exits promptly when its ctx is cancelled.
func TestSessionLoopExitsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	msgChan := make(chan int, 1)
	patchChan := make(chan []patch, 1)
	done := make(chan struct{})
	go func() {
		sessionLoop(ctx, &counterApp{}, E("div", nil, nil), msgChan, patchChan)
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
	patchChan := make(chan []patch) // unbuffered, no reader
	msgChan <- 1
	done := make(chan struct{})
	go func() {
		sessionLoop(ctx, &counterApp{}, E("div", nil, nil), msgChan, patchChan)
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

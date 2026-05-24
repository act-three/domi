package domi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// counterApp is a minimal App used in lifecycle tests. Each Update bumps n
// so View produces a different tree (and the diff produces real patches).
type counterApp struct{ n int }

func (a *counterApp) Update(int) Cmd[int] { a.n++; return Batch[int]() }
func (a *counterApp) View() (string, Node) {
	return "", Tag("div")()(Text(fmt.Sprintf("%d", a.n)))
}

// fragmentApp's View returns a Fragment so the framework treats its
// members as separate top-level children of the mount.
type fragmentApp struct{ n int }

func (a *fragmentApp) Update(int) Cmd[int] { a.n++; return Batch[int]() }
func (a *fragmentApp) View() (string, Node) {
	return "", Fragment(
		Tag("div")()(Text(fmt.Sprintf("a%d", a.n))),
		Tag("div")()(Text(fmt.Sprintf("b%d", a.n))),
	)
}

// newTestSession wires up a session for direct method tests, bypassing
// the http surface.
func newTestSession[Msg any](app App[Msg]) *session[Msg] {
	ctx, cancel := context.WithCancel(context.Background())
	_, view := app.View()
	return &session[Msg]{
		ctx:    ctx,
		cancel: cancel,
		app:    app,
		ready:  make(chan struct{}, 1),
		view:   lower(view),
		active: time.Now(),
	}
}

// A Fragment returned from View lowers to multiple top-level children,
// and apply diffs them positionally against the previous frame.
func TestSessionApplyFragmentAtRoot(t *testing.T) {
	s := newTestSession(&fragmentApp{})
	defer s.cancel()
	s.apply(1)
	if len(s.patchSets) != 1 {
		t.Fatalf("expected one patchSet, got %d", len(s.patchSets))
	}
	// Each <div> child's text changes (a0→a1, b0→b1), producing two
	// set_text patches. The exact shape isn't the contract — what matters
	// is that *both* top-level siblings were diffed, not just one.
	if n := len(s.patchSets[0]); n < 2 {
		t.Fatalf("expected patches for both Fragment siblings, got %d: %+v", n, s.patchSets[0])
	}
}

// The Document option swaps the default HTML shell for the App's
// builder, lets it own <head>, and still attaches the session marker
// to body.
func TestHandlerDocumentOption(t *testing.T) {
	custom := func(title string, body Node) Node {
		return Tag("html")()(
			Tag("head")()(
				Tag("title")()(Text("custom:"+title)),
				Tag("meta")(Name("name")("test"), Name("content")("hello")),
			),
			body,
		)
	}
	h := Handler(func() (*counterApp, Cmd[int]) {
		return &counterApp{}, Batch[int]()
	}, Document(custom))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	body := w.Body.String()
	if !strings.Contains(body, `<title>custom:</title>`) {
		t.Fatalf("custom Document not invoked; body: %s", body)
	}
	if !strings.Contains(body, `<meta name="test" content="hello">`) {
		t.Fatalf("custom head content missing; body: %s", body)
	}
	if !strings.Contains(body, `data-domi-session=`) {
		t.Fatalf("session marker not attached to body; got: %s", body)
	}
	// The default bootstrap script must not appear when Document is set —
	// the App is responsible for loading the client itself.
	if strings.Contains(body, "Domi.run()") {
		t.Fatalf("default bootstrap leaked into custom Document; got: %s", body)
	}
}

// idleWatch cancels the session once activity falls behind by d.
func TestSessionIdleWatchFires(t *testing.T) {
	const d = 50 * time.Millisecond
	s := newTestSession(&counterApp{})
	defer s.cancel()
	go s.idleWatch(d)
	select {
	case <-s.ctx.Done():
	case <-time.After(d * 10):
		t.Fatal("idleWatch did not cancel an idle session")
	}
}

// touch defers the idle deadline. Repeated touches keep the session
// alive past d; once they stop, idleWatch fires.
func TestSessionIdleWatchTouchDefers(t *testing.T) {
	const d = 50 * time.Millisecond
	s := newTestSession(&counterApp{})
	defer s.cancel()
	go s.idleWatch(d)

	// Touch four times across ~2d to verify the deadline keeps sliding.
	for range 4 {
		time.Sleep(d / 2)
		s.touch()
	}
	if s.ctx.Err() != nil {
		t.Fatalf("session cancelled despite recent touches: %v", s.ctx.Err())
	}

	// Stop touching; cancellation should follow within roughly d.
	select {
	case <-s.ctx.Done():
	case <-time.After(d * 10):
		t.Fatal("idleWatch did not cancel after touches stopped")
	}
}

// A session whose client never attaches SSE expires after
// SessionTimeout: the watchdog cancels its ctx and the server removes it
// from the live-session map.
func TestServerSessionTimeoutNeverAttached(t *testing.T) {
	const d = 50 * time.Millisecond
	sv := newServer(
		func() (*counterApp, Cmd[int]) { return &counterApp{}, Batch[int]() },
		[]Option{SessionTimeout(d)},
	)
	sv.handleRoot(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	sv.mu.Lock()
	if len(sv.m) != 1 {
		sv.mu.Unlock()
		t.Fatalf("expected 1 session, got %d", len(sv.m))
	}
	var s *session[int]
	for _, ss := range sv.m {
		s = ss
	}
	sv.mu.Unlock()

	select {
	case <-s.ctx.Done():
	case <-time.After(d * 10):
		t.Fatal("session ctx not cancelled after SessionTimeout")
	}

	// The watcher goroutine deletes from the map once ctx is done.
	deadline := time.Now().Add(d * 5)
	for time.Now().Before(deadline) {
		sv.mu.Lock()
		n := len(sv.m)
		sv.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(d / 10)
	}
	t.Fatal("session not removed from server map")
}

// (*session).spawn hands the session ctx to the Cmd body so cmds can
// honor cancellation.
func TestSessionSpawnPassesSessionCtx(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	got := make(chan context.Context, 1)
	cmd := Func(func(c context.Context) int {
		got <- c
		return 0
	})
	s.spawn(cmd)
	select {
	case c := <-got:
		if c != s.ctx {
			t.Fatalf("spawn should pass session ctx, got a different one")
		}
	case <-time.After(time.Second):
		t.Fatal("Cmd body never ran")
	}
}

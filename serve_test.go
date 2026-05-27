package domi

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// counterApp is a minimal App used in lifecycle tests. Each Update bumps n
// so View produces a different tree (and the diff produces real patches).
type counterApp struct{ n int }

func (a *counterApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *counterApp) View(context.Context) (string, Node) {
	return "", Tag("div")()(Text(fmt.Sprintf("%d", a.n)))
}
func (a *counterApp) Subscriptions(context.Context) Sub[int] { return Sub[int]{} }

// fragmentApp's View returns a Fragment so the framework treats its
// members as separate top-level children of the mount.
type fragmentApp struct{ n int }

func (a *fragmentApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *fragmentApp) View(context.Context) (string, Node) {
	return "", Fragment(
		Tag("div")()(Text(fmt.Sprintf("a%d", a.n))),
		Tag("div")()(Text(fmt.Sprintf("b%d", a.n))),
	)
}
func (a *fragmentApp) Subscriptions(context.Context) Sub[int] { return Sub[int]{} }

// newTestSession wires up a session for direct method tests, bypassing
// the http surface. The session points at a default-configured server
// so apply, idleWatch, and friends can read config fields without going
// through Handler.
func newTestSession[Msg any](app App[Msg]) *session[Msg] {
	const replayWindow = 128
	ctx, cancel := context.WithCancel(context.Background())
	_, view := app.View(ctx)
	return &session[Msg]{
		ctx:    ctx,
		cancel: cancel,
		app:    app,
		sv: &server[Msg]{
			config: handlerConfig{
				replayWindow: replayWindow,
				keepalive:    25 * time.Second,
			},
			onURLChange:  func(*url.URL) Msg { var zero Msg; return zero },
			onURLRequest: func(URLRequest) Msg { var zero Msg; return zero },
		},
		log:    make([]frame, replayWindow),
		view:   lower(view),
		active: time.Now(),
	}
}

// A Fragment returned from View lowers to multiple top-level children,
// and apply diffs them positionally against the previous frame.
func TestSessionApplyFragmentAtRoot(t *testing.T) {
	s := newTestSession(&fragmentApp{})
	defer s.cancel()
	s.apply(s.ctx, 1)
	if s.head != 1 {
		t.Fatalf("expected head=1 after one apply, got %d", s.head)
	}
	// Each <div> child's text changes (a0→a1, b0→b1), producing two
	// set_text patches. The exact shape isn't the contract — what matters
	// is that *both* top-level siblings were diffed, not just one.
	f := s.log[1%uint64(len(s.log))]
	if n := len(f.patches); n < 2 {
		t.Fatalf("expected patches for both Fragment siblings, got %d: %+v", n, f.patches)
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
	h := Handler(
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) {
			return &counterApp{}, Batch[int]()
		},
		func(URLRequest) int { return 0 },
		func(*url.URL) int { return 0 },
		Document(custom),
	)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	body := w.Body.String()
	if !strings.Contains(body, `<title>custom:</title>`) {
		t.Fatalf("custom Document not invoked; body: %s", body)
	}
	if !strings.Contains(body, `<meta content="hello" name="test">`) {
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
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) { return &counterApp{}, Batch[int]() },
		func(URLRequest) int { return 0 },
		func(*url.URL) int { return 0 },
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

// runSSE invokes (*session).handleSSE in a goroutine, lets it run for d
// (long enough to do its initial replay/resync/keepalive work), then
// cancels the request context and waits for the handler to return.
// Returns the bytes the handler wrote.
func runSSE[Msg any](t *testing.T, s *session[Msg], lastEventID string, d time.Duration) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/sse/x", nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	reqCtx, reqCancel := context.WithCancel(context.Background())
	defer reqCancel()
	req = req.WithContext(reqCtx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleSSE(rec, req)
		close(done)
	}()
	time.Sleep(d)
	reqCancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleSSE did not return after request ctx cancel")
	}
	return rec.Body.String()
}

// The patch log is a fixed-size ring sized to ReplayWindow. Oldest
// frames are overwritten as new ones land; head keeps climbing.
func TestSessionApplyRingBuffer(t *testing.T) {
	const window = 2
	s := newTestSession(&counterApp{})
	s.sv.config.replayWindow = window
	s.log = make([]frame, window)
	defer s.cancel()
	s.apply(s.ctx, 1) // seq 1 → log[1]
	s.apply(s.ctx, 2) // seq 2 → log[0] (overwrites zero-value)
	s.apply(s.ctx, 3) // seq 3 → log[1] (overwrites seq 1)
	if s.head != 3 {
		t.Fatalf("expected head=3, got %d", s.head)
	}
	if s.log[0].seq != 2 || s.log[1].seq != 3 {
		t.Fatalf("expected ring [{seq:2},{seq:3}]; got seqs %d, %d", s.log[0].seq, s.log[1].seq)
	}
}

// A fresh client (no Last-Event-ID, empty log) gets no patches —
// handleSSE establishes the stream and waits for activity.
func TestSessionSSEFreshClient(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	out := runSSE(t, s, "", 30*time.Millisecond)
	if strings.Contains(out, "event: patch") {
		t.Fatalf("expected no patches for fresh empty session, got: %s", out)
	}
}

// A client reconnecting within the replay window receives only the
// frames it missed, each tagged with its monotonic id.
func TestSessionSSEReplayWithinWindow(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.apply(s.ctx, 1)
	s.apply(s.ctx, 2)
	s.apply(s.ctx, 3)
	out := runSSE(t, s, "1", 30*time.Millisecond)
	if !strings.Contains(out, "id: 2") || !strings.Contains(out, "id: 3") {
		t.Fatalf("expected frames 2 and 3 in output, got: %s", out)
	}
	if strings.Contains(out, "id: 1\n") {
		t.Fatalf("expected frame 1 to be skipped, got: %s", out)
	}
}

// A client far enough behind that the oldest needed frame has aged out
// of the log gets a single Reset frame carrying the current view's
// HTML; subsequent reconnects from that Last-Event-ID see no replay.
func TestSessionSSEResyncOutOfWindow(t *testing.T) {
	const window = 2
	s := newTestSession(&counterApp{})
	s.sv.config.replayWindow = window
	s.log = make([]frame, window)
	defer s.cancel()
	// Four apply calls overflow the window of 2, so the oldest two
	// frames are gone from the ring. Client at seq 1 needs them.
	s.apply(s.ctx, 1)
	s.apply(s.ctx, 2)
	s.apply(s.ctx, 3)
	s.apply(s.ctx, 4)
	out := runSSE(t, s, "1", 30*time.Millisecond)
	if !strings.Contains(out, `"op":"reset"`) {
		t.Fatalf("expected reset patch for out-of-window client, got: %s", out)
	}
	if !strings.Contains(out, "id: 4\n") {
		t.Fatalf("expected reset to carry head=4 as its id, got: %s", out)
	}
	if !strings.Contains(out, "<div>4</div>") {
		t.Fatalf("expected rendered view in reset html, got: %s", out)
	}
}

// A client whose Last-Event-ID is higher than anything the server has
// issued (stale session, different server instance) gets resynced to
// current state.
func TestSessionSSEResyncAheadOfHead(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.apply(s.ctx, 1)
	out := runSSE(t, s, "42", 30*time.Millisecond)
	if !strings.Contains(out, `"op":"reset"`) {
		t.Fatalf("expected reset for stale client, got: %s", out)
	}
}

// A malformed Last-Event-ID is a client bug, not a transient glitch —
// reject the request with 400 so EventSource gives up rather than
// retrying forever.
func TestSessionSSEBadLastEventID(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	req := httptest.NewRequest("GET", "/sse/x", nil)
	req.Header.Set("Last-Event-ID", "not-a-number")
	rec := httptest.NewRecorder()
	s.handleSSE(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// SSE disconnect no longer ends the session — the session keeps living
// (so a reconnect can resume from the patch log). Only the idle timeout
// or an explicit cancel ends it now.
func TestSessionSSEDisconnectDoesNotCancel(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	_ = runSSE(t, s, "", 20*time.Millisecond)
	if s.ctx.Err() != nil {
		t.Fatalf("session ctx cancelled by SSE disconnect: %v", s.ctx.Err())
	}
}

// With nothing else to send, the SSE handler emits keepalive comment
// lines on the configured cadence. Useful for proxies that drop idle
// streams, and the activity touch keeps the session alive even with no
// patches flowing.
func TestSessionSSEKeepalive(t *testing.T) {
	s := newTestSession(&counterApp{})
	s.sv.config.keepalive = 10 * time.Millisecond
	defer s.cancel()
	out := runSSE(t, s, "", 50*time.Millisecond)
	if !strings.Contains(out, ": keepalive") {
		t.Fatalf("expected keepalive in SSE output: %s", out)
	}
}

// A second SSE attach evicts the first: the first handler's attachment
// ctx fires and its loop returns. The session itself is not cancelled.
func TestSessionSSEEviction(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()

	first := func() (*httptest.ResponseRecorder, chan struct{}, context.CancelFunc) {
		req := httptest.NewRequest("GET", "/sse/x", nil)
		ctx, cancel := context.WithCancel(context.Background())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			s.handleSSE(rec, req)
			close(done)
		}()
		return rec, done, cancel
	}

	_, done1, cancel1 := first()
	defer cancel1()
	// Give g1 time to take the slot.
	time.Sleep(20 * time.Millisecond)

	_, done2, cancel2 := first()
	defer cancel2()

	select {
	case <-done1:
	case <-time.After(time.Second):
		t.Fatal("first SSE attachment was not evicted by the second")
	}
	if s.ctx.Err() != nil {
		t.Fatalf("session ctx cancelled by eviction: %v", s.ctx.Err())
	}

	cancel2()
	<-done2
}

// (*session).spawn runs each Cmd body in its own goroutine.
func TestSessionSpawnRunsCmds(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	done := make(chan struct{})
	cmd := Func(func() int {
		close(done)
		return 0
	})
	s.spawn(cmd)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Cmd body never ran")
	}
}

// subApp lets tests control the subscription set dynamically.
// Subscriptions returns whatever sub is set to at the time.
type subApp struct {
	sub Sub[int]
}

func (a *subApp) Update(context.Context, int) Cmd[int]   { return Batch[int]() }
func (a *subApp) View(context.Context) (string, Node)    { return "", Tag("div")()() }
func (a *subApp) Subscriptions(context.Context) Sub[int] { return a.sub }

type tickKey struct{ id string }

// A subscription's event stream runs in its own goroutine and
// dispatches Msgs through apply.
func TestSessionSubStartsAndDispatches(t *testing.T) {
	ready := make(chan struct{})
	app := &subApp{sub: Subscription(tickKey{"a"}, func(ctx context.Context) iter.Seq[int] {
		return func(yield func(int) bool) {
			close(ready)
			yield(42)
		}
	})}
	s := newTestSession[int](app)
	defer s.cancel()
	// Initial diffSubs to wire up the subscription.
	s.mu.Lock()
	s.updateSubs(app.Subscriptions(s.ctx))
	s.mu.Unlock()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("subscription goroutine never started")
	}
	// Give apply time to process.
	time.Sleep(20 * time.Millisecond)
}

// A subscription removed from the set has its context cancelled.
func TestSessionSubCancelledOnRemoval(t *testing.T) {
	app := &subApp{sub: Subscription(tickKey{"a"}, func(ctx context.Context) iter.Seq[int] {
		return func(yield func(int) bool) {
			<-ctx.Done()
		}
	})}
	s := newTestSession[int](app)
	defer s.cancel()

	s.mu.Lock()
	s.updateSubs(app.Subscriptions(s.ctx))
	if len(s.subs) != 1 {
		s.mu.Unlock()
		t.Fatalf("expected 1 active sub, got %d", len(s.subs))
	}
	s.mu.Unlock()

	// Remove all subscriptions.
	app.sub = Sub[int]{}
	s.mu.Lock()
	s.updateSubs(app.Subscriptions(s.ctx))
	if len(s.subs) != 0 {
		s.mu.Unlock()
		t.Fatalf("expected 0 active subs after removal, got %d", len(s.subs))
	}
	s.mu.Unlock()
}

// An unchanged key is not restarted across diffs.
func TestSessionSubPersistsAcrossDiffs(t *testing.T) {
	starts := make(chan struct{}, 10)
	app := &subApp{sub: Subscription(tickKey{"a"}, func(ctx context.Context) iter.Seq[int] {
		return func(yield func(int) bool) {
			starts <- struct{}{}
			<-ctx.Done()
		}
	})}
	s := newTestSession[int](app)
	defer s.cancel()

	s.mu.Lock()
	s.updateSubs(app.Subscriptions(s.ctx))
	s.mu.Unlock()

	<-starts // first start

	// Diff again with the same key — should NOT restart.
	s.mu.Lock()
	s.updateSubs(app.Subscriptions(s.ctx))
	s.mu.Unlock()

	select {
	case <-starts:
		t.Fatal("subscription was restarted despite unchanged key")
	case <-time.After(50 * time.Millisecond):
		// good — no restart
	}
}

// Subs composes multiple Sub values into one.
func TestSubsComposition(t *testing.T) {
	noop := func(context.Context) iter.Seq[int] { return func(func(int) bool) {} }
	a := Subscription[int](tickKey{"a"}, noop)
	b := Subscription[int](tickKey{"b"}, noop)
	combined := Subs(a, b)
	if len(combined.s) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(combined.s))
	}
}

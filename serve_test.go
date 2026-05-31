package domi

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
func (a *counterApp) Preview(ctx context.Context, _ *url.URL) (string, Node, bool) {
	t, v := a.View(ctx)
	return t, v, true
}

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
func (a *fragmentApp) Preview(ctx context.Context, _ *url.URL) (string, Node, bool) {
	t, v := a.View(ctx)
	return t, v, true
}

// titledApp changes both its body and its document title each Update, so
// frames carry a SetTitle effect alongside the DOM patches.
type titledApp struct{ n int }

func (a *titledApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *titledApp) View(context.Context) (string, Node) {
	return fmt.Sprintf("title-%d", a.n), Tag("div")()(Text(fmt.Sprintf("%d", a.n)))
}
func (a *titledApp) Subscriptions(context.Context) Sub[int] { return Sub[int]{} }
func (a *titledApp) Preview(ctx context.Context, _ *url.URL) (string, Node, bool) {
	t, v := a.View(ctx)
	return t, v, true
}

// previewApp renders a body that depends on both a per-Update counter and
// the current route, and previews a route without changing state. So a
// prefetch produces a non-empty patchset that differs from the live view,
// and a later Update both moves the live view and rebases the preview.
// The /deny route is refused, exercising the DeletePreview path.
type previewApp struct {
	n     int
	route string
}

func (a *previewApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *previewApp) body() Node {
	return Tag("div")()(Text(fmt.Sprintf("%s-%d", a.route, a.n)))
}
func (a *previewApp) View(context.Context) (string, Node)    { return a.route, a.body() }
func (a *previewApp) Subscriptions(context.Context) Sub[int] { return Sub[int]{} }
func (a *previewApp) Preview(_ context.Context, u *url.URL) (string, Node, bool) {
	if u.Path == "/deny" {
		return "", nil, false
	}
	p := *a
	p.route = u.Path
	return p.route, p.body(), true
}

// newTestSession wires up a session for direct method tests, bypassing
// the http surface. The session points at a default-configured server
// so apply, idleWatch, and friends can read config fields without going
// through Handler.
func newTestSession[Msg any](app App[Msg]) *session[Msg] {
	const replayWindow = 128
	ctx, cancel := context.WithCancel(context.Background())
	_, view := app.View(ctx)
	nodes, h := lower(view)
	return &session[Msg]{
		ctx:    ctx,
		cancel: cancel,
		app:    app,
		logger: slog.New(slog.DiscardHandler),
		sv: &server[Msg]{
			replayWindow: replayWindow,
			keepalive:    25 * time.Second,

			onURLChange:  func(*url.URL) Msg { var zero Msg; return zero },
			onURLRequest: func(URLRequest) Msg { var zero Msg; return zero },
		},
		log:      make([]frame, replayWindow),
		base:     baseInitial,
		view:     nodes,
		handlers: h,
		active:   time.Now(),
	}
}

// A Fragment returned from View lowers to multiple top-level children,
// and apply diffs them positionally against the previous frame.
func TestSessionApplyFragmentAtRoot(t *testing.T) {
	s := newTestSession(&fragmentApp{})
	defer s.cancel()
	s.apply(s.ctx, 1, nil)
	if s.head != 1 {
		t.Fatalf("expected head=1 after one apply, got %d", s.head)
	}
	// Each <div> child's text changes (a0→a1, b0→b1), producing two
	// patches inside the frame's single ApplyPatch effect. The exact
	// shape isn't the contract — what matters is that *both* top-level
	// siblings were diffed, not just one.
	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 1 || f.Effects[0].Type != effectApplyPatch {
		t.Fatalf("expected one ApplyPatch effect, got %+v", f.Effects)
	}
	if n := len(f.Effects[0].Patches); n < 2 {
		t.Fatalf("expected patches for both Fragment siblings, got %d: %+v", n, f.Effects[0].Patches)
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

// A Dispatch message routes to the handler named by its key, rebuilds
// that handler's Msg, and applies it — landing as a frame.
func TestHandleEventDispatch(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()

	// Register an int Msg (counterApp's Msg type) the way On would, then
	// dispatch its key.
	a := On("click")(1).(attr)
	s.handlers = s.handlers.merge(a.handlers)
	body := fmt.Sprintf(`{"Type":"Dispatch","Handler":%q}`, a.attr.Value)
	rec := httptest.NewRecorder()
	s.handleEvent(rec, httptest.NewRequest("POST", "/event/x", strings.NewReader(body)))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	// apply runs in a goroutine; wait for the dispatched Msg to land.
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		head := s.head
		s.mu.Unlock()
		if head == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Dispatch did not apply a Msg (no frame produced)")
		}
		time.Sleep(5 * time.Millisecond)
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
	s.sv.replayWindow = window
	s.log = make([]frame, window)
	defer s.cancel()
	s.apply(s.ctx, 1, nil) // seq 1 → log[1]
	s.apply(s.ctx, 2, nil) // seq 2 → log[0] (overwrites zero-value)
	s.apply(s.ctx, 3, nil) // seq 3 → log[1] (overwrites seq 1)
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
	if strings.Contains(out, "event: effect") {
		t.Fatalf("expected no effect frames for fresh empty session, got: %s", out)
	}
}

// A client reconnecting within the replay window receives only the
// frames it missed, each tagged with its monotonic id.
func TestSessionSSEReplayWithinWindow(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.apply(s.ctx, 1, nil)
	s.apply(s.ctx, 2, nil)
	s.apply(s.ctx, 3, nil)
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
	s.sv.replayWindow = window
	s.log = make([]frame, window)
	defer s.cancel()
	// Four apply calls overflow the window of 2, so the oldest two
	// frames are gone from the ring. Client at seq 1 needs them.
	s.apply(s.ctx, 1, nil)
	s.apply(s.ctx, 2, nil)
	s.apply(s.ctx, 3, nil)
	s.apply(s.ctx, 4, nil)
	out := runSSE(t, s, "1", 30*time.Millisecond)
	if !strings.Contains(out, `"Op":"Reset"`) {
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
	s.apply(s.ctx, 1, nil)
	out := runSSE(t, s, "42", 30*time.Millisecond)
	if !strings.Contains(out, `"Op":"Reset"`) {
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
	s.sv.keepalive = 10 * time.Millisecond
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
func (a *subApp) Preview(ctx context.Context, _ *url.URL) (string, Node, bool) {
	t, v := a.View(ctx)
	return t, v, true
}

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

// apply with a push nav caches the outgoing s.view (not the new
// view) under the nav's outgoing snapshot id.
func TestSessionSnapshotCacheOnApply(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	origView := s.view
	s.apply(s.ctx, 1, &nav{push: "/about", outgoingID: "snap1"})
	s.mu.Lock()
	sn, ok := s.snapshots["snap1"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("snapshot not cached after apply with push nav")
	}
	// The cached view should be the outgoing view, not the new one.
	if len(sn.view) != len(origView) {
		t.Fatalf("cached snapshot should be outgoing view (len %d), got len %d", len(origView), len(sn.view))
	}
}

// Load wires up a nav whose load target is the requested URL, leaving
// the dispatched Msg as the zero value. The url may be absolute and
// cross-origin, unlike PushURL/ReplaceURL.
func TestLoadCmdProducesLoadNav(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	const target = "https://example.com/logout"
	var got *nav
	for f := range Load[int](target).s {
		_, got = f(s)
	}
	if got == nil {
		t.Fatal("Load produced no nav")
	}
	if got.load != target {
		t.Fatalf("nav.load = %q, want %q", got.load, target)
	}
	if got.push != "" || got.replace != "" {
		t.Fatalf("Load nav set push/replace: %+v", got)
	}
}

// apply with a load nav emits a lone LoadURL effect and, because the
// document is replaced wholesale, runs neither Update nor onURLChange.
func TestSessionApplyLoadNav(t *testing.T) {
	app := &counterApp{}
	s := newTestSession[int](app)
	defer s.cancel()
	var changed int
	s.sv.onURLChange = func(*url.URL) int { changed++; return 0 }

	const target = "https://example.com/logout"
	s.apply(s.ctx, 1, &nav{load: target})

	if app.n != 0 {
		t.Fatalf("Update ran for a load nav (counter=%d), want 0", app.n)
	}
	if changed != 0 {
		t.Fatalf("onURLChange ran for a load nav (count=%d), want 0", changed)
	}
	if s.head != 1 {
		t.Fatalf("expected one frame after load nav, got head=%d", s.head)
	}
	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 1 || f.Effects[0].Type != effectLoadURL {
		t.Fatalf("expected a lone LoadURL effect, got %+v", f.Effects)
	}
	if f.Effects[0].URL != target {
		t.Fatalf("LoadURL effect URL = %q, want %q", f.Effects[0].URL, target)
	}
}

// apply orders the effects so the client snapshots the outgoing page
// before it mutates: PushURL leads, the DOM Patch follows, and SetTitle
// trails (so the outgoing snapshot still captures the old title).
func TestSessionApplyEffectOrder(t *testing.T) {
	s := newTestSession[int](&titledApp{})
	defer s.cancel()
	s.apply(s.ctx, 1, &nav{push: "/about", outgoingID: "snap1"})

	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 3 {
		t.Fatalf("expected PushURL, Patch, SetTitle effects, got %+v", f.Effects)
	}
	if e := f.Effects[0]; e.Type != effectPushURL || e.URL != "/about" || e.ID != "snap1" {
		t.Fatalf("effect[0] should be PushURL{/about, snap1}, got %+v", e)
	}
	if e := f.Effects[1]; e.Type != effectApplyPatch || len(e.Patches) == 0 {
		t.Fatalf("effect[1] should be a non-empty ApplyPatch, got %+v", e)
	}
	if e := f.Effects[2]; e.Type != effectSetTitle || e.Title != "title-1" {
		t.Fatalf("effect[2] should be SetTitle{title-1}, got %+v", e)
	}
}

// prefetch holds the preview and emits a lone SetPreview effect carrying
// the previewed page's title and url as data, the rebasing patchset, and
// a candidate snapshot id naming the cached outgoing (current) page.
func TestSessionPrefetchEmitsSetPreview(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)

	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 1 || f.Effects[0].Type != effectSetPreview {
		t.Fatalf("expected a lone SetPreview effect, got %+v", f.Effects)
	}
	e := f.Effects[0]
	if e.Title != "/next" || e.URL != "/next" {
		t.Fatalf("SetPreview = %+v, want title and url %q", e, "/next")
	}
	if e.ID == "" {
		t.Fatal("SetPreview must carry a candidate snapshot id")
	}
	if len(e.Patches) == 0 {
		t.Fatal("SetPreview should carry the patchset from current view to preview")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		t.Fatal("prefetch should leave the preview outstanding")
	}
	if _, ok := s.preview.log[e.ID]; !ok {
		t.Fatalf("candidate (outgoing) view %q not recorded in the preview log", e.ID)
	}
	// A speculative preview must not touch the navigation snapshot cache.
	if _, ok := s.snapshots[e.ID]; ok {
		t.Fatalf("candidate %q leaked into the snapshot cache", e.ID)
	}
}

// A denied prefetch emits a lone DeletePreview effect and holds no
// preview, so the click falls back to a normal request.
func TestSessionPrefetchDenyEmitsDeletePreview(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/deny")
	s.prefetch(s.ctx, u)

	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 1 || f.Effects[0].Type != effectDeletePreview {
		t.Fatalf("expected a lone DeletePreview effect, got %+v", f.Effects)
	}
	if f.Effects[0].URL != "/deny" {
		t.Fatalf("DeletePreview URL = %q, want %q", f.Effects[0].URL, "/deny")
	}
	s.mu.Lock()
	pending := s.preview != nil
	s.mu.Unlock()
	if pending {
		t.Fatal("a denied prefetch must not set an outstanding preview")
	}
}

// While a preview is outstanding, each later frame is augmented with a
// SetPreview that rebases the preview onto the new view (so the client
// always holds a clean patchset from its current DOM to the preview), and
// caches that view as a fresh candidate outgoing snapshot.
func TestSessionPreviewRebasedOnApply(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)

	s.apply(s.ctx, 0, nil)
	f := s.log[2%uint64(len(s.log))]
	var ap, sp *effect
	for i := range f.Effects {
		switch f.Effects[i].Type {
		case effectApplyPatch:
			ap = &f.Effects[i]
		case effectSetPreview:
			sp = &f.Effects[i]
		}
	}
	if ap == nil || sp == nil {
		t.Fatalf("frame should carry both ApplyPatch and SetPreview, got %+v", f.Effects)
	}
	if sp.URL != "/next" || sp.ID == "" {
		t.Fatalf("rebased SetPreview = %+v, want url %q with a candidate id", sp, "/next")
	}
	if len(sp.Patches) == 0 {
		t.Fatal("rebased SetPreview should carry a non-empty patchset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		t.Fatal("preview should remain outstanding after a rebasing apply")
	}
	if _, ok := s.preview.log[sp.ID]; !ok {
		t.Fatalf("candidate view %q not recorded in the preview log", sp.ID)
	}
}

// commitPreview installs the held preview as the current view, adopts the
// candidate id as the base, and clears the outstanding preview. The apply
// that follows a real commit then diffs against the installed view.
func TestSessionCommitPreviewInstallsView(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)
	cand := s.log[1%uint64(len(s.log))].Effects[0].ID

	s.commitPreview(s.ctx, cand)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base != cand {
		t.Fatalf("base = %q, want candidate %q", s.base, cand)
	}
	if s.preview != nil {
		t.Fatal("pending preview should be cleared after commit")
	}
	if s.title != "/next" {
		t.Fatalf("title = %q, want %q", s.title, "/next")
	}
	want, _ := lower(Tag("div")()(Text("/next-0")))
	if !reflect.DeepEqual(s.view, want) {
		t.Fatalf("committed view = %+v, want the previewed page", s.view)
	}
	// The outgoing page (the view at prefetch) is promoted into the
	// snapshot cache under the committed candidate id, for back nav.
	sn, ok := s.snapshots[cand]
	if !ok {
		t.Fatal("committed outgoing view should move into the snapshot cache")
	}
	if outgoing, _ := lower(Tag("div")()(Text("/-0"))); !reflect.DeepEqual(sn.view, outgoing) {
		t.Fatalf("promoted snapshot view = %+v, want the outgoing page", sn.view)
	}
}

// commitPreview ignores a commit when no preview is held — a malformed or
// malicious client message must not mutate session state.
func TestSessionCommitPreviewWithoutHeldPreview(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	origView := s.view

	s.commitPreview(s.ctx, "snap") // must not panic

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview != nil {
		t.Fatalf("preview = %v, want nil", s.preview)
	}
	if s.base != baseInitial {
		t.Fatalf("base = %q, want it left at %q", s.base, baseInitial)
	}
	if !reflect.DeepEqual(s.view, origView) {
		t.Fatalf("view changed to %+v, want it untouched", s.view)
	}
}

// Once a preview's candidate log fills, the server freezes it: the next
// frame carries a DeletePreview instead of a SetPreview and stops
// rebasing, but the retained candidates stay so an in-flight click still
// commits.
func TestSessionPreviewFreezesAtLimit(t *testing.T) {
	const limit = 128 // preview.addView's internal cap
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u) // candidate 1

	// Drive enough applies to fill the log. Each view-changing apply adds
	// one candidate until the limit, then the next freezes the preview.
	for i := 0; i < limit; i++ {
		s.apply(s.ctx, 0, nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil || !s.preview.frozen {
		t.Fatalf("preview should be frozen after %d updates", limit)
	}
	if n := len(s.preview.log); n != limit {
		t.Fatalf("frozen log holds %d candidates, want %d (all retained)", n, limit)
	}
	// The freeze frame is the most recent: a DeletePreview, no SetPreview.
	f := s.log[s.head%uint64(len(s.log))]
	var del, set bool
	for _, e := range f.Effects {
		del = del || e.Type == effectDeletePreview
		set = set || e.Type == effectSetPreview
	}
	if !del || set {
		t.Fatalf("freeze frame should carry DeletePreview and no SetPreview, got %+v", f.Effects)
	}
}

// A new prefetch supersedes the outstanding preview: the server replaces
// it wholesale, dropping the previous one's candidate log. The client
// drops its preview at the same moment (when it requests the new one), so
// nothing is left to claim the discarded candidates.
func TestSessionPrefetchSupersedes(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()

	first, _ := url.Parse("/a")
	s.prefetch(s.ctx, first)
	firstCand := s.log[1%uint64(len(s.log))].Effects[0].ID

	second, _ := url.Parse("/b")
	s.prefetch(s.ctx, second)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil || s.preview.url != "/b" {
		t.Fatalf("outstanding preview = %+v, want /b", s.preview)
	}
	if _, ok := s.preview.log[firstCand]; ok {
		t.Fatal("superseded preview's candidates should be discarded")
	}
}

// restoreSnapshot swaps s.view and s.title to the cached values
// and updates the epoch.
func TestSessionSnapshotRestore(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	origView := s.view
	origTitle := s.title

	// Cache a different view under a snapshot id.
	otherView, _ := lower(Tag("div")()(Text("other")))
	s.cacheSnapshot("snap1", otherView, "other title")

	s.restoreSnapshot("snap1")
	if s.title != "other title" {
		t.Fatalf("expected title %q, got %q", "other title", s.title)
	}
	if s.base != "snap1" {
		t.Fatalf("expected base %q, got %q", "snap1", s.base)
	}

	// Restoring a nonexistent id still updates the base but
	// leaves view and title unchanged.
	s.view = origView
	s.title = origTitle
	s.restoreSnapshot("nonexistent")
	if s.title != origTitle {
		t.Fatalf("restoreSnapshot with bad id changed title to %q", s.title)
	}
	if s.base != "nonexistent" {
		t.Fatalf("expected base %q, got %q", "nonexistent", s.base)
	}
}

// The snapshot cache evicts the oldest entries when full.
func TestSessionSnapshotEviction(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	view, _ := lower(Tag("div")()(Text("x")))
	for i := range snapshotCacheSize + 5 {
		s.cacheSnapshot(fmt.Sprintf("s%d", i), view, "t")
	}
	if len(s.snapshots) != snapshotCacheSize {
		t.Fatalf("expected %d snapshots, got %d", snapshotCacheSize, len(s.snapshots))
	}
	// The first 5 should be evicted.
	for i := range 5 {
		if _, ok := s.snapshots[fmt.Sprintf("s%d", i)]; ok {
			t.Fatalf("s%d should have been evicted", i)
		}
	}
	// The rest should be present.
	for i := 5; i < snapshotCacheSize+5; i++ {
		if _, ok := s.snapshots[fmt.Sprintf("s%d", i)]; !ok {
			t.Fatalf("s%d should be present", i)
		}
	}
}

// Frames carry the session's base so the client can drop stale frames.
func TestSessionFrameBase(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.apply(s.ctx, 1, nil) // base "00000..."
	s.mu.Lock()
	if s.log[1].Base != baseInitial {
		t.Fatalf("expected base %q, got %q", baseInitial, s.log[1].Base)
	}
	s.base = "snap1"
	s.mu.Unlock()
	s.apply(s.ctx, 2, nil) // base "snap1"
	s.mu.Lock()
	if s.log[2].Base != "snap1" {
		t.Fatalf("expected base %q, got %q", "snap1", s.log[2].Base)
	}
	s.mu.Unlock()
}

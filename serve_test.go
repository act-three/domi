package domi

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"ily.dev/domi/internal/vdom"
)

// counterApp is a minimal App used in lifecycle tests. Each Update bumps n
// so View produces a different tree (and the diff produces real patches).
type counterApp struct{ n int }

func (a *counterApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *counterApp) View(context.Context) (string, Node) {
	return "", Tag("div")(Text(fmt.Sprintf("%d", a.n)))
}
func (a *counterApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *counterApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// staticApp's view never changes, so an Update produces no patches.
type staticApp struct{}

func (a *staticApp) Update(context.Context, int) Cmd[int] { return Batch[int]() }
func (a *staticApp) View(context.Context) (string, Node) {
	return "", Tag("div")(Text("static"))
}
func (a *staticApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *staticApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// fragmentApp's View returns a Fragment so the framework treats its
// members as separate top-level children of the mount.
type fragmentApp struct{ n int }

func (a *fragmentApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *fragmentApp) View(context.Context) (string, Node) {
	return "", Fragment(
		Tag("div")(Text(fmt.Sprintf("a%d", a.n))),
		Tag("div")(Text(fmt.Sprintf("b%d", a.n))),
	)
}
func (a *fragmentApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *fragmentApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// titledApp changes both its body and its document title each Update, so
// frames carry a SetTitle effect alongside the DOM patches.
type titledApp struct{ n int }

func (a *titledApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *titledApp) View(context.Context) (string, Node) {
	return fmt.Sprintf("title-%d", a.n), Tag("div")(Text(fmt.Sprintf("%d", a.n)))
}
func (a *titledApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *titledApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// previewApp renders a body that depends on both a per-Update counter and
// the current route, and previews a route without changing state. So a
// prefetch produces a non-empty patchset that differs from the live view,
// and a later Update both moves the live view and rebases the preview.
// The /deny route is refused, exercising the DeletePreview path; /bad
// returns a non-relative dest, a contract violation that panics; and
// /redirect lands on /landing, exercising the preview redirect.
type previewApp struct {
	n     int
	route string
}

func (a *previewApp) Update(context.Context, int) Cmd[int] { a.n++; return Batch[int]() }
func (a *previewApp) body() Node {
	return Tag("div")(Text(fmt.Sprintf("%s-%d", a.route, a.n)))
}
func (a *previewApp) View(context.Context) (string, Node)    { return a.route, a.body() }
func (a *previewApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *previewApp) Preview(_ context.Context, u *url.URL) (string, string, Node) {
	switch u.Path {
	case "/deny":
		return "", "", nil
	case "/bad":
		return "http://evil.example/", "", nil
	}
	p := *a
	p.route = u.Path
	if u.Path == "/redirect" {
		p.route = "/landing"
	}
	return p.route, p.route, p.body()
}

// newTestSession wires up a session for direct method tests, bypassing
// the http surface. The session points at a default-configured server
// so apply, idleWatch, and friends can read config fields without going
// through Handler.
func newTestSession[Msg any](app App[Msg]) *session[Msg] {
	const replayWindow = 128
	ctx, cancel := context.WithCancel(context.Background())
	_, view := app.View(ctx)
	nodes, h := lower(0, view)
	s := &session[Msg]{
		ctx:    ctx,
		cancel: cancel,
		app:    app,
		logger: slog.New(slog.DiscardHandler),
		sv: &server[Msg]{
			replayWindow: replayWindow,
			keepalive:    25 * time.Second,

			onURLChange:  func(*url.URL) Msg { var zero Msg; return zero },
			onURLRequest: func(*url.URL, bool) Msg { var zero Msg; return zero },
		},
		log:       make([]frame, replayWindow),
		base:      verInitial,
		ver:       verInitial,
		view:      nodes,
		active:    time.Now(),
		snapshots: newTreeRing(snapshotRingSize),
		recent:    newTreeRing(recentRingSize),
	}
	s.tables = map[string]table[Msg]{verInitial: typed[Msg](h)}
	s.recent.put(verInitial, tree{view: nodes})
	return s
}

// A Fragment returned from View lowers to multiple top-level children,
// and apply diffs them positionally against the previous frame.
func TestSessionApplyFragmentAtRoot(t *testing.T) {
	s := newTestSession(&fragmentApp{})
	defer s.cancel()
	s.apply(s.ctx, []int{1}, nil)
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
// to domi-root.
func TestHandlerDocumentOption(t *testing.T) {
	custom := func(title string, body Node) Node {
		return Tag("html")(
			Tag("head")(
				Tag("title")(Text("custom:"+title)),
				Tag("meta", Name("name", "test"), Name("content", "hello")),
			),
			body,
		)
	}
	h := Handler(
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) {
			return &counterApp{}, Batch[int]()
		},
		func(*url.URL, bool) int { return 0 },
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
	if !strings.Contains(body, ` prefix="`) {
		t.Fatalf("session marker not attached to domi-root; got: %s", body)
	}
	// The default bootstrap script must not appear when Document is set —
	// the App is responsible for loading the client itself.
	if strings.Contains(body, "Domi.run()") {
		t.Fatalf("default bootstrap leaked into custom Document; got: %s", body)
	}
}

// InternalURLPrefix moves every framework-served URL beneath the chosen
// path: the rendered session prefix, the client bootstrap import, the
// path the client runtime is served at, and the event routes the client
// posts back to. The site root stays the app's. A trailing slash in the
// option is tolerated; path.Join folds the variants together.
func TestHandlerInternalURLPrefix(t *testing.T) {
	h := Handler(
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) {
			return &counterApp{}, Batch[int]()
		},
		func(*url.URL, bool) int { return 0 },
		func(*url.URL) int { return 0 },
		InternalURLPrefix("/-/domi/"),
		Logger(slog.New(slog.DiscardHandler)),
	)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()

	// The session's internal-URL base is the prefix joined with the id,
	// joined cleanly (no doubled slash from the option's trailing one).
	m := regexp.MustCompile(` prefix="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("session marker missing; body: %s", body)
	}
	base := m[1]
	if !strings.HasPrefix(base, "/-/domi/") || base == "/-/domi/" || strings.Contains(base[1:], "//") {
		t.Fatalf("session prefix not cleanly under /-/domi/: %q", base)
	}

	// The bootstrap imports the client runtime from under the prefix, and
	// that path actually serves the script.
	src := regexp.MustCompile(`import \* as Domi from "(/-/domi/domi\.[0-9a-f]+\.js)"`).FindStringSubmatch(body)
	if src == nil {
		t.Fatalf("client import not under prefix; body: %s", body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", src[1], nil))
	if w.Code != http.StatusOK {
		t.Fatalf("client runtime not served at %s: status %d", src[1], w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("client runtime content-type = %q", ct)
	}

	// The event sink answers under the prefix for the live session: a
	// well-formed envelope is accepted, not met with the wrapper's 404 for
	// an unknown session (which is what an unprefixed POST would hit).
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", base+"/event", strings.NewReader("{}")))
	if w.Code == http.StatusNotFound {
		t.Fatalf("event route not wired under prefix at %s/event", base)
	}
}

// A Dispatch message routes to the handler named by its key, rebuilds
// that handler's Msg, and applies it — landing as a frame.
func TestHandleEventDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestSession(&counterApp{})
		defer s.cancel()

		// Register an unmarshal function the way lowering would, under the
		// session's current tree version, then dispatch its key. The client
		// sends the bare handler key, stripping the pathSet key, plus the
		// version id of the tree its DOM displays.
		s.tables[s.ver] = table[int]{"k1": msgInt(1)}
		body := fmt.Sprintf(`{"Type":"Dispatch","Handler":"k1","Ver":%q}`, s.ver)
		rec := httptest.NewRecorder()
		s.handleEvent(rec, httptest.NewRequest("POST", "/x/event", strings.NewReader(body)))

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		// apply runs in a goroutine; the settled bubble has run it.
		synctest.Wait()
		s.mu.Lock()
		head := s.head
		s.mu.Unlock()
		if head != 1 {
			t.Fatal("Dispatch did not apply a Msg (no frame produced)")
		}
	})
}

// A frame that changes the tree carries a fresh version id naming the
// new tree, so the client learns the name along with the change.
func TestApplyMintsVerOnTreeChange(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()

	old := s.ver
	s.apply(s.ctx, []int{1}, nil) // counterApp's view changes every Update
	if s.ver == old {
		t.Fatal("apply with patches did not mint a new ver")
	}
	f := s.log[s.head%uint64(len(s.log))]
	var got string
	for _, e := range f.Effects {
		if e.Type == effectApplyPatch {
			got = e.Ver
		}
	}
	if got != s.ver {
		t.Fatalf("ApplyPatch Ver = %q, want %q", got, s.ver)
	}
}

// An apply that leaves the tree unchanged mints nothing: the client's
// echoed name stays valid for the tree it still displays.
func TestApplyKeepsVerWithoutPatches(t *testing.T) {
	s := newTestSession(&staticApp{})
	defer s.cancel()

	old := s.ver
	s.apply(s.ctx, []int{0}, nil)
	if s.ver != old {
		t.Fatalf("apply without patches changed ver from %q to %q", old, s.ver)
	}
	if s.head != 0 {
		t.Fatalf("apply without effects appended a frame (head = %d)", s.head)
	}
}

// idleWatch cancels the session once activity falls behind by d.
func TestSessionIdleWatchFires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const d = 50 * time.Millisecond
		s := newTestSession(&counterApp{})
		defer s.cancel()
		go s.idleWatch(d)
		select {
		case <-s.ctx.Done():
		case <-time.After(d * 10):
			t.Fatal("idleWatch did not cancel an idle session")
		}
	})
}

// touch defers the idle deadline. Repeated touches keep the session
// alive past d; once they stop, idleWatch fires.
func TestSessionIdleWatchTouchDefers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
	})
}

// A session whose client never attaches SSE expires after
// SessionTimeout: the watchdog cancels its ctx and the server removes it
// from the live-session map.
func TestServerSessionTimeoutNeverAttached(t *testing.T) {
	const d = 50 * time.Millisecond
	sv := newServer(
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) { return &counterApp{}, Batch[int]() },
		func(*url.URL, bool) int { return 0 },
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
	req := httptest.NewRequest("GET", "/x/events", nil)
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
	s.apply(s.ctx, []int{1}, nil) // seq 1 → log[1]
	s.apply(s.ctx, []int{2}, nil) // seq 2 → log[0] (overwrites zero-value)
	s.apply(s.ctx, []int{3}, nil) // seq 3 → log[1] (overwrites seq 1)
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
	s.apply(s.ctx, []int{1}, nil)
	s.apply(s.ctx, []int{2}, nil)
	s.apply(s.ctx, []int{3}, nil)
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
	s.apply(s.ctx, []int{1}, nil)
	s.apply(s.ctx, []int{2}, nil)
	s.apply(s.ctx, []int{3}, nil)
	s.apply(s.ctx, []int{4}, nil)
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
	s.apply(s.ctx, []int{1}, nil)
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
	req := httptest.NewRequest("GET", "/x/events", nil)
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
		req := httptest.NewRequest("GET", "/x/events", nil)
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

// A nil Cmd is the empty Batch's degenerate twin: spawning it runs
// nothing, so Update can return a cmd-or-nil with no guard at the use
// site.
func TestSessionSpawnNilCmdRunsNothing(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.spawn(nil) // must not panic
}

// Batch treats a nil Cmd like an empty Batch, so it drops out of the
// lowered sequence and the surviving commands still run.
func TestBatchNilCmdContributesNothing(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	done := make(chan struct{})
	cmd := Batch[int](nil, Func(func() int {
		close(done)
		return 0
	}), nil)
	s.spawn(cmd)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("surviving Cmd body never ran")
	}
}

// subApp lets tests control the subscription set dynamically.
// Subscriptions returns whatever sub is set to at the time.
type subApp struct {
	sub Sub[int]
}

func (a *subApp) Update(context.Context, int) Cmd[int]   { return Batch[int]() }
func (a *subApp) View(context.Context) (string, Node)    { return "", Tag("div") }
func (a *subApp) Subscriptions(context.Context) Sub[int] { return a.sub }
func (a *subApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

type tickKey struct{ id string }

// A subscription's event stream runs in its own goroutine and
// dispatches Msgs through apply.
func TestSessionSubStartsAndDispatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		// Settle the bubble so the yielded value's apply finishes.
		synctest.Wait()
	})
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
	app.sub = nil
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
	combined := Subs[int](a, b).(subs[int])
	if len(combined) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(combined))
	}
}

// Subs treats a nil Sub like an empty Subs, so it drops out and the
// surviving subscriptions remain.
func TestSubsNilContributesNothing(t *testing.T) {
	noop := func(context.Context) iter.Seq[int] { return func(func(int) bool) {} }
	a := Subscription[int](tickKey{"a"}, noop)
	combined := Subs[int](nil, a, nil).(subs[int])
	if len(combined) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(combined))
	}
}

// A nil Sub is the empty Subs' degenerate twin: reconciling it cancels
// any active sources and starts none, so Subscriptions can return a
// sub-or-nil with no guard at the use site.
func TestUpdateSubsNilCancelsAll(t *testing.T) {
	app := &subApp{}
	s := newTestSession(app)
	defer s.cancel()
	started := make(chan struct{})
	app.sub = Subscription(tickKey{"a"}, func(ctx context.Context) iter.Seq[int] {
		return func(yield func(int) bool) {
			close(started)
			<-ctx.Done()
		}
	})
	s.updateSubs(app.Subscriptions(s.ctx))
	<-started
	app.sub = nil
	s.updateSubs(app.Subscriptions(s.ctx)) // nil must not panic
	if len(s.subs) != 0 {
		t.Fatalf("nil Sub should cancel all sources, %d remain", len(s.subs))
	}
}

// apply with a push nav stores the outgoing s.view (not the new view)
// under the outgoing tree's ver.
func TestSessionSnapshotStoredOnApply(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	origView := s.view
	origVer := s.ver
	s.apply(s.ctx, []int{1}, &nav{push: "/about"})
	s.mu.Lock()
	sn, ok := s.snapshots.get(origVer)
	s.mu.Unlock()
	if !ok {
		t.Fatal("snapshot not stored under the outgoing ver after apply with push nav")
	}
	// The stored view should be the outgoing view, not the new one.
	if len(sn.view) != len(origView) {
		t.Fatalf("stored snapshot should be outgoing view (len %d), got len %d", len(origView), len(sn.view))
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
	for f := range Load[int](target).(batch[int]) {
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
	s.apply(s.ctx, []int{1}, &nav{load: target})

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
	s.apply(s.ctx, []int{1}, &nav{push: "/about"})

	f := s.log[1%uint64(len(s.log))]
	if len(f.Effects) != 3 {
		t.Fatalf("expected PushURL, Patch, SetTitle effects, got %+v", f.Effects)
	}
	if e := f.Effects[0]; e.Type != effectPushURL || e.URL != "/about" {
		t.Fatalf("effect[0] should be PushURL{/about}, got %+v", e)
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
// the ver naming the preview tree. The outgoing (current) page becomes a
// candidate in the preview log under its own ver — the same name the
// client computes locally.
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
	// Without a redirect the destination is the requested URL, sent
	// unconditionally so the client never has to reconstruct it.
	if e.Dest != "/next" {
		t.Fatalf("SetPreview Dest = %q, want the requested url %q", e.Dest, "/next")
	}
	if e.Ver == "" {
		t.Fatal("SetPreview must carry the preview tree's ver")
	}
	if len(e.Patches) == 0 {
		t.Fatal("SetPreview should carry the patchset from current view to preview")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		t.Fatal("prefetch should leave the preview outstanding")
	}
	if _, ok := s.preview.log[s.ver]; !ok {
		t.Fatalf("candidate (outgoing) view %q not recorded in the preview log", s.ver)
	}
	// A speculative preview must not touch the navigation snapshot history.
	if _, ok := s.snapshots.get(s.ver); ok {
		t.Fatalf("candidate %q leaked into the snapshot history", s.ver)
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

// A prefetch whose Preview redirects keeps the requested URL as the
// client's match key but carries the destination in Dest, so committing
// the preview navigates to — and later routes on — the page actually
// rendered rather than the URL the user hovered.
func TestSessionPrefetchRedirectCarriesDest(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/redirect")
	s.prefetch(s.ctx, u)

	e := s.log[1%uint64(len(s.log))].Effects[0]
	if e.Type != effectSetPreview {
		t.Fatalf("expected SetPreview, got %+v", e)
	}
	if e.URL != "/redirect" {
		t.Fatalf("SetPreview URL = %q, want the requested url %q", e.URL, "/redirect")
	}
	if e.Dest != "/landing" || e.Title != "/landing" {
		t.Fatalf("SetPreview = %+v, want dest and title %q", e, "/landing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil || s.preview.dest != "/landing" {
		t.Fatalf("preview = %+v, want dest %q", s.preview, "/landing")
	}
}

// A non-relative dest violates the Preview contract — the same rule
// PushURL enforces — so prefetch panics rather than letting a bad URL
// slip through where it would be easy to miss.
func TestSessionPrefetchBadDestPanics(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/bad")
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on a non-relative Preview dest")
		}
	}()
	s.prefetch(s.ctx, u)
}

// While a preview is outstanding, each later frame is augmented with a
// SetPreview that rebases the preview onto the new view (so the client
// always holds a clean patchset from its current DOM to the preview), and
// stores that view as a fresh candidate outgoing snapshot.
func TestSessionPreviewRebasedOnApply(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)

	s.apply(s.ctx, []int{0}, nil)
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
	if sp.URL != "/next" {
		t.Fatalf("rebased SetPreview = %+v, want url %q", sp, "/next")
	}
	if len(sp.Patches) == 0 {
		t.Fatal("rebased SetPreview should carry a non-empty patchset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The new live tree (named by the ApplyPatch's ver) becomes a fresh
	// candidate in the preview log.
	if ap.Ver != s.ver {
		t.Fatalf("ApplyPatch Ver = %q, want current ver %q", ap.Ver, s.ver)
	}
	if s.preview == nil {
		t.Fatal("preview should remain outstanding after a rebasing apply")
	}
	if _, ok := s.preview.log[s.ver]; !ok {
		t.Fatalf("candidate view %q not recorded in the preview log", s.ver)
	}
}

// commitPreview installs the held preview as the current view, roots the
// new patch lineage at the outgoing candidate's ver, and clears the
// outstanding preview. The apply that follows a real commit then diffs
// against the installed view.
func TestSessionCommitPreviewInstallsView(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)
	cand := s.ver // the outgoing tree's ver names the candidate
	pver := s.log[1%uint64(len(s.log))].Effects[0].Ver

	s.commitPreview(s.ctx, cand)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base != cand {
		t.Fatalf("base = %q, want candidate %q", s.base, cand)
	}
	if s.ver != pver {
		t.Fatalf("ver = %q, want the preview tree's %q", s.ver, pver)
	}
	if s.preview != nil {
		t.Fatal("pending preview should be cleared after commit")
	}
	if s.title != "/next" {
		t.Fatalf("title = %q, want %q", s.title, "/next")
	}
	want, _ := lower(0, Tag("div")(Text("/next-0")))
	if !reflect.DeepEqual(s.view, want) {
		t.Fatalf("committed view = %+v, want the previewed page", s.view)
	}
	// The outgoing page (the view at prefetch) is promoted into the
	// snapshot history under the committed candidate id, for back nav.
	sn, ok := s.snapshots.get(cand)
	if !ok {
		t.Fatal("committed outgoing view should move into the snapshot history")
	}
	if outgoing, _ := lower(0, Tag("div")(Text("/-0"))); !reflect.DeepEqual(sn.view, outgoing) {
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
	if s.base != verInitial {
		t.Fatalf("base = %q, want it left at %q", s.base, verInitial)
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
		s.apply(s.ctx, []int{0}, nil)
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
	firstCand := s.ver // the current tree names the candidate

	// Move the live tree so the second prefetch's candidate gets a
	// different name than the first's.
	s.apply(s.ctx, []int{0}, nil)

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

// restoreSnapshot swaps s.view and s.title to the stored values
// and updates the epoch.
func TestSessionSnapshotRestore(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	origView := s.view
	origTitle := s.title

	// Store a different view under its ver.
	otherView, _ := lower(0, Tag("div")(Text("other")))
	s.snapshots.put("ver1", tree{view: otherView, title: "other title"})

	s.restoreSnapshot("ver1")
	if s.title != "other title" {
		t.Fatalf("expected title %q, got %q", "other title", s.title)
	}
	if s.base != "ver1" {
		t.Fatalf("expected base %q, got %q", "ver1", s.base)
	}
	if s.ver != "ver1" {
		t.Fatalf("expected ver %q, got %q", "ver1", s.ver)
	}

	// Restoring a nonexistent ver still roots a new lineage there but
	// leaves view, title, and ver unchanged: the server's tree is still
	// the one its ver names.
	s.view = origView
	s.title = origTitle
	s.restoreSnapshot("nonexistent")
	if s.title != origTitle {
		t.Fatalf("restoreSnapshot with bad ver changed title to %q", s.title)
	}
	if s.base != "nonexistent" {
		t.Fatalf("expected base %q, got %q", "nonexistent", s.base)
	}
	if s.ver != "ver1" {
		t.Fatalf("restoreSnapshot with bad ver changed ver to %q", s.ver)
	}
}

// A path set delivered only in a frame the client drops would otherwise
// strand: addPathSets marks it sent the moment a frame is built, so no
// later render re-delivers it. Snapshots carry the path sets known at
// their version, and restoring one resets s.pathSets to that set — so a
// set added after the snapshot (in a frame the rebase drops) is forgotten
// and re-sent, while sets the snapshot captured survive.
func TestSessionSnapshotRestoreResetsPathSets(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()

	known := pathSet{{"currentTarget", "value"}}
	s.pathSets = map[string]pathSet{known.key(): known}
	s.snapshots.put("ver1", tree{view: s.view, title: s.title, pathSets: maps.Clone(s.pathSets)})

	// A later frame delivers a new set, but the client drops it (it has
	// rebased away). On the server the set still lands in s.pathSets.
	stranded := pathSet{{"currentTarget", "dataset", "id"}}
	s.pathSets[stranded.key()] = stranded

	s.restoreSnapshot("ver1")

	if _, ok := s.pathSets[stranded.key()]; ok {
		t.Fatal("restore kept a set added after the snapshot; a frame-dropped set stays stranded")
	}
	if _, ok := s.pathSets[known.key()]; !ok {
		t.Fatal("restore dropped a set the snapshot captured; the next render would needlessly re-send it")
	}

	// The restore must not alias the stored snapshot, or a later delivery
	// would leak in and a second restore would resurrect it.
	s.pathSets[stranded.key()] = stranded
	if sn, _ := s.snapshots.get("ver1"); sn.pathSets[stranded.key()] != nil {
		t.Fatal("restore aliased the stored snapshot's path sets")
	}
}

// commitPreview rebases onto the preview and resets s.pathSets to the
// preview's prefetch baseline, so a set stranded in a frame the client
// dropped at the rebase is re-delivered rather than withheld.
func TestSessionCommitPreviewResetsPathSets(t *testing.T) {
	s := newTestSession[int](&previewApp{route: "/"})
	defer s.cancel()
	u, _ := url.Parse("/next")
	s.prefetch(s.ctx, u)
	cand := s.ver

	// Strand a set as if its frame were dropped when the client committed:
	// on the server it lands in s.pathSets after the preview captured its
	// baseline at prefetch.
	stranded := pathSet{{"currentTarget", "dataset", "id"}}
	s.mu.Lock()
	if s.pathSets == nil {
		s.pathSets = map[string]pathSet{}
	}
	s.pathSets[stranded.key()] = stranded
	s.mu.Unlock()

	s.commitPreview(s.ctx, cand)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pathSets[stranded.key()]; ok {
		t.Fatal("commitPreview left a set stranded; it didn't reset to the preview baseline")
	}
}

// The snapshot history evicts the oldest entries when full.
func TestSessionSnapshotEviction(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	view, _ := lower(0, Tag("div")(Text("x")))
	for i := range snapshotRingSize + 5 {
		s.snapshots.put(fmt.Sprintf("s%d", i), tree{view: view, title: "t"})
	}
	if s.snapshots.len() != snapshotRingSize {
		t.Fatalf("expected %d snapshots, got %d", snapshotRingSize, s.snapshots.len())
	}
	// The first 5 should be evicted.
	for i := range 5 {
		if _, ok := s.snapshots.get(fmt.Sprintf("s%d", i)); ok {
			t.Fatalf("s%d should have been evicted", i)
		}
	}
	// The rest should be present.
	for i := 5; i < snapshotRingSize+5; i++ {
		if _, ok := s.snapshots.get(fmt.Sprintf("s%d", i)); !ok {
			t.Fatalf("s%d should be present", i)
		}
	}
}

// Re-putting a ver refreshes its recency rather than consuming a second
// eviction slot: a tree put again moves to the young end of the ring
// instead of aging out at its first put's position, and the hole it
// leaves behind doesn't evict an innocent neighbor.
func TestSessionSnapshotRePutRefreshesAge(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	view, _ := lower(0, Tag("div")(Text("x")))
	s.snapshots.put("a", tree{view: view, title: "t"})
	for i := range snapshotRingSize - 2 {
		s.snapshots.put(fmt.Sprintf("s%d", i), tree{view: view, title: "t"})
	}
	// "a" is the oldest entry. Put it again, then add two more distinct
	// vers: the first recycles the hole "a" left, the second evicts the
	// oldest survivor — s0, not "a".
	s.snapshots.put("a", tree{view: view, title: "t"})
	s.snapshots.put("y", tree{view: view, title: "t"})
	s.snapshots.put("z", tree{view: view, title: "t"})
	if _, ok := s.snapshots.get("a"); !ok {
		t.Fatal("re-put ver evicted at its original age")
	}
	if _, ok := s.snapshots.get("s0"); ok {
		t.Fatal("oldest distinct ver should have been evicted")
	}
	if s.snapshots.len() != snapshotRingSize {
		t.Fatalf("expected %d snapshots, got %d", snapshotRingSize, s.snapshots.len())
	}
}

// Frames carry the session's base so the client can drop stale frames.
func TestSessionFrameBase(t *testing.T) {
	s := newTestSession(&counterApp{})
	defer s.cancel()
	s.apply(s.ctx, []int{1}, nil) // base "11111..."
	s.mu.Lock()
	if s.log[1].Base != verInitial {
		t.Fatalf("expected base %q, got %q", verInitial, s.log[1].Base)
	}
	s.base = "ver2"
	s.mu.Unlock()
	s.apply(s.ctx, []int{2}, nil) // base "ver2"
	s.mu.Lock()
	if s.log[2].Base != "ver2" {
		t.Fatalf("expected base %q, got %q", "ver2", s.log[2].Base)
	}
	s.mu.Unlock()
}

// msgInt returns an unmarshal function producing n, ignoring the event.
func msgInt(n int) func(jsontext.Value) (int, error) {
	return func(jsontext.Value) (int, error) { return n, nil }
}

// captureApp renders a button whose handler captures the current value
// of a per-Update counter, and records every non-negative Msg it
// receives. Updates with -1 just bump the counter. body controls
// whether the rendered DOM changes with the counter (fresh trees and
// vers per Update) or stays constant (same tree, refreshed bindings).
type captureApp struct {
	body func(n int) Node
	n    int
	got  []int
}

func (a *captureApp) Update(_ context.Context, m int) Cmd[int] {
	if m >= 0 {
		a.got = append(a.got, m)
	}
	a.n++
	return Batch[int]()
}

func (a *captureApp) View(context.Context) (string, Node) {
	n := a.n
	return "", Tag("button", On("click", msgInt(n)))(a.body(a.n))
}
func (a *captureApp) Subscriptions(context.Context) Sub[int] { return nil }
func (a *captureApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// soleKey returns the only handler key in the table for ver.
func soleKey[Msg any](t *testing.T, s *session[Msg], ver string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	table := s.tables[ver]
	if len(table) != 1 {
		t.Fatalf("expected 1 handler in table for %q, got %d", ver, len(table))
	}
	for k := range table {
		return k
	}
	panic("unreachable")
}

// waitGot settles the bubble and compares the app's recorded Msgs.
// Must run inside a synctest bubble: Wait returns once every goroutine
// is durably blocked, so a dispatched Msg has either landed or provably
// never will — which lets the negative tests assert inaction outright.
func waitGot(t *testing.T, s *session[int], a *captureApp, want []int) {
	t.Helper()
	synctest.Wait()
	s.mu.Lock()
	got := slices.Clone(a.got)
	s.mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Dispatch resolves a handler against the exact tree the client
// displayed: when the tree changes, the same handler key under the old
// version still fires the old render's function, while the new version
// fires the new one. The key itself is identical across renders — the
// element's address — so the versions are doing all the work.
func TestDispatchIsVersionExact(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &captureApp{body: func(n int) Node { return Textf("%d", n) }}
		s := newTestSession[int](app)
		defer s.cancel()

		ver0 := s.ver
		s.apply(s.ctx, []int{-1}, nil) // n: 0 → 1; body changes; fresh ver
		ver1 := s.ver
		if ver1 == ver0 {
			t.Fatal("changed tree should have minted a fresh ver")
		}
		key := soleKey(t, s, ver1)
		if key != soleKey(t, s, ver0) {
			t.Fatal("handler key should be the element's address, stable across renders")
		}

		s.apply(s.ctx, s.resolve(s.ctx, ver0, key, nil), nil)
		waitGot(t, s, app, []int{0})
		s.apply(s.ctx, s.resolve(s.ctx, ver1, key, nil), nil)
		waitGot(t, s, app, []int{0, 1})
	})
}

// When an apply changes captures but not the DOM — a subscription tick
// re-rendering an unchanged tree — no ver is minted and the live
// table's bindings are refreshed in place, so events fire the latest
// functions rather than stale captures.
func TestDispatchRefreshesUnchangedTree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &captureApp{body: func(int) Node { return Text("retry") }}
		s := newTestSession[int](app)
		defer s.cancel()

		ver0 := s.ver
		s.apply(s.ctx, []int{-1}, nil) // n: 0 → 1; body unchanged; same ver
		if s.ver != ver0 {
			t.Fatal("unchanged tree should keep its ver")
		}
		s.apply(s.ctx, s.resolve(s.ctx, ver0, soleKey(t, s, ver0), nil), nil)
		waitGot(t, s, app, []int{1})
	})
}

// An event naming a version the session never produced is dropped.
func TestDispatchUnknownVerDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &captureApp{body: func(int) Node { return Text("x") }}
		s := newTestSession[int](app)
		defer s.cancel()

		s.apply(s.ctx, s.resolve(s.ctx, "nonexistent", soleKey(t, s, s.ver), nil), nil)
		waitGot(t, s, app, nil)
	})
}

// A handler whose function produces some other app's Msg type is a
// coding error: typed panics when the harvest lands, at render time,
// naming the event and both types.
func TestTypedMismatchPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for a mistyped handler")
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, `On("click")`) || !strings.Contains(msg, "int") {
			t.Fatalf("panic should name the event and the wanted Msg type, got %q", msg)
		}
	}()
	typed[int](handlers{"k": {fn: msgFn("oops"), event: "click"}})
}

// An error from the app's unmarshal function skips the event, like a
// failing decoder in Elm.
func TestDispatchUnmarshalErrorSkips(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &captureApp{body: func(int) Node { return Text("x") }}
		s := newTestSession[int](app)
		defer s.cancel()

		s.tables[s.ver] = table[int]{"bad": func(jsontext.Value) (int, error) {
			return 7, fmt.Errorf("no thanks")
		}}
		s.apply(s.ctx, s.resolve(s.ctx, s.ver, "bad", nil), nil)
		waitGot(t, s, app, nil)
	})
}

// pathSetApp renders one handler carrying a non-empty path set.
type pathSetApp struct{}

func (pathSetApp) Update(context.Context, int) Cmd[int] { return Batch[int]() }
func (pathSetApp) View(context.Context) (string, Node) {
	return "", Tag("input", On("input", msgInt(1), []string{"target", "value"}))()
}
func (pathSetApp) Subscriptions(context.Context) Sub[int]                   { return nil }
func (pathSetApp) Preview(context.Context, *url.URL) (string, string, Node) { return "", "", nil }

// The initial render seeds the client's path-set map from the same
// handlers it renders, so a handler's path set ships with the first page
// instead of waiting for a resync. The body attribute and s.pathSets are
// built from one harvest; this guards their ordering in handleRoot.
func TestHandleRootSeedsPathSets(t *testing.T) {
	sv := newServer(
		func(context.Context, *url.URL) (pathSetApp, Cmd[int]) { return pathSetApp{}, Batch[int]() },
		func(*url.URL, bool) int { return 0 },
		func(*url.URL) int { return 0 },
		nil,
	)
	rec := httptest.NewRecorder()
	sv.handleRoot(rec, httptest.NewRequest("GET", "/", nil))
	html := rec.Body.String()

	// The input handler's path-set key must appear in the seed blob, not
	// only in the domi-msg-input attribute that references it.
	psKey := pathSet{{"target", "value"}}.key()

	const marker = `path-sets="`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no %s attribute in render:\n%s", marker, html)
	}
	blob := html[i+len(marker):]
	blob = blob[:strings.IndexByte(blob, '"')]
	if !strings.Contains(blob, psKey) {
		t.Fatalf("path set %q not seeded into body blob %q", psKey, blob)
	}
}

// The initial render mounts the view in a <domi-root> wrapper just
// inside body, with the session markers riding on the wrapper. Body
// itself carries nothing and is never patched, so nodes that browser
// extensions and other scripts add to it stay outside the managed
// tree; display:contents keeps the wrapper itself out of layout.
func TestHandleRootMountsWrapperInsideBody(t *testing.T) {
	sv := newServer(
		func(context.Context, *url.URL) (*counterApp, Cmd[int]) { return &counterApp{}, Batch[int]() },
		func(*url.URL, bool) int { return 0 },
		func(*url.URL) int { return 0 },
		nil,
	)
	rec := httptest.NewRecorder()
	sv.handleRoot(rec, httptest.NewRequest("GET", "/", nil))
	html := rec.Body.String()

	if !strings.Contains(html, "<body><domi-root ") {
		t.Fatalf("the mount is not the sole layer between body and the view:\n%s", html)
	}
	if !regexp.MustCompile(`<domi-root [^>]*style="display:contents"`).MatchString(html) {
		t.Fatalf("mount lacks display:contents:\n%s", html)
	}
	if !regexp.MustCompile(`<domi-root [^>]* prefix="`).MatchString(html) {
		t.Fatalf("session marker not on the mount:\n%s", html)
	}
	if !strings.Contains(html, "<div>0</div></domi-root></body>") {
		t.Fatalf("view not mounted inside the wrapper:\n%s", html)
	}
}

// --- optimistic mutations (DOM-43) ---

// moveMsg is the message a sortApp's change handler reports: relocate Key
// ahead of Before (or to the end when Before is empty).
type moveMsg struct{ Key, Before string }

// sortApp renders a keyed <ul> with a change handler, and reorders its
// model when the handler's move is accepted. reject models a server that
// declines the move, so the reconciling diff has to revert it.
type sortApp struct {
	order  []string
	move   moveMsg
	reject bool
}

func (a *sortApp) Update(_ context.Context, m moveMsg) Cmd[moveMsg] {
	if !a.reject {
		a.order = reorder(a.order, m.Key, m.Before)
	}
	return Batch[moveMsg]()
}

func (a *sortApp) View(context.Context) (string, Node) {
	rows := make([]Node, len(a.order))
	for i, k := range a.order {
		rows[i] = WithKey(k, Tag("li")(Text(k)))
	}
	return "", Tag("ul", On("change", func(jsontext.Value) (moveMsg, error) { return a.move, nil }))(rows...)
}

func (a *sortApp) Subscriptions(context.Context) Sub[moveMsg] { return nil }
func (a *sortApp) Preview(ctx context.Context, u *url.URL) (string, string, Node) {
	t, v := a.View(ctx)
	return u.String(), t, v
}

// reorder returns order with key moved ahead of before, or to the end when
// before is empty or absent.
func reorder(order []string, key, before string) []string {
	out := slices.DeleteFunc(slices.Clone(order), func(k string) bool { return k == key })
	at := len(out)
	if before != "" {
		if j := slices.Index(out, before); j >= 0 {
			at = j
		}
	}
	return slices.Insert(out, at, key)
}

// keyedUL builds the same keyed <ul> a sortApp renders, minus the handler —
// for constructing expected reconstructions.
func keyedUL(keys ...string) Node {
	rows := make([]Node, len(keys))
	for i, k := range keys {
		rows[i] = WithKey(k, Tag("li")(Text(k)))
	}
	return Tag("ul")(rows...)
}

func lastFrame[Msg any](s *session[Msg]) frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.log[s.head%uint64(len(s.log))]
}

func hasApplyPatch(f frame) bool {
	for _, e := range f.Effects {
		if e.Type == effectApplyPatch {
			return true
		}
	}
	return false
}

// topMove is a one-move mutation set within the top-level keyed list:
// relocate key ahead of before (or to the end when before is empty).
func topMove(key, before string) []vdom.ClientMutation {
	at := []vdom.Step{vdom.Index(0), vdom.Key(key)}
	return []vdom.ClientMutation{{Op: "move", From: at, To: at, Before: before}}
}

// optimistic drives an event carrying a client mutation set the way
// handleEvent does — replay the mutations, then apply the resolved
// msgs — then settles the bubble so callers can assert on the
// finished apply. Must run inside a synctest bubble.
func optimistic(s *session[moveMsg], ver, handler string, muts []vdom.ClientMutation) {
	s.applyClientMutations(s.ctx, ver, muts)
	s.apply(s.ctx, s.resolve(s.ctx, ver, handler, nil), nil)
	synctest.Wait()
}

// reconstruct replays a reported move onto the acted-on tree, rebuilding
// what the client is showing — the same tree a fresh render of the moved
// order produces (minus the handler the move carries along).
func TestSessionReconstructReplaysMove(t *testing.T) {
	s := newTestSession[moveMsg](&sortApp{order: []string{"a", "b", "c"}})
	defer s.cancel()

	s.mu.Lock()
	got, err := s.reconstruct(s.ver, topMove("c", "a"))
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_, moved := (&sortApp{order: []string{"c", "a", "b"}}).View(context.Background())
	want, _ := lower(0, moved)
	if !reflect.DeepEqual(got.view, want) {
		t.Fatalf("reconstructed view = %+v,\nwant %+v", got.view, want)
	}
}

// reconstruct fails for a version the session no longer holds, so the
// caller can fall back to a reset rather than trust a bad replay.
func TestSessionReconstructUnknownVersion(t *testing.T) {
	s := newTestSession[moveMsg](&sortApp{order: []string{"a", "b"}})
	defer s.cancel()
	s.mu.Lock()
	_, err := s.reconstruct("gone", topMove("a", ""))
	s.mu.Unlock()
	if err == nil {
		t.Fatal("expected an error for an unretained version")
	}
}

// When the server's render agrees with the optimistic move, the forward
// diff is empty: no DOM patch is sent, so the row never visibly reverts.
// The lineage rebases onto the client's derived version.
func TestDispatchOptimisticAgreementPaintsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}, move: moveMsg{Key: "c", Before: "a"}}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		key := soleKey(t, s, v0)

		optimistic(s, v0, key, topMove("c", "a"))

		derived := v0 + verMutatedSuffix
		s.mu.Lock()
		base, ver, order := s.base, s.ver, slices.Clone(app.order)
		s.mu.Unlock()
		if base != derived || ver != derived {
			t.Fatalf("base/ver = %q/%q, want both %q", base, ver, derived)
		}
		if !slices.Equal(order, []string{"c", "a", "b"}) {
			t.Fatalf("model order = %v, want [c a b]", order)
		}
		if hasApplyPatch(lastFrame(s)) {
			t.Fatal("agreement emitted a DOM patch; the optimistic row should stand untouched")
		}
	})
}

// When the server declines the move, the forward diff is the correction:
// a DOM patch that returns the row to its server-known place.
func TestDispatchOptimisticRejectionReverts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}, move: moveMsg{Key: "c", Before: "a"}, reject: true}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		key := soleKey(t, s, v0)

		optimistic(s, v0, key, topMove("c", "a"))

		s.mu.Lock()
		base, order := s.base, slices.Clone(app.order)
		s.mu.Unlock()
		if base != v0+verMutatedSuffix {
			t.Fatalf("base = %q, want derived", base)
		}
		if !slices.Equal(order, []string{"a", "b", "c"}) {
			t.Fatalf("model order = %v, want unchanged [a b c]", order)
		}
		if !hasApplyPatch(lastFrame(s)) {
			t.Fatal("rejection should emit a corrective DOM patch")
		}
	})
}

// A second optimistic move arriving before the first's catch-up chains: the
// server reconstructs from the tree the first move left, so both stand.
func TestDispatchOptimisticChains(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}, move: moveMsg{Key: "c", Before: "a"}}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		key := soleKey(t, s, v0)

		// Move 1: c before a → [c a b].
		optimistic(s, v0, key, topMove("c", "a"))
		d1 := v0 + verMutatedSuffix

		// Move 2, echoing the first's derived version: b before c → [b c a].
		app.move = moveMsg{Key: "b", Before: "c"}
		optimistic(s, d1, key, topMove("b", "c"))

		s.mu.Lock()
		base, order := s.base, slices.Clone(app.order)
		s.mu.Unlock()
		if want := d1 + verMutatedSuffix; base != want {
			t.Fatalf("base = %q, want twice-derived %q", base, want)
		}
		if !slices.Equal(order, []string{"b", "c", "a"}) {
			t.Fatalf("model order = %v, want [b c a]", order)
		}
	})
}

// An unrelated update can advance the live version between a client's last
// sync and an optimistic action it based on that sync. The acted-on tree is
// still retained among recent renders, so the server reconstructs and diffs
// forward — a minimal correction, not a disruptive reset.
func TestDispatchOptimisticSurvivesRacedUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}, move: moveMsg{Key: "c", Before: "a"}}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		key := soleKey(t, s, v0)

		// An unrelated update advances the live version off v0.
		s.apply(s.ctx, []moveMsg{moveMsg{Key: "a", Before: ""}}, nil)
		if s.ver == v0 {
			t.Fatal("setup: expected the unrelated update to mint a fresh version")
		}

		// The client acted on v0 — still retained — not the live version.
		optimistic(s, v0, key, topMove("c", "a"))

		derived := v0 + verMutatedSuffix
		s.mu.Lock()
		base := s.base
		_, rebased := s.recent.get(derived)
		s.mu.Unlock()
		if base != derived {
			t.Fatalf("base = %q, want derived %q", base, derived)
		}
		if !rebased {
			t.Fatal("expected reconstruct to rebase onto the optimistic tree, not reset")
		}
	})
}

// Only when the acted-on tree is genuinely gone — evicted from the recent
// ring (its handler table outlives it) and never snapshotted — does the
// action run and the client reset onto its derived base to rebuild from the
// authoritative tree.
func TestDispatchOptimisticResetsWhenTreeGone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}, move: moveMsg{Key: "c", Before: "a"}}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		key := soleKey(t, s, v0)

		// An unrelated update advances the live version, then enough renders to
		// evict v0's tree from the recent ring entirely.
		s.apply(s.ctx, []moveMsg{moveMsg{Key: "a", Before: ""}}, nil) // [a b c] → [b c a]
		s.mu.Lock()
		for i := 0; i < recentRingSize; i++ {
			s.recent.put(fmt.Sprintf("filler-%d", i), tree{})
		}
		s.mu.Unlock()

		optimistic(s, v0, key, topMove("c", "a"))

		f := lastFrame(s)
		if f.Base != v0+verMutatedSuffix {
			t.Fatalf("reset frame base = %q, want the client's derived base", f.Base)
		}
		if !hasApplyPatch(f) {
			t.Fatal("reset should rebuild the client's tree with a DOM patch")
		}
	})
}

// A mutation-carrying envelope whose handler never runs — its Msg
// fails to decode — still converges at the event: the replay rebased
// the view, and the render pass emits the correction returning the
// moved row to the model's order, rather than leaving the move
// standing until some unrelated update.
func TestDispatchMutatedUndecodedRerenders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &sortApp{order: []string{"a", "b", "c"}}
		s := newTestSession[moveMsg](app)
		defer s.cancel()
		v0 := s.ver
		s.mu.Lock()
		s.tables[v0] = table[moveMsg]{"bad": func(jsontext.Value) (moveMsg, error) {
			return moveMsg{}, fmt.Errorf("no thanks")
		}}
		s.mu.Unlock()

		body := fmt.Sprintf(
			`{"Type":"Dispatch","Handler":"bad","Ver":%q,"Mutations":[{"Op":"move","From":[0,"c"],"To":[0,"c"],"Before":"a"}]}`, v0)
		rec := httptest.NewRecorder()
		s.handleEvent(rec, httptest.NewRequest("POST", "/x/event", strings.NewReader(body)))
		synctest.Wait()

		s.mu.Lock()
		order := slices.Clone(app.order)
		s.mu.Unlock()
		if !slices.Equal(order, []string{"a", "b", "c"}) {
			t.Fatalf("model order = %v, want untouched [a b c]", order)
		}
		f := lastFrame(s)
		if f.Base != v0+verMutatedSuffix {
			t.Fatalf("frame base = %q, want derived %q", f.Base, v0+verMutatedSuffix)
		}
		if !hasApplyPatch(f) {
			t.Fatal("the render pass should emit the corrective patch at the event")
		}
	})
}

// An event naming several handlers resolves to all their Msgs and
// applies them in one pass: Update runs for each, in key order, and a
// single render emits a single frame.
func TestDispatchCoalescesHandlers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		app := &captureApp{body: func(n int) Node { return Textf("%d", n) }}
		s := newTestSession[int](app)
		defer s.cancel()
		s.tables[s.ver] = table[int]{"k1": msgInt(1), "k2": msgInt(2)}

		body := fmt.Sprintf(`{"Type":"Dispatch","Handler":"k1,k2","Ver":%q}`, s.ver)
		rec := httptest.NewRecorder()
		s.handleEvent(rec, httptest.NewRequest("POST", "/x/event", strings.NewReader(body)))
		synctest.Wait()

		waitGot(t, s, app, []int{1, 2})
		s.mu.Lock()
		head := s.head
		s.mu.Unlock()
		if head != 1 {
			t.Fatalf("frames = %d, want 1: several handlers render once", head)
		}
	})
}

// An envelope that resolves to nothing and carried no mutations does
// nothing: no Update, no render.
func TestDispatchUnresolvedSkipsApply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var views int
		app := &captureApp{body: func(n int) Node { views++; return Textf("%d", n) }}
		s := newTestSession[int](app)
		defer s.cancel()
		before := views

		body := fmt.Sprintf(`{"Type":"Dispatch","Handler":"nope","Ver":%q}`, s.ver)
		rec := httptest.NewRecorder()
		s.handleEvent(rec, httptest.NewRequest("POST", "/x/event", strings.NewReader(body)))
		synctest.Wait()

		waitGot(t, s, app, nil)
		if views != before {
			t.Fatalf("render passes = %d, want none", views-before)
		}
	})
}

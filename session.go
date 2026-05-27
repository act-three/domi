package domi

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ily.dev/domi/internal/vdom"
)

// frame is one entry in a session's patch log: the seq is monotonic per
// session and travels back to the client as the SSE event id, so a
// reconnecting client can ask for everything after the seq it last saw.
type frame struct {
	seq     uint64
	base    string // snapshot id this frame's patches are relative to
	patches []vdom.Patch
}

// Each new SSE request builds a fresh sseAttachment and evicts any
// previous one by cancelling its ctx. At most one consumer ever reads
// from ready, so signals can't race across reconnects.
type sseAttachment struct {
	ctx    context.Context
	cancel context.CancelFunc
	ready  chan struct{}
}

type session[Msg any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	id     string
	app    App[Msg]
	sv     *server[Msg]
	logger *slog.Logger

	mu           sync.Mutex // protects the following fields
	title        string
	view         []vdom.Node
	log          []frame        // fixed-size ring; log[seq%len(log)] holds frame seq
	head         uint64         // seq of the most recent frame; 0 if none
	base         string         // snapshot id the client's DOM is built on; "" initially
	sse          *sseAttachment // nil if no current consumer
	active       time.Time
	subs         map[any]context.CancelFunc // active subscription keys → cancel
	snapshots    map[string]snapshot        // snapshot id → cached vdom
	snapshotAge  []string                   // ring of ids in insertion order, for eviction
	snapshotNext int                        // next write position in snapshotAge
}

const snapshotCacheSize = 30

type snapshot struct {
	view  []vdom.Node
	title string
}

func (s *session[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	appCtx := mergedContext{s.ctx, ctx}
	app, cmd := s.sv.appf(appCtx, req.URL)
	title, view := app.View(appCtx)
	s.app = app
	s.title, s.view = title, lower(view)
	s.updateSubs(app.Subscriptions(appCtx))
	s.spawn(cmd)

	body := Tag("body")(Name("data-domi-session")(s.id))(view)
	root := lowerOne(s.sv.config.document(title, body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, "<!doctype html>"); err != nil {
		s.logger.DebugContext(ctx, "response", "error", err)
		return
	}
	if err := vdom.RenderTo(w, root); err != nil {
		s.logger.DebugContext(ctx, "response", "error", err)
		return
	}
}

// spawn runs each Cmd body in its own goroutine and feeds the
// resulting Msg and effect back into apply.
func (s *session[Msg]) spawn(cmd Cmd[Msg]) {
	for f := range cmd.s {
		go func() {
			msg, effect := f(s)
			s.apply(s.ctx, msg, effect...)
		}()
	}
}

func (s *session[Msg]) apply(ctx context.Context, msg Msg, extra ...vdom.Patch) {
	// s.mu serializes the whole update cycle, including the user's Update
	// and View. If those grow expensive, split state under a second lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := s.app.Update(ctx, msg)
	title, view := s.app.View(ctx)
	next := lower(view)
	ps := vdom.Diff(s.view, next)
	if title != s.title {
		ps = append(ps, vdom.SetTitle(title))
	}
	// Cache the outgoing view before overwriting it.
	for _, p := range extra {
		if id := p.SnapshotID(); id != "" {
			s.cacheSnapshot(id, s.view, s.title)
			break
		}
	}
	ps = append(ps, extra...)
	if len(ps) > 0 {
		s.head++
		s.log[s.head%uint64(len(s.log))] = frame{seq: s.head, base: s.base, patches: ps}
		if s.sse != nil {
			select {
			case s.sse.ready <- struct{}{}:
			default:
			}
		}
	}
	s.view = next
	s.title = title
	s.updateSubs(s.app.Subscriptions(ctx))
	s.spawn(cmd)
}

// updateSubs reconciles the active subscription set against wanted.
// New keys start a goroutine that iterates the event stream;
// absent keys cancel their goroutine via context.
func (s *session[Msg]) updateSubs(wanted Sub[Msg]) {
	next := make(map[any]func(context.Context) iter.Seq[Msg], len(wanted.s))
	for _, e := range wanted.s {
		next[e.key] = e.events
	}
	for key, cancel := range s.subs {
		if _, ok := next[key]; !ok {
			cancel()
			delete(s.subs, key)
		}
	}
	for key, events := range next {
		if _, ok := s.subs[key]; ok {
			continue
		}
		if s.subs == nil {
			s.subs = make(map[any]context.CancelFunc)
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.subs[key] = cancel
		go func() {
			for msg := range events(ctx) {
				if ctx.Err() != nil {
					break
				}
				s.apply(s.ctx, msg)
			}
		}()
	}
}

func (s *session[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var envelope struct {
		H          string         `json:"h,omitempty"`
		E          jsontext.Value `json:"e,omitempty"`
		Type       string         `json:"type,omitempty"`
		URL        string         `json:"url,omitempty"`
		Internal   bool           `json:"internal,omitempty"`
		SnapshotID string         `json:"snapshotId,omitempty"`
	}
	if err := json.UnmarshalRead(req.Body, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.touch()

	switch envelope.Type {
	case "urlRequest":
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad urlRequest URL", "url", envelope.URL, "error", err)
			break
		}
		msg := s.sv.onURLRequest(URLRequest{URL: u, Internal: envelope.Internal})
		go s.apply(mergedContext{s.ctx, ctx}, msg)
		w.WriteHeader(http.StatusNoContent)
		return
	case "urlChange":
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad urlChange URL", "url", envelope.URL, "error", err)
			break
		}
		if envelope.SnapshotID != "" {
			s.restoreSnapshot(envelope.SnapshotID)
		}
		msg := s.sv.onURLChange(u)
		go s.apply(mergedContext{s.ctx, ctx}, msg)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	for h := range strings.SplitSeq(envelope.H, ",") {
		if h == "" {
			continue
		}
		raw, ok := lookupHandler(h)
		if !ok {
			s.logger.WarnContext(ctx, "unknown msg", "key", h)
			continue
		}
		msg, err := unmarshalMsg[Msg](raw, envelope.E)
		if err != nil {
			s.logger.WarnContext(ctx, "msg unmarshal",
				"key", h,
				"error", err,
				"msg", string(raw),
				"event", string(envelope.E),
			)
			continue
		}
		go s.apply(mergedContext{s.ctx, ctx}, msg)
	}
	w.WriteHeader(http.StatusNoContent)
}

// touch defers the idle timeout.
func (s *session[Msg]) touch() {
	s.mu.Lock()
	s.active = time.Now()
	s.mu.Unlock()
}

// idleWatch cancels the session after d of inactivity. Started as a
// goroutine at session creation; exits when s.ctx fires.
func (s *session[Msg]) idleWatch(d time.Duration) {
	for {
		s.mu.Lock()
		wait := d - time.Since(s.active)
		s.mu.Unlock()
		if wait <= 0 {
			s.cancel()
			return
		}
		select {
		case <-time.After(wait):
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *session[Msg]) handleSSE(w http.ResponseWriter, req *http.Request) {
	var seen uint64
	if v := req.Header.Get("Last-Event-ID"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "bad Last-Event-ID", http.StatusBadRequest)
			return
		}
		seen = n
	}

	att := s.attachSSE()
	defer s.detachSSE(att)

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	rc.Flush()

	if resync, view, title, head, base := s.needsResync(seen); resync {
		f := frame{seq: head, base: base, patches: []vdom.Patch{
			vdom.Reset(view),
			vdom.SetTitle(title),
		}}
		if err := writeFrame(w, rc, f); err != nil {
			s.logger.DebugContext(req.Context(), "sse", "error", err)
			return
		}
		seen = head
	}

	for {
		s.touch()
		for _, f := range s.framesSince(seen) {
			if err := writeFrame(w, rc, f); err != nil {
				s.logger.DebugContext(req.Context(), "sse", "error", err)
				return
			}
			seen = f.seq
		}

		select {
		case <-att.ready:
		case <-att.ctx.Done():
			return
		case <-req.Context().Done():
			return
		case <-time.After(s.sv.config.keepalive):
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				s.logger.DebugContext(req.Context(), "sse", "error", err)
				return
			}
			rc.Flush()
		}
	}
}

// attachSSE evicts any prior SSE consumer by cancelling its
// attachment ctx, then returns a fresh attachment for the new caller
// to watch.
func (s *session[Msg]) attachSSE() *sseAttachment {
	attCtx, attCancel := context.WithCancel(s.ctx)
	att := &sseAttachment{
		ctx:    attCtx,
		cancel: attCancel,
		ready:  make(chan struct{}, 1),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.sse
	s.sse = att
	s.active = time.Now()
	if prev != nil {
		prev.cancel()
	}
	return att
}

// detachSSE clears att's slot on s only if it's still the current
// attachment — an eviction may have already replaced it.
func (s *session[Msg]) detachSSE(att *sseAttachment) {
	att.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sse == att {
		s.sse = nil
	}
}

// needsResync captures view/title/head atomically with the resync
// decision so the caller can write the resync frame against a single
// consistent snapshot — without it, head could advance between the
// decision and the write.
func (s *session[Msg]) needsResync(seen uint64) (resync bool, view []vdom.Node, title string, head uint64, base string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	head = s.head
	base = s.base
	oldest := uint64(1)
	if n := uint64(len(s.log)); head >= n {
		oldest = head - n + 1
	}
	if seen+1 < oldest || seen > head {
		return true, s.view, s.title, head, base
	}
	return false, nil, "", head, base
}

func (s *session[Msg]) framesSince(seen uint64) []frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seen >= s.head {
		return nil
	}
	n := uint64(len(s.log))
	out := make([]frame, 0, s.head-seen)
	for q := seen + 1; q <= s.head; q++ {
		out = append(out, s.log[q%n])
	}
	return out
}

func writeFrame(w http.ResponseWriter, rc *http.ResponseController, f frame) error {
	data, err := json.Marshal([]any{f.base, f.patches})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: patch\ndata: %s\n\n", f.seq, data); err != nil {
		return err
	}
	rc.Flush()
	return nil
}

// cacheSnapshot stores a vdom snapshot under id, evicting the oldest
// entry if the cache is at capacity. Must be called with s.mu held.
func (s *session[Msg]) cacheSnapshot(id string, view []vdom.Node, title string) {
	if s.snapshots == nil {
		s.snapshots = make(map[string]snapshot, snapshotCacheSize)
		s.snapshotAge = make([]string, snapshotCacheSize)
	}
	if old := s.snapshotAge[s.snapshotNext]; old != "" {
		delete(s.snapshots, old)
	}
	s.snapshotAge[s.snapshotNext] = id
	s.snapshotNext = (s.snapshotNext + 1) % snapshotCacheSize
	s.snapshots[id] = snapshot{view: view, title: title}
}

// restoreSnapshot replaces s.view and s.title with the cached snapshot
// for id, if present, and sets the session's base to id. The next
// apply cycle diffs against the restored view, producing corrective
// patches for any staleness. Frames committed under an older base
// are dropped by the client.
func (s *session[Msg]) restoreSnapshot(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = id
	if sn, ok := s.snapshots[id]; ok {
		s.view = sn.view
		s.title = sn.title
	}
}

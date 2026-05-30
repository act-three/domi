package domi

import (
	"context"
	"crypto/rand"
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

// baseInitial is the initial base ID for all clients.
const baseInitial = "00000000000000000000000000"

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
	base         string         // snapshot id the client's DOM is built on
	sse          *sseAttachment // nil if no current consumer
	active       time.Time
	subs         map[any]context.CancelFunc // active subscription keys → cancel
	snapshots    map[string]snapshot        // snapshot id → cached vdom
	snapshotAge  []string                   // ring of ids in insertion order, for eviction
	snapshotNext int                        // next write position in snapshotAge
	preview      *preview                   // the outstanding link preview, or nil
}

// Each new SSE request builds a fresh sseAttachment and evicts any
// previous one by cancelling its ctx. At most one consumer ever reads
// from ready, so signals can't race across reconnects.
type sseAttachment struct {
	ctx    context.Context
	cancel context.CancelFunc
	ready  chan struct{}
}

// frame is one entry in a session's patch log: the seq is monotonic per
// session and travels back to the client as the SSE event id, so a
// reconnecting client can ask for everything after the seq it last saw.
type frame struct {
	seq     uint64   // SSE event id
	Base    string   `json:",omitempty"` // required base ID, if set
	Effects []effect `json:",omitempty"`
}

// effect is one side-effect in a frame, run by the client in list order
// against the live document. Type selects which of the remaining fields
// carry its data.
type effect struct {
	Type    effectType
	Patches []vdom.Patch `json:",omitempty"` // ApplyPatch/SetPreview: DOM patches
	Title   string       `json:",omitempty"` // SetTitle/SetPreview: the document title
	URL     string       `json:",omitempty"` // PushURL/ReplaceURL/LoadURL/SetPreview/DeletePreview target
	ID      string       `json:",omitempty"` // PushURL: outgoing snapshot id; SetPreview: base snapshot id
}

type effectType string

const (
	effectApplyPatch    effectType = "ApplyPatch"    // apply DOM patches to the live tree
	effectSetTitle      effectType = "SetTitle"      // set document.title
	effectPushURL       effectType = "PushURL"       // snapshot outgoing page, then history.pushState
	effectReplaceURL    effectType = "ReplaceURL"    // history.replaceState
	effectLoadURL       effectType = "LoadURL"       // full-page navigation, leaving the session
	effectSetPreview    effectType = "SetPreview"    // hold a rebased link preview for instant navigation
	effectDeletePreview effectType = "DeletePreview" // drop the held link preview
)

// nav is the optional navigation side-effect attached to a [cmd]
// return value. apply turns push/outgoingID into a PushURL effect or
// replace into a ReplaceURL effect, ordered ahead of the DOM patches in
// the next frame. A load is handled separately: it replaces the whole
// document, so apply emits a lone LoadURL effect without an Update/View
// cycle.
type nav struct {
	push       string // PushURL target URL, or empty
	replace    string // ReplaceURL target URL, or empty
	load       string // Load target URL (full-page navigation), or empty
	outgoingID string // for push: id to cache outgoing s.view under
}

const snapshotCacheSize = 30

type snapshot struct {
	view  []vdom.Node
	title string
}

// preview is the session's single outstanding link preview.
// We store the preview's view as a value so we can generate a new
// patchset for each new view in the history.
type preview struct {
	url    string
	view   []vdom.Node
	title  string
	frozen bool // DeletePreview sent
	// log maps each emitted SetPreview id to the outgoing view the
	// client would leave by clicking after that frame, kept for its back
	// button. When the client navigates to a preview, the chosen candidate
	// is added to the snapshot cache.
	log map[string]snapshot
}

// addView logs view as the given base snapshot id.
// It returns false if there's no remaining capacity.
func (p *preview) addView(id string, view []vdom.Node, title string) bool {
	const cap = 128
	if len(p.log) >= cap {
		return false
	}
	if p.log == nil {
		p.log = make(map[string]snapshot)
	}
	p.log[id] = snapshot{view: view, title: title}
	return true
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
	root := lowerOne(s.sv.document(title, body))
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
// resulting Msg and optional nav back into apply.
func (s *session[Msg]) spawn(cmd Cmd[Msg]) {
	for f := range cmd.s {
		go func() {
			msg, n := f(s)
			s.apply(s.ctx, msg, n)
		}()
	}
}

func (s *session[Msg]) apply(ctx context.Context, msg Msg, n *nav) {
	// s.mu serializes the whole update cycle, including the user's Update
	// and View. If those grow expensive, split state under a second lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	// A load is a full-page browser navigation: the document is replaced
	// wholesale, so there's no Update to run, view to diff, or
	// subscriptions to reconcile. Emit a lone LoadURL effect and return;
	// the accompanying msg is the zero value and is intentionally dropped.
	if n != nil && n.load != "" {
		// No Base: a LoadURL frame has no DOM patches, so the client runs
		// it regardless of which snapshot its tree is built on.
		s.appendFrame(frame{Effects: []effect{{Type: effectLoadURL, URL: n.load}}})
		return
	}

	cmd := s.app.Update(ctx, msg)
	title, view := s.app.View(ctx)
	next := lower(view)
	patches := vdom.Diff(s.view, next)

	// Effect order is the client's execution order. A PushURL goes first
	// so the client snapshots the outgoing page (its current DOM and
	// title) and updates history before the DOM patches mutate it; the
	// SetTitle goes last so that snapshot still captures the old title.
	var effects []effect
	if n != nil {
		switch {
		case n.push != "":
			s.cacheSnapshot(n.outgoingID, s.view, s.title)
			effects = append(effects, effect{Type: effectPushURL, URL: n.push, ID: n.outgoingID})
		case n.replace != "":
			effects = append(effects, effect{Type: effectReplaceURL, URL: n.replace})
		}
	}
	if len(patches) > 0 {
		effects = append(effects, effect{Type: effectApplyPatch, Patches: patches})
	}
	if title != s.title {
		effects = append(effects, effect{Type: effectSetTitle, Title: title})
	}

	if s.preview != nil && !s.preview.frozen && len(patches) > 0 {
		candID := rand.Text()
		if s.preview.addView(candID, next, title) {
			effects = append(effects, effect{
				Type:    effectSetPreview,
				Patches: vdom.Diff(next, s.preview.view),
				Title:   s.preview.title,
				URL:     s.preview.url,
				ID:      candID,
			})
		} else {
			s.preview.frozen = true
			effects = append(effects, effect{Type: effectDeletePreview, URL: s.preview.url})
		}
	}

	if len(effects) > 0 {
		s.appendFrame(frame{Base: s.base, Effects: effects})
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
				s.apply(s.ctx, msg, nil)
			}
		}()
	}
}

func (s *session[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	type msgType string
	const (
		msgDispatch   msgType = "Dispatch"   // a DOM event Msg
		msgURLRequest msgType = "URLRequest" // a link click
		msgPrefetch   msgType = "Prefetch"   // a link hover
		msgURLChange  msgType = "URLChange"  // a history navigation
	)
	var envelope struct {
		Type       msgType        `json:",omitempty"`
		Handler    string         `json:",omitempty"`
		Event      jsontext.Value `json:",omitempty"`
		URL        string         `json:",omitempty"`
		Internal   bool           `json:",omitempty"`
		SnapshotID string         `json:",omitempty"`
		ToPreview  bool           `json:",omitempty"`
	}
	if err := json.UnmarshalRead(req.Body, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.touch()

	switch envelope.Type {
	case msgDispatch:
		s.dispatch(ctx, envelope.Handler, envelope.Event)
	case msgURLRequest:
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad URLRequest URL", "url", envelope.URL, "error", err)
			break
		}
		msg := s.sv.onURLRequest(URLRequest{URL: u, Internal: envelope.Internal})
		go s.apply(mergedContext{s.ctx, ctx}, msg, nil)
	case msgPrefetch:
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad Prefetch URL", "url", envelope.URL, "error", err)
			break
		}
		go s.prefetch(mergedContext{s.ctx, ctx}, u)
	case msgURLChange:
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad URLChange URL", "url", envelope.URL, "error", err)
			break
		}
		switch {
		case envelope.ToPreview:
			s.commitPreview(ctx, envelope.SnapshotID)
		case envelope.SnapshotID != "":
			s.restoreSnapshot(envelope.SnapshotID)
		}
		msg := s.sv.onURLChange(u)
		go s.apply(mergedContext{s.ctx, ctx}, msg, nil)
	default:
		s.logger.WarnContext(ctx, "unknown client message", "type", string(envelope.Type))
		http.Error(w, "unknown type", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dispatch applies the app Msg for each handler key in a Dispatch
// message. The keys are comma-separated; each names a registered
// handler whose Msg is rebuilt from the event payload and fed to Update.
func (s *session[Msg]) dispatch(ctx context.Context, handler string, event jsontext.Value) {
	for key := range strings.SplitSeq(handler, ",") {
		if key == "" {
			continue
		}
		raw, ok := lookupHandler(key)
		if !ok {
			s.logger.WarnContext(ctx, "unknown msg", "key", key)
			continue
		}
		msg, err := unmarshalMsg[Msg](raw, event)
		if err != nil {
			s.logger.WarnContext(ctx, "msg unmarshal",
				"key", key,
				"error", err,
				"msg", string(raw),
				"event", string(event),
			)
			continue
		}
		go s.apply(mergedContext{s.ctx, ctx}, msg, nil)
	}
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
		f := frame{seq: head, Base: base, Effects: []effect{
			{Type: effectApplyPatch, Patches: []vdom.Patch{vdom.Reset(view)}},
			{Type: effectSetTitle, Title: title},
			{Type: effectDeletePreview},
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
		case <-time.After(s.sv.keepalive):
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
	s.preview = nil
	if sn, ok := s.snapshots[id]; ok {
		s.view = sn.view
		s.title = sn.title
	}
}

// prefetch renders App.Preview for u and, if allowed, makes it the
// outstanding preview. A denial emits DeletePreview, so the click falls
// back to a normal request where onURLRequest can still deny or redirect.
func (s *session[Msg]) prefetch(ctx context.Context, u *url.URL) {
	s.mu.Lock()
	defer s.mu.Unlock()
	href := u.String()
	title, view, ok := s.app.Preview(ctx, u)
	if !ok {
		if s.preview != nil && s.preview.url == href {
			s.preview = nil
		}
		s.appendFrame(frame{Effects: []effect{{Type: effectDeletePreview, URL: href}}})
		return
	}
	next := lower(view)
	id := rand.Text()
	p := &preview{url: href, view: next, title: title}
	p.addView(id, s.view, s.title)
	s.preview = p
	s.appendFrame(frame{Base: s.base, Effects: []effect{{
		Type:    effectSetPreview,
		Patches: vdom.Diff(s.view, next),
		Title:   title,
		URL:     href,
		ID:      id,
	}}})
}

// commitPreview installs the outstanding preview as the current view.
func (s *session[Msg]) commitPreview(ctx context.Context, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		// Bad client. It should not commit an invalid preview.
		s.logger.WarnContext(ctx, "bad preview commit")
		return
	}
	// The committed outgoing view becomes a real navigation snapshot for
	// the back button; the rest of the log is dropped with the preview.
	if cand, ok := s.preview.log[id]; ok {
		s.cacheSnapshot(id, cand.view, cand.title)
	}
	s.base = id
	s.view = s.preview.view
	s.title = s.preview.title
	s.preview = nil
}

// appendFrame commits f to the patch log with the next sequence
// number and signals any waiting SSE consumer. Must be called with
// s.mu held.
func (s *session[Msg]) appendFrame(f frame) {
	s.head++
	f.seq = s.head
	s.log[s.head%uint64(len(s.log))] = f
	if s.sse != nil {
		select {
		case s.sse.ready <- struct{}{}:
		default:
		}
	}
}

func writeFrame(w http.ResponseWriter, rc *http.ResponseController, f frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: effect\ndata: %s\n\n", f.seq, data); err != nil {
		return err
	}
	rc.Flush()
	return nil
}

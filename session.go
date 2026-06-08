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
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ily.dev/domi/internal/vdom"
)

// verInitial is the version id naming every session's initial tree.
// It is also each session's initial base: the first patch lineage is
// rooted in the initial tree.
const verInitial = "11111111111111111111111111"

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
	ver          string                // version id naming the current view; see effect.Ver
	tables       map[string]table[Msg] // tree version → its handler bindings; see dispatch
	pathSets     map[string]pathSet    // items already delivered to the client
	log          []frame               // fixed-size ring; log[seq%len(log)] holds frame seq
	head         uint64                // seq of the most recent frame; 0 if none
	base         string                // tree version the patch lineage builds on
	sse          *sseAttachment        // nil if no current consumer
	active       time.Time
	subs         map[any]context.CancelFunc // active subscription keys → cancel
	snapshots    map[string]snapshot        // snapshot ver → cached vdom
	snapshotAge  []string                   // ring of vers in insertion order, for eviction
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
	Base    string   `json:",omitempty"` // required base ver, if set
	Effects []effect `json:",omitempty"`
}

// effect is one side-effect in a frame, run by the client in list order
// against the live document. Type selects which of the remaining fields
// carry its data.
type effect struct {
	Type     effectType
	Patches  []vdom.Patch       `json:",omitempty"` // ApplyPatch/SetPreview: DOM patches
	Title    string             `json:",omitempty"` // SetTitle/SetPreview: the document title
	URL      string             `json:",omitempty"` // PushURL/ReplaceURL/LoadURL/SetPreview/DeletePreview target
	Ver      string             `json:",omitempty"` // tree version for ApplyPatch, SetPreview
	PathSets map[string]pathSet `json:",omitempty"`
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
	effectAddPathSets   effectType = "AddPathSets"
)

// nav is the optional navigation side-effect attached to a [cmd]
// return value. apply turns push into a PushURL effect or replace
// into a ReplaceURL effect, ordered ahead of the DOM patches in the
// next frame. A load is handled separately: it replaces the whole
// document, so apply emits a lone LoadURL effect without an
// Update/View cycle.
type nav struct {
	push    string // PushURL target URL, or empty
	replace string // ReplaceURL target URL, or empty
	load    string // Load target URL (full-page navigation), or empty
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
	ver    string // version id naming the preview tree
	frozen bool   // DeletePreview sent
	// log holds, keyed by its ver, each outgoing view the client would
	// leave by clicking after a given frame, kept for its back button.
	// When the client navigates to a preview, the chosen candidate is
	// added to the snapshot cache.
	log map[string]snapshot
}

// addView logs the outgoing view named ver as a back-button candidate.
// It returns false if there's no remaining capacity.
func (p *preview) addView(ver string, view []vdom.Node, title string) bool {
	const cap = 128
	if len(p.log) >= cap {
		return false
	}
	if p.log == nil {
		p.log = make(map[string]snapshot)
	}
	p.log[ver] = snapshot{view: view, title: title}
	return true
}

func (s *session[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	appCtx := mergedContext{s.ctx, ctx}
	app, cmd := s.sv.appf(appCtx, req.URL)
	title, view := app.View(appCtx)
	s.app = app
	nodes, h := lower(0, view)
	s.title, s.view = title, nodes
	s.tables[s.ver] = typed[Msg](h)
	s.addPathSets(h)

	// apply lowers the bare view, so handler addresses are rooted at
	// body, the patch root. The initial render must match: embed the
	// already-lowered view in the shell rather than re-addressing it
	// under the document element.
	children := make([]Node, len(nodes))
	for i, n := range nodes {
		children[i] = prelowered{n}
	}
	body := Tag("body")(
		Name("data-domi-session")(s.id),
		Name("data-domi-path-sets")(marshalPathSets(s.pathSets)),
	)(children...)
	// The document shell cannot contain event handlers.
	root, _ := lowerOne(0, s.sv.document(title, body))

	s.updateSubs(app.Subscriptions(appCtx))
	s.spawn(cmd)

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
	for f := range Batch[Msg](cmd).(batch[Msg]) {
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
	next, h := lower(0, view)
	add := s.addPathSets(h)
	patches := vdom.Diff(s.view, next)

	// A change to the tree mints a fresh version id naming it; the
	// outgoing tree keeps its old name in any snapshot taken below.
	// The harvest becomes the current version's table either way,
	// refreshing the live bindings when the tree didn't change.
	oldVer := s.ver
	if len(patches) > 0 {
		s.ver = rand.Text()
	}
	s.tables[s.ver] = typed[Msg](h)

	// Effect order is the client's execution order.
	var effects []effect
	if len(add) > 0 {
		// Goes before ApplyPatches to be ready for new handlers.
		effects = append(effects, effect{Type: effectAddPathSets, PathSets: add})
	}
	if n != nil {
		// Goes before ApplyPatches+SetTitle to snapshot outgoing state.
		switch {
		case n.push != "":
			s.cacheSnapshot(oldVer, s.view, s.title)
			effects = append(effects, effect{Type: effectPushURL, URL: n.push})
		case n.replace != "":
			effects = append(effects, effect{Type: effectReplaceURL, URL: n.replace})
		}
	}
	// ApplyPatches and SetTitle go together.
	if len(patches) > 0 {
		effects = append(effects, effect{Type: effectApplyPatch, Patches: patches, Ver: s.ver})
	}
	if title != s.title {
		effects = append(effects, effect{Type: effectSetTitle, Title: title})
	}

	if s.preview != nil && !s.preview.frozen && len(patches) > 0 {
		if s.preview.addView(s.ver, next, title) {
			effects = append(effects, effect{
				Type:    effectSetPreview,
				Patches: vdom.Diff(next, s.preview.view),
				Title:   s.preview.title,
				URL:     s.preview.url,
				Ver:     s.preview.ver,
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
	all := Subs[Msg](wanted).(subs[Msg])
	next := make(map[any]func(context.Context) iter.Seq[Msg], len(all))
	for _, e := range all {
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
		Type        msgType        `json:",omitempty"`
		Handler     string         `json:",omitempty"`
		Event       jsontext.Value `json:",omitempty"`
		URL         string         `json:",omitempty"`
		Internal    bool           `json:",omitempty"`
		SnapshotVer string         `json:",omitempty"`
		ToPreview   bool           `json:",omitempty"`
		// Ver echoes the version id of the tree the client displayed
		// when a Dispatch event fired. See effect.Ver.
		Ver string `json:",omitempty"`
	}
	if err := json.UnmarshalRead(req.Body, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.touch()

	switch envelope.Type {
	case msgDispatch:
		s.dispatch(ctx, envelope.Ver, envelope.Handler, envelope.Event)
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
			s.commitPreview(ctx, envelope.SnapshotVer)
		case envelope.SnapshotVer != "":
			s.restoreSnapshot(envelope.SnapshotVer)
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

// A table holds a tree version's handler bindings: handler key → the
// unmarshal function producing this session's Msg. It is the typed
// form of a lowering harvest; see typed.
type table[Msg any] map[string]func(jsontext.Value) (Msg, error)

// typed asserts that each handler returns Msg.
// If a function fails the type assertion, typed panics.
func typed[Msg any](h handlers) table[Msg] {
	t := make(table[Msg], len(h))
	for key, hd := range h {
		fn, ok := hd.fn.(func(jsontext.Value) (Msg, error))
		if !ok {
			panic(fmt.Sprintf("domi: On(%q) handler is %T, want %T", hd.event, hd.fn, fn))
		}
		t[key] = fn
	}
	return t
}

// dispatch applies the app Msg for each handler key in a Dispatch
// message. The keys are comma-separated; each names a binding in the
// table of the tree version the client displayed, whose unmarshal
// function builds the Msg fed to Update. An event for an unretained
// version is dropped, and an unmarshal error skips the event, like a
// failing decoder in Elm.
func (s *session[Msg]) dispatch(ctx context.Context, ver, handler string, event jsontext.Value) {
	table, ok := s.table(ver)
	if !ok {
		s.logger.DebugContext(ctx, "unknown tree version", "ver", ver)
		return
	}
	for key := range strings.SplitSeq(handler, ",") {
		if key == "" {
			continue
		}
		unmarshal, ok := table[key]
		if !ok {
			s.logger.WarnContext(ctx, "unknown handler", "ver", ver, "key", key)
			continue
		}
		msg, err := unmarshal(event)
		if err != nil {
			continue
		}
		go s.apply(mergedContext{s.ctx, ctx}, msg, nil)
	}
}

// table returns the handler bindings for the tree version named ver,
// or false if the session never produced (or no longer retains) it.
func (s *session[Msg]) table(ver string) (table[Msg], bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[ver]
	return t, ok
}

// addPathSets adds to s.pathSets
// the items from h that aren't already there,
// and returns the newly-added items.
func (s *session[Msg]) addPathSets(h handlers) map[string]pathSet {
	var add map[string]pathSet
	for _, hd := range h {
		k := hd.ps.key()
		if _, ok := s.pathSets[k]; ok {
			continue
		}
		if s.pathSets == nil {
			s.pathSets = make(map[string]pathSet)
		}
		s.pathSets[k] = hd.ps
		if add == nil {
			add = make(map[string]pathSet)
		}
		add[k] = hd.ps
	}
	return add
}

func marshalPathSets(m map[string]pathSet) string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
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

	if resync, view, title, head, base, ver, ps := s.needsResync(seen); resync {
		efs := []effect{}
		if len(ps) > 0 {
			efs = append(efs, effect{Type: effectAddPathSets, PathSets: ps})
		}
		efs = append(efs, []effect{
			{Type: effectApplyPatch, Patches: []vdom.Patch{vdom.Reset(view)}, Ver: ver},
			{Type: effectSetTitle, Title: title},
			{Type: effectDeletePreview},
		}...)
		f := frame{seq: head, Base: base, Effects: efs}
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
func (s *session[Msg]) needsResync(seen uint64) (resync bool, view []vdom.Node, title string, head uint64, base, ver string, ps map[string]pathSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	head = s.head
	base = s.base
	oldest := uint64(1)
	if n := uint64(len(s.log)); head >= n {
		oldest = head - n + 1
	}
	if seen+1 < oldest || seen > head {
		return true, s.view, s.title, head, base, s.ver, maps.Clone(s.pathSets)
	}
	return false, nil, "", head, base, "", nil
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

// cacheSnapshot stores a vdom snapshot, named by tree version ver,
// evicting the oldest entry if the cache is at capacity. Re-caching a
// ver already present refreshes its recency rather than consuming a
// second eviction slot. Must be called with s.mu held.
func (s *session[Msg]) cacheSnapshot(ver string, view []vdom.Node, title string) {
	if s.snapshots == nil {
		s.snapshots = make(map[string]snapshot, snapshotCacheSize)
		s.snapshotAge = make([]string, snapshotCacheSize)
	}
	if _, ok := s.snapshots[ver]; ok {
		// Clear the old ring slot — a later write recycles the hole —
		// and fall through to re-append at the young end.
		for i, old := range s.snapshotAge {
			if old == ver {
				s.snapshotAge[i] = ""
				break
			}
		}
	}
	if old := s.snapshotAge[s.snapshotNext]; old != "" {
		delete(s.snapshots, old)
	}
	s.snapshotAge[s.snapshotNext] = ver
	s.snapshotNext = (s.snapshotNext + 1) % snapshotCacheSize
	s.snapshots[ver] = snapshot{view: view, title: title}
}

// restoreSnapshot replaces s.view and s.title with the cached tree
// named ver, if present, and roots a new patch lineage there. The next
// apply cycle diffs against the restored view, producing corrective
// patches for any staleness. Frames committed under an older base
// are dropped by the client.
func (s *session[Msg]) restoreSnapshot(ver string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = ver
	s.preview = nil
	if sn, ok := s.snapshots[ver]; ok {
		s.view = sn.view
		s.title = sn.title
		s.ver = ver
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
	next, h := lower(0, view)
	add := s.addPathSets(h)
	p := &preview{url: href, view: next, title: title, ver: rand.Text()}
	s.tables[p.ver] = typed[Msg](h)
	p.addView(s.ver, s.view, s.title)
	s.preview = p
	var effects []effect
	if len(add) > 0 {
		effects = append(effects, effect{Type: effectAddPathSets, PathSets: add})
	}
	effects = append(effects, effect{
		Type:    effectSetPreview,
		Patches: vdom.Diff(s.view, next),
		Title:   title,
		URL:     href,
		Ver:     p.ver,
	})
	s.appendFrame(frame{Base: s.base, Effects: effects})
}

// commitPreview installs the outstanding preview as the current view.
// ver names the outgoing candidate the client is leaving behind.
func (s *session[Msg]) commitPreview(ctx context.Context, ver string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		// Bad client. It should not commit an invalid preview.
		s.logger.WarnContext(ctx, "bad preview commit")
		return
	}
	// The committed outgoing view becomes a real navigation snapshot for
	// the back button; the rest of the log is dropped with the preview.
	if cand, ok := s.preview.log[ver]; ok {
		s.cacheSnapshot(ver, cand.view, cand.title)
	}
	s.base = ver
	s.view = s.preview.view
	s.title = s.preview.title
	s.ver = s.preview.ver
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

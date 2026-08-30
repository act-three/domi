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
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"ily.dev/domi/internal/vdom"
)

// verInitial is the version id naming every instance's initial tree.
// It is also each instance's initial base: the first patch lineage is
// rooted in the initial tree.
const verInitial = "11111111111111111111111111"

// verMutatedSuffix names the tree a client mints when it applies a
// mutation: the version it acted on, plus this suffix. The
// server derives the same name from the version the client echoes, so the
// two agree without it ever travelling on the wire.
const verMutatedSuffix = "-mutated"

type instance[Msg any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	id     string
	app    App[Msg]
	sv     *Server[Msg]
	logger *slog.Logger

	mu        sync.Mutex // protects the following fields
	title     string
	view      []vdom.Node
	ver       string                // version id naming the current view; see effect.Ver
	tables    map[string]table[Msg] // tree version → its handler bindings; see dispatch
	pathSets  map[string]pathSet    // items already delivered to the client
	log       []frame               // fixed-size ring; log[seq%len(log)] holds frame seq
	head      uint64                // seq of the most recent frame; 0 if none
	base      string                // tree version the patch lineage builds on
	sse       *sseAttachment        // nil if no current consumer
	active    time.Time
	subs      map[any]context.CancelFunc // active subscription keys → cancel
	snapshots treeRing                   // back/forward snapshots, keyed by tree version
	recent    treeRing                   // recent renders, for applying client mutations
	preview   *preview                   // the outstanding link preview, or nil
}

// Each new SSE request builds a fresh sseAttachment and evicts any
// previous one by cancelling its ctx. At most one consumer ever reads
// from ready, so signals can't race across reconnects.
type sseAttachment struct {
	ctx    context.Context
	cancel context.CancelFunc
	ready  chan struct{}
}

// frame is one entry in an instance's patch log: the seq is monotonic per
// instance and travels back to the client as the SSE event id, so a
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
	Dest     string             `json:",omitempty"` // SetPreview: the URL this preview navigates to (equals URL unless the app redirected)
	Ver      string             `json:",omitempty"` // tree version for ApplyPatch, SetPreview
	PathSets map[string]pathSet `json:",omitempty"`
}

type effectType string

const (
	effectApplyPatch    effectType = "ApplyPatch"    // apply DOM patches to the live tree
	effectSetTitle      effectType = "SetTitle"      // set document.title
	effectPushURL       effectType = "PushURL"       // snapshot outgoing page, then history.pushState
	effectReplaceURL    effectType = "ReplaceURL"    // history.replaceState
	effectLoadURL       effectType = "LoadURL"       // full-page navigation, leaving the instance
	effectSetPreview    effectType = "SetPreview"    // hold a rebased link preview for instant navigation
	effectDeletePreview effectType = "DeletePreview" // drop the held link preview
	effectAddPathSets   effectType = "AddPathSets"
)

// nav is the navigation side-effect of a cmd.
// apply turns push into a PushURL effect or replace
// into a ReplaceURL effect, ordered ahead of the DOM patches in the
// next frame. A load is handled separately: it replaces the whole
// document, so apply emits a lone LoadURL effect without an
// Update/View cycle.
type nav struct {
	push    *url.URL // PushURL target, or nil
	replace *url.URL // ReplaceURL target, or nil
	load    string   // Load target URL (full-page navigation), or empty
}

const snapshotRingSize = 30

// recentRingSize bounds the recent-render ring: a handful of the latest
// trees a client may still be acting on. It need only cover the brief
// window an unrelated update can open between a client's last sync and a
// client mutation based on that sync (plus a short chain of un-acked
// client mutations); older bases fall back to a reset.
const recentRingSize = 16

// preview is the instance's single outstanding link preview.
// We store the preview's view as a value so we can generate a new
// patchset for each new view in the history.
type preview struct {
	url    string // the requested URL, the key the client matches on
	dest   string // the URL this preview navigates to (equals url unless the app redirects)
	view   []vdom.Node
	title  string
	ver    string // version id naming the preview tree
	frozen bool   // DeletePreview sent
	// log holds, keyed by its ver, each outgoing view the client would
	// leave by clicking after a given frame, kept for its back button.
	// When the client navigates to a preview, the chosen candidate is
	// added to the snapshot history.
	log map[string]tree
	// pathSets is the set delivered to the client when this preview was
	// prefetched. Committing rebases onto the preview (see commitPreview),
	// so this becomes the baseline the next render re-delivers from.
	pathSets map[string]pathSet
}

// addView logs the outgoing view named ver as a back-button candidate.
// It returns false if there's no remaining capacity.
func (p *preview) addView(ver string, sn tree) bool {
	const cap = 128
	if len(p.log) >= cap {
		return false
	}
	if p.log == nil {
		p.log = map[string]tree{}
	}
	p.log[ver] = sn
	return true
}

func (s *instance[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	appCtx := mergedContext{s.ctx, ctx}
	app, cmd := s.sv.appf(appCtx, req.URL)
	title, view := app.View(appCtx)
	s.app = app
	nodes, h := lower(0, view)
	s.title, s.view = title, nodes
	s.tables[s.ver] = typed[Msg](h)
	s.addPathSets(h)
	// Retain the initial render so a client's first client-initiated mutation
	// can be reconstructed against even if an unrelated update has since
	// advanced the live version.
	s.recent.put(s.ver, tree{view: nodes, title: title, pathSets: maps.Clone(s.pathSets)})

	// apply lowers the bare view, so handler addresses are rooted at
	// domi-root. The initial render must match: embed the
	// already-lowered view in the shell rather than re-addressing it
	// under the document element.
	children := make([]Node, len(nodes))
	for i, n := range nodes {
		children[i] = prelowered{n}
	}
	body := Tag("body")(
		element{ // Can't use Tag here because domi-root is reserved.
			tag: "domi-root",
			attrs: []Attr{
				Name("style", "display:contents"),
				Name("prefix", path.Join("/", s.sv.prefix, s.id)),
				Name("path-sets", marshalPathSets(s.pathSets)),
			},
			children: children,
		},
	)
	// The document shell cannot contain event handlers.
	root, _ := lowerOne(0, s.sv.document(s.sv.clientPath, title, body))

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

// spawn runs each command in its own goroutine and feeds the
// resulting Msg and optional nav back into apply.
func (s *instance[Msg]) spawn(cmd Cmd[Msg]) {
	for _, c := range Batch[Msg](cmd).(batch[Msg]) {
		go func() {
			var m []Msg
			switch {
			case c.f != nil:
				m = []Msg{c.f()}
			case c.nav.push != nil:
				m = []Msg{s.sv.onURLChange(c.nav.push.Clone())}
			case c.nav.replace != nil:
				m = []Msg{s.sv.onURLChange(c.nav.replace.Clone())}
			}
			s.apply(s.ctx, m, c.nav)
		}()
	}
}

// apply calls Update for each msg in order,
// then renders once and emits a frame for the view's diff and any effects.
// Applying no msgs is just a render pass.
func (s *instance[Msg]) apply(ctx context.Context, msgs []Msg, n *nav) {
	// s.mu serializes the whole update cycle, including the user's Update
	// and View. If those grow expensive, split state under a second lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	// A load is a full-page browser navigation: the document is replaced
	// wholesale, so there's no Update to run, view to diff, or
	// subscriptions to reconcile. Emit a lone LoadURL effect and return;
	// any accompanying msgs are intentionally dropped.
	if n != nil && n.load != "" {
		// No Base: a LoadURL frame has no DOM patches, so the client runs
		// it regardless of which snapshot its tree is built on.
		s.appendFrame(frame{Effects: []effect{{Type: effectLoadURL, URL: n.load}}})
		return
	}

	cmds := make([]Cmd[Msg], len(msgs))
	for i, msg := range msgs {
		cmds[i] = s.app.Update(ctx, msg)
	}
	cmd := Batch(cmds...)
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
		case n.push != nil:
			s.snapshots.put(oldVer, tree{view: s.view, title: s.title, pathSets: maps.Clone(s.pathSets)})
			effects = append(effects, effect{Type: effectPushURL, URL: n.push.String()})
		case n.replace != nil:
			effects = append(effects, effect{Type: effectReplaceURL, URL: n.replace.String()})
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
		if s.preview.addView(s.ver, tree{view: next, title: title, pathSets: maps.Clone(s.pathSets)}) {
			effects = append(effects, effect{
				Type:    effectSetPreview,
				Patches: vdom.Diff(next, s.preview.view),
				Title:   s.preview.title,
				URL:     s.preview.url,
				Dest:    s.preview.dest,
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
	// Retain this render so a client acting on it can be reconstructed
	// against even after an unrelated update advances the live version.
	s.recent.put(s.ver, tree{view: next, title: title, pathSets: maps.Clone(s.pathSets)})
	s.updateSubs(s.app.Subscriptions(ctx))
	s.spawn(cmd)
}

// updateSubs reconciles the active subscription set against wanted.
// New keys start a goroutine that iterates the event stream;
// absent keys cancel their goroutine via context.
func (s *instance[Msg]) updateSubs(wanted Sub[Msg]) {
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
				s.apply(s.ctx, []Msg{msg}, nil)
			}
		}()
	}
}

func (s *instance[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
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
		// Mutations carries optional client-initiated DOM changes.
		Mutations []vdom.ClientMutation `json:",omitempty"`
	}
	if err := json.UnmarshalRead(req.Body, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.touch()

	switch envelope.Type {
	case msgDispatch:
		// The client can optionally apply DOM mutations before an event.
		// We must do the same here, to stay in sync with its state.
		mutated := len(envelope.Mutations) > 0
		if mutated {
			s.applyClientMutations(ctx, envelope.Ver, envelope.Mutations)
		}
		msgs := s.resolve(ctx, envelope.Ver, envelope.Handler, envelope.Event)
		if len(msgs) > 0 || mutated {
			// If msgs is empty, we still apply it if there are
			// client mutations, to send a corrective diff.
			go s.apply(mergedContext{s.ctx, ctx}, msgs, nil)
		}
	case msgURLRequest:
		u, err := url.Parse(envelope.URL)
		if err != nil {
			s.logger.WarnContext(ctx, "bad URLRequest URL", "url", envelope.URL, "error", err)
			break
		}
		msg := s.sv.onURLRequest(u, envelope.Internal)
		go s.apply(mergedContext{s.ctx, ctx}, []Msg{msg}, nil)
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
		go s.apply(mergedContext{s.ctx, ctx}, []Msg{msg}, nil)
	default:
		s.logger.WarnContext(ctx, "unknown client message", "type", string(envelope.Type))
		http.Error(w, "unknown type", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// A table holds a tree version's handler bindings: handler key → the
// unmarshal function producing this instance's Msg. It is the typed
// form of a lowering harvest; see typed.
type table[Msg any] map[string]func(jsontext.Value) (Msg, error)

// typed adapts each handler to return Msg, the instance's type.
// See adapt.
func typed[Msg any](h handlers) table[Msg] {
	t := make(table[Msg], len(h))
	for key, hd := range h {
		t[key] = adapt[Msg](hd)
	}
	return t
}

// resolve decodes a Dispatch message's comma-separated handler keys
// into the Msgs they carry, in key order.
// Each key specifies a binding in the table of the client's tree version.
// The binding's unmarshal function builds the Msg.
// An missing version resolves to nothing.
// An unknown key or an unmarshal error (like a failing decoder in Elm)
// skips that handler.
func (s *instance[Msg]) resolve(ctx context.Context, ver, handler string, event jsontext.Value) []Msg {
	table, ok := s.table(ver)
	if !ok {
		s.logger.WarnContext(ctx, "unknown tree version", "ver", ver)
		return nil
	}
	var msgs []Msg
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
		msgs = append(msgs, msg)
	}
	return msgs
}

// applyClientMutations brings the server's tree into line with what the
// client is showing after it mutates the DOM, so the dispatch that
// follows diffs from there to the authoritative render. The client
// applied muts to its DOM and rebased onto a derived version; the server
// reconstructs that same tree and rebases its lineage onto it. When the
// following render agrees the diff is empty and the client's
// mutation stands with no repaint; when it doesn't, the diff is the
// correction. If the acted-on tree can't be reconstructed — it named a
// version the server no longer retains, or a mutation didn't fit — the
// client is reset onto its derived base to rebuild from the authoritative
// tree instead.
func (s *instance[Msg]) applyClientMutations(ctx context.Context, ver string, muts []vdom.ClientMutation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	derived := ver + verMutatedSuffix
	client, err := s.reconstruct(ver, muts)
	if err != nil {
		// Can't reconstruct what the client shows: re-root the lineage at the
		// derived base and rebuild the client's tree from the authoritative one.
		s.logger.WarnContext(ctx, "client state reconstruct failed", "ver", ver, "error", err)
		s.base = derived
		s.appendFrame(frame{Base: derived, Effects: resetEffects(s.view, s.title, s.ver, maps.Clone(s.pathSets))})
		return
	}
	s.base = derived
	s.ver = derived
	s.view = client.view
	s.title = client.title
	s.pathSets = client.pathSets
	// The mutation relocates nodes, not handlers, and the client's DOM
	// still carries the acted-on render's bindings (the move didn't
	// re-render them). Carry that table onto the derived version so a
	// chained mutation echoing it still resolves its handler.
	if tbl, ok := s.tables[ver]; ok {
		s.tables[derived] = tbl
	}
	// Retain this tree among recent renders so a chained mutation
	// echoing this derived version can use it as a base.
	s.recent.put(derived, tree{view: client.view, title: client.title, pathSets: maps.Clone(client.pathSets)})
}

// reconstruct replays muts onto the tree the client acted on — the live
// view when ver still names it, else a cached snapshot — to rebuild what
// the client is now showing. The returned snapshot carries the acted-on
// version's path sets, so rebasing onto it re-delivers any set the client
// missed (see restoreSnapshot). It errors if the acted-on tree is gone or
// a mutation doesn't fit, signalling the caller to reset rather than trust
// a bad reconstruction. Must be called with s.mu held.
func (s *instance[Msg]) reconstruct(ver string, muts []vdom.ClientMutation) (tree, error) {
	base, ok := s.actedOn(ver)
	if !ok {
		return tree{}, fmt.Errorf("no retained tree for version %q", ver)
	}
	v, err := vdom.Apply(base.view, muts)
	if err != nil {
		return tree{}, err
	}
	return tree{view: v, title: base.title, pathSets: maps.Clone(base.pathSets)}, nil
}

// actedOn returns the tree named ver: the live view when ver is current,
// else a recent render (which another update may have moved off of).
// else a back/forward snapshot. Must be called with s.mu held.
func (s *instance[Msg]) actedOn(ver string) (tree, bool) {
	if ver == s.ver {
		return tree{view: s.view, title: s.title, pathSets: s.pathSets}, true
	}
	if sn, ok := s.recent.get(ver); ok {
		return sn, true
	}
	return s.snapshots.get(ver)
}

// resetEffects rebuilds the client's tree from scratch and clears transient
// client state: re-deliver the path sets so the rebuilt handlers resolve,
// Reset the view named ver, set the title, and drop any held preview (stale
// against the new tree). ps is taken as the caller's to keep.
func resetEffects(view []vdom.Node, title, ver string, ps map[string]pathSet) []effect {
	efs := []effect{}
	if len(ps) > 0 {
		efs = append(efs, effect{Type: effectAddPathSets, PathSets: ps})
	}
	return append(efs,
		effect{Type: effectApplyPatch, Patches: []vdom.Patch{vdom.Reset(view)}, Ver: ver},
		effect{Type: effectSetTitle, Title: title},
		effect{Type: effectDeletePreview},
	)
}

// table returns the handler bindings for the tree version named ver,
// or false if the instance never produced (or no longer retains) it.
func (s *instance[Msg]) table(ver string) (table[Msg], bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tables[ver]
	return t, ok
}

// addPathSets adds to s.pathSets
// the items from h that aren't already there,
// and returns the newly-added items.
func (s *instance[Msg]) addPathSets(h handlers) map[string]pathSet {
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
func (s *instance[Msg]) touch() {
	s.mu.Lock()
	s.active = time.Now()
	s.mu.Unlock()
}

// idleWatch cancels the instance after d of inactivity. Started as a
// goroutine at instance creation; exits when s.ctx fires.
func (s *instance[Msg]) idleWatch(d time.Duration) {
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

func (s *instance[Msg]) handleSSE(w http.ResponseWriter, req *http.Request) {
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
		f := frame{seq: head, Base: base, Effects: resetEffects(view, title, ver, ps)}
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
func (s *instance[Msg]) attachSSE() *sseAttachment {
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
func (s *instance[Msg]) detachSSE(att *sseAttachment) {
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
func (s *instance[Msg]) needsResync(seen uint64) (resync bool, view []vdom.Node, title string, head uint64, base, ver string, ps map[string]pathSet) {
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

func (s *instance[Msg]) framesSince(seen uint64) []frame {
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

// restoreSnapshot replaces s.view, s.title, and s.pathSets with the
// stored state named ver, if present, and roots a new patch lineage
// there. The next apply cycle diffs against the restored view, producing
// corrective patches for any staleness; restoring the snapshot's path
// sets likewise lets that render re-deliver any path set that reached the
// client only in a frame dropped at the rebase. Frames committed under an
// older base are dropped by the client.
func (s *instance[Msg]) restoreSnapshot(ver string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = ver
	s.preview = nil
	if sn, ok := s.snapshots.get(ver); ok {
		s.view = sn.view
		s.title = sn.title
		s.ver = ver
		s.pathSets = maps.Clone(sn.pathSets)
	}
}

// prefetch renders App.Preview for u and, if allowed, makes it the
// outstanding preview. A denial emits DeletePreview, so the click falls
// back to a normal request where onURLRequest can still deny or redirect.
// The app may redirect the navigation
// by returning a dest that differs from u.
// The preview then lands on dest.
// An empty dest denies the preview.
// A non-empty dest must be relative, like a PushURL target; a malformed
// one is an app bug and panics.
func (s *instance[Msg]) prefetch(ctx context.Context, u *url.URL) {
	s.mu.Lock()
	defer s.mu.Unlock()
	href := u.String()
	dest, title, view := s.app.Preview(ctx, u)
	if dest == "" {
		if s.preview != nil && s.preview.url == href {
			s.preview = nil
		}
		s.appendFrame(frame{Effects: []effect{{Type: effectDeletePreview, URL: href}}})
		return
	}
	du := mustParseRelativeURL("domi.App.Preview", dest)
	next, h := lower(0, view)
	add := s.addPathSets(h)
	p := &preview{url: href, dest: du.String(), view: next, title: title, ver: rand.Text(), pathSets: maps.Clone(s.pathSets)}
	s.tables[p.ver] = typed[Msg](h)
	p.addView(s.ver, tree{view: s.view, title: s.title, pathSets: maps.Clone(s.pathSets)})
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
		Dest:    p.dest,
		Ver:     p.ver,
	})
	s.appendFrame(frame{Base: s.base, Effects: effects})
}

// commitPreview installs the outstanding preview as the current view.
// ver names the outgoing candidate the client is leaving behind, stored
// for its back button. Path-set delivery resets to the preview's baseline
// — the sets known when it was prefetched, which the client had received
// before it could commit — so any set stranded in a frame dropped at the
// rebase is re-delivered by the next render.
func (s *instance[Msg]) commitPreview(ctx context.Context, ver string) {
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
		s.snapshots.put(ver, cand)
	}
	s.base = ver
	s.view = s.preview.view
	s.title = s.preview.title
	s.ver = s.preview.ver
	s.pathSets = maps.Clone(s.preview.pathSets)
	s.preview = nil
}

// appendFrame commits f to the patch log with the next sequence
// number and signals any waiting SSE consumer. Must be called with
// s.mu held.
func (s *instance[Msg]) appendFrame(f frame) {
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

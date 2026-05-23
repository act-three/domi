package domi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ily.dev/domi/internal/vdom"
)

//go:embed client.js
var clientJS []byte

var clientJSPath = func() string {
	h := sha256.Sum256(clientJS)
	return fmt.Sprintf("/domi.%x.js", h[:4])
}()

// Handler serves an [App].
// At the start of a session,
// the returned Handler calls f
// to obtain a fresh App instance plus an initial [Cmd].
// This instance is associated with the session,
// so each browser gets its own independent state.
func Handler[Msg any, A App[Msg]](f func() (A, Cmd[Msg])) http.Handler {
	store := newSessionStore[Msg]()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot(f, store))
	mux.HandleFunc("GET /sse/{id}", handleSSE(store))
	mux.HandleFunc("POST /event/{id}", handleEvent(store))
	mux.HandleFunc("GET "+clientJSPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		http.ServeContent(w, req, "domi.js", time.Time{}, bytes.NewReader(clientJS))
	})
	return mux
}

// ---- session bookkeeping ----

// sessionState holds the per-session goroutine plumbing. ctx is the
// authoritative "is this session alive" signal — cancelling it tears
// down the session loop, unblocks any in-flight Cmd sends, and triggers
// the auto-removal watcher installed in handleRoot.
type sessionState[Msg any] struct {
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan Msg
	patchRx chan []vdom.Patch
	mu      sync.Mutex
	taken   bool // true after an SSE consumer has been attached
}

type sessionStore[Msg any] struct {
	mu sync.Mutex
	m  map[string]*sessionState[Msg]
}

func newSessionStore[Msg any]() *sessionStore[Msg] {
	return &sessionStore[Msg]{m: make(map[string]*sessionState[Msg])}
}

func (s *sessionStore[Msg]) put(id string, st *sessionState[Msg]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = st
}

func (s *sessionStore[Msg]) get(id string) (*sessionState[Msg], bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[id]
	return st, ok
}

func (s *sessionStore[Msg]) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

func newSessionID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// ---- session loop ----

// sessionLoop drains msgChan, applies Update/View/diff, and ships patches
// to patchTx. Exits when ctx is cancelled; in-flight patches that can't
// be delivered are dropped (the SSE consumer is also going away).
//
// Title changes ride along in the same patch list as a [vdom.SetTitle]
// op, prepended when the App's reported title differs from the previous
// frame's.
func sessionLoop[Msg any](
	ctx context.Context,
	app App[Msg],
	prevTitle string,
	prev []vdom.Node,
	msgChan chan Msg,
	patchTx chan<- []vdom.Patch,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			cmd := app.Update(msg)
			title, view := app.View()
			next := lower(view)
			patches := vdom.Diff(prev, next)
			if title != prevTitle {
				patches = append([]vdom.Patch{vdom.SetTitle(title)}, patches...)
			}
			if len(patches) > 0 {
				select {
				case patchTx <- patches:
				case <-ctx.Done():
					return
				}
			}
			prev = next
			prevTitle = title
			spawnCmd(ctx, cmd, msgChan)
		}
	}
}

// spawnCmd runs each Cmd in its own goroutine. The ctx is passed to the
// Cmd's body (which should respect it) and also guards the send back to
// msgChan, so a cancelled session doesn't leak goroutines waiting on a
// reader that will never come.
func spawnCmd[Msg any](ctx context.Context, cmd Cmd[Msg], msgTx chan<- Msg) {
	for fn := range cmd.s {
		go func(f func(context.Context) Msg) {
			msg := f(ctx)
			select {
			case msgTx <- msg:
			case <-ctx.Done():
			}
		}(fn)
	}
}

// ---- HTTP handlers ----

func handleRoot[Msg any, A App[Msg]](f func() (A, Cmd[Msg]), store *sessionStore[Msg]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := newSessionID()
		app, cmd := f()
		title, view := app.View()
		initial := lower(view)

		ctx, cancel := context.WithCancel(context.Background())
		st := &sessionState[Msg]{
			ctx:     ctx,
			cancel:  cancel,
			msgChan: make(chan Msg, 64),
			patchRx: make(chan []vdom.Patch, 64),
		}
		store.put(id, st)
		// Auto-remove from the store when the session is torn down
		// (SSE drop, explicit cancel, eventual timeout). Centralizing
		// the deletion here means handlers only need to call cancel().
		go func() {
			<-ctx.Done()
			store.delete(id)
		}()

		go sessionLoop(ctx, app, title, initial, st.msgChan, st.patchRx)
		spawnCmd(ctx, cmd, st.msgChan)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html>")
		_, _ = io.WriteString(w, vdom.Render(document(title, id, view)))
	}
}

// eventEnvelope is the JSON body the client posts on every dispatch.
// H is a comma-separated list of handler hashes (one per On() bound to
// the firing element); E is the kitchen-sink event payload that gets
// spliced into each Msg's `domi:"event"` field, if any.
type eventEnvelope struct {
	H string         `json:"h"`
	E jsontext.Value `json:"e"`
}

func handleEvent[Msg any](store *sessionStore[Msg]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st, ok := store.get(id)
		if !ok {
			http.Error(w, "session", http.StatusNotFound)
			return
		}
		var env eventEnvelope
		if err := json.UnmarshalRead(r.Body, &env); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, h := range strings.Split(env.H, ",") {
			if h == "" {
				continue
			}
			raw, ok := lookupHandler(h)
			if !ok {
				// Unknown hash — handler not registered (stale render after
				// restart, or a forged value). Skip rather than fail the batch.
				continue
			}
			msg, err := spliceEvent[Msg](raw, env.E)
			if err != nil {
				continue
			}
			select {
			case st.msgChan <- msg:
			case <-st.ctx.Done():
				http.Error(w, "session gone", http.StatusGone)
				return
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSSE[Msg any](store *sessionStore[Msg]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		st, ok := store.get(id)
		if !ok {
			http.Error(w, "session", http.StatusNotFound)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		st.mu.Lock()
		if st.taken {
			st.mu.Unlock()
			http.Error(w, "sse already attached", http.StatusConflict)
			return
		}
		st.taken = true
		st.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher.Flush()

		// SSE disconnect ends the session (reconnection is a separate
		// roadmap item). Cancel triggers the watcher in handleRoot to
		// remove the session from the store.
		defer st.cancel()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case patches, ok := <-st.patchRx:
				if !ok {
					return
				}
				data, err := json.Marshal(patches)
				if err != nil {
					return
				}
				if _, err := fmt.Fprintf(w, "event: patch\ndata: %s\n\n", data); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// document returns the HTML shell that wraps body. The inline module
// script mirrors what a bundled app would do.
//
//	`import * as Domi from <hashed path>; Domi.run()`
func document(title, sessionID string, body Node) vdom.Node {
	return vdom.Element(Tag("html")()(
		Tag("head")()(
			Tag("meta")(Name("charset", "utf-8")),
			Tag("title")()(Text(title)),
			Tag("script")(Name("type", "module"))(
				Text(fmt.Sprintf(`import * as Domi from %q; Domi.run();`, clientJSPath)),
			),
		),
		Tag("body")(Name("data-domi-session", sessionID))(body),
	).(element))
}

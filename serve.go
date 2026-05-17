package domi

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

//go:embed static/domi.js
var staticFS embed.FS

// Handler returns an http.Handler that serves the domi App. The factory
// `newApp` is called once per session to produce a fresh app instance with
// its own state. The caller is responsible for listening (e.g. via
// http.ListenAndServe).
func Handler[Msg any](newApp func() App[Msg]) http.Handler {
	store := newSessionStore[Msg]()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot(newApp, store))
	mux.HandleFunc("GET /sse/{id}", handleSSE(store))
	mux.HandleFunc("POST /event/{id}", handleEvent(store))
	mux.HandleFunc("GET /domi.js", func(w http.ResponseWriter, _ *http.Request) {
		b, err := staticFS.ReadFile("static/domi.js")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(b)
	})
	return mux
}

// ---- session bookkeeping ----

type sessionState[Msg any] struct {
	msgChan chan Msg
	patchRx chan []patch
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

func sessionLoop[Msg any](
	app App[Msg],
	prev Node,
	msgChan chan Msg,
	patchTx chan<- []patch,
) {
	for msg := range msgChan {
		cmd := app.Update(msg)
		next := app.View()
		if patches := diff(prev, next); len(patches) > 0 {
			patchTx <- patches
		}
		prev = next
		spawnCmd(cmd, msgChan)
	}
}

func spawnCmd[Msg any](cmd Cmd[Msg], msgTx chan<- Msg) {
	for _, fn := range cmd.fns {
		go func(f func(context.Context) Msg) {
			ctx := context.Background()
			msgTx <- f(ctx)
		}(fn)
	}
}

// ---- HTTP handlers ----

func handleRoot[Msg any](newApp func() App[Msg], store *sessionStore[Msg]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := newSessionID()
		app := newApp()
		cmd := app.Init()
		initial := app.View()
		title := app.Title()
		body := render(initial)

		st := &sessionState[Msg]{
			msgChan: make(chan Msg, 64),
			patchRx: make(chan []patch, 64),
		}
		store.put(id, st)

		go sessionLoop(app, initial, st.msgChan, st.patchRx)
		spawnCmd(cmd, st.msgChan)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page(title, body, id))
	}
}

// eventEnvelope is the JSON body the client posts on every dispatch.
// H is a comma-separated list of handler hashes (one per On() bound to
// the firing element); E is the kitchen-sink event payload that gets
// spliced into each Msg's `domi:"event"` field, if any.
type eventEnvelope struct {
	H string          `json:"h"`
	E json.RawMessage `json:"e"`
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
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
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
			case <-r.Context().Done():
				http.Error(w, "client gone", http.StatusGone)
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

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				store.delete(id)
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

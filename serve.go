package domi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
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
func Handler[Msg any, A App[Msg]](f func() (A, Cmd[Msg]), o ...Option) http.Handler {
	sv := newServer(f, o)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", sv.handleRoot)
	mux.HandleFunc("GET /sse/{id}", sv.handleSSE)
	mux.HandleFunc("POST /event/{id}", sv.handleEvent)
	mux.HandleFunc("GET "+clientJSPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		http.ServeContent(w, req, "domi.js", time.Time{}, bytes.NewReader(clientJS))
	})
	return mux
}

type server[Msg any] struct {
	config handlerConfig
	appf   func() (App[Msg], Cmd[Msg])

	mu sync.Mutex
	m  map[string]*session[Msg]
}

func newServer[Msg any, A App[Msg]](f func() (A, Cmd[Msg]), opts []Option) *server[Msg] {
	sv := &server[Msg]{
		appf: func() (App[Msg], Cmd[Msg]) { return f() },
		m:    make(map[string]*session[Msg]),
		config: handlerConfig{
			document:       defaultDocument,
			logger:         slog.Default(),
			sessionTimeout: 48 * time.Hour,
			replayWindow:   128,
			keepalive:      25 * time.Second,
		},
	}
	for _, o := range opts {
		switch o := o.(type) {
		case documentOption:
			sv.config.document = o.f
		case sessionTimeoutOption:
			sv.config.sessionTimeout = o.d
		case replayWindowOption:
			sv.config.replayWindow = o.n
		case keepaliveOption:
			sv.config.keepalive = o.d
		case loggerOption:
			sv.config.logger = o.l
		}
	}
	return sv
}

func (sv *server[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	id := rand.Text()
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, sessionIDKey{}, id)
	s := &session[Msg]{
		ctx:    ctx,
		cancel: cancel,
		id:     id,
		sv:     sv,
		logger: sv.config.logger.With("session", id),
		log:    make([]frame, sv.config.replayWindow),
		active: time.Now(),
	}
	sv.put(id, s)
	go s.idleWatch(sv.config.sessionTimeout)
	go func() {
		<-ctx.Done()
		sv.delete(id)
	}()
	s.handleRoot(w, req)
}

func (sv *server[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
	s, ok := sv.get(req.PathValue("id"))
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	s.handleEvent(w, req)
}

func (sv *server[Msg]) handleSSE(w http.ResponseWriter, req *http.Request) {
	s, ok := sv.get(req.PathValue("id"))
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	s.handleSSE(w, req)
}

func (sv *server[Msg]) put(id string, s *session[Msg]) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.m[id] = s
}

func (sv *server[Msg]) get(id string) (*session[Msg], bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	s, ok := sv.m[id]
	return s, ok
}

func (sv *server[Msg]) delete(id string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	delete(sv.m, id)
}

func defaultDocument(title string, body Node) Node {
	return Tag("html")()(
		Tag("head")()(
			Tag("meta")(Name("charset")("utf-8")),
			Tag("title")()(Text(title)),
			Tag("script")(Name("type")("module"))(
				Text(fmt.Sprintf(`import * as Domi from %q; Domi.run();`, clientJSPath)),
			),
		),
		body,
	)
}

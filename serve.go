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
	"net/url"
	"path"
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
//
// At the start of a session,
// the returned Handler calls f,
// providing the initial request URL,
// to obtain a fresh App instance plus an initial [Cmd].
// The context carries the session ID (see [SessionID])
// and is cancelled when the session ends.
// This instance is associated with the session,
// so each browser gets its own independent state.
//
// When the user clicks a link,
// the framework intercepts the navigation
// and calls onURLRequest to produce a Msg.
// The app's Update decides how to handle the request,
// typically by returning a [PushURL] or [ReplaceURL] command.
//
// When the URL changes
// (from a navigation command or browser back/forward),
// the framework calls onURLChange to produce a Msg.
// The app's Update typically translates the URL into a route
// and updates its state accordingly.
func Handler[Msg any, A App[Msg]](
	f func(context.Context, *url.URL) (A, Cmd[Msg]),
	onURLRequest func(URLRequest) Msg,
	onURLChange func(*url.URL) Msg,
	o ...Option,
) http.Handler {
	sv := newServer(f, onURLRequest, onURLChange, o)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+path.Join("/", sv.prefix, "{id}/events"), sv.handleSSE)
	mux.HandleFunc("POST "+path.Join("/", sv.prefix, "{id}/event"), sv.handleEvent)
	mux.HandleFunc("GET "+sv.clientPath, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=31536000, immutable")
		http.ServeContent(w, req, "domi.js", time.Time{}, bytes.NewReader(clientJS))
	})
	mux.HandleFunc("GET /", sv.handleRoot)
	return mux
}

type server[Msg any] struct {
	// Config. Never changed after init. Safe to read concurrently.
	document       func(clientPath, title string, body Node) Node
	logger         *slog.Logger
	sessionTimeout time.Duration
	replayWindow   int
	keepalive      time.Duration
	prefix         string // namespace for internal URLs, e.g. "/-/domi"; "" for the site root
	clientPath     string // full path the client runtime is served at, prefix included

	appf         func(context.Context, *url.URL) (App[Msg], Cmd[Msg])
	onURLRequest func(URLRequest) Msg
	onURLChange  func(*url.URL) Msg

	mu sync.Mutex
	m  map[string]*session[Msg]
}

func newServer[Msg any, A App[Msg]](
	f func(context.Context, *url.URL) (A, Cmd[Msg]),
	onURLRequest func(URLRequest) Msg,
	onURLChange func(*url.URL) Msg,
	opts []Option,
) *server[Msg] {
	sv := &server[Msg]{
		document:       defaultDocument,
		logger:         slog.Default(),
		sessionTimeout: 48 * time.Hour,
		replayWindow:   128,
		keepalive:      25 * time.Second,

		appf:         func(ctx context.Context, u *url.URL) (App[Msg], Cmd[Msg]) { return f(ctx, u) },
		onURLRequest: onURLRequest,
		onURLChange:  onURLChange,
		m:            make(map[string]*session[Msg]),
	}
	for _, o := range opts {
		switch o := o.(type) {
		case documentOption:
			sv.document = func(_, title string, body Node) Node {
				return o.f(title, body)
			}
		case internalURLPrefixOption:
			sv.prefix = o.p
		case sessionTimeoutOption:
			sv.sessionTimeout = o.d
		case replayWindowOption:
			sv.replayWindow = o.n
		case keepaliveOption:
			sv.keepalive = o.d
		case loggerOption:
			sv.logger = o.l
		}
	}
	sv.clientPath = path.Join("/", sv.prefix, clientJSPath)
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
		logger: sv.logger.With("session", id),
		log:    make([]frame, sv.replayWindow),
		base:   verInitial,
		ver:    verInitial,
		tables: make(map[string]table[Msg]),
		active: time.Now(),
	}
	sv.put(id, s)
	go s.idleWatch(sv.sessionTimeout)
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

func defaultDocument(clientPath, title string, body Node) Node {
	return Tag("html")()(
		Tag("head")()(
			Tag("meta")(Name("charset")("utf-8")),
			Tag("title")()(Text(title)),
			Tag("script")(Name("type")("module"))(
				Text(fmt.Sprintf(`import * as Domi from %q; Domi.run();`, clientPath)),
			),
		),
		body,
	)
}

package domi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"
)

// A Server serves an [App].
// It implements [http.Handler].
type Server[Msg any] struct {
	// Config. Never changed after init. Safe to read concurrently.
	mux             http.ServeMux
	document        func(clientPath, title string, body Node) Node
	logger          *slog.Logger
	instanceTimeout time.Duration
	replayWindow    int
	keepalive       time.Duration
	prefix          string // namespace for internal URLs, e.g. "/-/domi"; "" for the site root
	clientPath      string // full path the client runtime is served at, prefix included

	appf         func(context.Context, *url.URL) (App[Msg], Cmd[Msg])
	onURLRequest func(*url.URL, bool) Msg
	onURLChange  func(*url.URL) Msg

	mu sync.Mutex
	m  map[string]*instance[Msg]
}

// NewServer returns a Server that serves an [App].
//
// On initial page load, the Server calls f
// with the request URL
// to obtain an instance of the App and an initial Cmd.
// The context contains the instance ID (see [InstanceID])
// and is cancelled when the instance ends.
//
// When the user clicks a link,
// domi intercepts the navigation
// and calls onURLRequest to produce a Msg.
// Param internal indicates whether the link target
// is to the same origin as the current page.
// Method Update decides how to handle the request,
// typically by returning a [PushURL] or [ReplaceURL] command.
//
// When the URL changes
// (from a navigation command or the browser's back and forward buttons),
// domi calls onURLChange to produce a Msg.
// The app's Update method then updates its state accordingly.
//
// Option values provide further control over the Server's behavior.
func NewServer[Msg any, A App[Msg]](
	f func(context.Context, *url.URL) (A, Cmd[Msg]),
	onURLRequest func(u *url.URL, internal bool) Msg,
	onURLChange func(*url.URL) Msg,
	o ...Option,
) *Server[Msg] {
	sv := &Server[Msg]{
		document:        defaultDocument,
		logger:          slog.Default(),
		instanceTimeout: 48 * time.Hour,
		replayWindow:    128,
		keepalive:       25 * time.Second,

		appf:         func(ctx context.Context, u *url.URL) (App[Msg], Cmd[Msg]) { return f(ctx, u) },
		onURLRequest: onURLRequest,
		onURLChange:  onURLChange,
		m:            make(map[string]*instance[Msg]),
	}
	for _, o := range o {
		switch o := o.(type) {
		case documentOption:
			sv.document = func(_, title string, body Node) Node {
				return o.f(title, body)
			}
		case internalURLPrefixOption:
			sv.prefix = o.p
		case instanceTimeoutOption:
			sv.instanceTimeout = o.d
		case replayWindowOption:
			sv.replayWindow = o.n
		case keepaliveOption:
			sv.keepalive = o.d
		case loggerOption:
			sv.logger = o.l
		}
	}
	sv.clientPath = path.Join("/", sv.prefix, "domi."+clientJSDigest+".js")
	sv.mux.HandleFunc("GET "+path.Join("/", sv.prefix, "{id}/events"), sv.handleSSE)
	sv.mux.HandleFunc("POST "+path.Join("/", sv.prefix, "{id}/event"), sv.handleEvent)
	sv.mux.HandleFunc("GET "+sv.clientPath, clientJSHandler)
	sv.mux.HandleFunc("GET /", sv.handleRoot)
	return sv
}

func (sv *Server[Msg]) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	sv.mux.ServeHTTP(w, req)
}

func (sv *Server[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	id := rand.Text()
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, instanceIDKey{}, id)
	s := &instance[Msg]{
		ctx:       ctx,
		cancel:    cancel,
		id:        id,
		sv:        sv,
		logger:    sv.logger.With("instance", id),
		log:       make([]frame, sv.replayWindow),
		base:      verInitial,
		ver:       verInitial,
		tables:    make(map[string]table[Msg]),
		active:    time.Now(),
		snapshots: newTreeRing(snapshotRingSize),
		recent:    newTreeRing(recentRingSize),
	}
	sv.put(id, s)
	go s.idleWatch(sv.instanceTimeout)
	go func() {
		<-ctx.Done()
		sv.delete(id)
	}()
	s.handleRoot(w, req)
}

func (sv *Server[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
	s, ok := sv.get(req.PathValue("id"))
	if !ok {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	s.handleEvent(w, req)
}

func (sv *Server[Msg]) handleSSE(w http.ResponseWriter, req *http.Request) {
	s, ok := sv.get(req.PathValue("id"))
	if !ok {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	s.handleSSE(w, req)
}

func (sv *Server[Msg]) put(id string, s *instance[Msg]) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.m[id] = s
}

func (sv *Server[Msg]) get(id string) (*instance[Msg], bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	s, ok := sv.m[id]
	return s, ok
}

func (sv *Server[Msg]) delete(id string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	delete(sv.m, id)
}

func defaultDocument(clientPath, title string, body Node) Node {
	return Tag("html")(
		Tag("head")(
			Tag("meta", Name("charset", "utf-8")),
			Tag("title")(Text(title)),
			Tag("script", Name("type", "module"), Name("src", clientPath)),
		),
		body,
	)
}

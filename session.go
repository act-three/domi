package domi

import (
	"context"
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

type patchSet []vdom.Patch

type session[Msg any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	id    string
	app   App[Msg]
	sv    *server[Msg]
	ready chan struct{} // unblocks when a new view & patchset is ready

	mu        sync.Mutex // protects the following fields
	title     string
	view      []vdom.Node
	patchSets []patchSet // TODO use a better data structure
	taken     bool       // true after an SSE consumer has been attached
	active    time.Time  // most recent activity; idleWatch reads this
}

func (s *session[Msg]) handleRoot(w http.ResponseWriter, req *http.Request) {
	app, cmd := s.sv.appf()
	title, view := app.View()
	s.app = app
	s.title, s.view = title, lower(view)
	s.spawn(cmd)

	body := Tag("body")(Name("data-domi-session")(s.id))(view)
	root := lowerOne(s.sv.config.document(title, body))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<!doctype html>")
	_, _ = io.WriteString(w, vdom.Render(root))
}

// spawn runs each Cmd in its own goroutine. The session ctx is handed to
// the Cmd's body so cmds can honor cancellation; the returned Msg feeds
// straight back into apply.
func (s *session[Msg]) spawn(cmd Cmd[Msg]) {
	for f := range cmd.s {
		go func() {
			s.apply(f(s.ctx))
		}()
	}
}

func (s *session[Msg]) apply(msg Msg) {
	// s.mu serializes the whole update cycle, including the user's Update
	// and View. If those grow expensive, split state under a second lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := s.app.Update(msg)
	title, view := s.app.View()
	next := lower(view)
	ps := vdom.Diff(s.view, next)
	if title != s.title {
		ps = append(ps, vdom.SetTitle(title))
	}
	if len(ps) > 0 {
		s.patchSets = append(s.patchSets, ps)
		select {
		case s.ready <- struct{}{}:
		default:
		}
	}
	s.view = next
	s.title = title
	s.spawn(cmd)
}

func (s *session[Msg]) handleEvent(w http.ResponseWriter, req *http.Request) {
	var envelope struct {
		H string         `json:"h"`
		E jsontext.Value `json:"e"`
	}
	if err := json.UnmarshalRead(req.Body, &envelope); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.touch()
	for h := range strings.SplitSeq(envelope.H, ",") {
		if h == "" {
			continue
		}
		raw, ok := lookupHandler(h)
		if !ok {
			continue // unknown, skip.
		}
		msg, err := unmarshalMsg[Msg](raw, envelope.E)
		if err != nil {
			// TODO: this should not fail. what do? log? http error code?
			continue
		}
		go s.apply(msg)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *session[Msg]) claimSSE() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taken {
		return false
	}
	s.taken = true
	s.active = time.Now()
	return true
}

// touch records activity on the session, deferring the idle timeout.
func (s *session[Msg]) touch() {
	s.mu.Lock()
	s.active = time.Now()
	s.mu.Unlock()
}

// idleWatch cancels the session once it has been idle for d.
// Runs as a goroutine started at session creation
// and exits when the session ctx is cancelled for any reason.
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
	if !s.claimSSE() {
		http.Error(w, "sse already attached", http.StatusConflict)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	rc.Flush()

	// SSE disconnect ends the session (reconnection is a separate
	// roadmap item). Cancel triggers the watcher in handleRoot to
	// remove the session from the server.
	defer s.cancel()

	ctx := req.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ready:
			s.mu.Lock()
			ps := s.patchSets
			s.patchSets = nil
			s.mu.Unlock()
			for _, p := range ps {
				data, err := json.Marshal(p)
				if err != nil {
					return
				}
				if _, err := fmt.Fprintf(w, "event: patch\ndata: %s\n\n", data); err != nil {
					return
				}
				rc.Flush()
				s.touch()
			}
		}
	}
}

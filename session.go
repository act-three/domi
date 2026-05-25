package domi

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
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

	id  string
	app App[Msg]
	sv  *server[Msg]

	mu     sync.Mutex // protects the following fields
	title  string
	view   []vdom.Node
	log    []frame        // fixed-size ring; log[seq%len(log)] holds frame seq
	head   uint64         // seq of the most recent frame; 0 if none
	sse    *sseAttachment // nil if no current consumer
	active time.Time
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
	_ = vdom.RenderTo(w, root)
}

// spawn hands the session ctx to each Cmd body so cmds can honor
// cancellation; the returned Msg feeds back into apply.
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
		s.head++
		s.log[s.head%uint64(len(s.log))] = frame{seq: s.head, patches: ps}
		if s.sse != nil {
			select {
			case s.sse.ready <- struct{}{}:
			default:
			}
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

	if resync, view, title, head := s.needsResync(seen); resync {
		f := frame{seq: head, patches: []vdom.Patch{
			vdom.Reset(view),
			vdom.SetTitle(title),
		}}
		if err := writeFrame(w, rc, f); err != nil {
			return
		}
		seen = head
	}

	for {
		s.touch()
		for _, f := range s.framesSince(seen) {
			if err := writeFrame(w, rc, f); err != nil {
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
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
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
func (s *session[Msg]) needsResync(seen uint64) (resync bool, view []vdom.Node, title string, head uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	head = s.head
	oldest := uint64(1)
	if n := uint64(len(s.log)); head >= n {
		oldest = head - n + 1
	}
	if seen+1 < oldest || seen > head {
		return true, s.view, s.title, head
	}
	return false, nil, "", head
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
	data, err := json.Marshal(f.patches)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: patch\ndata: %s\n\n", f.seq, data); err != nil {
		return err
	}
	return rc.Flush()
}

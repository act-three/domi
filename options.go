package domi

import (
	"log/slog"
	"time"
)

// An Option configures a [Handler].
type Option interface {
	isOption()
}

type handlerConfig struct {
	document       func(title string, body Node) Node
	logger         *slog.Logger
	sessionTimeout time.Duration
	replayWindow   int
	keepalive      time.Duration
}

// Document supplies a custom builder for the initial HTML shell.
// The builder is called once per session
// with the initial document title and the body element.
// It is responsible for returning a complete html element.
// The framework always writes the HTML5 doctype declaration
// before the html element.
//
//	domi.Handler(newApp, domi.Document(func(title string, body domi.Node) domi.Node {
//	    return html.HTML()(
//	        html.Head()(
//	            html.Meta(attr.Charset("utf-8")),
//	            html.Title()(domi.Text(title)),
//	            html.Script(attr.Type("module"), attr.Src("/bundle.js")),
//	        ),
//	        body,
//	    )
//	}))
//
// Apps using Document are responsible for loading the Domi client JavaScript.
// See [Bundling the Client] for details.
func Document(f func(title string, body Node) Node) Option {
	return documentOption{f}
}

type documentOption struct {
	f func(title string, body Node) Node
}

func (documentOption) isOption() {}

// SessionTimeout sets how long a session can remain idle before the
// framework releases it.
// The default timeout is 48 hours.
func SessionTimeout(d time.Duration) Option {
	return sessionTimeoutOption{d}
}

type sessionTimeoutOption struct {
	d time.Duration
}

func (sessionTimeoutOption) isOption() {}

// ReplayWindow sets how many recent patch frames a session retains
// for SSE clients to resume from after a transient disconnect.
// Clients reconnecting within this window
// receive only the patches they missed;
// clients further behind get a full resync of the current view.
// The default window is 128 frames.
func ReplayWindow(n int) Option {
	return replayWindowOption{n}
}

type replayWindowOption struct {
	n int
}

func (replayWindowOption) isOption() {}

// Keepalive sets the maximum SSE connection idle time
// before the server sends an SSE comment line to the client.
// Keepalives keep proxies from killing an idle connection.
// The default interval is 25 seconds.
func Keepalive(d time.Duration) Option {
	return keepaliveOption{d}
}

type keepaliveOption struct {
	d time.Duration
}

func (keepaliveOption) isOption() {}

// Logger sets the structured logger used by the framework
// for internal diagnostics such as malformed client events
// and handler registry misses.
// The default logger is [slog.Default].
func Logger(l *slog.Logger) Option {
	return loggerOption{l}
}

type loggerOption struct {
	l *slog.Logger
}

func (loggerOption) isOption() {}

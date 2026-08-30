package domi

import (
	"log/slog"
	"path"
	"time"
)

// An Option configures a [Server].
type Option interface{ isOption() }

func (documentOption) isOption()          {}
func (internalURLPrefixOption) isOption() {}
func (keepaliveOption) isOption()         {}
func (loggerOption) isOption()            {}
func (replayWindowOption) isOption()      {}
func (instanceTimeoutOption) isOption()   {}

type (
	documentOption struct {
		f func(title string, body Node) Node
	}
	internalURLPrefixOption struct{ p string }
	keepaliveOption         struct{ d time.Duration }
	loggerOption            struct{ l *slog.Logger }
	replayWindowOption      struct{ n int }
	instanceTimeoutOption   struct{ d time.Duration }
)

// Document supplies a custom builder for the initial HTML shell.
// The builder is called once per browser page load
// with the initial document title and the body element.
// It is responsible for returning a complete html element.
// Domi unconditionally writes the HTML5 doctype declaration
// to its HTTP response
// before the html element.
//
//	domi.Document(func(title string, body domi.Node) domi.Node {
//	    return html.HTML()(
//	        html.Head()(
//	            html.Meta(attr.Charset("utf-8")),
//	            html.Title()(domi.Text(title)),
//	            html.Script(attr.Type("module"), attr.Src("/bundle.js")),
//	        ),
//	        body,
//	    )
//	})
//
// Apps using Document are responsible for loading the Domi client JavaScript.
// See Serving the Client JavaScript Module in the package documentation for details.
func Document(f func(title string, body Node) Node) Option { return documentOption{f} }

// InternalURLPrefix specifies the prefix p
// used for domi's internal URL paths.
// This lets the application guarantee that domi's
// internal URL paths don't overlap with paths the app uses.
//
// The default prefix is "/".
func InternalURLPrefix(p string) Option {
	return internalURLPrefixOption{path.Clean("/" + p)}
}

// Keepalive sets how long an SSE connection is left idle
// before the server sends an SSE comment line to the client.
// This traffic prevents proxies from killing an idle connection.
// The default keepalive time is 25 seconds.
func Keepalive(d time.Duration) Option { return keepaliveOption{d} }

// Logger sets the structured logger used by domi
// for internal diagnostics such as malformed client events
// and handler registry misses.
// The default logger is [slog.Default].
func Logger(l *slog.Logger) Option { return loggerOption{l} }

// ReplayWindow sets the number of recent patch frames an instance retains
// for SSE clients to resume from after a transient disconnection.
// Clients reconnecting within this window
// receive the patches they missed.
// Clients further behind get a full resync of the current view.
// The default window is 128 frames.
func ReplayWindow(n int) Option { return replayWindowOption{n} }

// InstanceTimeout sets how long an instance can remain idle
// before domi considers it garbage and deletes it.
// The default timeout is 48 hours.
func InstanceTimeout(d time.Duration) Option { return instanceTimeoutOption{d} }

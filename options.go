package domi

import "time"

// An Option configures a [Handler].
type Option interface {
	isOption()
}

type handlerConfig struct {
	document       func(title string, body Node) Node
	sessionTimeout time.Duration
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

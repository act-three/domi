package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"ily.dev/domi"
	"ily.dev/domi/attr"
	"ily.dev/domi/html"
)

type Route int

const (
	Home Route = iota
	About
	Post
	NotFound
)

type Msg struct {
	URLRequest *url.URL
	Internal   bool
	URLChange  *url.URL
}

type N = domi.Node

var (
	text = domi.Text
	a    = html.A
	div  = html.Div
	h1   = html.H1
	h2   = html.H2
	p    = html.P
	nav  = html.Nav
	li   = html.LI
	ul   = html.UL
)

type App struct {
	route  Route
	postID string
}

func newApp(_ context.Context, u *url.URL) (*App, domi.Cmd[Msg]) {
	app := &App{}
	app.applyRoute(u)
	return app, nil
}

func (app *App) applyRoute(u *url.URL) {
	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "" || path == "/":
		app.route = Home
	case path == "/about":
		app.route = About
	case strings.HasPrefix(path, "/posts/"):
		app.postID = strings.TrimPrefix(path, "/posts/")
		if app.postID != "" {
			app.route = Post
		} else {
			app.route = NotFound
		}
	default:
		app.route = NotFound
	}
}

func (app *App) Update(_ context.Context, msg Msg) domi.Cmd[Msg] {
	if msg.URLRequest != nil {
		if msg.Internal {
			return domi.PushURL[Msg](msg.URLRequest.String())
		}
		// External links escape the SPA with a full page load.
		return domi.Load[Msg](msg.URLRequest.String())
	}
	if msg.URLChange != nil {
		app.applyRoute(msg.URLChange)
	}
	return nil
}

func (app *App) View(_ context.Context) (string, N) {
	return app.view()
}

func (app *App) Subscriptions(_ context.Context) (s domi.Sub[Msg]) { return s }

// Preview renders the page for u without mutating state — for instant
// navigation when the user hovers over a link. It returns u as the
// destination, since this app routes each URL to itself. NotFound
// routes return an empty dest, declining the preview so the click falls
// back to normal navigation rather than racing the user to a 404.
func (app *App) Preview(_ context.Context, u *url.URL) (string, string, N) {
	preview := *app
	preview.applyRoute(u)
	if preview.route == NotFound {
		return "", "", nil
	}
	t, v := preview.view()
	return u.String(), t, v
}

func (app *App) view() (string, N) {
	return app.title(), div(attr.Style("font-family:system-ui;max-width:40rem;margin:0 auto;padding:2rem"))(
		navbar(),
		app.page(),
	)
}

func (app *App) title() string {
	switch app.route {
	case Home:
		return "home"
	case About:
		return "about"
	case Post:
		return fmt.Sprintf("post %s", app.postID)
	default:
		return "not found"
	}
}

func (app *App) page() N {
	switch app.route {
	case Home:
		return div()(
			h1()(text("home")),
			p()(text("welcome! try the links above, or these:")),
			ul()(
				li()(a(attr.Href("/posts/hello-world"))(text("hello world"))),
				li()(a(attr.Href("/posts/navigation"))(text("navigation in domi"))),
				li()(a(attr.Href("/posts/tea"))(text("the elm architecture"))),
			),
		)
	case About:
		return div()(
			h1()(text("about")),
			p()(text("this example demonstrates client-side navigation in domi.")),
			p()(text("link clicks are intercepted and routed through the server. "+
				"the browser URL and DOM update in a single frame.")),
		)
	case Post:
		return div()(
			h1()(text(app.postID)),
			p()(domi.Textf("you are reading post %q.", app.postID)),
			p()(a(attr.Href("/"))(text("back to home"))),
		)
	default:
		return div()(
			h1()(text("not found")),
			p()(text("there's nothing here.")),
			p()(a(attr.Href("/"))(text("go home"))),
		)
	}
}

func navbar() N {
	link := func(href, label string) N {
		return li(attr.Style("display:inline"))(
			a(attr.Href(href), attr.Style("margin-right:1rem"))(text(label)),
		)
	}
	return nav(attr.Style("margin-bottom:2rem;border-bottom:1px solid #ccc;padding-bottom:1rem"))(
		ul(attr.Style("list-style:none;padding:0;margin:0"))(
			link("/", "home"),
			link("/about", "about"),
			link("/posts/hello-world", "a post"),
			link("/does-not-exist", "404"),
		),
	)
}

func main() {
	h := domi.Handler(
		newApp,
		func(u *url.URL, internal bool) Msg { return Msg{URLRequest: u, Internal: internal} },
		func(u *url.URL) Msg { return Msg{URLChange: u} },
	)
	addr := "127.0.0.1:3012"
	log.Printf("nav listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

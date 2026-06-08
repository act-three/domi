// Command panes is a three-column layout — sidebar, a scrollable item
// list, and a detail pane — that demonstrates how domi preserves element
// state across link navigation.
//
// Clicking an item shows its detail in the right pane without touching
// the list, so the list keeps its scroll position. The sidebar's text
// input keeps whatever you typed, and its <details> disclosure keeps its
// open/closed state — none of that lives in the virtual DOM, so the
// navigation patch leaves it alone.
//
// A ticker drives a progress bar in the shared shell (and occasionally
// prepends to the list) so there is always a server update in flight
// while you hover and click. That exercises the preview rebasing: each
// frame refreshes the held preview against the new view, so the click
// still navigates as an incremental patch rather than a snapshot restore.
package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ily.dev/domi"
	"ily.dev/domi/attr"
	"ily.dev/domi/html"
)

type Msg struct {
	URLRequest *domi.URLRequest `json:"-"`
	URLChange  *url.URL         `json:"-"`
	Tick       bool             `json:"-"`
}

type N = domi.Node

var (
	text    = domi.Text
	style   = attr.Style
	div     = html.Div
	h1      = html.H1
	h2      = html.H2
	p       = html.P
	li      = html.LI
	a       = html.A
	keyedUL = domi.Keyed("ul")
)

type route int

const (
	routeHome route = iota
	routeItem
)

type App struct {
	route  route
	itemID int
	tick   int   // drives the progress bar
	items  []int // item ids, newest first
	nextID int
}

func newApp(_ context.Context, u *url.URL) (*App, domi.Cmd[Msg]) {
	app := &App{nextID: 60}
	for id := 60; id >= 1; id-- {
		app.items = append(app.items, id)
	}
	app.applyRoute(u)
	return app, nil
}

func (app *App) applyRoute(u *url.URL) {
	path := strings.TrimRight(u.Path, "/")
	if rest, ok := strings.CutPrefix(path, "/item/"); ok {
		if id, err := strconv.Atoi(rest); err == nil {
			app.route, app.itemID = routeItem, id
			return
		}
	}
	app.route, app.itemID = routeHome, 0
}

func (app *App) Update(_ context.Context, msg Msg) domi.Cmd[Msg] {
	switch {
	case msg.URLRequest != nil:
		if msg.URLRequest.Internal {
			return domi.PushURL[Msg](msg.URLRequest.URL.String())
		}
		return domi.Load[Msg](msg.URLRequest.URL.String())
	case msg.URLChange != nil:
		app.applyRoute(msg.URLChange)
	case msg.Tick:
		app.tick++
		// Every so often, prepend a new item. A prepend grows the list
		// above the current scroll position — the case where a preview's
		// static target, reverting the prepend for a frame before the
		// catch-up restores it, can nudge the list's scroll.
		if app.tick%4 == 0 {
			app.nextID++
			app.items = append([]int{app.nextID}, app.items...)
			if len(app.items) > 120 {
				app.items = app.items[:120]
			}
		}
	}
	return nil
}

func (app *App) View(_ context.Context) (string, N) { return app.view() }

// Subscriptions runs a ticker that nudges the shared shell on a steady
// cadence, guaranteeing a server update lands in the window between a
// link hover and the click that follows it.
func (app *App) Subscriptions(_ context.Context) domi.Sub[Msg] {
	return domi.Subscription("tick", func(ctx context.Context) iter.Seq[Msg] {
		return func(yield func(Msg) bool) {
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if !yield(Msg{Tick: true}) {
						return
					}
				}
			}
		}
	})
}

// Preview renders the page for u without mutating state, so a hover can
// pre-render the detail pane. Copying the App is enough: applyRoute only
// touches route/itemID, and the render reads items without changing them.
func (app *App) Preview(_ context.Context, u *url.URL) (string, N, bool) {
	preview := *app
	preview.applyRoute(u)
	t, v := preview.view()
	return t, v, true
}

func (app *App) view() (string, N) {
	return app.title(), div(style("display:flex;height:100vh;margin:0;font-family:system-ui"))(
		sidebar(),
		middle(app),
		detail(app),
	)
}

func (app *App) title() string {
	if app.route == routeItem {
		return fmt.Sprintf("item %d", app.itemID)
	}
	return "panes"
}

func sidebar() N {
	return div(style("width:13rem;flex:none;padding:1rem;border-right:1px solid #ccc;background:#fafafa"))(
		h2(style("margin-top:0"))(text("panes")),
		p(style("font-size:0.8rem;color:#666"))(
			text("type below, scroll the list, toggle details — then click an item. it all survives the navigation."),
		),
		html.Input(attr.Type("text"), attr.Placeholder("uncontrolled input"), style("width:100%;box-sizing:border-box")),
		html.Details(style("margin-top:1rem;font-size:0.85rem"))(
			html.Summary()(text("client-state details")),
			p()(text("this disclosure keeps its open/closed state across navigation.")),
		),
		p(style("margin-top:1rem"))(a(attr.Href("/"))(text("home"))),
	)
}

func middle(app *App) N {
	return div(style("width:16rem;flex:none;display:flex;flex-direction:column;border-right:1px solid #ccc"))(
		progressBar(app.tick),
		div(style("flex:1;overflow:auto"))(
			keyedUL(style("list-style:none;margin:0;padding:0"))(func(yield func(string, N) bool) {
				for _, id := range app.items {
					if !yield(strconv.Itoa(id), itemRow(id)) {
						return
					}
				}
			}),
		),
	)
}

func progressBar(tick int) N {
	pct := tick % 101
	return div(style("padding:0.6rem 0.75rem;border-bottom:1px solid #eee;flex:none"))(
		div(style("font-size:0.8rem;color:#666"))(domi.Textf("live progress %d%%", pct)),
		div(style("height:6px;background:#eee;border-radius:3px;margin-top:5px"))(
			div(attr.Stylef("height:6px;width:%d%%;background:#4a90d9;border-radius:3px", pct))(),
		),
	)
}

func itemRow(id int) N {
	return li()(
		a(attr.Href(fmt.Sprintf("/item/%d", id)), style("display:block;padding:0.4rem 0.75rem;color:#333;text-decoration:none;border-bottom:1px solid #f0f0f0"))(
			domi.Textf("item %d", id),
		),
	)
}

func detail(app *App) N {
	if app.route != routeItem {
		return div(style("flex:1;padding:2rem;color:#888"))(text("select an item from the list →"))
	}
	return div(style("flex:1;padding:2rem"))(
		h1(style("margin-top:0"))(domi.Textf("item %d", app.itemID)),
		p()(domi.Textf("detail view for item %d.", app.itemID)),
		p(style("color:#666;font-size:0.9rem"))(
			text("the list kept its scroll position, and the sidebar input and details kept their state — the navigation only patched this pane."),
		),
		p()(a(attr.Href("/"))(text("← back to home"))),
	)
}

func main() {
	h := domi.Handler(
		newApp,
		func(r domi.URLRequest) Msg { return Msg{URLRequest: &r} },
		func(u *url.URL) Msg { return Msg{URLChange: u} },
	)
	addr := "127.0.0.1:3013"
	log.Printf("panes listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

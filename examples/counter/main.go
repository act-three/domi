package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"ily.dev/domi"
	"ily.dev/domi/attr"
	"ily.dev/domi/event"
	"ily.dev/domi/html"
)

type Msg struct {
	Tag string
}

type N = domi.Node

var (
	text   = domi.Text
	style  = attr.Style
	div    = html.Div
	h1     = html.H1
	button = html.Button
)

type Counter struct {
	count int
}

func newCounter(_ context.Context, _ *url.URL) (*Counter, domi.Cmd[Msg]) {
	return &Counter{}, nil
}

func (c *Counter) Update(_ context.Context, msg Msg) domi.Cmd[Msg] {
	switch msg.Tag {
	case "Increment":
		c.count++
	case "Decrement":
		c.count--
	case "Reset":
		c.count = 0
	}
	return nil
}

func (c *Counter) View(_ context.Context) (string, N) {
	return fmt.Sprintf("Counter (%d)", c.count),
		div(style("font-family:system-ui;padding:2rem"))(
			h1()(text(fmt.Sprintf("Count: %d", c.count))),
			button(event.Click(Msg{"Decrement"}))(text("-")),
			button(event.Click(Msg{"Increment"}))(text("+")),
			button(event.Click(Msg{"Reset"}))(text("reset")),
		)
}

func (c *Counter) Subscriptions(_ context.Context) (s domi.Sub[Msg]) { return s }

func (c *Counter) Preview(ctx context.Context, _ *url.URL) (string, string, N) {
	return "", "", nil
}

func main() {
	sv := domi.NewServer(
		newCounter,
		func(*url.URL) Msg { return Msg{} },
		func(*url.URL) Msg { return Msg{} },
	)
	addr := "127.0.0.1:3010"
	log.Printf("counter listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, sv))
}

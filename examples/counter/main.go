package main

import (
	"fmt"
	"log"
	"net/http"

	"ily.dev/domi"
	"ily.dev/domi/attr"
	"ily.dev/domi/event"
	"ily.dev/domi/html"
)

type Msg struct {
	Tag string `json:"msg"`
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

func newCounter() (*Counter, domi.Cmd[Msg]) {
	return &Counter{}, domi.Batch[Msg]()
}

func (c *Counter) Update(msg Msg) domi.Cmd[Msg] {
	switch msg.Tag {
	case "Increment":
		c.count++
	case "Decrement":
		c.count--
	case "Reset":
		c.count = 0
	}
	return domi.Batch[Msg]()
}

func (c *Counter) View() (string, N) {
	return fmt.Sprintf("Counter (%d)", c.count),
		div(style("font-family:system-ui;padding:2rem"))(
			h1()(text(fmt.Sprintf("Count: %d", c.count))),
			button(event.Click(Msg{"Decrement"}))(text("-")),
			button(event.Click(Msg{"Increment"}))(text("+")),
			button(event.Click(Msg{"Reset"}))(text("reset")),
		)
}

func main() {
	h := domi.Handler(newCounter)
	addr := "127.0.0.1:3010"
	log.Printf("counter listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

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

type (
	N = domi.Node
	A = domi.Attr
)

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

func (c *Counter) Init() domi.Cmd[Msg] { return domi.CmdNone[Msg]() }

func (c *Counter) Update(msg Msg) domi.Cmd[Msg] {
	switch msg.Tag {
	case "Increment":
		c.count++
	case "Decrement":
		c.count--
	case "Reset":
		c.count = 0
	}
	return domi.CmdNone[Msg]()
}

func (c *Counter) View() N {
	return div(
		[]A{style("font-family:system-ui;padding:2rem")},
		[]N{
			h1(nil, []N{text(fmt.Sprintf("Count: %d", c.count))}),
			button([]A{event.Click(Msg{"Decrement"})}, []N{text("-")}),
			button([]A{event.Click(Msg{"Increment"})}, []N{text("+")}),
			button([]A{event.Click(Msg{"Reset"})}, []N{text("reset")}),
		},
	)
}

func (c *Counter) Title() string {
	return fmt.Sprintf("Counter (%d)", c.count)
}

func main() {
	h := domi.Handler(func() domi.App[Msg] { return &Counter{} })
	addr := "127.0.0.1:3010"
	log.Printf("counter listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

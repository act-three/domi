package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"ily.dev/domi"
	"ily.dev/domi/attr"
	"ily.dev/domi/event"
	"ily.dev/domi/html"
)

type Msg struct {
	Tag string `json:"msg"`
	ID  uint64 `json:"id,omitempty"`
}

type N = domi.Node

var (
	text    = domi.Text
	style   = attr.Style
	div     = html.Div
	h1      = html.H1
	span    = html.Span
	keyedUL = domi.Keyed("ul")
	li      = html.LI
	button  = html.Button
	onClick = event.Click
)

type Item struct {
	ID   uint64
	Text string
	Done bool
}

type Todos struct {
	items  []Item
	nextID uint64
}

func newTodos(_ context.Context) (*Todos, domi.Cmd[Msg]) {
	t := &Todos{}
	for _, s := range []string{"learn go generics", "spike domi", "ship something"} {
		t.nextID++
		t.items = append(t.items, Item{ID: t.nextID, Text: s})
	}
	return t, domi.Batch[Msg]()
}

func (t *Todos) Update(_ context.Context, msg Msg) domi.Cmd[Msg] {
	switch msg.Tag {
	case "Add":
		t.nextID++
		t.items = append(t.items, Item{
			ID:   t.nextID,
			Text: fmt.Sprintf("item %d", t.nextID),
		})
	case "Toggle":
		for i := range t.items {
			if t.items[i].ID == msg.ID {
				t.items[i].Done = !t.items[i].Done
			}
		}
	case "Remove":
		out := t.items[:0]
		for _, it := range t.items {
			if it.ID != msg.ID {
				out = append(out, it)
			}
		}
		t.items = out
	case "MoveUp":
		for i, it := range t.items {
			if it.ID == msg.ID && i > 0 {
				t.items[i-1], t.items[i] = t.items[i], t.items[i-1]
				break
			}
		}
	}
	return domi.Batch[Msg]()
}

func (t *Todos) Subscriptions(_ context.Context) (s domi.Sub[Msg]) { return s }

func (t *Todos) View(_ context.Context) (string, N) {
	return "todos",
		div(style("font-family:system-ui;padding:2rem;max-width:32rem"))(
			h1()(text("todos")),
			keyedUL(style("list-style:none;padding:0"))(func(yield func(string, N) bool) {
				for _, it := range t.items {
					if !yield(strconv.FormatUint(it.ID, 10), itemRow(it)) {
						return
					}
				}
			}),
			button(onClick(Msg{Tag: "Add"}))(text("+ add item")),
		)
}

func itemRow(it Item) N {
	labelStyle := "flex:1"
	toggleLabel := "✓"
	if it.Done {
		labelStyle = "text-decoration:line-through;color:#888;flex:1"
		toggleLabel = "↺"
	}
	return li(style("display:flex;gap:0.5rem;align-items:center;padding:0.25rem 0"))(
		span(style(labelStyle))(text(it.Text)),
		button(onClick(Msg{Tag: "Toggle", ID: it.ID}))(text(toggleLabel)),
		button(onClick(Msg{Tag: "MoveUp", ID: it.ID}))(text("↑")),
		button(onClick(Msg{Tag: "Remove", ID: it.ID}))(text("×")),
	)
}

func main() {
	h := domi.Handler(newTodos)
	addr := "127.0.0.1:3011"
	log.Printf("todos listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

package main

import (
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

type (
	N = domi.Node
	A = domi.Attr
)

var (
	text    = domi.Text
	style   = attr.Style
	div     = html.Div
	h1      = html.H1
	span    = html.Span
	ul      = html.Ul
	li      = html.Li
	button  = html.Button
	onClick = event.Click[Msg]
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

func newTodos() *Todos {
	t := &Todos{}
	for _, s := range []string{"learn go generics", "spike domi", "ship something"} {
		t.nextID++
		t.items = append(t.items, Item{ID: t.nextID, Text: s})
	}
	return t
}

func (t *Todos) Init() domi.Cmd[Msg] { return domi.CmdNone[Msg]() }

func (t *Todos) Update(msg Msg) domi.Cmd[Msg] {
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
	return domi.CmdNone[Msg]()
}

func (t *Todos) View() N {
	items := make([]N, len(t.items))
	for i, it := range t.items {
		items[i] = itemRow(it).WithKey(strconv.FormatUint(it.ID, 10))
	}
	return div(
		[]A{style("font-family:system-ui;padding:2rem;max-width:32rem")},
		[]N{
			h1(nil, []N{text("todos")}),
			ul([]A{style("list-style:none;padding:0")}, items),
			button([]A{onClick(Msg{Tag: "Add"})}, []N{text("+ add item")}),
		},
	)
}

func itemRow(it Item) N {
	labelStyle := "flex:1"
	toggleLabel := "✓"
	if it.Done {
		labelStyle = "text-decoration:line-through;color:#888;flex:1"
		toggleLabel = "↺"
	}
	return li(
		[]A{style("display:flex;gap:0.5rem;align-items:center;padding:0.25rem 0")},
		[]N{
			span([]A{style(labelStyle)}, []N{text(it.Text)}),
			button([]A{onClick(Msg{Tag: "Toggle", ID: it.ID})}, []N{text(toggleLabel)}),
			button([]A{onClick(Msg{Tag: "MoveUp", ID: it.ID})}, []N{text("↑")}),
			button([]A{onClick(Msg{Tag: "Remove", ID: it.ID})}, []N{text("×")}),
		},
	)
}

func (t *Todos) Title() string { return "todos" }

func main() {
	h := domi.Handler(func() domi.App[Msg] { return newTodos() })
	addr := "127.0.0.1:3011"
	log.Printf("todos listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

package domi

import (
	"encoding/json/jsontext"
	"maps"
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

// msgFn returns an unmarshal function that ignores the event payload
// and produces tag, for tests that only care about identity.
func msgFn(tag string) func(jsontext.Value) (string, error) {
	return func(jsontext.Value) (string, error) { return tag, nil }
}

// A handler's key derives from its element's address alone, so two
// renders of the same view shape — with brand-new functions — produce
// byte-identical trees: the diff is quiet and the client keeps its
// attrs while the server rebinds the keys to the new functions.
func TestOnAddressStableAcrossRenders(t *testing.T) {
	build := func(tag string) ([]vdom.Node, handlers) {
		return lower(0, Tag("div")()(
			Text("greetings"),
			Tag("button")(On("click", msgFn(tag)))(Text("x")),
		))
	}
	a, ha := build("first render")
	b, hb := build("second render")
	if got := vdom.Diff(a, b); len(got) != 0 {
		t.Fatalf("re-rendered handlers should not patch, got %+v", got)
	}
	if !maps.Equal(keysOf(ha), keysOf(hb)) {
		t.Fatalf("handler keys diverged across renders: %v vs %v", keysOf(ha), keysOf(hb))
	}
}

func keysOf(h handlers) map[string]bool {
	out := make(map[string]bool, len(h))
	for k := range h {
		out[k] = true
	}
	return out
}

// Identical elements at different positions get different keys — even
// when they are literally the same Node value placed twice.
func TestOnAddressDistinguishesPosition(t *testing.T) {
	btn := Tag("button")(On("click", msgFn("x")))(Text("x"))
	_, h := lower(0, Tag("div")()(btn, btn))
	if len(h) != 2 {
		t.Fatalf("expected 2 handler keys for 2 positions, got %d: %v", len(h), keysOf(h))
	}
}

// A keyed child's address follows its key, not its index, so a reorder
// keeps every handler key — mirroring the differ, which matches keyed
// children by identity and emits moves rather than rewrites.
func TestOnKeyedAddressSurvivesReorder(t *testing.T) {
	build := func(order ...string) ([]vdom.Node, handlers) {
		rows := make([]Node, len(order))
		for i, k := range order {
			rows[i] = WithKey(k, Tag("li")(On("click", msgFn(k)))(Text(k)))
		}
		return lower(0, Tag("ul")()(rows...))
	}
	_, ha := build("a", "b", "c")
	_, hb := build("c", "a", "b")
	if !maps.Equal(keysOf(ha), keysOf(hb)) {
		t.Fatalf("keyed reorder changed handler keys: %v vs %v", keysOf(ha), keysOf(hb))
	}
}

// In a mixed child list, an unkeyed child's address follows its gap —
// the run of unkeyed children it occupies, numbered by the keyed
// siblings before it — so reordering the keyed run around unchanged
// unkeyed content renames nothing: the header's and footer's handler
// keys survive alongside the keyed children's.
func TestOnMixedAddressSurvivesKeyedReorder(t *testing.T) {
	build := func(order ...string) ([]vdom.Node, handlers) {
		rows := make([]Node, len(order))
		for i, k := range order {
			rows[i] = WithKey(k, Tag("li")(On("click", msgFn(k)))(Text(k)))
		}
		return lower(0, Tag("ul")()(
			Tag("li")(On("click", msgFn("header")))(Text("header")),
			Fragment(rows...),
			Tag("li")(On("click", msgFn("footer")))(Text("footer")),
		))
	}
	_, ha := build("a", "b", "c")
	_, hb := build("c", "a", "b")
	if !maps.Equal(keysOf(ha), keysOf(hb)) {
		t.Fatalf("mixed reorder changed handler keys: %v vs %v", keysOf(ha), keysOf(hb))
	}
}

// Multiple handlers for the same event on one element get distinct
// slots, and their keys combine comma-separated in the rendered attr.
func TestOnSlotsDistinguishHandlers(t *testing.T) {
	n, h := lower(0, Tag("button")(
		On("click", msgFn("a")),
		On("click", msgFn("b")),
	)(Text("x")))
	if len(h) != 2 {
		t.Fatalf("expected 2 handler keys for 2 slots, got %d", len(h))
	}
	html := vdom.Render(n[0])
	for k := range h {
		if !strings.Contains(html, k) {
			t.Fatalf("handler key %q missing from render %q", k, html)
		}
	}
	if !strings.Contains(html, ",") {
		t.Fatalf("same-event handlers should comma-combine: %q", html)
	}
}

// Different events on one element get distinct keys: the event name
// participates in the key, not just the element address and slot.
func TestOnEventDistinguishesHandlers(t *testing.T) {
	_, h := lower(0, Tag("form")(
		On("click", msgFn("a")),
		On("submit", msgFn("b")),
	)(Text("x")))
	if len(h) != 2 {
		t.Fatalf("expected distinct keys for distinct events, got %d", len(h))
	}
}

// On with field paths content-addresses the path set and appends its
// key to the attribute value, after the handler key, so the client can
// look up the path set. The handler stays keyed by the handler part.
func TestOnPathSet(t *testing.T) {
	n, h := lower(0, Tag("input")(On("input", msgFn("x"), []string{"target", "value"}))())
	html := vdom.Render(n[0])
	const marker = `domi-msg-input="`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no handler attr in render %q", html)
	}
	value := html[i+len(marker):]
	value = value[:strings.IndexByte(value, '"')]
	key, psKey, ok := strings.Cut(value, ":")
	if !ok {
		t.Fatalf("want key:ps attr value, got %q", value)
	}
	hd, ok := h[key]
	if !ok {
		t.Fatalf("handler not keyed by handler part %q of %q", key, value)
	}
	if psKey != hd.ps.key() {
		t.Fatalf("attr ps key %q != path set key %q", psKey, hd.ps.key())
	}
}

// A path set's content address ignores the order its field paths were
// given, so equivalent path sets share one client registration.
func TestPathSetCanonical(t *testing.T) {
	x := On("click", msgFn("m"), []string{"clientX"}, []string{"clientY"}).(attr)
	y := On("click", msgFn("m"), []string{"clientY"}, []string{"clientX"}).(attr)
	if xk, yk := x.handler.ps.key(), y.handler.ps.key(); xk == "" || xk != yk {
		t.Fatalf("reordered path sets got different keys: %q vs %q", xk, yk)
	}
}

// A handler with no field paths still carries a valid (empty) path set,
// content-addressed and referenced like any other.
func TestOnEmptyPathSet(t *testing.T) {
	n, _ := lower(0, Tag("button")(On("click", msgFn("x")))())
	html := vdom.Render(n[0])
	if !strings.Contains(html, ":"+pathSet(nil).key()) {
		t.Fatalf("bare handler should reference the empty path set: %q", html)
	}
}

// On panics on a nil unmarshal function, at construction, where the
// stack trace points at the offending call.
func TestOnNilUnmarshalPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil unmarshal function")
		}
	}()
	_ = On[string]("click", nil)
}

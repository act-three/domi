package domi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"hash/fnv"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"ily.dev/domi/internal/vdom"
)

// A handlers maps a handler key — the hash of an element's address
// plus an event slot, see [addr] — to its handler.
type handlers map[string]handler

// A handler is an unmarshal function (to construct a Msg when the
// event happens) and a path set (the fields to read from the browser's
// event object). fn boxes its Msg as any so that Node types and HTML
// constructors don't all have to be generic. vzero and pzero are a
// zero value and a nil pointer of the handler's Msg type, from which
// an instance recovers that type to check at render time that it is
// identical to, or implements, the instance's own Msg. See adapt.
type handler struct {
	fn    func(jsontext.Value) (any, error)
	vzero any
	pzero any
	ps    pathSet
	event string
}

// A pathSet is a set of field paths into a browser event.
// The client reads the value at each path and returns them to the server.
type pathSet [][]string

// key returns the hash of p.
func (p pathSet) key() string {
	raw, err := json.Marshal([][]string(p))
	if err != nil {
		panic(err) // a [][]string is always marshalable
	}
	h := fnv.New64a()
	h.Write(raw)
	return strconv.FormatUint(h.Sum64(), 16)
}

// merge adds src's entries to dst and returns the result, allocating
// only when dst is nil and src is non-empty. dst is mutated in place, so
// the caller must own it; src is only read. Either may be nil, so a
// handler-free subtree never allocates a map.
func (dst handlers) merge(src handlers) handlers {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(handlers, len(src))
	}
	maps.Copy(dst, src)
	return dst
}

// adapt returns hd's unmarshal function typed to produce Msg.
// hd's own Msg type must be identical to Msg, or implement it
// when it is an interface — the condition for a type assertion
// to Msg to succeed, which the adapter relies on — else adapt panics.
// The condition is itself settled by type assertion where it can be:
// vzero asserts to Msg when the handler's type is concrete,
// and pzero to *Msg when the two types are identical.
// Only when both fail is the handler's type recovered by reflection
// from pzero, to rule on an interface handler type
// that implements an interface Msg.
// The adapter asserts each event's result to Msg;
// a nil result from an interface handler type
// has no dynamic type to assert and is already the zero Msg.
func adapt[Msg any](hd handler) func(jsontext.Value) (Msg, error) {
	_, vok := hd.vzero.(Msg)
	_, pok := hd.pzero.(*Msg)
	if !vok && !pok {
		got, want := reflect.TypeOf(hd.pzero).Elem(), reflect.TypeFor[Msg]()
		if want.Kind() != reflect.Interface || !got.Implements(want) {
			panic(fmt.Sprintf("domi: On(%q) handler returns %v, want %v", hd.event, got, want))
		}
	}
	return func(v jsontext.Value) (msg Msg, err error) {
		r, err := hd.fn(v)
		if r != nil {
			msg = r.(Msg)
		}
		return msg, err
	}
}

// MapNode transforms the messages produced by n.
// It calls f to convert each message of type T
// to a message of type Msg.
//
// Msg must be the App's Msg type or a type that implements it.
// Handlers in n must produce T, or a type that implements it.
//
// MapNode lets an app embed a view with a different message type:
//
//	MapNode(func(m widget.Msg) Msg { return widgetMsg{m} }, widget.View(ctx))
func MapNode[T, Msg any](f func(T) Msg, n Node) Node {
	if f == nil {
		panic("domi: MapNode called with a nil function")
	}
	mapper := func(hd handler) handler {
		unmarshal := adapt[T](hd)
		return handler{
			fn: func(v jsontext.Value) (any, error) {
				m, err := unmarshal(v)
				if err != nil {
					return nil, err
				}
				return f(m), nil
			},
			vzero: *new(Msg),
			pzero: (*Msg)(nil),
			ps:    hd.ps,
			event: hd.event,
		}
	}
	return fragment(iterMap(iter.Seq[node](Fragment(n).(fragment)), func(c node) node {
		e, ok := c.(element)
		if !ok {
			return c
		}
		if inner := e.mapper; inner != nil {
			e.mapper = func(hd handler) handler { return mapper(inner(hd)) }
		} else {
			e.mapper = mapper
		}
		return e
	}))
}

// iterMap returns the sequence of f applied to each element of seq.
func iterMap[V, U any](seq iter.Seq[V], f func(V) U) iter.Seq[U] {
	return func(yield func(U) bool) {
		for v := range seq {
			if !yield(f(v)) {
				break
			}
		}
	}
}

// On calls f when the named browser event occurs,
// then calls Update with the resulting Msg value.
// Helpers for common events can be found in [ily.dev/domi/event].
//
// Msg must be the App's Msg type or a type that implements it.
//
// Each field value
// is a path of JavaScript property names rooted at the event object.
// The client reads the value at each path
// and includes it in the JSON object given to f.
// For instance,
//
//	On("input", f, []string{"target", "value"})
//
// calls f with the JSON text
//
//	{"target": {"value": "hello, world"}}
//
// If f returns an error, the event is discarded.
//
// The event name must be lowercase.
// If event is invalid or f is nil, On panics.
func On[Msg any](event string, f func(jsontext.Value) (Msg, error), field ...[]string) Attr {
	if !isValidName(event, nil) {
		panic(fmt.Sprintf("domi: invalid event name %q", event))
	}
	if f == nil {
		panic("domi: On called with a nil unmarshal function")
	}
	ps := pathSet(field)
	slices.SortFunc(ps, slices.Compare)
	return attr{
		attr: vdom.Attr{Name: "domi-msg-" + event},
		handler: &handler{
			fn:    func(v jsontext.Value) (any, error) { return f(v) },
			vzero: *new(Msg),
			pzero: (*Msg)(nil),
			ps:    ps,
			event: event,
		},
	}
}

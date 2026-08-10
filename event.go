package domi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"

	"ily.dev/domi/internal/vdom"
)

// A handlers maps a handler key — the hash of an element's address
// plus an event slot, see [addr] — to its handler.
type handlers map[string]handler

// A handler is an unmarshal function (to construct a Msg when the
// event happens) and a path set (the fields to read from the browser's
// event object). fn has type func(jsontext.Value) (Msg, error),
// stored as any so that Node types and HTML constructors don't
// all have to be generic.
type handler struct {
	fn    any
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

// On calls f when the named browser event occurs,
// then calls Update with the resulting Msg value.
// Helpers for common events can be found in [ily.dev/domi/event].
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
		attr:    vdom.Attr{Name: "domi-msg-" + event},
		handler: &handler{fn: f, ps: ps, event: event},
	}
}

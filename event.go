package domi

import (
	"encoding/json/v2"
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"ily.dev/domi/internal/vdom"
)

// A handlers maps a content-hash key to its handler.
type handlers map[string]handler

// A handler is a msg
// (to be returned to the app when the event happens)
// and a path set
// (to be read from the browser's event object).
type handler struct {
	msg []byte
	ps  pathSet
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

// On returns a builder for an attribute that binds msg to event
// on the resulting element.
// When the browser fires the named event,
// the framework calls Update(msg).
//
// Multiple On(event)(...) attributes on the same element all fire
// when the event occurs.
//
// Each field is a path of property names into the browser event.
// The client reads the value at each path,
// and the framework unmarshals the collected values
// into the msg field tagged domi:"event", if any.
// For example, On("input", []string{"target", "value"})
// obtains the value of the changed input element.
//
// If msg cannot be marshaled to JSON, On panics.
func On(event string, field ...[]string) func(msg any) Attr {
	ps := pathSet(field)
	slices.SortFunc(ps, slices.Compare)
	return func(msg any) Attr {
		if fi := eventFieldIndex(reflect.ValueOf(msg)); fi != nil {
			v := reflect.New(reflect.TypeOf(msg)).Elem()
			v.Set(reflect.ValueOf(msg))
			v.FieldByIndex(fi).SetZero()
			msg = v.Interface()
		}

		raw, err := json.Marshal(msg)
		if err != nil {
			panic(fmt.Errorf("bad msg for %s: %w", event, err))
		}
		h := fnv.New64a()
		h.Write(raw)
		key := strconv.FormatUint(h.Sum64(), 16)
		return attr{
			attr:     vdom.Attr{Name: "data-msg-" + event, Value: key + ":" + ps.key()},
			handlers: handlers{key: {msg: raw, ps: ps}},
		}
	}
}

// unmarshalMsg unmarshals msgb into a fresh Msg. Then, if Msg's type
// carries a `domi:"event"` field, unmarshals eventb into that field.
func unmarshalMsg[Msg any](msgb, eventb []byte) (Msg, error) {
	var msg Msg
	if err := json.Unmarshal(msgb, &msg); err != nil {
		return msg, err
	}
	if len(eventb) == 0 {
		return msg, nil
	}
	mv := reflect.ValueOf(&msg)
	if fi := eventFieldIndex(mv); fi != nil {
		ev := mv.Elem().FieldByIndex(fi).Addr().Interface()
		if err := json.Unmarshal(eventb, ev); err != nil {
			return msg, err
		}
	}
	return msg, nil
}

// eventFieldIndex returns the field index tagged domi:"event" or nil.
func eventFieldIndex(v reflect.Value) []int {
	v = reflect.Indirect(v)
	t := v.Type()
	if t.Kind() != reflect.Struct {
		return nil
	}
	for field := range t.Fields() {
		if field.Tag.Get("domi") == "event" {
			return field.Index
		}
	}
	return nil
}

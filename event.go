package domi

import (
	"encoding/json/v2"
	"fmt"
	"hash/fnv"
	"reflect"
	"strconv"
	"sync"
)

// On returns a builder for an attribute that binds msg to event
// on the resulting element.
// When the browser fires the named event,
// the framework calls Update(msg).
//
// Multiple On(event)(...) attributes on the same element all fire
// when the event occurs.
//
// If msg has a field tagged domi:"event",
// the framework unmarshals the event into that field.
// See [ily.dev/domi/event.Event] for a convenience type
// that captures everything the client sends.
//
// If msg cannot be marshaled to JSON, On panics.
func On(event string) func(msg any) Attr {
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
		return attr{Name: "data-msg-" + event, Value: registerHandler(raw)}
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

func registerHandler(raw []byte) string {
	h := fnv.New64a()
	h.Write(raw)
	key := strconv.FormatUint(h.Sum64(), 16)
	handlersMu.Lock()
	handlers[key] = raw
	handlersMu.Unlock()
	return key
}

func lookupHandler(key string) ([]byte, bool) {
	handlersMu.RLock()
	raw, ok := handlers[key]
	handlersMu.RUnlock()
	return raw, ok
}

// Process-wide registry of event-handler messages, keyed by a content
// hash of the marshaled Msg JSON. On() inserts; serve.go's handleEvent
// looks up. The map is content-addressable, so identical Msgs from any
// session share a slot; size is bounded by the number of distinct Msg
// values constructed by all running apps, which is small in practice
// (TEA apps have a handful of variants, sometimes parameterized by IDs).
var (
	handlersMu sync.RWMutex
	handlers   = map[string][]byte{}
)

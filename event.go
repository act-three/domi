package domi

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
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

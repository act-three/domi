package domi

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"sync"
)

// Event is the payload delivered with every event dispatch. To receive
// it, embed a field of this type in your Msg and mark it with a
// domi:"event" struct tag:
//
//	type Msg struct {
//	    Tag   string
//	    Event domi.Event `domi:"event"`
//	}
//
// On each dispatch the framework fills the tagged field with details of
// the firing event before calling Update; any value pre-set on the field
// in the [On] literal is ignored. Msgs delivered by a [Cmd] always have
// a zero Event field.
type Event struct {
	Type    string            `json:"type"`
	Key     string            `json:"key,omitempty"`
	Code    string            `json:"code,omitempty"`
	Button  int               `json:"button,omitempty"`
	ClientX int               `json:"clientX,omitempty"`
	ClientY int               `json:"clientY,omitempty"`
	Ctrl    bool              `json:"ctrl,omitempty"`
	Shift   bool              `json:"shift,omitempty"`
	Alt     bool              `json:"alt,omitempty"`
	Meta    bool              `json:"meta,omitempty"`
	Target  Target            `json:"target"`
	Form    map[string]string `json:"form,omitempty"`
}

// Target describes the element the event fired on.
type Target struct {
	Tag     string            `json:"tag"`
	ID      string            `json:"id,omitempty"`
	Name    string            `json:"name,omitempty"`
	Value   string            `json:"value,omitempty"`
	Checked bool              `json:"checked,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

// msgTypeInfo caches the result of scanning a Msg type for a `domi:"event"`
// tagged field. Stored per reflect.Type in msgTypeCache so we pay the
// reflection cost once per Msg type, not per On() call or per dispatch.
type msgTypeInfo struct {
	// eventFieldPath is the field index path to splice event payloads
	// into, or nil if the Msg type has no `domi:"event"` field.
	eventFieldPath []int
	// err is set if the Msg type is invalid (e.g. multiple tagged fields).
	// Surfaced via panic at registration so the bug shows up immediately.
	err error
}

var msgTypeCache sync.Map // map[reflect.Type]*msgTypeInfo

func msgTypeInfoFor(t reflect.Type) *msgTypeInfo {
	if got, ok := msgTypeCache.Load(t); ok {
		return got.(*msgTypeInfo)
	}
	info := scanMsgType(t)
	actual, _ := msgTypeCache.LoadOrStore(t, info)
	return actual.(*msgTypeInfo)
}

func scanMsgType(t reflect.Type) *msgTypeInfo {
	if t == nil || t.Kind() != reflect.Struct {
		return &msgTypeInfo{}
	}
	var path []int
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("domi") != "event" {
			continue
		}
		if path != nil {
			return &msgTypeInfo{err: fmt.Errorf("domi: %v has multiple fields tagged `domi:\"event\"` (only one allowed)", t)}
		}
		path = []int{i}
	}
	return &msgTypeInfo{eventFieldPath: path}
}

// On returns a builder for an attribute that binds msg to event on
// the resulting element: when the browser fires that event, the
// framework dispatches msg to the App's Update.
//
// Multiple On(event)(...) attributes on the same element all fire when
// the event occurs; each msg is dispatched to Update in the order the
// attributes were attached.
//
// If msg's type contains a field of type [Event] with a domi:"event"
// struct tag, the framework fills that field with the firing event's
// details before calling Update. The value of that field in the literal
// passed to the builder is ignored — only the other fields contribute
// to the Msg's identity.
func On(event string) func(msg any) Attr {
	return func(msg any) Attr {
		info := msgTypeInfoFor(reflect.TypeOf(msg))
		if info.err != nil {
			panic(info.err)
		}
		raw, err := json.Marshal(zeroEventField(msg, info.eventFieldPath))
		if err != nil {
			raw = []byte("null")
		}
		return attr{Name: "data-msg-" + event, Value: registerHandler(raw)}
	}
}

// zeroEventField returns msg with the field at fieldPath set to its zero
// value (or msg unchanged if fieldPath is nil). Works on struct values
// by making an addressable copy via reflect.New.
func zeroEventField(msg any, fieldPath []int) any {
	if fieldPath == nil {
		return msg
	}
	v := reflect.New(reflect.TypeOf(msg)).Elem()
	v.Set(reflect.ValueOf(msg))
	v.FieldByIndex(fieldPath).SetZero()
	return v.Interface()
}

// spliceEvent unmarshals rawMsg into a fresh Msg, then — if Msg's type
// carries a `domi:"event"` field — unmarshals eventBlob into that field.
// Used by serve.go's handleEvent on every dispatch.
func spliceEvent[Msg any](rawMsg, eventBlob []byte) (Msg, error) {
	var msg Msg
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return msg, err
	}
	if len(eventBlob) == 0 {
		return msg, nil
	}
	info := msgTypeInfoFor(reflect.TypeOf(msg))
	if info.eventFieldPath == nil {
		return msg, nil
	}
	field := reflect.ValueOf(&msg).Elem().FieldByIndex(info.eventFieldPath)
	if err := json.Unmarshal(eventBlob, field.Addr().Interface()); err != nil {
		return msg, err
	}
	return msg, nil
}

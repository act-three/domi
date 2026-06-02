// Package event provides prebound constructors for common DOM event
// handlers (event.Click, event.Submit, event.Input) and convenience
// types for the event payload the framework splices into tagged Msg
// fields.
package event

import "ily.dev/domi"

// Event is a convenience payload type covering the standard fields
// the framework's client emits with every event dispatch. To receive
// it, add a field of this type to your Msg and tag it `domi:"event"`:
//
//	type Msg struct {
//	    Tag   string
//	    Event event.Event `domi:"event"`
//	}
//
// The framework unmarshals the firing event's JSON payload into the
// tagged field, so a Msg that only needs a subset of these fields can
// use any struct type with matching JSON tags — Event is just a
// convenient shape that captures everything the client sends.
type Event struct {
	Type    string `json:"type"`
	Key     string `json:"key,omitempty"`
	Code    string `json:"code,omitempty"`
	Button  int    `json:"button,omitempty"`
	ClientX int    `json:"clientX,omitempty"`
	ClientY int    `json:"clientY,omitempty"`
	Ctrl    bool   `json:"ctrl,omitempty"`
	Shift   bool   `json:"shift,omitempty"`
	Alt     bool   `json:"alt,omitempty"`
	Meta    bool   `json:"meta,omitempty"`
	Target  Target `json:"target"`
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

var (
	Click  = domi.On("click")
	Submit = domi.On("submit")
	Input  = domi.On("input")
)

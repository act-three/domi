// Package event provides handlers for common DOM events.
//
// See [domi.On] to define custom event handlers.
package event

import (
	"encoding/json/jsontext"
	"encoding/json/v2"

	"ily.dev/domi"
)

// Click delivers msg when the element is clicked.
func Click[Msg any](msg Msg) domi.Attr {
	return domi.On("click", constant(msg))
}

// Submit delivers msg when the element's form is submitted.
func Submit[Msg any](msg Msg) domi.Attr {
	return domi.On("submit", constant(msg))
}

// Input responds to "input" events on text fields and text areas.
// It calls f with the new value and delivers the resulting message.
func Input[Msg any](f func(value string) Msg) domi.Attr {
	return domi.On("input", targetField("value", f), []string{"target", "value"})
}

// Check responds to "change" events on checkboxes and radio buttons.
// It calls f with the new value and delivers the resulting message.
func Check[Msg any](f func(checked bool) Msg) domi.Attr {
	return domi.On("change", targetField("checked", f), []string{"target", "checked"})
}

// constant adapts a fixed msg to domi.On's unmarshal function.
func constant[Msg any](msg Msg) func(jsontext.Value) (Msg, error) {
	return func(jsontext.Value) (Msg, error) { return msg, nil }
}

// targetField adapts a function of one target field to domi.On's
// unmarshal function. The client sends only the requested paths, so
// target holds exactly the named field.
func targetField[T, Msg any](field string, f func(T) Msg) func(jsontext.Value) (Msg, error) {
	return func(v jsontext.Value) (Msg, error) {
		var e struct {
			Target map[string]T `json:"target"`
		}
		err := json.Unmarshal(v, &e)
		return f(e.Target[field]), err
	}
}

// Package event provides prebound constructors for common DOM event
// handlers: event.Click, event.Submit, event.Input, event.Change, and
// event.Check.
package event

import "ily.dev/domi"

var (
	Click  = domi.On("click")
	Submit = domi.On("submit")

	// Input captures the target's value on each edit, Change on a
	// committed change; Check captures its checked state on a change
	// (checkboxes and radios).
	Input  = domi.On("input", []string{"target", "value"})
	Change = domi.On("change", []string{"target", "value"})
	Check  = domi.On("change", []string{"target", "checked"})
)

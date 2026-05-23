// Package event provides prebound constructors for common DOM event
// handlers. Each is a partial application of [domi.On]; the "On"
// prefix is omitted because the package name already conveys it —
// event.Click, event.Submit, event.Input.
package event

import "ily.dev/domi"

var (
	Click  = domi.On("click")
	Submit = domi.On("submit")
	Input  = domi.On("input")
)

// Package event provides convenience wrappers around domi.On for
// common DOM event handlers. The "On" prefix is omitted because the
// package name already conveys it: event.Click, event.Submit, event.Input.
package event

import "ily.dev/domi"

func Click[Msg any](msg Msg) domi.Attr  { return domi.On("click", msg) }
func Submit[Msg any](msg Msg) domi.Attr { return domi.On("submit", msg) }
func Input[Msg any](msg Msg) domi.Attr  { return domi.On("input", msg) }

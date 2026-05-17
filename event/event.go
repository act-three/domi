// Package event provides convenience wrappers around domi.On for
// common DOM event handlers. The "On" prefix is omitted because the
// package name already conveys it: event.Click, event.Submit, event.Input.
package event

import "ily.dev/domi"

func Click(msg any) domi.Attr  { return domi.On("click", msg) }
func Submit(msg any) domi.Attr { return domi.On("submit", msg) }
func Input(msg any) domi.Attr  { return domi.On("input", msg) }

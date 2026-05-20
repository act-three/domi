// Package event provides convenience constructors for common DOM event
// handlers. Each function is a thin wrapper around [domi.On]; the "On"
// prefix is omitted because the package name already conveys it —
// event.Click, event.Submit, event.Input.
package event

import "ily.dev/domi"

// Click binds msg to the element's "click" event.
func Click(msg any) domi.Attr { return domi.On("click", msg) }

// Submit binds msg to the element's "submit" event.
func Submit(msg any) domi.Attr { return domi.On("submit", msg) }

// Input binds msg to the element's "input" event.
func Input(msg any) domi.Attr { return domi.On("input", msg) }

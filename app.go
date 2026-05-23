package domi

import (
	"context"
	"iter"
	"slices"
)

// App is the state machine provided by a domi application.
// One instance holds the state for a single browser sesssion.
// See [Handler] for session lifecycle.
type App[Msg any] interface {
	// Update is responsible for updating the App state
	// in response to each Msg. It must not produce external
	// side-effects, only update its internal state.
	//
	// For external side-effects, such as database writes,
	// Update should return a [Cmd].
	Update(Msg) Cmd[Msg]

	// View returns the document title and body tree
	// to be displayed in the browser.
	View() (title string, n Node)
}

// A Cmd is a deferred side-effect that eventually produces a Msg.
// The framework runs each Cmd in its own goroutine
// and passes the resulting Msg back into Update.
type Cmd[Msg any] struct {
	s iter.Seq[func(context.Context) Msg]
}

// Func returns a [Cmd] that runs fn and dispatches its result back
// into Update.
func Func[Msg any](f func(context.Context) Msg) Cmd[Msg] {
	return Cmd[Msg]{slices.Values([]func(context.Context) Msg{f})}
}

// Batch returns a [Cmd] that runs each item in c concurrently.
// The resulting [Msg] values are dispatched to Update serially.
func Batch[Msg any](c ...Cmd[Msg]) Cmd[Msg] {
	return Cmd[Msg]{
		func(yield func(func(context.Context) Msg) bool) {
			for _, c := range c {
				for c := range c.s {
					if !yield(c) {
						return
					}
				}
			}
		},
	}
}

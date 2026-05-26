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
	// The context carries the session ID (see [SessionID])
	// and is cancelled when the session ends.
	//
	// For external side-effects, such as database writes,
	// Update should return a [Cmd].
	Update(context.Context, Msg) Cmd[Msg]

	// View returns the document title and body tree
	// to be displayed in the browser.
	//
	// The context carries the session ID (see [SessionID])
	// and is cancelled when the session ends.
	View(context.Context) (title string, n Node)
}

// A Cmd is a deferred side-effect that eventually produces a Msg.
// The framework runs each Cmd in its own goroutine
// and passes the resulting Msg back into Update.
type Cmd[Msg any] struct {
	s iter.Seq[func() Msg]
}

// Func returns a [Cmd] that runs fn and dispatches its result back
// into Update.
//
// The app should capture the context
// from [Update] or the [Handler] constructor for f to use.
func Func[Msg any](f func() Msg) Cmd[Msg] {
	return Cmd[Msg]{slices.Values([]func() Msg{f})}
}

// Batch returns a [Cmd] that runs each item in c concurrently.
// The resulting [Msg] values are dispatched to Update serially.
func Batch[Msg any](c ...Cmd[Msg]) Cmd[Msg] {
	return Cmd[Msg]{
		func(yield func(func() Msg) bool) {
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

package domi

import (
	"context"
	"iter"
	"net/url"
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
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	//
	// For external side-effects, such as database writes,
	// Update should return a [Cmd].
	Update(context.Context, Msg) Cmd[Msg]

	// View returns the document title and body tree
	// to be displayed in the browser.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	View(context.Context) (title string, n Node)

	// Subscriptions returns the set of active subscriptions.
	// The framework diffs this set between update cycles.
	// New subscriptions are connected to the App,
	// absent subscriptions are canceled.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	Subscriptions(context.Context) Sub[Msg]

	// Preview returns the document title and body tree
	// that would be displayed if the browser navigated
	// to the given URL, and whether the navigation is allowed.
	// It must not modify the App's state.
	//
	// The framework calls Preview to pre-render pages
	// the user is likely to visit (e.g. on link hover),
	// so navigation appears instant when the link is clicked.
	// When ok is false the framework discards the title and view
	// and the user's click falls back to normal navigation,
	// giving the app a chance to deny or redirect.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	Preview(context.Context, *url.URL) (title string, n Node, ok bool)
}

// A Cmd is a deferred side-effect that eventually produces a Msg.
// The framework runs each Cmd in its own goroutine
// and passes the resulting Msg back into Update.
type Cmd[Msg any] struct {
	s iter.Seq[cmd[Msg]]
}

// cmd is the internal function type of a [Cmd].
// It receives the session for access to framework state
// (e.g. the onURLChange callback for navigation commands)
// and returns a Msg to dispatch through Update
// and an optional [nav] describing a navigation side-effect
// the framework should apply alongside the Msg.
type cmd[Msg any] func(*session[Msg]) (Msg, *nav)

// Func returns a [Cmd] that runs fn and dispatches its result back
// into Update.
//
// The app should capture the context
// from [Update] or the [Handler] constructor for f to use.
func Func[Msg any](f func() Msg) Cmd[Msg] {
	return Cmd[Msg]{slices.Values([]cmd[Msg]{
		func(*session[Msg]) (Msg, *nav) {
			return f(), nil
		},
	})}
}

// Batch returns a [Cmd] that runs each item in c concurrently.
// The resulting Msg values are dispatched to Update serially.
func Batch[Msg any](c ...Cmd[Msg]) Cmd[Msg] {
	return Cmd[Msg]{
		func(yield func(cmd[Msg]) bool) {
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

// A Sub is a long-lived event source
// that produces Msg values in response to external stimuli.
// The zero value of Sub is a valid Sub that emits no messages.
type Sub[Msg any] struct{ s []sub[Msg] }

type sub[Msg any] struct {
	key    any
	events func(context.Context) iter.Seq[Msg]
}

// Subscription creates a [Sub] that runs f and dispatches
// each yielded Msg back into Update.
// The framework uses key to identify this subscription.
// If a key persists between update cycles, the source stays alive.
// If it disappears, the source is cancelled.
//
// The Seq returned from f must exit when its context becomes done,
// in addition to exiting when yield returns false.
func Subscription[Msg any, Key comparable](key Key, f func(context.Context) iter.Seq[Msg]) Sub[Msg] {
	return Sub[Msg]{s: []sub[Msg]{{key: key, events: f}}}
}

// Subs composes multiple [Sub] values into one.
func Subs[Msg any](ss ...Sub[Msg]) Sub[Msg] {
	var all []sub[Msg]
	for _, s := range ss {
		all = append(all, s.s...)
	}
	return Sub[Msg]{s: all}
}

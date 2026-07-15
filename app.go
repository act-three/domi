package domi

import (
	"context"
	"fmt"
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
	// side-effects. It must only mutate its internal state
	// (which can include persistent databases).
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	//
	// For external side-effects such as network I/O,
	// Update should return a Cmd.
	Update(context.Context, Msg) Cmd[Msg]

	// View returns the document title and body tree
	// to be displayed in the browser.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	View(context.Context) (title string, n Node)

	// Subscriptions returns the set of active subscriptions.
	// Domi diffs this set between update cycles.
	// New subscriptions are connected to the App,
	// absent subscriptions are canceled.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	Subscriptions(context.Context) Sub[Msg]

	// Preview returns the result of a potential navigation.
	// This result comprises the destination URL dest, title, and body n.
	// Preview must not modify the App's state.
	//
	// The call to Preview represents a hypothetical
	// onURLRequest call from the browser.
	// If Preview returns a nonempty dest value,
	// it must be the same as the app uses for the PushURL command
	// it issues in response to the URL request.
	// The value for n must be the same as what View returns
	// after a navigation to dest.
	//
	// An empty dest denotes that there is no preview available.
	// It is always safe to decline to provide a preview.
	// This method is an optimization only.
	// Domi calls Preview to pre-render pages
	// the user is likely to visit (e.g. on link hover),
	// so navigation appears instant when the link is clicked.
	//
	// The context carries the session ID (see SessionID)
	// as well as values from the HTTP request context, if any.
	// It is cancelled when the session ends.
	Preview(context.Context, *url.URL) (dest, title string, n Node)
}

// A Cmd is an operation with side-effects.
// It may perform network I/O
// or cause changes in browser state such as the location bar.
//
// Domi runs each Cmd in its own goroutine.
// When the Cmd completes, it produces a Msg describing its result,
// which domi provides to the app's Update method.
//
// A nil Cmd is a valid command that never executes.
// It means "there is no command to run".
type Cmd[Msg any] interface {
	isCmd()
}

// batch is the lowered form of a [Cmd]: a sequence of command
// functions that domi spawns concurrently.
type batch[Msg any] iter.Seq[cmd[Msg]]

func (batch[Msg]) isCmd() {}

// cmd is the internal function type of a [Cmd].
// It receives the session for access to framework state
// (e.g. the onURLChange callback for navigation commands)
// and returns a Msg to dispatch through Update
// and an optional [nav] describing a navigation side-effect
// domi should apply alongside the Msg.
type cmd[Msg any] func(*session[Msg]) (Msg, *nav)

// Func returns a Cmd that runs f.
func Func[Msg any](f func() Msg) Cmd[Msg] {
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(*session[Msg]) (Msg, *nav) {
			return f(), nil
		},
	}))
}

// Batch returns a Cmd that runs all the given Cmd values concurrently.
func Batch[Msg any](c ...Cmd[Msg]) Cmd[Msg] {
	return batch[Msg](func(yield func(cmd[Msg]) bool) {
		for _, c := range c {
			switch v := c.(type) {
			case nil:
				// A nil Cmd contributes nothing, like an empty Batch.
			case batch[Msg]:
				for f := range v {
					if !yield(f) {
						return
					}
				}
			default:
				panic(fmt.Sprintf("domi: cannot lower %T", c))
			}
		}
	})
}

// A Sub is a long-lived event source
// that produces Msg values in response to external stimuli.
//
// A nil Sub is a valid subscription that produces no messages.
type Sub[Msg any] interface {
	isSub()
}

// subs is the lowered form of a [Sub]: a flat set of event sources
// that domi reconciles between update cycles.
type subs[Msg any] []sub[Msg]

func (subs[Msg]) isSub() {}

type sub[Msg any] struct {
	key    any
	events func(context.Context) iter.Seq[Msg]
}

// Subscription creates a [Sub] that runs f and dispatches
// each yielded Msg back into Update.
// Domi uses key to identify this subscription.
// If a key persists between update cycles, the source stays alive.
// If it disappears, the source is cancelled.
//
// The Seq returned from f must exit when its context becomes done,
// in addition to exiting when yield returns false.
func Subscription[Msg any, Key comparable](key Key, f func(context.Context) iter.Seq[Msg]) Sub[Msg] {
	return subs[Msg]{{key: key, events: f}}
}

// Subs composes multiple [Sub] values into one.
func Subs[Msg any](s ...Sub[Msg]) Sub[Msg] {
	var all subs[Msg]
	for _, s := range s {
		switch v := s.(type) {
		case nil:
			// A nil Sub contributes nothing, like an empty Subs.
		case subs[Msg]:
			all = append(all, v...)
		default:
			panic(fmt.Sprintf("domi: cannot lower %T", s))
		}
	}
	return all
}

package domi

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"slices"
)

// App is the state machine provided by a domi application.
// One instance holds the state for a single browser page load.
// See [NewServer] for instance lifecycle.
//
// The context given to each method contains the instance ID (see [InstanceID])
// as well as values from the HTTP request context, if any.
// It is cancelled when the instance ends.
type App[Msg any] interface {
	// Update is responsible for updating the App state
	// in response to each Msg.
	//
	// Update should avoid long-running work and operations
	// that take unknown amounts of time, such as network I/O.
	// For these cases, Update should return a Cmd.
	Update(context.Context, Msg) Cmd[Msg]

	// View returns the document title and HTML contents
	// to be displayed in the browser.
	View(context.Context) (title string, n Node)

	// Subscriptions returns the set of active subscriptions.
	Subscriptions(context.Context) Sub[Msg]

	// Preview returns the result of a potential navigation.
	//
	// Preview must not modify the App state.
	//
	// The call to Preview represents a hypothetical
	// onURLRequest call from the browser. If Preview returns
	// a nonempty dest value, it must equal the value the app
	// would use for the PushURL command it issues in response
	// to the URL request. The value for n should be the same as
	// that returned by View after a navigation to dest.
	//
	// An empty dest denotes that there is no preview available.
	// It is always safe to decline to provide a preview.
	// This method is an optimization only. Domi calls Preview
	// to pre-render pages the user is likely to visit (e.g. on
	// link hover), so navigation appears instant when the link
	// is clicked.
	Preview(context.Context, *url.URL) (dest, title string, n Node)
}

// A Cmd is an arbitrary operation.
// It may do network I/O,
// perform long-running work,
// or cause changes in browser state such as the location bar.
// When a Cmd completes, it produces a Msg describing its result,
// which domi provides to the app's Update method.
//
// Domi runs each Cmd in its own goroutine.
//
// A nil Cmd is a valid command with no effect.
type Cmd[Msg any] interface {
	isCmd()
}

// batch is the lowered form of a [Cmd]: a sequence of commands
// that domi spawns concurrently.
type batch[Msg any] iter.Seq[cmd[Msg]]

func (batch[Msg]) isCmd() {}

// cmd is the lowered form of a single command:
// a Msg-producing function, or a navigation.
type cmd[Msg any] struct {
	f   func() Msg
	nav *nav
}

// Func returns a Cmd that calls f.
func Func[Msg any](f func() Msg) Cmd[Msg] {
	return batch[Msg](slices.Values([]cmd[Msg]{{f: f}}))
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

// MapCmd converts a Cmd[T] into a Cmd[Msg].
//
// It calls f to convert each message of type T
// to a message of type Msg.
func MapCmd[T, Msg any](f func(T) Msg, c Cmd[T]) Cmd[Msg] {
	if f == nil {
		panic("domi: MapCmd called with a nil function")
	}
	return batch[Msg](iterMap(iter.Seq[cmd[T]](Batch[T](c).(batch[T])), func(c cmd[T]) cmd[Msg] {
		if c.f == nil {
			return cmd[Msg]{nav: c.nav}
		}
		return cmd[Msg]{f: func() Msg { return f(c.f()) }}
	}))
}

// A Sub produces a sequence of Msg values,
// which domi provides to the app's Update method.
//
// A nil Sub is valid and produces no messages.
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

// Subscription returns a Sub that calls f to produce Msg values.
//
// Domi uses key to identify this subscription.
// If a key remains present in subsequent calls to [App.Subscriptions],
// the existing sequence is retained
// and f is not called again.
// If the key is absent,
// the sequence is discarded
// and its context is cancelled.
//
// The sequence returned from f must exit when its context becomes done,
// in addition to exiting when yield returns false.
func Subscription[Msg any, Key comparable](key Key, f func(context.Context) iter.Seq[Msg]) Sub[Msg] {
	return subs[Msg]{{key: key, events: f}}
}

// Subs composes multiple Sub values into one.
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

// MapSub converts a Sub[T] to a Sub[Msg].
//
// It calls f to convert each message of type T
// to a message of type Msg.
func MapSub[T, Msg any](f func(T) Msg, s Sub[T]) Sub[Msg] {
	if f == nil {
		panic("domi: MapSub called with a nil function")
	}
	all := Subs[T](s).(subs[T])
	mapped := make(subs[Msg], len(all))
	for i, s := range all {
		mapped[i] = sub[Msg]{
			key: s.key,
			events: func(ctx context.Context) iter.Seq[Msg] {
				return iterMap(s.events(ctx), f)
			},
		}
	}
	return mapped
}

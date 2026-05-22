package domi

import "context"

// App is the state machine a domi application implements. Implementations
// carry their own state (typically as fields on a pointer receiver); the
// framework owns the instance for the lifetime of a session and calls
// Update, View, and Title sequentially, so internal state needs no
// concurrency guard.
//
// Update is called for each dispatched Msg and may return a [Cmd] to
// produce follow-up Msgs. View is called after every Update; its return
// value is the source of truth for what the browser displays. Title
// returns the document title.
//
// Sessions are bootstrapped by the constructor passed to [Handler],
// which returns the initial App together with an initial [Cmd].
type App[Msg any] interface {
	Update(msg Msg) Cmd[Msg]
	View() Node
	Title() string
}

// Cmd is a deferred side-effect that eventually produces a Msg. Cmds
// are returned by an App's constructor and by [App.Update]; the
// framework runs each in its own goroutine and feeds the resulting Msg
// back into Update.
type Cmd[Msg any] struct {
	fns []func(context.Context) Msg
}

// CmdNone returns a [Cmd] that does nothing. Use it when there is no
// follow-up work to schedule.
func CmdNone[Msg any]() Cmd[Msg] { return Cmd[Msg]{} }

// CmdFn returns a [Cmd] that runs fn and dispatches its result back
// into Update. The context is cancelled when the session ends; fn
// should respect it for any blocking or long-running work.
func CmdFn[Msg any](fn func(context.Context) Msg) Cmd[Msg] {
	return Cmd[Msg]{fns: []func(context.Context) Msg{fn}}
}

// CmdBatch returns a [Cmd] that runs each input Cmd concurrently. The
// produced Msgs are dispatched to Update independently in whatever
// order they finish.
func CmdBatch[Msg any](cmds ...Cmd[Msg]) Cmd[Msg] {
	var out Cmd[Msg]
	for _, c := range cmds {
		out.fns = append(out.fns, c.fns...)
	}
	return out
}

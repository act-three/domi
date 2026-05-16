package domi

import "context"

// App is a TEA-shaped application. Implementations carry their own state
// (typically as fields on a pointer receiver) and the framework calls Update,
// View, Title sequentially per session — no concurrent access.
type App[Msg any] interface {
	Init() Cmd[Msg]
	Update(msg Msg) Cmd[Msg]
	View() Node
	Title() string
}

// Cmd is a deferred side-effect that eventually produces a Msg.
type Cmd[Msg any] struct {
	fns []func(context.Context) Msg
}

func CmdNone[Msg any]() Cmd[Msg] { return Cmd[Msg]{} }

func CmdFn[Msg any](fn func(context.Context) Msg) Cmd[Msg] {
	return Cmd[Msg]{fns: []func(context.Context) Msg{fn}}
}

func CmdBatch[Msg any](cmds ...Cmd[Msg]) Cmd[Msg] {
	var out Cmd[Msg]
	for _, c := range cmds {
		out.fns = append(out.fns, c.fns...)
	}
	return out
}

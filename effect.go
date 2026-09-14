package domi

import (
	"context"
	"fmt"
	"reflect"
)

// Effect returns a command with the given effect e.
//
// The server must have an effect handler for type E.
// See [EffectHandler].
// When the command runs,
// the handler is called to perform the effect.
//
// If no handler for E exists, the command panics.
func Effect[Msg, E any](e E) Cmd[Msg] {
	t := effectTypeOf[E]()
	return batch[Msg]{{eff: &effect{t: t, v: e}}}
}

// EffectHandler uses f to perform effects of type E.
//
// Each effect runs in its own goroutine.
// The resulting message is sent to [App.Update].
//
// The context contains the instance ID (see [InstanceID])
// and is cancelled when the instance ends.
func EffectHandler[E, Msg any](f func(context.Context, E) Msg) Option {
	if f == nil {
		panic("domi: EffectHandler called with a nil function")
	}
	return optionEffectHandler[Msg]{
		eff: effectTypeOf[E](),
		run: func(ctx context.Context, e any) Msg {
			return f(ctx, e.(E))
		},
	}
}

type effect struct {
	t reflect.Type
	v any
}

type effectTable[Msg any] map[reflect.Type]func(context.Context, any) Msg

type optionEffectHandlerOther interface{ types() (effect, msg string) }

type optionEffectHandler[Msg any] struct {
	eff reflect.Type
	run func(context.Context, any) Msg
}

func (optionEffectHandler[Msg]) isOption() {}

func (o optionEffectHandler[Msg]) types() (effect, msg string) {
	return o.eff.String(), reflect.TypeFor[Msg]().String()
}

func effectTypeOf[E any]() reflect.Type {
	t := reflect.TypeFor[E]()
	if t.Kind() == reflect.Interface {
		panic(fmt.Sprintf("domi: effect type %v must be concrete", t))
	}
	return t
}

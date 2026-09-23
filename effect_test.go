package domi

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
)

type testEffect struct{ value string }

type effectApp struct {
	App[string]
	received chan string
}

func (a *effectApp) Update(_ context.Context, m string) Cmd[string] {
	a.received <- m
	return nil
}

func (*effectApp) View(context.Context) (string, Node)       { return "", Text("effects") }
func (*effectApp) Subscriptions(context.Context) Sub[string] { return nil }

func TestEffectDispatch(t *testing.T) {
	for _, prefix := range []string{"first:", "second:"} {
		t.Run(prefix, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := &effectApp{received: make(chan string, 3)}
				s := newTestInstance(a)
				defer s.cancel()
				s.sv = NewServer(func(context.Context, *url.URL) (*effectApp, Cmd[string]) {
					return a, nil
				}, func(*url.URL) string { return "" }, func(*url.URL) string { return "" },
					EffectHandler(func(ctx context.Context, e testEffect) string {
						if ctx != s.ctx {
							t.Error("handler received the wrong instance context")
						}
						return prefix + e.value
					}),
				)
				c := Batch(nil, Effect[int](testEffect{"saved"}),
					Batch(Func(func() int { return 7 }), Effect[int](testEffect{"again"})))
				c = MapCmd(func(n int) int { return n + 1 }, c)
				s.spawn(MapCmd(strconv.Itoa, c))
				synctest.Wait()
				var got []string
				for range 3 {
					select {
					case m := <-a.received:
						got = append(got, m)
					default:
						t.Fatal("command message was not delivered")
					}
				}
				slices.Sort(got)
				want := []string{"8", prefix + "again", prefix + "saved"}
				if !slices.Equal(got, want) {
					t.Fatalf("messages = %q, want %q", got, want)
				}
			})
		})
	}
}

func TestEffectRegistration(t *testing.T) {
	for _, tt := range []struct {
		name, want string
		f          func()
	}{
		{"nil handler", "nil function", func() { EffectHandler[testEffect, string](nil) }},
		{"interface effect", "must be concrete", func() { Effect[int, any](testEffect{}) }},
		{"interface handler", "must be concrete", func() {
			EffectHandler(func(context.Context, any) int { return 0 })
		}},
		{"message type", "invalid msg type int", func() {
			NewServer(func(context.Context, *url.URL) (*effectApp, Cmd[string]) {
				return &effectApp{}, nil
			}, func(*url.URL) string { return "" }, func(*url.URL) string { return "" },
				EffectHandler(func(context.Context, testEffect) int { return 0 }),
			)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want %q", r, tt.want)
				}
			}()
			tt.f()
		})
	}
}

func TestEffectReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &effectApp{received: make(chan string, 1)}
		s := newTestInstance(a)
		defer s.cancel()
		s.sv = NewServer(func(context.Context, *url.URL) (*effectApp, Cmd[string]) {
			return a, nil
		}, func(*url.URL) string { return "" }, func(*url.URL) string { return "" },
			EffectHandler(func(_ context.Context, e testEffect) string { return "first:" + e.value }),
			EffectHandler(func(_ context.Context, e testEffect) string { return "second:" + e.value }),
		)
		s.spawn(Effect[string](testEffect{"saved"}))
		synctest.Wait()
		select {
		case got := <-a.received:
			if got != "second:saved" {
				t.Fatalf("message = %q, want %q", got, "second:saved")
			}
		default:
			t.Fatal("command message was not delivered")
		}
	})
}

func TestEffectNilPointer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &effectApp{received: make(chan string, 1)}
		s := newTestInstance(a)
		defer s.cancel()
		s.sv = NewServer(func(context.Context, *url.URL) (*effectApp, Cmd[string]) {
			return a, nil
		}, func(*url.URL) string { return "" }, func(*url.URL) string { return "" },
			EffectHandler(func(_ context.Context, e *testEffect) string {
				if e != nil {
					t.Error("nil effect was not preserved")
				}
				return "nil"
			}),
		)
		s.spawn(Effect[string]((*testEffect)(nil)))
		synctest.Wait()
		select {
		case <-a.received:
		default:
			t.Fatal("nil effect was not dispatched")
		}
	})
}

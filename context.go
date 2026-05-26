package domi

import "context"

type sessionIDKey struct{}

// SessionID returns the session ID inside an [App] or [Cmd],
// otherwise the empty string.
func SessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDKey{}).(string)
	return v
}

// mergedContext combines cancellation and values from a base context
// with additional values from a second context. Value lookups check
// the values context first, then fall through to the base.
type mergedContext struct {
	context.Context                 // cancellation + deadline + base values
	values          context.Context // additional values
}

func (c mergedContext) Value(key any) any {
	if v := c.values.Value(key); v != nil {
		return v
	}
	return c.Context.Value(key)
}

package domi

import "context"

type sessionIDKey struct{}

// SessionID returns the session ID inside a [Cmd],
// otherwise the empty string.
func SessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDKey{}).(string)
	return v
}

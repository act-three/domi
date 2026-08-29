package domi

import "context"

type instanceIDKey struct{}

// InstanceID returns the instance ID if present in ctx.
// An instance ID is present in [App] methods
// and the constructor given to [NewServer].
func InstanceID(ctx context.Context) string {
	v, _ := ctx.Value(instanceIDKey{}).(string)
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

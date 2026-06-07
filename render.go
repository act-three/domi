package domi

import (
	"io"

	"ily.dev/domi/internal/vdom"
)

// RenderTo writes the HTML for n to w.
//
// The output is static.
// Event handlers attached with [On] render as attributes but are inert.
// RenderTo is suitable only for static pages and node-level tests.
//
// It is valid to use any Node for n,
// including text, a fragment with multiple items, or an empty fragment.
// The only errors returned are from w.
func RenderTo(w io.Writer, n Node) error {
	nodes, _ := lower(0, n)
	for _, v := range nodes {
		if err := vdom.RenderTo(w, v); err != nil {
			return err
		}
	}
	return nil
}

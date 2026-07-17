package domi

import (
	"io"

	"ily.dev/domi/internal/vdom"
)

// RenderTo writes the HTML for n to w.
//
// The output is static.
// Event handlers (see [On]) are inert.
// RenderTo is suitable only for static pages and tests.
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

// Package attr provides prebound constructors for common HTML
// attributes. Each is a partial application of [domi.Name].
//
// Attribute combining rules (class joining, style joining, first-wins
// for others) are described on [domi.Attr].
package attr

import (
	"fmt"

	"ily.dev/domi"
)

var (
	Class       = domi.Name("class")
	ID          = domi.Name("id")
	Style       = domi.Name("style")
	Href        = domi.Name("href")
	Type        = domi.Name("type")
	Placeholder = domi.Name("placeholder")
	Value       = domi.Name("value")
)

// Boolean attributes. Present means true, absent means false.
var (
	Autofocus = domi.Bool("autofocus")
	Checked   = domi.Bool("checked")
	Disabled  = domi.Bool("disabled")
	Hidden    = domi.Bool("hidden")
	Multiple  = domi.Bool("multiple")
	Open      = domi.Bool("open")
	Readonly  = domi.Bool("readonly")
	Required  = domi.Bool("required")
	Selected  = domi.Bool("selected")
)

// Stylef returns a style attribute formatted with [fmt.Sprintf].
func Stylef(format string, a ...any) domi.Attr {
	return Style(fmt.Sprintf(format, a...))
}

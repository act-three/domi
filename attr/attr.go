// Package attr provides prebound constructors for common HTML
// attributes. Each is a partial application of [domi.Name].
//
// Attribute combining rules (class joining, style joining, first-wins
// for others) are described on [domi.Attr].
package attr

import "ily.dev/domi"

var (
	Class       = domi.Name("class")
	ID          = domi.Name("id")
	Style       = domi.Name("style")
	Href        = domi.Name("href")
	Type        = domi.Name("type")
	Placeholder = domi.Name("placeholder")
	Value       = domi.Name("value")
)

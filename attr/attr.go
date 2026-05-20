// Package attr provides convenience constructors for common HTML
// attributes. Each function is a thin wrapper around [domi.Attribute].
package attr

import "ily.dev/domi"

// Class returns a class="..." attribute. When multiple Class attributes
// appear on the same element their values are joined with a single space.
func Class(s string) domi.Attr { return domi.Attribute("class", s) }

// ID returns an id="..." attribute.
func ID(s string) domi.Attr { return domi.Attribute("id", s) }

// Style returns a style="..." attribute. When multiple Style attributes
// appear on the same element their values are joined with a semicolon.
func Style(s string) domi.Attr { return domi.Attribute("style", s) }

// Href returns an href="..." attribute.
func Href(s string) domi.Attr { return domi.Attribute("href", s) }

// Type returns a type="..." attribute.
func Type(s string) domi.Attr { return domi.Attribute("type", s) }

// Placeholder returns a placeholder="..." attribute.
func Placeholder(s string) domi.Attr { return domi.Attribute("placeholder", s) }

// Value returns a value="..." attribute.
func Value(s string) domi.Attr { return domi.Attribute("value", s) }

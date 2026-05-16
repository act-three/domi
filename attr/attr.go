// Package attr provides convenience wrappers around domi.Attribute for
// common HTML attributes.
package attr

import "ily.dev/domi"

func Class(s string) domi.Attr       { return domi.Attribute("class", s) }
func ID(s string) domi.Attr          { return domi.Attribute("id", s) }
func Style(s string) domi.Attr       { return domi.Attribute("style", s) }
func Href(s string) domi.Attr        { return domi.Attribute("href", s) }
func Type(s string) domi.Attr        { return domi.Attribute("type", s) }
func Placeholder(s string) domi.Attr { return domi.Attribute("placeholder", s) }
func Value(s string) domi.Attr       { return domi.Attribute("value", s) }

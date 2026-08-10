package domi

import (
	"fmt"
	"iter"
	"strings"

	"ily.dev/domi/internal/vdom"
)

// Bypass annotates a link to use the browser's built-in navigation,
// rather than being intercepted by domi.
var Bypass Attr = Name("domi-bypass")

// An Attr is an HTML attribute.
//
// A given attribute name does not appear more than once
// in the rendered output for an element.
// When an attribute is declared more than once:
//
//   - If it is a combining attribute,
//     domi combines the declared values into a single value.
//     See [RegisterCombining].
//   - Otherwise, only the first occurrence appears.
//
// For instance:
//
//	Tag("div", Name("class", "a"), Name("class", "b")) // <div class="a b">
//	Tag("div", Name("value", "a"), Name("value", "b")) // <div value="a">
//
// A nil Attr is a valid Attr that emits nothing.
type Attr interface {
	isAttr()
}

// attr is the normalized form of an [Attr]: the [vdom.Attr] to render
// plus the event handler it carries, if any. A handler attr's value is
// assigned during lowering, when its element's address is known.
type attr struct {
	attr    vdom.Attr
	handler *handler
}

func (attr) isAttr() {}

// isReservedAttr returns whether name is reserved for internal use only.
func isReservedAttr(name string) bool {
	return strings.HasPrefix(name, "domi-") && name != "domi-bypass"
}

// Name returns an HTML attribute with the given name and value.
// Helpers for common attributes can be found in [ily.dev/domi/attr].
//
// Providing no value arguments or the empty string
// produces a name-only attribute.
// Name(s) is equivalent to Name(s, "").
//
//	Name("value")      // value
//	Name("value", "")  // value
//	Name("value", "a") // value="a"
//
// Providing multiple value arguments produces multiple attribute declarations.
// Name(s, a, b, ...) is equivalent to Group(Name(s, a), Name(s, b), ...).
// These combine using the same rules described on [Attr] and [RegisterCombining].
// In particular, for most attributes, only the first value will be used.
//
//	Name("value", "a")      // value="a"
//	Name("value", "a", "b") // value="a"
//	Name("class", "a")      // class="a"
//	Name("class", "a", "b") // class="a b"
//
// The given name must be lowercase,
// except for foreign-content (SVG and MathML) mixed-case names
// like viewBox.
//
// This package defines a custom attribute ([Bypass]) for use by applications.
// Its name has the prefix "domi-".
// Other attribute names with that prefix are reserved.
//
// If name is invalid or reserved, Name panics.
func Name(name string, value ...string) Attr {
	mustValidAttrName(name)
	if isReservedAttr(name) {
		panic(fmt.Sprintf("domi: attribute %s is reserved", name))
	}
	switch len(value) {
	case 0:
		return attr{vdom.Attr{Name: name}, nil}
	case 1:
		return attr{vdom.Attr{Name: name, Value: value[0]}, nil}
	}
	var a []Attr
	for _, v := range value {
		a = append(a, attr{vdom.Attr{Name: name, Value: v}, nil})
	}
	return Group(a...)
}

// group is the lowered form of a [Group]: a sequence of attrs that
// splats into a parent's attribute list.
type group iter.Seq[attr]

func (group) isAttr() {}

// A Group is a sequence of HTML attributes.
func Group(a ...Attr) Attr {
	return group(func(yield func(attr) bool) {
		for _, a := range a {
			switch v := a.(type) {
			case nil:
				// A nil Attr contributes nothing, like an empty Group.
			case attr:
				if !yield(v) {
					return
				}
			case group:
				for inner := range v {
					if !yield(inner) {
						return
					}
				}
			default:
				panic(fmt.Sprintf("domi: cannot lower %T", a))
			}
		}
	})
}

// RegisterCombining registers name as a "combining" attribute.
// When a combining attribute appears more than once in an HTML node,
// the values are combined, separated by sep,
// into a single attribute in the rendered output.
//
// RegisterCombining must be called before Handler.
// This is typically done in an init function in packages
// that define custom attributes.
//
// The initial set of combining attributes is:
//
//	name  sep
//	class " "
//	style ";"
func RegisterCombining(name, sep string) {
	vdom.RegisterCombining(name, sep)
}

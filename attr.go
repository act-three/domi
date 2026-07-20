package domi

import (
	"fmt"
	"iter"
	"strings"

	"ily.dev/domi/internal/vdom"
)

// Bypass annotates a link to use the browser's built-in navigation,
// rather than being intercepted by domi.
var Bypass Attr = Bool("domi-bypass")(true)

// An Attr is an HTML attribute.
//
// In rendered output,
// a single attribute name does not appear more than once
// on any given element:
//
//  1. For each combining attribute,
//     domi combines the values into a single value.
//     See [RegisterCombining] for more.
//  2. Event handlers are combined internally.
//  3. For all other attributes,
//     only the first occurrence appears.
//
// Attribute names must be lowercase,
// except for foreign-content (SVG and MathML) mixed-case names
// like viewBox.
//
// Package domi defines custom attributes for use by applications,
// all with names that begin with "domi-".
// Other attribute names with that prefix are reserved.
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

// Name constructs an HTML attribute with the given name and value.
//
// Providing the empty string or zero value arguments
// produces a name-only attribute.
// Name(s)() is equivalent to Name(s)("").
//
//	Name("value")()    // value
//	Name("value")("")  // value
//	Name("value")("a") // value="a"
//
// Providing multiple value arguments produces multiple attribute declarations.
// Name(s)(a, b, ...) is equivalent to Group(Name(s)(a), Name(s)(b), ...).
// These combine using the same rules described on [Attr] and [RegisterCombining].
// In particular, for most attributes, only the first value will be used.
//
//	Name("value")("a")      // value="a"
//	Name("value")("a", "b") // value="a"
//	Name("class")("a")      // class="a"
//	Name("class")("a", "b") // class="a b"
//	Name("style")("a")      // style="a"
//	Name("style")("a", "b") // style="a;b"
//
// If name is invalid or reserved, Name panics.
// See [Attr].
func Name(name string) func(value ...string) Attr {
	mustValidAttrName(name)
	if isReservedAttr(name) {
		panic(fmt.Sprintf("domi: attribute %s is reserved", name))
	}
	return func(value ...string) Attr {
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
}

// Bool constructs a boolean HTML attribute.
//
// For [boolean attributes] as defined in the HTML spec,
// such as disabled and checked,
// true emits a name-only attribute and false emits nothing:
//
//	Tag("input")(Bool("disabled")(true))  // <input disabled>
//	Tag("input")(Bool("disabled")(false)) // <input>
//
// For [enumerated attributes]
// with exactly two permitted values that mean true and false
// (such as autocorrect, draggable, spellcheck, and translate),
// Bool emits the keyword for the given state:
//
//	Bool("spellcheck")(true)   // spellcheck="true"
//	Bool("translate")(true)    // translate="yes"
//	Bool("autocorrect")(false) // autocorrect="off"
//
// For all other names, Bool panics.
//
// [enumerated attributes]: https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#keywords-and-enumerated-attributes
// [boolean attributes]: https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#boolean-attributes
func Bool(name string) func(bool) Attr {
	mustValidAttrName(name)
	if isReservedAttr(name) {
		panic(fmt.Sprintf("domi: attribute %s is reserved", name))
	}
	// TODO: maybe define RegisterBoolean and RegisterEnumerated.
	if booleanAttr[name] {
		return func(v bool) Attr {
			if v {
				return attr{vdom.Attr{Name: name}, nil}
			}
			return Group()
		}
	}
	if kw, ok := enumeratedBool[name]; ok {
		return func(v bool) Attr {
			if v {
				return attr{vdom.Attr{Name: name, Value: kw[0]}, nil}
			}
			return attr{vdom.Attr{Name: name, Value: kw[1]}, nil}
		}
	}
	panic(fmt.Sprintf("domi: attribute %s is not boolean", name))
}

// enumeratedBool is the set of HTML attributes
// that take exactly two keyword values meaning true and false,
// rather than using presence/absence semantics,
// mapped to those keywords in {true, false} order.
// These look boolean,
// but are technically enumerated attributes in the HTML spec.
// See https://html.spec.whatwg.org/multipage/indices.html#attributes-3.
var enumeratedBool = map[string][2]string{
	"autocorrect":        {"on", "off"},
	"draggable":          {"true", "false"},
	"spellcheck":         {"true", "false"},
	"translate":          {"yes", "no"},
	"writingsuggestions": {"true", "false"},
}

// booleanAttr is the set of boolean attributes,
// which represent true by presence and false by absence.
var booleanAttr = map[string]bool{
	// custom attrs
	"domi-bypass": true,

	// defined in the spec
	// (note that "hidden" is not listed.
	// it is an enumerated attribute with a third state, "until-found".)
	// see https://html.spec.whatwg.org/multipage/indices.html#attributes-3
	"allowfullscreen":                 true,
	"alpha":                           true,
	"async":                           true,
	"autofocus":                       true,
	"autoplay":                        true,
	"checked":                         true,
	"controls":                        true,
	"default":                         true,
	"defer":                           true,
	"disabled":                        true,
	"formnovalidate":                  true,
	"headingreset":                    true,
	"inert":                           true,
	"ismap":                           true,
	"itemscope":                       true,
	"loop":                            true,
	"multiple":                        true,
	"muted":                           true,
	"nomodule":                        true,
	"novalidate":                      true,
	"open":                            true,
	"playsinline":                     true,
	"readonly":                        true,
	"required":                        true,
	"reversed":                        true,
	"selected":                        true,
	"shadowrootclonable":              true,
	"shadowrootcustomelementregistry": true,
	"shadowrootdelegatesfocus":        true,
	"shadowrootserializable":          true,
}

// group is the lowered form of a [Group]: a sequence of attrs that
// splats into a parent's attribute list.
type group iter.Seq[attr]

func (group) isAttr() {}

// A Group is a sequence of HTML attributes.
// It contributes its contents
// to its parent's child list in order,
// as if they had been written there directly.
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

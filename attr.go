package domi

import (
	"fmt"
	"iter"

	"ily.dev/domi/internal/vdom"
)

var (
	// Bypass annotates a link to use the browser's built-in navigation,
	// rather than being intercepted by domi.
	Bypass Attr = Bool("data-domi-bypass")(true)

	// Opaque marks an element as opaque, ignored by the virtual DOM diff.
	// Such an element is inserted,
	// and then never modified until its eventual removal (if any).
	// Any changes to its contents during its existence are ignored.
	// This allows client-side browser code to take ownership of the element
	// without worrying about patches modifying it underfoot.
	//
	// An opaque element must be keyed.
	// See [WithKey].
	// Inserting an opaque node anywhere else panics.
	Opaque Attr = internal(vdom.Opaque)
)

// internal returns vdom "internal" attribute a as an Attr.
func internal(a vdom.Attr) Attr {
	return attr{attr: a}
}

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
func Name(name string) func(value ...string) Attr {
	// TODO: panic on boolean attributes.
	// See https://html.spec.whatwg.org/multipage/indices.html#attributes-3.
	// Maybe also define RegisterBoolean.
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
// For [enumerated attributes]
// with exactly two permitted values "true" and "false",
// (contenteditable, draggable, spellcheck, and translate),
// Bool emits a string value.
//
//	Bool("contenteditable")(true)  // contenteditable="true"
//	Bool("contenteditable")(false) // contenteditable="false"
//
// For all other names, including standard [boolean attributes]
// like disabled and checked,
// true emits a name-only attribute and false emits nothing:
//
//	Tag("input")(Bool("disabled")(true))  // <input disabled>
//	Tag("input")(Bool("disabled")(false)) // <input>
//
// [enumerated attributes]: https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#keywords-and-enumerated-attributes
// [boolean attributes]: https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#boolean-attributes
func Bool(name string) func(bool) Attr {
	// BUG: translate should produce "yes"/"no", NOT "true"/"false".
	// TODO: handle other enumerated bools ("yes"/"no" and "on"/"off").
	// See https://html.spec.whatwg.org/multipage/indices.html#attributes-3.
	// Maybe also define RegisterEnumerated.
	//
	// Future godoc:
	// For [enumerated attributes]
	// with exactly two permitted values that map to true and false
	// ("on"/"off", "yes"/"no", and "true"/"false"),
	// Bool emits a string value.
	// See [RegisterEnumerated].
	if enumeratedBool[name] {
		return func(v bool) Attr {
			if v {
				return attr{vdom.Attr{Name: name, Value: "true"}, nil}
			}
			return attr{vdom.Attr{Name: name, Value: "false"}, nil}
		}
	}
	return func(v bool) Attr {
		if v {
			return attr{vdom.Attr{Name: name}, nil}
		}
		return Group()
	}
}

// enumeratedBool is the set of HTML attributes that take the string
// values "true" and "false" rather than using presence/absence
// semantics. These look boolean but are technically enumerated
// attributes in the HTML spec.
var enumeratedBool = map[string]bool{
	"contenteditable": true,
	"draggable":       true,
	"spellcheck":      true,
	"translate":       true,
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

// hasOpaque reports whether attrs contains the [Opaque] marker,
// looking through nested groups. Unknown Attr implementations panic
// here, at construction, as they otherwise would when lowered.
func hasOpaque(attrs []Attr) bool {
	for a := range Group(attrs...).(group) {
		if a.attr == vdom.Opaque {
			return true
		}
	}
	return false
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

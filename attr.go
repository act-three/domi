package domi

import (
	"fmt"
	"iter"

	"ily.dev/domi/internal/vdom"
)

var (
	// Bypass annotates a link to use the browser's built-in navigation,
	// rather than going through the framework.
	Bypass = Bool("data-domi-bypass")

	// Opaque marks an element as opaque, ignored by the virtual DOM diff.
	// Such an element is inserted,
	// and then never modified until its eventual removal (if any).
	// Any changes to its contents during its existence are ignored.
	// This allows client-side browser code to take ownership of the element
	// without worrying about patches modifying it underfoot.
	//
	// An opaque element must be a keyed child. See [Keyed].
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
//     the framework combines the values into a single value.
//     See [RegisterCombining] for more.
//  2. Event handlers are combined internally.
//  3. For all other attributes,
//     only the first occurrence appears.
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

// Name returns a builder for an HTML attribute with the given name. (e.g. class).
// Call it to obtain an [Attr] with the given value (e.g. class="foo").
func Name(name string) func(value string) Attr {
	return func(value string) Attr {
		return attr{vdom.Attr{Name: name, Value: value}, nil}
	}
}

// Bool returns a builder for a boolean HTML attribute. For standard
// boolean attributes (disabled, checked, …), true means present
// (name-only) and false means absent:
//
//	Tag("input")(attr.Disabled(true))()   // <input disabled>
//	Tag("input")(attr.Disabled(false))()  // <input>
//
// For enumerated boolean attributes (contenteditable, draggable,
// spellcheck, translate), true and false emit the corresponding
// string value instead:
//
//	Tag("div")(attr.ContentEditable(true))()   // <div contenteditable="true">
//	Tag("div")(attr.ContentEditable(false))()  // <div contenteditable="false">
func Bool(name string) func(bool) Attr {
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
//
// Groups may be nested arbitrarily.
func Group(a ...Attr) Attr {
	return group(func(yield func(attr) bool) {
		for _, a := range a {
			switch v := a.(type) {
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

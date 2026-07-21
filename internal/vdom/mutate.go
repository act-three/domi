package vdom

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
)

// A ClientMutation is a change applied by a client:
// a structural move, or a form control's committed state.
// See [Apply].
//
// The set of ClientMutation operations
// is distinct from the patch ops emitted by Diff.
// Replay never writes a client-chosen attribute name,
// which would let a hostile client plant handler attributes
// or URLs in the server's tree.
type ClientMutation struct {
	Op     string `json:",omitempty"` // move, setvalue, settext, setchecked, setselected
	From   []Step `json:",omitempty"` // move: path to the moved element
	To     []Step `json:",omitempty"` // move: path to the moved element's destination
	Before string `json:",omitempty"` // move: anchor key in the dest container; "" means append

	// Path names the form control a control-state op targets.
	Path     []Step `json:",omitempty"`
	Value    string `json:",omitempty"` // setvalue, settext: the committed value
	Checked  bool   `json:",omitempty"` // setchecked: the committed checkedness
	Selected bool   `json:",omitempty"` // setselected: the committed selectedness
}

// A Step is one component of a ClientMutation path to a node.
// It can be a key (JSON string) or index (JSON number).
type Step struct {
	key   string // nonempty means keyed
	index int
}

// Index returns a positional path step.
func Index(i int) Step { return Step{index: i} }

// Key returns a keyed path step.
func Key(k string) Step { return Step{key: k} }

// UnmarshalJSONFrom decodes a step
// from a JSON string (a key) or number (an index).
func (s *Step) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case '"':
		s.key = tok.String()
	case '0':
		i, err := tok.Int()
		if err != nil {
			return err
		}
		s.index = int(i)
	default:
		return fmt.Errorf("vdom: mutation path step is a %q, want a key or index", tok.Kind())
	}
	return nil
}

// resolve returns the index of the child this step selects among
// nodes. A keyed step looks up the child carrying its key; a
// positional step is a direct index.
func (s Step) resolve(nodes []Node) (int, error) {
	if s.key != "" {
		if i := keyIndex(nodes, s.key); i >= 0 {
			return i, nil
		}
		return 0, fmt.Errorf("vdom: mutation path key %q not found", s.key)
	}
	if s.index < 0 || s.index >= len(nodes) {
		return 0, fmt.Errorf("vdom: mutation path index %d out of range %d", s.index, len(nodes))
	}
	return s.index, nil
}

// Apply applies muts onto roots,
// and returns the new root set that results.
// The trees in roots are unchanged.
//
// If a mutation can't be applied,
// for instance if it specifies a invalid path,
// Apply returns an error.
func Apply(roots []Node, muts []ClientMutation) ([]Node, error) {
	var err error
	for _, m := range muts {
		switch m.Op {
		case "move":
			roots, err = applyMove(roots, m.From, m.To, m.Before)
		case "setvalue":
			roots, err = applySetValue(roots, m.Path, m.Value)
		case "settext":
			roots, err = applySetText(roots, m.Path, m.Value)
		case "setchecked":
			roots, err = applySetChecked(roots, m.Path, m.Checked)
		case "setselected":
			roots, err = applySetSelected(roots, m.Path, m.Selected)
		default:
			err = fmt.Errorf("vdom: unknown mutation op %q", m.Op)
		}
		if err != nil {
			return nil, err
		}
	}
	return roots, nil
}

// applyControl rewrites the form control at path with f, after
// vetting that the path stays outside opaque subtrees: an opaque
// element and its contents are app-owned and frozen in the vdom, so
// no replay may rewrite them.
func applyControl(roots []Node, path []Step, f func(Element) (Element, error)) ([]Node, error) {
	if err := vetOutsideOpaque(roots, path); err != nil {
		return nil, err
	}
	return updateAt(roots, path, f)
}

// vetOutsideOpaque errors if path lands on or descends through an
// opaque element.
func vetOutsideOpaque(nodes []Node, path []Step) error {
	for _, s := range path {
		i, err := s.resolve(nodes)
		if err != nil {
			return err
		}
		e, ok := nodes[i].(Element)
		if !ok {
			return fmt.Errorf("vdom: mutation path descends into a text node")
		}
		if e.opaque {
			return fmt.Errorf("vdom: mutation path enters opaque element %q", e.key)
		}
		nodes = e.children
	}
	return nil
}

// inputType returns e's type attribute, folded the way a browser
// matches attribute keywords: ASCII case-insensitively.
func inputType(e Element) string {
	return lowerASCII(attrValue(e.attrs, "type"))
}

// lowerASCII lowercases ASCII letters only, leaving other runes as
// they are.
func lowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// attrValue returns the value of the attr named name, "" if absent.
func attrValue(attrs []Attr, name string) string {
	i, found := slices.BinarySearchFunc(attrs, Attr{Name: name}, Attr.cmp)
	if !found {
		return ""
	}
	return attrs[i].Value
}

// applySetValue records a text-mode input's committed value as its
// value attribute. Inputs whose value attribute means something other
// than the displayed text — file (filename mode), checkbox and radio
// (default/on mode) — are rejected: their committed state is not a
// value fact.
func applySetValue(roots []Node, path []Step, value string) ([]Node, error) {
	return applyControl(roots, path, func(e Element) (Element, error) {
		if e.tag != "input" {
			return Element{}, fmt.Errorf("vdom: setvalue targets a <%s>, want an <input>", e.tag)
		}
		if t := inputType(e); t == "file" || t == "checkbox" || t == "radio" {
			return Element{}, fmt.Errorf("vdom: setvalue on an input of type %q", t)
		}
		e.attrs = withAttr(e.attrs, Attr{Name: "value", Value: value})
		return e, nil
	})
}

// applySetText records a textarea's committed value as its text
// content. The empty value leaves no text child, matching
// canonicalization, which drops empty text.
func applySetText(roots []Node, path []Step, value string) ([]Node, error) {
	return applyControl(roots, path, func(e Element) (Element, error) {
		if e.tag != "textarea" {
			return Element{}, fmt.Errorf("vdom: settext targets a <%s>, want a <textarea>", e.tag)
		}
		if value == "" {
			return e.withChildren(nil), nil
		}
		return e.withChildren([]Node{Text(value)}), nil
	})
}

// applySetChecked records a checkbox's or radio's committed
// checkedness as the presence of its checked attribute.
func applySetChecked(roots []Node, path []Step, checked bool) ([]Node, error) {
	return applyControl(roots, path, func(e Element) (Element, error) {
		if e.tag != "input" {
			return Element{}, fmt.Errorf("vdom: setchecked targets a <%s>, want an <input>", e.tag)
		}
		if t := inputType(e); t != "checkbox" && t != "radio" {
			return Element{}, fmt.Errorf("vdom: setchecked on an input of type %q", t)
		}
		if checked {
			e.attrs = withAttr(e.attrs, Attr{Name: "checked"})
		} else {
			e.attrs = withoutAttr(e.attrs, "checked")
		}
		return e, nil
	})
}

// applySetSelected records an option's committed selectedness as the
// presence of its selected attribute.
func applySetSelected(roots []Node, path []Step, selected bool) ([]Node, error) {
	return applyControl(roots, path, func(e Element) (Element, error) {
		if e.tag != "option" {
			return Element{}, fmt.Errorf("vdom: setselected targets a <%s>, want an <option>", e.tag)
		}
		if selected {
			e.attrs = withAttr(e.attrs, Attr{Name: "selected"})
		} else {
			e.attrs = withoutAttr(e.attrs, "selected")
		}
		return e, nil
	})
}

// applyMove relocates a keyed child named by from to the container named by
// to, placing it before the child named before — or at the end when before is
// empty. Each path names the child itself: its last step is the child's key
// and the steps before it address its container — an element, or the root
// list itself when no steps remain. The destination key (to's last step)
// equals the source key unless the client re-keyed the child to avoid a
// collision in the destination. Paths address the tree the way the client
// walks it (see updateAt); the rewrite is functional.
func applyMove(roots []Node, from, to []Step, before string) ([]Node, error) {
	srcPath, fromKey, err := splitKey(from)
	if err != nil {
		return nil, err
	}
	dstPath, toKey, err := splitKey(to)
	if err != nil {
		return nil, err
	}

	var moved Element
	tree, err := withList(roots, srcPath, func(kids []Node, _ string) ([]Node, error) {
		j := keyIndex(kids, fromKey)
		if j < 0 {
			return nil, fmt.Errorf("vdom: move key %q absent from source", fromKey)
		}
		// A keyed child is an Element by construction; see childKey.
		moved = kids[j].(Element)
		return slices.Delete(slices.Clone(kids), j, j+1), nil
	})
	if err != nil {
		return nil, err
	}

	if toKey != fromKey {
		// The client re-keyed the moved node to dodge a collision in the
		// destination; mirror it, key and attribute both.
		moved = moved.WithKey(toKey, moved.opaque)
	}

	return withList(tree, dstPath, func(kids []Node, tag string) ([]Node, error) {
		if IsRawTextElement(tag) || IsVoid(tag) {
			return nil, fmt.Errorf("vdom: move destination <%s> cannot hold element children", tag)
		}
		if keyIndex(kids, toKey) >= 0 {
			return nil, fmt.Errorf("vdom: move key %q already in destination", toKey)
		}
		pos := len(kids)
		if before != "" {
			if pos = keyIndex(kids, before); pos < 0 {
				return nil, fmt.Errorf("vdom: move anchor %q absent from destination", before)
			}
		}
		return slices.Insert(slices.Clone(kids), pos, Node(moved)), nil
	})
}

// withList applies f to the child list its path names — the roots
// themselves when the path is empty (the domi-root mount is the
// container), else the children of the element at path — rebuilding
// the ancestors around f's result. f also receives the container's
// tag, empty for the root.
func withList(nodes []Node, path []Step, f func(kids []Node, tag string) ([]Node, error)) ([]Node, error) {
	if len(path) == 0 {
		return f(nodes, "")
	}
	return updateAt(nodes, path, func(e Element) (Element, error) {
		kids, err := f(e.children, e.tag)
		if err != nil {
			return Element{}, err
		}
		return e.withChildren(kids), nil
	})
}

// splitKey separates a move path into the path to the child's container and
// the child's own key — the last step, which must be keyed.
func splitKey(path []Step) ([]Step, string, error) {
	if len(path) == 0 {
		return nil, "", fmt.Errorf("vdom: empty move path")
	}
	last := path[len(path)-1]
	if last.key == "" {
		return nil, "", fmt.Errorf("vdom: move path does not end in a key")
	}
	return path[:len(path)-1], last.key, nil
}

// updateAt returns nodes with the element at path replaced by f applied to
// it, rebuilding the ancestors along the way (Elements are immutable, so
// each level is cloned rather than mutated).
//
// Each path step selects a child: a key resolves to the child carrying
// it, an index to a position — so a keyed container is reached by key
// and survives reordering. It errors if a step doesn't fit (a missing
// key, an out-of-range index, a descent through a text node).
func updateAt(nodes []Node, path []Step, f func(Element) (Element, error)) ([]Node, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("vdom: empty mutation path")
	}
	i, err := path[0].resolve(nodes)
	if err != nil {
		return nil, err
	}
	e, ok := nodes[i].(Element)
	if !ok {
		return nil, fmt.Errorf("vdom: mutation path descends into a text node")
	}

	var ne Element
	if len(path) == 1 {
		if ne, err = f(e); err != nil {
			return nil, err
		}
	} else {
		kids, err := updateAt(e.children, path[1:], f)
		if err != nil {
			return nil, err
		}
		ne = e.withChildren(kids)
	}

	out := slices.Clone(nodes)
	out[i] = ne
	return out, nil
}

// withChildren returns a copy of e carrying children in place of its own,
// for rebuilding an ancestor along an updateAt path.
func (e Element) withChildren(children []Node) Element {
	e.children = children
	return e
}

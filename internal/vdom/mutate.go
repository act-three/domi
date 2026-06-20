package vdom

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
)

// A ClientMutation is a structural change applied by a client.
// See [Apply].
//
// The set of ClientMutation operations
// is distinct from the patch ops emitted by Diff.
type ClientMutation struct {
	Op     string `json:",omitempty"` // move
	From   []Step `json:",omitempty"` // path to the moved element
	To     []Step `json:",omitempty"` // path to the moved element's destination
	Before string `json:",omitempty"` // anchor key in the dest container; "" means append
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
		return fmt.Errorf("vdom: move path step is a %q, want a key or index", tok.Kind())
	}
	return nil
}

// resolve returns the index of the child this step selects among nodes,
// whose parent's keys are keys (nil at the positional top level). A keyed
// step looks its key up in keys; a positional step is a direct index.
func (s Step) resolve(nodes []Node, keys []string) (int, error) {
	if s.key != "" {
		if keys == nil {
			return 0, fmt.Errorf("vdom: move path key %q in a positional container", s.key)
		}
		if i := slices.Index(keys, s.key); i >= 0 {
			return i, nil
		}
		return 0, fmt.Errorf("vdom: move path key %q not found", s.key)
	}
	if s.index < 0 || s.index >= len(nodes) {
		return 0, fmt.Errorf("vdom: move path index %d out of range %d", s.index, len(nodes))
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
		default:
			err = fmt.Errorf("vdom: unknown mutation op %q", m.Op)
		}
		if err != nil {
			return nil, err
		}
	}
	return roots, nil
}

// applyMove relocates a keyed child named by from to the container named by
// to, placing it before the child named before — or at the end when before is
// empty. Each path names the child itself: its last step is the child's key
// and the steps before it address its container. The destination key (to's
// last step) equals the source key unless the client re-keyed the child to
// avoid a collision in the destination. Paths address the tree the way the
// client walks it (see updateAt); the rewrite is functional.
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
	tree, err := updateAt(roots, nil, srcPath, func(src Element) (Element, error) {
		if src.keys == nil {
			return Element{}, fmt.Errorf("vdom: move source is not a keyed container")
		}
		j := slices.Index(src.keys, fromKey)
		if j < 0 {
			return Element{}, fmt.Errorf("vdom: move key %q absent from source", fromKey)
		}
		me, ok := src.children[j].(Element)
		if !ok {
			return Element{}, fmt.Errorf("vdom: move key %q names a non-element child", fromKey)
		}
		moved = me
		return src.removeChildAt(j), nil
	})
	if err != nil {
		return nil, err
	}

	return updateAt(tree, nil, dstPath, func(dst Element) (Element, error) {
		if dst.keys == nil {
			return Element{}, fmt.Errorf("vdom: move destination is not a keyed container")
		}
		pos := len(dst.children)
		if before != "" {
			if pos = slices.Index(dst.keys, before); pos < 0 {
				return Element{}, fmt.Errorf("vdom: move anchor %q absent from destination", before)
			}
		}
		return dst.insertChildAt(pos, moved, toKey), nil
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
// nodes are the children of a parent whose keys are keys (nil at the
// positional top level). Each path step selects a child: a key resolves
// against keys, an index against nodes — so a keyed container is reached by
// key and survives reordering. It errors if a step doesn't fit (a key in a
// positional container, a missing key, an out-of-range index, a descent
// through a text node).
func updateAt(nodes []Node, keys []string, path []Step, f func(Element) (Element, error)) ([]Node, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("vdom: empty move path")
	}
	i, err := path[0].resolve(nodes, keys)
	if err != nil {
		return nil, err
	}
	e, ok := nodes[i].(Element)
	if !ok {
		return nil, fmt.Errorf("vdom: move path descends into a text node")
	}

	var ne Element
	if len(path) == 1 {
		if ne, err = f(e); err != nil {
			return nil, err
		}
	} else {
		kids, err := updateAt(e.children, e.keys, path[1:], f)
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
// for rebuilding an ancestor whose child count is unchanged (its keys, if
// any, stay aligned).
func (e Element) withChildren(children []Node) Element {
	return Element{tag: e.tag, attrs: e.attrs, children: children, keys: e.keys, opaque: e.opaque}
}

// removeChildAt returns a copy of keyed element e without the child and
// key at index i.
func (e Element) removeChildAt(i int) Element {
	return Element{
		tag:      e.tag,
		attrs:    e.attrs,
		children: slices.Delete(slices.Clone(e.children), i, i+1),
		keys:     slices.Delete(slices.Clone(e.keys), i, i+1),
		opaque:   e.opaque,
	}
}

// insertChildAt returns a copy of keyed element e with child inserted at
// index i under key.
func (e Element) insertChildAt(i int, child Element, key string) Element {
	return Element{
		tag:      e.tag,
		attrs:    e.attrs,
		children: slices.Insert(slices.Clone(e.children), i, Node(child)),
		keys:     slices.Insert(slices.Clone(e.keys), i, key),
		opaque:   e.opaque,
	}
}

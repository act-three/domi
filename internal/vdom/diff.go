package vdom

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
)

// Patch is a single mutation op the client applies to its DOM. The
// outer type is opaque: the inner patch is unreachable from outside
// this package, so a Patch can be passed to json.Marshal and nothing
// else.
//
// `Replace` and `InsertChild` carry a pre-rendered HTML fragment
// rather than a serialized Node — the client parses it via a
// <template> element.
//
// `SetTitle` is the lone non-DOM op: it sets document.title and
// carries no path. Produced by [SetTitle].
//
// InsertChild / RemoveChild / MoveChild come in two flavours,
// chosen by which diff function produced them:
//
//   - Positional (from diffPositional, for unkeyed children): use
//     Index / From / To to address siblings by position. The pointer
//     types let the encoder distinguish "no index" (omitted) from
//     "index 0" (emitted as 0).
//   - Identity-based (from diffKeyed, for keyed children): use Key /
//     Before to address siblings by their data-domi-key. The client
//     keeps a per-parent Map<key, ChildNode> to resolve them in O(1).
//     An empty Before means "insert/move to the end".
type Patch struct{ p patch }

// MarshalJSONTo marshals p to JSON in the vdom wire format.
func (p Patch) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, p.p)
}

type patch struct {
	Op     string
	Path   []int
	Value  string `json:",omitempty"`
	Name   string `json:",omitempty"`
	HTML   string `json:",omitempty"`
	Index  *int   `json:",omitempty"`
	From   *int   `json:",omitempty"`
	To     *int   `json:",omitempty"`
	Key    string `json:",omitempty"`
	Before string `json:",omitempty"`
	ID     string `json:",omitempty"`
}

// Diff produces the minimal patch list that transforms old into new.
// Root-level children are coalesced to match the DOM shape (element
// children are coalesced at construction in [NewElement]).
func Diff(old, new []Node) []Patch {
	patches := diffPositional(coalesceText(old), coalesceText(new), nil, nil)
	out := make([]Patch, len(patches))
	for i, p := range patches {
		out[i] = Patch{p: p}
	}
	return out
}

// SetTitle returns a Patch that sets document.title on the client.
// Apply order matches the surrounding patch list, so callers can place
// it before or after DOM patches as needed.
func SetTitle(title string) Patch {
	return Patch{p: patch{Op: "SetTitle", Value: title}}
}

// PushURL returns a Patch that calls history.pushState on the client,
// adding an entry to the browser's navigation history.
// The id identifies this snapshot for later restoration on popstate.
func PushURL(url, id string) Patch {
	return Patch{p: patch{Op: "PushURL", Value: url, ID: id}}
}

// ReplaceURL returns a Patch that calls history.replaceState on the
// client, replacing the current navigation history entry.
func ReplaceURL(url string) Patch {
	return Patch{p: patch{Op: "ReplaceURL", Value: url}}
}

// Load returns a Patch that triggers a full-page browser navigation to
// url via window.location, leaving the session behind. Unlike
// [PushURL], which updates history in place, the browser fetches a
// fresh document; url may therefore be absolute and cross-origin.
func Load(url string) Patch {
	return Patch{p: patch{Op: "Load", Value: url}}
}

// Reset replaces the root's entire subtree with the given children.
func Reset(children []Node) Patch {
	var b strings.Builder
	for _, c := range children {
		_ = RenderTo(&b, c)
	}
	return Patch{p: patch{Op: "Reset", HTML: b.String()}}
}

func diffNode(old, new Node, path []int, out []patch) []patch {
	switch o := old.(type) {
	case Raw:
		n, isRaw := new.(Raw)
		if !isRaw || o != n {
			return append(out, patch{Op: "Replace", Path: slices.Clone(path), HTML: Render(new)})
		}
	case Element:
		// Replace on tag mismatch, or on keyed-vs-positional mismatch.
		// The latter is treated as structural even at the same tag: the
		// children would lose their key-based identities (or gain ones
		// the client doesn't have indexes for), and a wholesale rebuild
		// is simpler than reconstructing the per-parent Map<key, child>.
		n, isElement := new.(Element)
		if !isElement || o.tag != n.tag || (o.keys == nil) != (n.keys == nil) {
			return append(out, patch{Op: "Replace", Path: slices.Clone(path), HTML: Render(new)})
		}
		out = diffAttrs(o.attrs, n.attrs, path, out)
		if o.keys != nil {
			out = diffKeyed(o.children, n.children, o.keys, n.keys, path, out)
		} else {
			out = diffPositional(o.children, n.children, path, out)
		}
	}
	return out
}

// diffAttrs emits SetAttr and RemoveAttr patches for changes
// between old and new. Both slices are sorted by name (guaranteed by
// NewElement), so a single merge-scan suffices with no maps.
func diffAttrs(old, new []Attr, path []int, out []patch) []patch {
	i, j := 0, 0
	for i < len(old) && j < len(new) {
		switch strings.Compare(old[i].Name, new[j].Name) {
		case -1:
			out = append(out, patch{Op: "RemoveAttr", Path: slices.Clone(path), Name: old[i].Name})
			i++
		case +1:
			out = append(out, patch{Op: "SetAttr", Path: slices.Clone(path), Name: new[j].Name, Value: new[j].Value})
			j++
		default:
			if old[i].Value != new[j].Value {
				out = append(out, patch{Op: "SetAttr", Path: slices.Clone(path), Name: new[j].Name, Value: new[j].Value})
			}
			i++
			j++
		}
	}
	for ; i < len(old); i++ {
		out = append(out, patch{Op: "RemoveAttr", Path: slices.Clone(path), Name: old[i].Name})
	}
	for ; j < len(new); j++ {
		out = append(out, patch{Op: "SetAttr", Path: slices.Clone(path), Name: new[j].Name, Value: new[j].Value})
	}
	return out
}

func diffPositional(old, new []Node, path []int, out []patch) []patch {
	for i := len(old) - 1; i >= len(new); i-- {
		out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Index: &i})
	}
	common := min(len(old), len(new))
	for i := range common {
		out = diffNode(old[i], new[i], append(path, i), out)
	}
	for i := len(old); i < len(new); i++ {
		out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Index: &i, HTML: Render(new[i])})
	}
	return out
}

// diffKeyed reconciles two keyed-children regions, given parallel slices
// of children and their keys. Caller guarantees len(kids)==len(keys) on
// both sides.
//
// It runs Snabbdom's four-rule head/tail loop until none of the rules
// fire, then — if anything is left in the unknown middle — falls
// through to a Vue 3-style LIS to minimize moves. Both phases emit
// identity-based ops (key + before), so the server never tracks
// positions and the client resolves siblings in O(1) via a per-parent
// Map.
//
// Content diffs for matched pairs are deferred until after all
// structural patches are emitted, so paths (which use new-position
// childNodes traversal) point at the right elements when they apply.
func diffKeyed(oldKids, newKids []Node, oldKeys, newKeys []string, path []int, out []patch) []patch {
	oldStart, newStart := 0, 0
	oldEnd, newEnd := len(oldKids)-1, len(newKids)-1

	type deferredMatch struct {
		oldNode, newNode Node
		newIdx           int
	}
	var deferred []deferredMatch

	// beforeKey returns the key of new[newEnd+1] (the start of the tail
	// or whatever sits just past the unhandled new region); "" means the
	// element should land at the end.
	beforeKey := func() string {
		if newEnd+1 < len(newKeys) {
			return newKeys[newEnd+1]
		}
		return ""
	}

	// Snabbdom's four-rule head/tail loop.
	for oldStart <= oldEnd && newStart <= newEnd {
		switch {
		case oldKeys[oldStart] == newKeys[newStart]:
			// Rule 1: match at the head; no structural change.
			deferred = append(deferred, deferredMatch{oldKids[oldStart], newKids[newStart], newStart})
			oldStart++
			newStart++
		case oldKeys[oldEnd] == newKeys[newEnd]:
			// Rule 2: match at the tail; no structural change.
			deferred = append(deferred, deferredMatch{oldKids[oldEnd], newKids[newEnd], newEnd})
			oldEnd--
			newEnd--
		case oldKeys[oldStart] == newKeys[newEnd]:
			// Rule 3: old head moved to new tail. Move it to land just
			// before whatever sits past the unhandled tail.
			k := oldKeys[oldStart]
			out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: k, Before: beforeKey()})
			deferred = append(deferred, deferredMatch{oldKids[oldStart], newKids[newEnd], newEnd})
			oldStart++
			newEnd--
		case oldKeys[oldEnd] == newKeys[newStart]:
			// Rule 4: old tail moved to new head. Move it to land just
			// before the current head of unhandled old (old[oldStart]) —
			// the key-based equivalent of Snabbdom's
			// insertBefore(oldEnd.elm, oldStart.elm).
			k := oldKeys[oldEnd]
			out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: k, Before: oldKeys[oldStart]})
			deferred = append(deferred, deferredMatch{oldKids[oldEnd], newKids[newStart], newStart})
			oldEnd--
			newStart++
		default:
			// None of the four rules apply; the middle is genuinely
			// shuffled. Hand off to LIS below.
			goto middle
		}
	}

middle:
	emitDeferred := func(out []patch) []patch {
		for _, d := range deferred {
			out = diffNode(d.oldNode, d.newNode, append(path, d.newIdx), out)
		}
		return out
	}

	if oldStart > oldEnd {
		// Only inserts left. Walk right-to-left so each insert's `before`
		// anchor — the sibling at newIdx+1 — is already in place when its
		// patch is applied. (Forward order would reference siblings that
		// the next iteration hasn't inserted yet.) Mirrors the LIS branch.
		for i := newEnd; i >= newStart; i-- {
			before := ""
			if i+1 < len(newKeys) {
				before = newKeys[i+1]
			}
			out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Key: newKeys[i], HTML: Render(newKids[i]), Before: before})
		}
		return emitDeferred(out)
	}
	if newStart > newEnd {
		// Only removes left.
		for i := oldStart; i <= oldEnd; i++ {
			out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Key: oldKeys[i]})
		}
		return emitDeferred(out)
	}

	// Unknown middle: LIS.
	keyToNewIdx := make(map[string]int, newEnd-newStart+1)
	for i := newStart; i <= newEnd; i++ {
		keyToNewIdx[newKeys[i]] = i
	}

	toPatch := newEnd - newStart + 1
	// newToOld[j-newStart] = oldIndex+1 of the matched old node, or 0 if
	// position j is a fresh insert. The +1 encoding matches Vue 3 so 0
	// can mean "unmatched" in the LIS input.
	newToOld := make([]int, toPatch)
	moved := false
	maxNewSeen := 0
	patched := 0

	for i := oldStart; i <= oldEnd; i++ {
		if patched >= toPatch {
			break
		}
		if j, ok := keyToNewIdx[oldKeys[i]]; ok {
			newToOld[j-newStart] = i + 1
			if j >= maxNewSeen {
				maxNewSeen = j
			} else {
				moved = true
			}
			deferred = append(deferred, deferredMatch{oldKids[i], newKids[j], j})
			patched++
		}
	}

	// Remove unmatched old middle entries (forward iteration is fine —
	// identity-based removes don't depend on sibling positions).
	for i := oldStart; i <= oldEnd; i++ {
		if _, ok := keyToNewIdx[oldKeys[i]]; !ok {
			out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Key: oldKeys[i]})
		}
	}

	var lis []int
	if moved {
		lis = longestIncreasingSubseq(newToOld)
	}
	lisIdx := len(lis) - 1

	// Right-to-left walk: for each new position, either insert, move
	// before its anchor (new[newIdx+1]), or leave alone if LIS-stable.
	for i := toPatch - 1; i >= 0; i-- {
		newIdx := newStart + i

		before := ""
		if newIdx+1 < len(newKeys) {
			before = newKeys[newIdx+1]
		}

		if newToOld[i] == 0 {
			out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Key: newKeys[newIdx], HTML: Render(newKids[newIdx]), Before: before})
			continue
		}
		if !moved {
			continue
		}
		if lisIdx >= 0 && i == lis[lisIdx] {
			lisIdx--
			continue
		}
		out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: newKeys[newIdx], Before: before})
	}

	return emitDeferred(out)
}

// longestIncreasingSubseq returns the indices into arr (in ascending order)
// that form a longest strictly-increasing subsequence of the positive
// values in arr. Zero values are treated as "no old match" and are not
// eligible for the subsequence — those positions are inserts and always
// need a structural patch regardless of LIS membership.
//
// Used by diffKeyed to identify which matched-and-relocated nodes can
// stay put. Patience-sorting based, O(n log n).
func longestIncreasingSubseq(arr []int) []int {
	n := len(arr)
	if n == 0 {
		return nil
	}
	pred := make([]int, n)
	// tails[k] is the index in arr of the smallest possible tail of any
	// increasing subseq of length k+1 seen so far.
	tails := make([]int, 0, n)
	for i, v := range arr {
		if v == 0 {
			continue
		}
		// Find leftmost k in tails where arr[tails[k]] >= v.
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if arr[tails[mid]] < v {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			pred[i] = tails[lo-1]
		} else {
			pred[i] = -1
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	if len(tails) == 0 {
		return nil
	}
	// Reconstruct by following predecessors from the last tail.
	out := make([]int, len(tails))
	k := tails[len(tails)-1]
	for i := len(tails) - 1; i >= 0; i-- {
		out[i] = k
		k = pred[k]
	}
	return out
}

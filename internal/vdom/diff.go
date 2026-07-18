package vdom

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
)

// Patch is a single mutation op the client applies to its DOM. The
// outer type is opaque: the inner patch is unreachable from outside
// this package, so a Patch can be passed to json.Marshal and nothing
// else.
//
// Applying a Patch is a pure mutation of the target DOM subtree: it
// touches only nodes reachable from the root it is applied to, with no
// document-, history-, or browser-level effects. A patch list can
// therefore be replayed against a detached clone to build a tree off to
// the side.
//
// `Replace` and `InsertChild` carry a pre-rendered HTML fragment
// rather than a serialized Node — the client parses it via a
// <template> element. `SetText` carries the new text content in
// `Value`, which the client writes to the target text node in place.
//
// InsertChild / RemoveChild / MoveChild come in two flavours, chosen
// by whether the child they act on is keyed:
//
//   - Positional (for unkeyed children): use Index / From / To to
//     address siblings by childNodes position. The pointer types let
//     the encoder distinguish "no index" (omitted) from "index 0"
//     (emitted as 0).
//   - Identity-based (for keyed children): use Key / Before to address
//     siblings by their data-domi-key. The client keeps a per-parent
//     Map<key, ChildNode> to resolve them in O(1). An empty Before
//     means "after the parent's last keyed child" — the end, in a
//     fully keyed parent — or plain append when it has no keyed child;
//     a move whose subject already is the last keyed child stays put,
//     leaving its unkeyed neighbors undisturbed.
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
	validateChildren(new)
	patches := diffChildren(coalesceText(old), coalesceText(new), nil, nil)
	out := make([]Patch, len(patches))
	for i, p := range patches {
		out[i] = Patch{p: p}
	}
	return out
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
	case Text:
		// Both text: update the node's content in place. Otherwise the
		// node type changed, so replace the whole subtree.
		if n, isText := new.(Text); isText {
			if o != n {
				out = append(out, patch{Op: "SetText", Path: slices.Clone(path), Value: string(n)})
			}
			return out
		}
		return append(out, patch{Op: "Replace", Path: slices.Clone(path), HTML: Render(new)})
	case Element:
		n, isElement := new.(Element)
		if !isElement || o.tag != n.tag {
			return append(out, patch{Op: "Replace", Path: slices.Clone(path), HTML: Render(new)})
		}
		if o.opaque && n.opaque {
			// An opaque element is never diffed.
			return out
		} else if o.opaque != n.opaque {
			// Opacity toggled between renders; Replace hands the subtree
			// cleanly into or out of the framework's control.
			return append(out, patch{Op: "Replace", Path: slices.Clone(path), HTML: Render(new)})
		}
		out = diffAttrs(o.attrs, n.attrs, path, out)
		out = diffChildren(o.children, n.children, path, out)
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

// diffChildren reconciles two child lists sharing a parent at path.
// Every list is treated uniformly: its keyed children — however
// interleaved with unkeyed ones — are reconciled by identity, and the
// gaps of unkeyed children around them are reconciled positionally.
// All-keyed and all-unkeyed lists are the degenerate cases (every gap
// empty, or a single gap spanning the list) rather than separate code
// paths.
//
// Three phases, in emission order:
//
//  1. diffKeyed reconciles the keyed subsequences, emitting
//     identity-based structural ops (insert/move/remove by key).
//  2. Those ops relocate only the keyed elements — unkeyed siblings
//     stay physically put — so the client's unkeyed children now sit
//     in gaps matching neither the old nor the new tree. simulate
//     replays the ops the way the client applies them, and each
//     resulting gap is diffed positionally against its new-tree
//     counterpart (gaps pair by ordinal: after phase 1 both lists
//     hold exactly the new keyed elements in the new order).
//  3. Content diffs for key-matched pairs, deferred by diffKeyed, are
//     emitted last: their paths use new-tree childNodes indices, which
//     resolve only once the parent's child list is structurally final.
func diffChildren(oldKids, newKids []Node, path []int, out []patch) []patch {
	oldK, newK := extractKeyed(oldKids), extractKeyed(newKids)
	mark := len(out)
	out, deferred := diffKeyed(oldK, newK, path, out)
	// Gaps exist only where unkeyed children do; when both lists are
	// fully keyed there is provably nothing to simulate or repair.
	if len(oldK.kids) < len(oldKids) || len(newK.kids) < len(newKids) {
		out = diffGaps(simulate(oldKids, out[mark:]), newKids, path, out)
	}
	for _, d := range deferred {
		out = diffNode(d.oldNode, d.newNode, append(path, d.newIdx), out)
	}
	return out
}

// keyedSeq is the keyed subsequence of a child list: the keyed
// children in order, their keys, and each child's childNodes index in
// the full list.
type keyedSeq struct {
	kids []Node
	keys []string
	idx  []int
}

// extractKeyed pulls the keyed subsequence out of a child list.
func extractKeyed(kids []Node) keyedSeq {
	n := 0
	for _, c := range kids {
		if childKey(c) != "" {
			n++
		}
	}
	if n == 0 {
		return keyedSeq{}
	}
	s := keyedSeq{make([]Node, 0, n), make([]string, 0, n), make([]int, 0, n)}
	for i, c := range kids {
		if k := childKey(c); k != "" {
			s.kids = append(s.kids, c)
			s.keys = append(s.keys, k)
			s.idx = append(s.idx, i)
		}
	}
	return s
}

// deferredMatch is a key-matched (old, new) element pair whose content
// diff is deferred until the parent's child list is structurally
// final; newIdx is the pair's childNodes index in the new tree.
type deferredMatch struct {
	oldNode, newNode Node
	newIdx           int
}

// diffKeyed reconciles the keyed subsequences of two child lists,
// emitting identity-based structural ops and returning the matched
// pairs for the caller to content-diff once the parent is structurally
// settled. Anchors name the next keyed sibling in the new order —
// never an unkeyed node — with the empty string standing for "after
// the last keyed child" (see [Patch]); whatever imprecision that
// leaves relative to unkeyed siblings, the caller's gap diffs repair.
//
// It runs Snabbdom's four-rule head/tail loop until none of the rules
// fire, then — if anything is left in the unknown middle — falls
// through to a Vue 3-style LIS to minimize moves. Both phases emit
// identity-based ops (key + before), so the server never tracks
// positions and the client resolves siblings in O(1) via a per-parent
// Map.
func diffKeyed(old, new keyedSeq, path []int, out []patch) ([]patch, []deferredMatch) {
	oldStart, newStart := 0, 0
	oldEnd, newEnd := len(old.kids)-1, len(new.kids)-1

	var deferred []deferredMatch

	// beforeKey returns the anchor for placing new[i]: the key of its
	// next keyed sibling in the new order, or "" — after the last keyed
	// child — when there is none. Every anchor the keyed phase emits
	// comes from here; the client's anchor resolution and the differ's
	// simulation both mirror this one rule.
	beforeKey := func(i int) string {
		if i+1 < len(new.keys) {
			return new.keys[i+1]
		}
		return ""
	}

	// Snabbdom's four-rule head/tail loop.
	for oldStart <= oldEnd && newStart <= newEnd {
		switch {
		case old.keys[oldStart] == new.keys[newStart]:
			// Rule 1: match at the head; no structural change.
			deferred = append(deferred, deferredMatch{old.kids[oldStart], new.kids[newStart], new.idx[newStart]})
			oldStart++
			newStart++
		case old.keys[oldEnd] == new.keys[newEnd]:
			// Rule 2: match at the tail; no structural change.
			deferred = append(deferred, deferredMatch{old.kids[oldEnd], new.kids[newEnd], new.idx[newEnd]})
			oldEnd--
			newEnd--
		case old.keys[oldStart] == new.keys[newEnd]:
			// Rule 3: old head moved to new tail. Move it to land just
			// before whatever sits past the unhandled tail.
			k := old.keys[oldStart]
			out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: k, Before: beforeKey(newEnd)})
			deferred = append(deferred, deferredMatch{old.kids[oldStart], new.kids[newEnd], new.idx[newEnd]})
			oldStart++
			newEnd--
		case old.keys[oldEnd] == new.keys[newStart]:
			// Rule 4: old tail moved to new head. Move it to land just
			// before the current head of unhandled old (old[oldStart]) —
			// the key-based equivalent of Snabbdom's
			// insertBefore(oldEnd.elm, oldStart.elm).
			k := old.keys[oldEnd]
			out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: k, Before: old.keys[oldStart]})
			deferred = append(deferred, deferredMatch{old.kids[oldEnd], new.kids[newStart], new.idx[newStart]})
			oldEnd--
			newStart++
		default:
			// None of the four rules apply; the middle is genuinely
			// shuffled. Hand off to LIS below.
			goto middle
		}
	}

middle:
	if oldStart > oldEnd {
		// Only inserts left. Walk right-to-left so each insert's `before`
		// anchor — the next keyed sibling — is already in place when its
		// patch is applied. (Forward order would reference siblings that
		// the next iteration hasn't inserted yet.) Mirrors the LIS branch.
		for i := newEnd; i >= newStart; i-- {
			out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Key: new.keys[i], HTML: Render(new.kids[i]), Before: beforeKey(i)})
		}
		return out, deferred
	}
	if newStart > newEnd {
		// Only removes left.
		for i := oldStart; i <= oldEnd; i++ {
			out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Key: old.keys[i]})
		}
		return out, deferred
	}

	// Unknown middle: LIS.
	keyToNewIdx := make(map[string]int, newEnd-newStart+1)
	for i := newStart; i <= newEnd; i++ {
		keyToNewIdx[new.keys[i]] = i
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
		if j, ok := keyToNewIdx[old.keys[i]]; ok {
			newToOld[j-newStart] = i + 1
			if j >= maxNewSeen {
				maxNewSeen = j
			} else {
				moved = true
			}
			deferred = append(deferred, deferredMatch{old.kids[i], new.kids[j], new.idx[j]})
			patched++
		}
	}

	// Remove unmatched old middle entries (forward iteration is fine —
	// identity-based removes don't depend on sibling positions).
	for i := oldStart; i <= oldEnd; i++ {
		if _, ok := keyToNewIdx[old.keys[i]]; !ok {
			out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Key: old.keys[i]})
		}
	}

	var lis []int
	if moved {
		lis = longestIncreasingSubseq(newToOld)
	}
	lisIdx := len(lis) - 1

	// Right-to-left walk: for each new position, either insert, move
	// before its anchor (the next keyed sibling), or leave alone if
	// LIS-stable.
	for i := toPatch - 1; i >= 0; i-- {
		newIdx := newStart + i

		if newToOld[i] == 0 {
			out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Key: new.keys[newIdx], HTML: Render(new.kids[newIdx]), Before: beforeKey(newIdx)})
			continue
		}
		if !moved {
			continue
		}
		if lisIdx >= 0 && i == lis[lisIdx] {
			lisIdx--
			continue
		}
		out = append(out, patch{Op: "MoveChild", Path: slices.Clone(path), Key: new.keys[newIdx], Before: beforeKey(newIdx)})
	}

	return out, deferred
}

// simulate replays keyed structural ops over the old child list the
// way the client applies them, returning the intermediate list the
// client holds once those ops have run. The ops relocate only the
// keyed elements — unkeyed children stay physically put while keys
// shuffle around them — so this list, not the old tree, is what each
// gap's positional diff must start from. A child a keyed op inserted
// appears as a bare keyed [Element]: inserted elements only ever
// delimit gaps, so nothing beyond the key is ever consulted.
//
// The anchor semantics here must match the client's applyPatch
// exactly; in particular the empty-Before rule (after the last keyed
// child, no-op when the moved child already is it, plain append when
// there is none) and the append fallback for an anchor that names
// nothing. The client resolves keyed ops through a per-parent Map and
// the DOM's sibling links, so the replay mirrors its data structures
// too — a doubly-linked list over an arena, plus a key index — and
// thereby its costs: O(children + ops), with the empty-Before tail
// scan bounded by the trailing unkeyed run just as it is in the DOM.
func simulate(oldKids []Node, ops []patch) []Node {
	if len(ops) == 0 {
		return oldKids
	}

	// Entry 0 is a sentinel bridging tail to head; entries 1..n are
	// the old children, and inserted children extend the arena.
	nodes := make([]Node, len(oldKids)+1, len(oldKids)+1+len(ops))
	copy(nodes[1:], oldKids)
	prev := make([]int, len(nodes), cap(nodes))
	next := make([]int, len(nodes), cap(nodes))
	byKey := make(map[string]int)
	for i := 1; i < len(nodes); i++ {
		prev[i], next[i-1] = i-1, i
		if k := childKey(nodes[i]); k != "" {
			byKey[k] = i
		}
	}
	prev[0] = len(nodes) - 1

	unlink := func(i int) {
		next[prev[i]], prev[next[i]] = next[i], prev[i]
	}
	linkBefore := func(i, ref int) {
		prev[i], next[i] = prev[ref], ref
		next[prev[ref]], prev[ref] = i, i
	}
	// lastKeyed returns the arena index of the last keyed child, or 0
	// (the sentinel) when there is none — the client's tail scan.
	lastKeyed := func() int {
		for i := prev[0]; i != 0; i = prev[i] {
			if childKey(nodes[i]) != "" {
				return i
			}
		}
		return 0
	}
	// anchor resolves Before to the entry to insert in front of, 0
	// meaning the end: the named keyed child (append when it names
	// nothing, like the client's missing-anchor fallback), or the
	// successor of the last keyed child for the empty anchor (append
	// when no keyed child exists — the sentinel's successor would be
	// the head, not the end).
	anchor := func(before string) int {
		if before != "" {
			if i, ok := byKey[before]; ok {
				return i
			}
			return 0
		}
		if lk := lastKeyed(); lk != 0 {
			return next[lk]
		}
		return 0
	}

	for _, p := range ops {
		switch p.Op {
		case "RemoveChild":
			if i, ok := byKey[p.Key]; ok {
				unlink(i)
				delete(byKey, p.Key)
			}
		case "InsertChild":
			nodes = append(nodes, Element{key: p.Key})
			prev, next = append(prev, 0), append(next, 0)
			i := len(nodes) - 1
			linkBefore(i, anchor(p.Before))
			byKey[p.Key] = i
		case "MoveChild":
			i, ok := byKey[p.Key]
			if !ok || p.Before == p.Key {
				break // moving nothing, or before itself: both no-ops
			}
			if p.Before == "" && lastKeyed() == i {
				break // already the last keyed child; hold still
			}
			ref := anchor(p.Before)
			unlink(i)
			linkBefore(i, ref)
		}
	}

	sim := make([]Node, 0, len(nodes)-1)
	for i := next[0]; i != 0; i = next[i] {
		sim = append(sim, nodes[i])
	}
	return sim
}

// keyIndex returns the index of the child with the given key, or -1.
func keyIndex(kids []Node, key string) int {
	return slices.IndexFunc(kids, func(n Node) bool { return childKey(n) == key })
}

// diffGaps walks the simulated and new child lists in lockstep,
// diffing each gap of unkeyed children positionally against its
// new-tree counterpart. After simulate the two lists hold the same
// keyed elements in the same order, so gaps pair one-to-one, each pair
// bounded by the same keyed delimiters.
//
// Each new gap's start index is the base for its positional ops'
// whole-childList indices. Those indices are valid at apply time
// because finalization is monotone left to right: when a gap's ops
// run, everything before it — earlier gaps finalized, keyed elements
// placed by phase 1 — is already in final new-tree shape, and no
// later patch in the stream targets an index at or below a finalized
// position (a gap's removes precede its in-place content diffs, its
// inserts land above them, later gaps' ops start at their own higher
// base, and the deferred keyed content diffs are structural only at
// deeper paths). The same monotonicity is why matched pairs' content
// diffs can be emitted inline here, mid-stream, while the keyed
// phase's must be deferred: a gap match's index is final the moment
// its gap's removes have run, but a key match's index is not final
// until every gap has settled.
func diffGaps(sim, newKids []Node, path []int, out []patch) []patch {
	i, j := 0, 0
	for {
		gs := i
		for i < len(sim) && childKey(sim[i]) == "" {
			i++
		}
		base := j
		for j < len(newKids) && childKey(newKids[j]) == "" {
			j++
		}
		out = diffPositional(sim[gs:i], newKids[base:j], base, path, out)
		if i == len(sim) && j == len(newKids) {
			return out
		}
		if i == len(sim) || j == len(newKids) || childKey(sim[i]) != childKey(newKids[j]) {
			panic(fmt.Sprintf("domi: internal: keyed delimiters desynced at %d/%d", i, j))
		}
		i++
		j++
	}
}

// diffPositional reconciles one gap of unkeyed children by position.
// base is the gap's start index in the parent's (new-tree) childNodes;
// every emitted index and path is offset by it. Within the gap the
// usual discipline keeps indices valid at apply time: removes walk
// descending, then matched pairs diff in place, then inserts walk
// ascending.
func diffPositional(old, new []Node, base int, path []int, out []patch) []patch {
	for i := len(old) - 1; i >= len(new); i-- {
		gi := base + i
		out = append(out, patch{Op: "RemoveChild", Path: slices.Clone(path), Index: &gi})
	}
	common := min(len(old), len(new))
	for i := range common {
		out = diffNode(old[i], new[i], append(path, base+i), out)
	}
	for i := len(old); i < len(new); i++ {
		gi := base + i
		out = append(out, patch{Op: "InsertChild", Path: slices.Clone(path), Index: &gi, HTML: Render(new[i])})
	}
	return out
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

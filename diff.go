package domi

import "encoding/json/v2"

// patch is a single mutation op the client applies to its DOM. One struct,
// op-tagged at marshal time — Go's encoding of a tagged union.
//
// `replace` and `insert_child` carry a pre-rendered HTML fragment rather
// than a serialized Node — the client parses it via a <template> element.
//
// insert_child / remove_child / move_child come in two flavours, chosen
// by which diff function produced them:
//
//   - Positional (from diffPositional, for unkeyed children): use idx /
//     from / to to address siblings by position.
//   - Identity-based (from diffKeyed, for keyed children): use key /
//     before to address siblings by their data-domi-key. The client
//     keeps a per-parent Map<key, ChildNode> to resolve them in O(1).
//     An empty `before` means "insert/move to the end".
type patch struct {
	op   string
	path []int

	// Op-specific fields. Only the relevant ones are emitted.
	value  string
	name   string
	html   string
	idx    int
	from   int
	to     int
	key    string
	before string
	// keyed is set on insert_child / remove_child / move_child that
	// came from diffKeyed; it selects the identity-based wire shape.
	keyed bool
}

func (p patch) MarshalJSON() ([]byte, error) {
	out := map[string]any{"op": p.op, "path": p.path}
	switch p.op {
	case "set_text":
		out["value"] = p.value
	case "set_attr":
		out["name"] = p.name
		out["value"] = p.value
	case "remove_attr":
		out["name"] = p.name
	case "replace":
		out["html"] = p.html
	case "insert_child":
		out["html"] = p.html
		if p.keyed {
			out["key"] = p.key
			if p.before != "" {
				out["before"] = p.before
			}
		} else {
			out["idx"] = p.idx
		}
	case "remove_child":
		if p.keyed {
			out["key"] = p.key
		} else {
			out["idx"] = p.idx
		}
	case "move_child":
		if p.keyed {
			out["key"] = p.key
			if p.before != "" {
				out["before"] = p.before
			}
		} else {
			out["from"] = p.from
			out["to"] = p.to
		}
	}
	return json.Marshal(out)
}

// diff produces the minimal patch list that transforms old into next.
func diff(old, next node) []patch {
	var out []patch
	diffNode(old, next, []int{}, &out)
	return out
}

func diffNode(old, next node, path []int, out *[]patch) {
	switch o := old.(type) {
	case text:
		n, isText := next.(text)
		if !isText {
			*out = append(*out, patch{op: "replace", path: clonePath(path), html: render(next)})
			return
		}
		if o.value != n.value {
			*out = append(*out, patch{op: "set_text", path: clonePath(path), value: n.value})
		}
	case element:
		// Replace on tag mismatch, or on keyed-vs-positional mismatch.
		// The latter is treated as structural even at the same tag: the
		// children would lose their key-based identities (or gain ones
		// the client doesn't have indexes for), and a wholesale rebuild
		// is simpler than reconstructing the per-parent Map<key, child>.
		n, isElement := next.(element)
		if !isElement || o.tag != n.tag || (o.keys == nil) != (n.keys == nil) {
			*out = append(*out, patch{op: "replace", path: clonePath(path), html: render(next)})
			return
		}
		diffAttrs(o.attrs, n.attrs, path, out)
		if o.keys != nil {
			diffKeyed(o.children, n.children, o.keys, n.keys, path, out)
		} else {
			diffChildren(o.children, n.children, path, out)
		}
	}
}

func diffAttrs(old, next []Attr, path []int, out *[]patch) {
	o := combinedAttrs(old)
	n := combinedAttrs(next)
	oldByName := make(map[string]string, len(o))
	for _, a := range o {
		oldByName[a.name] = a.value
	}
	nextByName := make(map[string]string, len(n))
	for _, a := range n {
		nextByName[a.name] = a.value
	}
	// Emit sets in next-occurrence order so patches are deterministic.
	for _, a := range n {
		if existing, ok := oldByName[a.name]; !ok || existing != a.value {
			*out = append(*out, patch{op: "set_attr", path: clonePath(path), name: a.name, value: a.value})
		}
	}
	// Emit removes in old-occurrence order.
	for _, a := range o {
		if _, ok := nextByName[a.name]; !ok {
			*out = append(*out, patch{op: "remove_attr", path: clonePath(path), name: a.name})
		}
	}
}

func diffChildren(old, next []node, path []int, out *[]patch) {
	// Coalesce adjacent text siblings before diffing. The HTML parser
	// merges adjacent text into one DOM Text node on round-trip, so
	// position-indexed patches must address children using the merged
	// count — otherwise insert_child/remove_child idx would walk off
	// the end of the parent's childNodes on the client.
	old = coalesceText(old)
	next = coalesceText(next)
	diffPositional(old, next, path, out)
}

// coalesceText concatenates adjacent text-node children into a single
// text node, matching the shape the HTML parser produces. Returns the
// input slice unchanged when no coalescing happens. Element entries
// pass through untouched — they're not text and aren't merged.
func coalesceText(children []node) []node {
	merged := false
	for i := 1; i < len(children); i++ {
		_, prev := children[i-1].(text)
		_, cur := children[i].(text)
		if prev && cur {
			merged = true
			break
		}
	}
	if !merged {
		return children
	}
	out := make([]node, 0, len(children))
	var buf string
	flush := func() {
		if buf != "" {
			out = append(out, text{value: buf})
			buf = ""
		}
	}
	for _, c := range children {
		if t, ok := c.(text); ok {
			buf += t.value
			continue
		}
		flush()
		out = append(out, c)
	}
	flush()
	return out
}

func diffPositional(old, next []node, path []int, out *[]patch) {
	for i := len(old) - 1; i >= len(next); i-- {
		*out = append(*out, patch{op: "remove_child", path: clonePath(path), idx: i})
	}
	common := min(len(old), len(next))
	for i := range common {
		diffNode(old[i], next[i], append(path, i), out)
	}
	for i := len(old); i < len(next); i++ {
		*out = append(*out, patch{op: "insert_child", path: clonePath(path), idx: i, html: render(next[i])})
	}
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
func diffKeyed(oldKids, newKids []node, oldKeys, newKeys []string, path []int, out *[]patch) {
	oldStart, newStart := 0, 0
	oldEnd, newEnd := len(oldKids)-1, len(newKids)-1

	type deferredMatch struct {
		oldNode, newNode node
		newIdx           int
	}
	var deferred []deferredMatch

	// beforeKey returns the key of next[newEnd+1] (the start of the tail
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
			*out = append(*out, patch{op: "move_child", path: clonePath(path), keyed: true, key: k, before: beforeKey()})
			deferred = append(deferred, deferredMatch{oldKids[oldStart], newKids[newEnd], newEnd})
			oldStart++
			newEnd--
		case oldKeys[oldEnd] == newKeys[newStart]:
			// Rule 4: old tail moved to new head. Move it to land just
			// before the current head of unhandled old (old[oldStart]) —
			// the key-based equivalent of Snabbdom's
			// insertBefore(oldEnd.elm, oldStart.elm).
			k := oldKeys[oldEnd]
			*out = append(*out, patch{op: "move_child", path: clonePath(path), keyed: true, key: k, before: oldKeys[oldStart]})
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
	emitDeferred := func() {
		for _, d := range deferred {
			diffNode(d.oldNode, d.newNode, append(path, d.newIdx), out)
		}
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
			*out = append(*out, patch{op: "insert_child", path: clonePath(path), keyed: true, key: newKeys[i], html: render(newKids[i]), before: before})
		}
		emitDeferred()
		return
	}
	if newStart > newEnd {
		// Only removes left.
		for i := oldStart; i <= oldEnd; i++ {
			*out = append(*out, patch{op: "remove_child", path: clonePath(path), keyed: true, key: oldKeys[i]})
		}
		emitDeferred()
		return
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
			*out = append(*out, patch{op: "remove_child", path: clonePath(path), keyed: true, key: oldKeys[i]})
		}
	}

	var lis []int
	if moved {
		lis = longestIncreasingSubseq(newToOld)
	}
	lisIdx := len(lis) - 1

	// Right-to-left walk: for each new position, either insert, move
	// before its anchor (next[newIdx+1]), or leave alone if LIS-stable.
	for i := toPatch - 1; i >= 0; i-- {
		newIdx := newStart + i

		before := ""
		if newIdx+1 < len(newKeys) {
			before = newKeys[newIdx+1]
		}

		if newToOld[i] == 0 {
			*out = append(*out, patch{op: "insert_child", path: clonePath(path), keyed: true, key: newKeys[newIdx], html: render(newKids[newIdx]), before: before})
			continue
		}
		if !moved {
			continue
		}
		if lisIdx >= 0 && i == lis[lisIdx] {
			lisIdx--
			continue
		}
		*out = append(*out, patch{op: "move_child", path: clonePath(path), keyed: true, key: newKeys[newIdx], before: before})
	}

	emitDeferred()
}

func clonePath(p []int) []int {
	out := make([]int, len(p))
	copy(out, p)
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

package vdom

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"iter"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// canonNode is the canonical form of an element tree used for diff/apply
// comparison. Both the Go-side Render(next) and the bun-side post-apply
// HTML pass through golang.org/x/net/html.ParseFragment + walkParsed,
// so the comparison sees only what the HTML5 parser sees — attribute
// order, void-element slash, whitespace quirks, and adjacent-text
// merging all wash out.
type canonNode struct {
	Text  string            `json:"text,omitempty"`
	Tag   string            `json:"tag,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Kids  []canonNode       `json:"kids,omitempty"`
}

// canonicalize parses an HTML fragment and returns its single root
// element in canonical form. Both sides of the diff/apply comparison
// wrap their child lists in a <domi-root> element, mirroring the
// production mount, so the fragment is single-rooted by construction.
func canonicalize(t *testing.T, htmlStr string) *canonNode {
	t.Helper()
	// ParseFragment with a <body> context handles general flow content
	// without inventing html/head wrappers around the fragment. The
	// context node needs DataAtom populated (not just Data) — the
	// parser uses atom equality, not string equality, internally.
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(htmlStr), ctx)
	if err != nil {
		t.Fatalf("canonicalize: parse %q: %v", htmlStr, err)
	}
	if len(nodes) != 1 {
		t.Fatalf("canonicalize: expected exactly one root, got %d for %q", len(nodes), htmlStr)
	}
	return walkParsed(nodes[0])
}

func walkParsed(n *html.Node) *canonNode {
	switch n.Type {
	case html.TextNode:
		if n.Data == "" {
			return nil
		}
		return &canonNode{Text: n.Data}
	case html.ElementNode:
		out := &canonNode{Tag: n.Data}
		if len(n.Attr) > 0 {
			out.Attrs = make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				out.Attrs[a.Key] = a.Val
			}
		}
		var textBuf string
		flushText := func() {
			if textBuf != "" {
				out.Kids = append(out.Kids, canonNode{Text: textBuf})
				textBuf = ""
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				textBuf += c.Data
				continue
			}
			flushText()
			if k := walkParsed(c); k != nil {
				out.Kids = append(out.Kids, *k)
			}
		}
		flushText()
		return out
	}
	return nil
}

// ---- random generator ----
//
// genElement, genNode, and genAttrs each take a seed and instantiate
// their own random source from it. The result depends only on (seed,
// depth), so any subtree's generation can be replayed in isolation —
// in particular, a single iteration's failure reproduces from just
// the iteration seed without replaying any predecessors. Static
// config lives in package-level vars below since none of it depends
// on RNG state.

var (
	// Only tags whose parsing has no implicit-close rules — keeps the
	// HTML round-trip an identity. <p> closes on block children, <li>
	// closes on a sibling <li>, etc.; including them would let the
	// generator emit trees the parser rearranges.
	genTags = []string{"div", "span", "section", "article", "header", "footer"}

	genAttrNames  = []string{"class", "id", "data-x", "title"}
	genAttrValues = []string{"x", "y", "z", ""}
	genTexts      = []string{"hi", "bye", "x", "y", "a < b & c"}
	genKeys       = []string{"a", "b", "c", "d", "e", "f"}
)

const (
	genMaxDepth         = 4
	genMaxChildren      = 5
	genMaxAttrs         = 3
	genAllKeyedChance   = 25 // percent of parents with every child keyed
	genAllUnkeyedChance = 25 // percent of parents with no child keyed
	genChildKeyChance   = 40 // percent per child, in the remaining mixed parents
	genTextChance       = 30 // percent
)

func genElement(seed uint64, depth int) Element {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	tag := genTags[rng.IntN(len(genTags))]
	attrs := genAttrs(rng.Uint64())
	n := 0
	if depth < genMaxDepth {
		n = rng.IntN(genMaxChildren + 1)
	}
	if n == 0 {
		return NewElement(tag, attrs, nil)
	}
	return NewElement(tag, attrs, genKids(rng, n, depth+1))
}

// genKids generates a child list of n children at the given depth,
// picking the list's shape first: every child keyed, none, or a
// per-child mix. The pure shapes are the differ's degenerate cases and
// the mix exercises the general one; keeping all three common
// preserves coverage of each. Shared by elements and root lists —
// roots are the domi-root mount's children and reconcile the same way.
func genKids(rng *rand.Rand, n, depth int) []Node {
	var keyChance int
	switch mode := rng.IntN(100); {
	case mode < genAllKeyedChance:
		keyChance = 100
	case mode < genAllKeyedChance+genAllUnkeyedChance:
		keyChance = 0
	default:
		keyChance = genChildKeyChance
	}

	avail := slices.Clone(genKeys)
	rng.Shuffle(len(avail), func(i, j int) { avail[i], avail[j] = avail[j], avail[i] })
	children := make([]Node, n)
	for i := range n {
		if rng.IntN(100) < keyChance && len(avail) > 0 {
			// WithKey mirrors domi's lowering: the key rides the element
			// and its domi-key attribute, so the rendered HTML
			// carries the identity the diff/apply round-trip needs. (A
			// keyed child must be an element, so a keyed slot always
			// generates one.)
			children[i] = genElement(rng.Uint64(), depth).WithKey(avail[0], false)
			avail = avail[1:]
		} else {
			children[i] = genNode(rng.Uint64(), depth)
		}
	}
	return children
}

func genNode(seed uint64, depth int) Node {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	if depth >= genMaxDepth || rng.IntN(100) < genTextChance {
		return Text(genTexts[rng.IntN(len(genTexts))])
	}
	return genElement(rng.Uint64(), depth)
}

// genChildren generates a random root list — the shape a lowered View
// takes, and the shape [Diff] diffs — with the same keyed/unkeyed
// shapes as any child list. Adjacent text nodes are allowed; Diff
// coalesces them, like it does for a production view.
func genChildren(seed uint64) []Node {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	return genKids(rng, rng.IntN(genMaxChildren+1), 0)
}

func genAttrs(seed uint64) iter.Seq[Attr] {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	n := rng.IntN(genMaxAttrs + 1)
	if n == 0 {
		return attrs()
	}
	used := map[string]bool{}
	out := make([]Attr, 0, n)
	for len(out) < n && len(used) < len(genAttrNames) {
		name := genAttrNames[rng.IntN(len(genAttrNames))]
		if used[name] {
			continue
		}
		used[name] = true
		value := genAttrValues[rng.IntN(len(genAttrValues))]
		out = append(out, Attr{Name: name, Value: value})
	}
	return slices.Values(out)
}

// ---- property test ----

// TestDiffApplyProperty runs N iterations of: pick a random seed,
// generate two child lists from it, run the production [Diff] through
// the production JS applier (jsdom under bun, patching a <domi-root>
// wrapper like the instance does), and compare the post-apply HTML to
// the rendered new list via golang.org/x/net/html — so attr-order,
// void-slash, and adjacent-text-merge differences in HTML
// serialization wash out.
//
// Each iteration runs as its own t.Run subtest named for its seed, so
// the failing iteration is identified by name without scrolling. The
// default failure message is concise — just the seed and a one-line
// reason. Re-run an individual failure verbosely by setting DOMI_SEED
// to the seed reported in that failure:
//
//	DOMI_SEED=12345 go test -run TestDiffApplyProperty
//
// In that mode a failure dumps the rendered HTML on both sides, the
// canonical JSON forms, and the full patch list.
func TestDiffApplyProperty(t *testing.T) {
	a := startBunApplier(t)

	// DOMI_SEED replays a single iteration with verbose output.
	if s := os.Getenv("DOMI_SEED"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatalf("bad DOMI_SEED %q: %v", s, err)
		}
		t.Run(fmt.Sprintf("seed=%d", n), func(t *testing.T) {
			t.Logf("DOMI_SEED=%d", n)
			checkProperty(t, a, n, true)
		})
		return
	}

	const iterations = 2000
	for range iterations {
		seed := rand.Uint64()
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Logf("DOMI_SEED=%d", seed)
			checkProperty(t, a, seed, false)
		})
	}
}

// checkProperty runs one (old, new) round-trip with the given seed.
// When verbose, a failure dumps the rendered HTML, canonical JSON,
// and patch list inline; otherwise it surfaces the seed plus a hint
// at how to replay the case verbosely.
func checkProperty(t *testing.T, a *bunApplier, seed uint64, verbose bool) {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	old := genChildren(rng.Uint64())
	new := genChildren(rng.Uint64())

	patches := diffList(old, new)
	gotHTML, err := a.apply(renderList(old), patches)
	if err != nil {
		if verbose {
			t.Fatalf("bun apply: %v\nold:  %s\nnew:  %s\npatches: %s",
				err, renderList(old), renderList(new), patchDebug(patches))
		}
		t.Fatalf("bun apply: %v  (replay: DOMI_SEED=%d go test -run TestDiffApplyProperty)", err, seed)
	}
	want := canonicalize(t, "<domi-root>"+renderList(new)+"</domi-root>")
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		if verbose {
			t.Fatalf("diff/apply mismatch\nold:  %s\nnew:  %s\ngot html: %s\nwant: %s\ngot:  %s\npatches (%d): %s",
				renderList(old), renderList(new), gotHTML,
				jsonStr(want), jsonStr(got),
				len(patches), patchDebug(patches))
		}
		t.Fatalf("diff/apply mismatch  (replay: DOMI_SEED=%d go test -run TestDiffApplyProperty)", seed)
	}
}

// diffList runs the production [Diff] and unwraps the resulting
// patches for the bun harness and patchDebug.
func diffList(old, new []Node) []patch {
	ps := Diff(old, new)
	out := make([]patch, len(ps))
	for i, p := range ps {
		out[i] = p.p
	}
	return out
}

func renderList(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		_ = RenderTo(&b, n)
	}
	return b.String()
}

// TestSetAttrEmptyValueAppliesAsEmptyString pins the JS-side coercion
// for set_attr patches whose value is the empty string. The Go wire
// format omits the `value` field via omitempty when it's "", so without
// nullish-coalescing on the JS side the applier passed `undefined` to
// setAttribute and the DOM ended up with attr="undefined". The property
// test exercises this randomly now that genAttrs emits ""; this fixture
// pins the specific shape down so it can't silently regress.
func TestSetAttrEmptyValueAppliesAsEmptyString(t *testing.T) {
	a := startBunApplier(t)

	old := []Node{NewElement("div", attrs(), nil)}
	new := []Node{NewElement("div", attrs(Attr{Name: "class", Value: ""}), nil)}

	gotHTML, err := a.apply(renderList(old), diffList(old, new))
	if err != nil {
		t.Fatalf("bun apply: %v", err)
	}
	want := canonicalize(t, "<domi-root>"+renderList(new)+"</domi-root>")
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s, want %s (raw html: %s)", jsonStr(got), jsonStr(want), gotHTML)
	}
}

// TestTextToEmptyRemovesTextNode pins the canonical DOM shape for a text
// node whose content transitions to empty: rendered HTML parses without
// an empty text child, so the patch removes the text node instead of
// setting it to "".
func TestTextToEmptyRemovesTextNode(t *testing.T) {
	a := startBunApplier(t)

	old := []Node{el("div", tx("hi"))}
	new := []Node{el("div", tx(""))}

	patches := diffList(old, new)
	if len(patches) != 1 || patches[0].Op != "RemoveChild" || patches[0].Index == nil || *patches[0].Index != 0 {
		t.Fatalf("expected one RemoveChild, got %+v", patches)
	}

	gotHTML, err := a.apply(renderList(old), patches)
	if err != nil {
		t.Fatalf("bun apply: %v", err)
	}
	want := canonicalize(t, "<domi-root>"+renderList(new)+"</domi-root>")
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s, want %s (raw html: %s)", jsonStr(got), jsonStr(want), gotHTML)
	}
}

// roundTrip diffs old against new, applies the patches through the
// production client applier, and asserts the resulting DOM matches
// render(new) canonically.
func roundTrip(t *testing.T, a *bunApplier, old, new []Node) {
	t.Helper()
	patches := diffList(old, new)
	gotHTML, err := a.apply(renderList(old), patches)
	if err != nil {
		t.Fatalf("bun apply: %v\npatches: %s", err, patchDebug(patches))
	}
	want := canonicalize(t, "<domi-root>"+renderList(new)+"</domi-root>")
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s, want %s (raw html: %s)\npatches: %s",
			jsonStr(got), jsonStr(want), gotHTML, patchDebug(patches))
	}
}

// TestMixedShuffleRoundTrip drives the motivating counterexample for
// gap simulation — [K:a, u, K:b] → [K:b, u, K:a] — through the real
// client applier: the keyed move strands u at the tail, and the gap
// patches must bring it back to the middle.
func TestMixedShuffleRoundTrip(t *testing.T) {
	a := startBunApplier(t)
	old := []Node{mixed("ul", kid{"a", li("a")}, kid{"", li("u")}, kid{"b", li("b")})}
	new := []Node{mixed("ul", kid{"b", li("b")}, kid{"", li("u")}, kid{"a", li("a")})}
	roundTrip(t, a, old, new)
}

// TestMixedFooterAppendRoundTrip pins the client half of the empty-
// Before anchor rule: the inserted item must land between the keyed
// run and the unkeyed footer, not after the footer.
func TestMixedFooterAppendRoundTrip(t *testing.T) {
	a := startBunApplier(t)
	old := []Node{mixed("ul", kid{"", li("header")}, kid{"a", li("a")}, kid{"", li("footer")})}
	new := []Node{mixed("ul", kid{"", li("header")}, kid{"a", li("a")}, kid{"b", li("b")}, kid{"", li("footer")})}
	roundTrip(t, a, old, new)
}

// TestEmptyBeforeMoveAlreadyLastKeyedClientNoOp pins the other client
// half of the empty-Before rule with a hand-built patch: moving the
// last keyed child "after the last keyed child" must hold it still
// rather than hoist it over the unkeyed content behind it.
func TestEmptyBeforeMoveAlreadyLastKeyedClientNoOp(t *testing.T) {
	a := startBunApplier(t)
	initial := `<ul><li domi-key="a">a</li><li>u0</li><li domi-key="b">b</li><li>u1</li></ul>`
	gotHTML, err := a.apply(initial, []patch{{Op: "MoveChild", Path: []int{0}, Key: "b"}})
	if err != nil {
		t.Fatalf("bun apply: %v", err)
	}
	want := canonicalize(t, "<domi-root>"+initial+"</domi-root>")
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("move should be a no-op, got %s, want %s", jsonStr(got), jsonStr(want))
	}
}

// TestReplaceKeepsChildMapInStep pins the client's keyed-child map
// maintenance across a Replace: replacing a keyed child (the patch a
// key-matched tag or opacity change produces) must update the cached
// map, or a later keyed op resolves to the detached old node —
// resurrecting it here, where the final move would otherwise bring
// back the pre-Replace content.
func TestReplaceKeepsChildMapInStep(t *testing.T) {
	a := startBunApplier(t)
	initial := `<ul><li domi-key="a">a</li><li domi-key="b">b</li></ul>`
	gotHTML, err := a.apply(initial, []patch{
		{Op: "MoveChild", Path: []int{0}, Key: "b", Before: "a"},             // primes the map; [b, a]
		{Op: "Replace", Path: []int{0, 1}, HTML: `<li domi-key="a">a2</li>`}, // [b, a2]
		{Op: "MoveChild", Path: []int{0}, Key: "a", Before: "b"},             // [a2, b]
	})
	if err != nil {
		t.Fatalf("bun apply: %v", err)
	}
	want := canonicalize(t, `<domi-root><ul><li domi-key="a">a2</li><li domi-key="b">b</li></ul></domi-root>`)
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s, want %s", jsonStr(got), jsonStr(want))
	}
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v, jsontext.WithIndent("  "))
	return string(b)
}

func patchDebug(patches []patch) string {
	var b strings.Builder
	for i, p := range patches {
		fmt.Fprintf(&b, "\n  [%d] op=%s path=%v", i, p.Op, p.Path)
		if p.Name != "" {
			fmt.Fprintf(&b, " name=%s", p.Name)
		}
		if p.Value != "" {
			fmt.Fprintf(&b, " value=%q", p.Value)
		}
		if p.Key != "" {
			fmt.Fprintf(&b, " key=%s", p.Key)
		}
		if p.Before != "" {
			fmt.Fprintf(&b, " before=%s", p.Before)
		}
		if p.Index != nil {
			fmt.Fprintf(&b, " index=%d", *p.Index)
		}
		if p.From != nil {
			fmt.Fprintf(&b, " from=%d to=%d", *p.From, *p.To)
		}
		if p.HTML != "" {
			fmt.Fprintf(&b, " html=%q", p.HTML)
		}
	}
	return b.String()
}

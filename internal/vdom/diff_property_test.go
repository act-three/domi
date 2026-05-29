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
// element in canonical form. The generator only produces single-rooted
// trees, so a multi-root result is a programming error.
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
	genTexts      = []string{"hi", "bye", "x", "y"}
	genKeys       = []string{"a", "b", "c", "d", "e", "f"}
)

const (
	genMaxDepth    = 4
	genMaxChildren = 5
	genMaxAttrs    = 3
	genKeyedChance = 50 // percent
	genTextChance  = 30 // percent
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
		return NewElement(tag, attrs, nil, nil)
	}

	// Decide whether this is a keyed parent. Keyed children must all be
	// elements (no text), so we always generate elements when keyed.
	keyed := rng.IntN(100) < genKeyedChance && n <= len(genKeys)
	if keyed {
		keys := slices.Clone(genKeys)
		rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		keys = keys[:n]
		children := make([]Node, n)
		for i := range n {
			// Mirror what domi.Keyed does at construction: append the
			// data-domi-key attribute so the rendered HTML carries the
			// identity the diff/apply round-trip needs.
			children[i] = genElement(rng.Uint64(), depth+1).WithAttr(Attr{Name: "data-domi-key", Value: keys[i]})
		}
		return NewElement(tag, attrs, children, keys)
	}
	children := make([]Node, 0, n)
	for range n {
		children = append(children, genNode(rng.Uint64(), depth+1))
	}
	return NewElement(tag, attrs, children, nil)
}

func genNode(seed uint64, depth int) Node {
	rng := rand.New(rand.NewPCG(seed, 0xC0FFEE))
	if depth >= genMaxDepth || rng.IntN(100) < genTextChance {
		return Text(genTexts[rng.IntN(len(genTexts))])
	}
	return genElement(rng.Uint64(), depth)
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
// generate two trees from it, run the diff through the production JS
// applier (jsdom under bun), and compare the post-apply HTML to
// Render(new) via golang.org/x/net/html — so attr-order, void-slash,
// and adjacent-text-merge differences in HTML serialization wash out.
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
	old := genElement(rng.Uint64(), 0)
	new := genElement(rng.Uint64(), 0)

	patches := diffOne(old, new)
	gotHTML, err := a.apply(Render(old), patches)
	if err != nil {
		if verbose {
			t.Fatalf("bun apply: %v\nold:  %s\nnew:  %s\npatches: %s",
				err, Render(old), Render(new), patchDebug(patches))
		}
		t.Fatalf("bun apply: %v  (replay: DOMI_SEED=%d go test -run TestDiffApplyProperty)", err, seed)
	}
	want := canonicalize(t, Render(new))
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		if verbose {
			t.Fatalf("diff/apply mismatch\nold:  %s\nnew:  %s\ngot html: %s\nwant: %s\ngot:  %s\npatches (%d): %s",
				Render(old), Render(new), gotHTML,
				jsonStr(want), jsonStr(got),
				len(patches), patchDebug(patches))
		}
		t.Fatalf("diff/apply mismatch  (replay: DOMI_SEED=%d go test -run TestDiffApplyProperty)", seed)
	}
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

	old := NewElement("div", attrs(), nil, nil)
	new := NewElement("div", attrs(Attr{Name: "class", Value: ""}), nil, nil)

	gotHTML, err := a.apply(Render(old), diffOne(old, new))
	if err != nil {
		t.Fatalf("bun apply: %v", err)
	}
	want := canonicalize(t, Render(new))
	got := canonicalize(t, gotHTML)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s, want %s (raw html: %s)", jsonStr(got), jsonStr(want), gotHTML)
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

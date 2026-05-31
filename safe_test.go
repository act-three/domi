package domi

import (
	"math/rand/v2"
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

func renderSafe(t *testing.T, s string) string {
	t.Helper()
	nodes, _ := lower(Safe(s))
	var buf strings.Builder
	for _, n := range nodes {
		_ = vdom.RenderTo(&buf, n)
	}
	return buf.String()
}

func TestSafe(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain text", "hello", "hello"},
		{"text with angle bracket", "a < b", "a &lt; b"},
		{"bold", "<b>bold</b>", "<b>bold</b>"},
		{"emphasis", "<em>italic</em>", "<em>italic</em>"},
		{"link with href",
			`<a href="/foo">link</a>`,
			`<a href="/foo">link</a>`},
		{"void element",
			`<img src="/pic.jpg" alt="pic">`,
			`<img alt="pic" src="/pic.jpg">`},
		{"nested",
			"<p><b>bold</b> text</p>",
			"<p><b>bold</b> text</p>"},
		{"mixed content",
			"hello <b>world</b>",
			"hello <b>world</b>"},
		{"deep nesting",
			"<div><p><b>deep</b></p></div>",
			"<div><p><b>deep</b></p></div>"},

		// Sanitization: dangerous tags removed entirely
		{"script removed",
			"<script>alert(1)</script>",
			""},
		{"style removed",
			"<style>body{color:red}</style>",
			""},
		{"nested script removed",
			"before<script><b>bad</b></script>after",
			"beforeafter"},
		{"iframe removed",
			`<iframe src="evil.html"></iframe>`,
			""},

		// Sanitization: unknown tags unwrapped
		{"unknown tag unwrapped",
			"<blink>text</blink>",
			"text"},
		{"unknown nested unwrapped",
			"<div><blink><b>text</b></blink></div>",
			"<div><b>text</b></div>"},

		// Sanitization: disallowed attributes stripped
		{"onclick stripped",
			`<div onclick="evil()">hi</div>`,
			"<div>hi</div>"},
		{"class on b stripped",
			`<b class="red">text</b>`,
			"<b>text</b>"},
		{"allowed attr kept disallowed stripped",
			`<a href="/ok" onclick="bad()">link</a>`,
			`<a href="/ok">link</a>`},

		// Sanitization: dangerous URL schemes
		{"javascript href removed",
			`<a href="javascript:alert(1)">xss</a>`,
			"<a>xss</a>"},
		{"data src removed",
			`<img src="data:text/html,<script>alert(1)</script>">`,
			"<img>"},
		{"file src removed",
			`<img src="file:///x">`,
			"<img>"},
		{"vbscript href removed",
			`<a href="vbscript:exec">xss</a>`,
			"<a>xss</a>"},

		// Embedded tabs/newlines bypass (WHATWG URL §4.1)
		{"javascript with tab",
			"<a href=\"java\tscript:alert(1)\">xss</a>",
			"<a>xss</a>"},
		{"javascript with newline",
			"<a href=\"java\nscript:alert(1)\">xss</a>",
			"<a>xss</a>"},
		{"javascript with carriage return",
			"<a href=\"java\rscript:alert(1)\">xss</a>",
			"<a>xss</a>"},
		{"javascript scattered whitespace",
			"<a href=\"j\ta\nv\ra\tscript:alert(1)\">xss</a>",
			"<a>xss</a>"},
		{"data with tab",
			"<img src=\"da\tta:text/html,<script>alert(1)</script>\">",
			"<img>"},

		// Table attributes
		{"table with colspan",
			`<table><tr><td colspan="2">cell</td></tr></table>`,
			`<table><tbody><tr><td colspan="2">cell</td></tr></tbody></table>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderSafe(t, tt.input); got != tt.want {
				t.Errorf("Safe(%q)\n got %q\nwant %q",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestIsSafeURLPropertyRejectsSchemes checks that for any blocked
// scheme, inserting arbitrary tabs, newlines, and carriage returns
// into the scheme name is still caught. This guards against the
// WHATWG URL Standard §4.1 normalization that browsers apply.
func TestIsSafeURLPropertyRejectsSchemes(t *testing.T) {
	const iterations = 2000
	schemes := []string{"javascript", "data", "file", "vbscript"}
	rng := rand.New(rand.NewPCG(0xBEEF, 0))

	for i := range iterations {
		scheme := schemes[rng.IntN(len(schemes))]
		mangled := injectWhitespace(rng, scheme)
		url := mangled + ":alert(1)"
		if isSafeURL(url) {
			t.Fatalf("[%d] isSafeURL(%q) = true, want false (scheme %q mangled to %q)",
				i, url, scheme, mangled)
		}
	}
}

// injectWhitespace inserts random \t, \n, \r between characters of s.
func injectWhitespace(rng *rand.Rand, s string) string {
	ws := []rune{'\t', '\n', '\r'}
	var b strings.Builder
	for _, r := range s {
		// 40% chance of injecting whitespace before this char
		if rng.IntN(5) < 2 {
			b.WriteRune(ws[rng.IntN(len(ws))])
		}
		b.WriteRune(r)
	}
	return b.String()
}

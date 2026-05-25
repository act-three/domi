package domi

import (
	"strings"
	"testing"

	"ily.dev/domi/internal/vdom"
)

func renderSafe(t *testing.T, s string) string {
	t.Helper()
	nodes := lower(Safe(s))
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

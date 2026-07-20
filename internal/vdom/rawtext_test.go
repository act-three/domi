package vdom

import "testing"

// CheckRawText accepts only text that reparses as the element's
// entire content. It is deliberately conservative — rejection needs
// just the substrings a breakout would require, not proof that one
// occurs — so some of the invalid cases below would in fact
// round-trip; what matters is that nothing invalid is accepted and
// that ordinary script and style content passes.
func TestCheckRawText(t *testing.T) {
	valid := []struct {
		tag, text string
	}{
		{"script", ""},
		{"script", "alert(1)"},
		{"script", "if (a < b && c > d) e();"},
		// "<script" without "</" or "<!--" is inert script data.
		{"script", "var s = '<script>';"},
		// A comment alone cannot hide the closing tag.
		{"script", "<!-- hidden -->"},
		{"script", "<!--"},
		// An end-tag sequence needs "</" immediately before the name.
		{"script", "a</scrip>b"},
		{"script", "</ script>"},
		{"style", ".a > .b { color: red }"},
		// Style's raw text has no comment handling, so "<!--<style>"
		// is inert.
		{"style", "<!--<style>"},
	}
	for _, c := range valid {
		if err := CheckRawText(c.tag, c.text); err != nil {
			t.Errorf("CheckRawText(%q, %q) = %v, want nil", c.tag, c.text, err)
		}
	}

	invalid := []struct {
		tag, text string
	}{
		{"script", "</script>"},
		{"script", "var a = '</script><img src=x onerror=alert(1)>';"},
		// Case-insensitively, wherever it appears, whatever follows:
		// a following space or "/" still reads as an end tag, and a
		// following letter is rejected conservatively.
		{"script", "</SCRIPT>"},
		{"script", "x</script y"},
		{"script", "</script/"},
		{"script", "</scripts>"},
		{"script", "</script"},
		// "<!--" plus "<script" can enter the double-escaped state,
		// where the element's own closing tag does not end it and the
		// element swallows whatever markup follows.
		{"script", "<!--<script>"},
		{"script", "<!--<script>var a = 1;"},
		// Rejected even balanced: the tokenizer's comment handling is
		// subtle enough that the check refuses the ingredients rather
		// than simulating the outcome.
		{"script", "<!--<script>var a = 1;//-->"},
		{"style", "</style>"},
		{"style", "</STYLE >"},
		{"style", "a { content: '</style' }"},
	}
	for _, c := range invalid {
		if err := CheckRawText(c.tag, c.text); err == nil {
			t.Errorf("CheckRawText(%q, %q) = nil, want error", c.tag, c.text)
		}
	}
}

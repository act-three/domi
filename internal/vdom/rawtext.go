package vdom

import (
	"fmt"
	"strings"
)

// IsRawTextElement reports whether tag contains CDATA-style raw text
// whose content an HTML parser does not entity-decode.
// In practice, this is only "script" and "style".
// (Tags textarea and title are not raw text,
// so ordinary text escaping already renders them correctly.)
func IsRawTextElement(tag string) bool {
	return tag == "script" || tag == "style"
}

// CheckRawText reports whether text is safe to write verbatim
// as the content of a raw-text element (see [IsRawTextElement])
// with the given tag.
// CheckRawText is conservative:
// everything it accepts is safe,
// and some safe strings are not accepted.
// (For instance, a balanced "<!--<script>...</script>-->" comment,
// or "</scriptx>" with an extra letter on the tag.)
func CheckRawText(tag, text string) error {
	// Soundness: a parser recognizes an end tag only at a contiguous,
	// case-insensitive "</" + tag name (HTML spec §13.2.5.17), so text
	// without that substring cannot end the element early. The only
	// other way to lose the element's closing tag is script's
	// double-escaped state (§13.2.5.27), reachable only through "<!--"
	// and then "<" + "script"; text lacking either substring leaves
	// the parser in a state from which the closing tag ends the
	// element. ToLower is a fold in the reject direction only — it can
	// map non-ASCII runes into a match the parser's ASCII-insensitive
	// names would miss — which errs conservative.
	lower := strings.ToLower(text)
	if strings.Contains(lower, "</"+tag) {
		return fmt.Errorf("<%s> text contains %q, which could end the element early", tag, "</"+tag)
	}
	if tag == "script" && strings.Contains(lower, "<!--") && strings.Contains(lower, "<script") {
		return fmt.Errorf(`<script> text combines "<!--" with "<script", which could hide the element's closing tag`)
	}
	return nil
}

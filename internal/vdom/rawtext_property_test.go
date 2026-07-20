package vdom

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// rawTextTokens are fragments chosen to probe the raw-text tokenizer
// behaviors CheckRawText guards against: end-tag sequences with each
// kind of name ending, partial and mis-cased spellings, the
// comment delimiters and their overlapping prefixes, and inert
// filler.
var rawTextTokens = []string{
	"</script>", "</script ", "</Script/", "</script\n", "</script",
	"</scrip", "<script>", "<script ", "<script",
	"</style>", "</style ", "</style",
	"<!--", "-->", "--", "->", "!",
	"<", "/", ">", "-", "x", " ", "\n", "\r",
}

func randomRawText(rng *rand.Rand) string {
	n := rng.IntN(8)
	var sb strings.Builder
	for range n {
		sb.WriteString(rawTextTokens[rng.IntN(len(rawTextTokens))])
	}
	return sb.String()
}

// TestCheckRawTextSound checks CheckRawText's one guarantee against
// the reference HTML parser: any text it accepts must round-trip —
// the serialized element reparses with exactly that text as its
// content, leaving a trailing sibling intact. Rejected text carries
// no claim (the check is deliberately conservative), so only the
// accept direction is asserted. The input is pre-normalized (CR → LF)
// the way the parser's input preprocessing would, so newline folding
// — which applies to all text, escaped or raw — doesn't read as a
// raw-text failure.
func TestCheckRawTextSound(t *testing.T) {
	const iterations = 3000
	for _, tag := range []string{"script", "style"} {
		rng := rand.New(rand.NewPCG(56, 0))
		accepted := 0
		for i := range iterations {
			s := randomRawText(rng)
			if CheckRawText(tag, s) != nil {
				continue
			}
			accepted++
			serialized := "<" + tag + ">" + s + "</" + tag + "><b>ok</b>"
			nodes := parseFragment(t, htmlNormalize(serialized))
			roundTrips := len(nodes) == 2 &&
				nodes[0].Data == tag && collectText(nodes[0]) == htmlNormalize(s) &&
				nodes[1].Data == "b" && collectText(nodes[1]) == "ok"
			if !roundTrips {
				t.Fatalf("[%s %d] CheckRawText accepted %q but it does not round-trip\n  serialized: %q\n  parsed to %d nodes",
					tag, i, s, serialized, len(nodes))
			}
		}
		if accepted == 0 {
			t.Fatalf("[%s] no generated text was accepted; the property was never exercised", tag)
		}
	}
}

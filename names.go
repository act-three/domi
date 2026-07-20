package domi

import (
	"fmt"
	"unicode/utf8"
)

// Rendered output must reparse into the tree it was rendered from:
// patch addressing assumes the vdom and the live DOM are 1:1 in
// shape and names. The browser's HTML parser lowercases every tag
// and attribute name it reads, then restores the canonical
// mixed-case spellings of known foreign-content (SVG and MathML)
// names from its case-adjustment tables. A name survives that round
// trip unchanged — and so is valid in domi — only if it is lowercase
// or one of those canonical spellings. A name must also match the
// XML Name production: HTML parsing accepts names like 1x or a?b,
// but the client applies attribute patches with setAttribute, which
// enforces the production and would throw InvalidCharacterError
// mid-patch. Constructors panic on invalid names. [UnsafeParseRaw]
// rejects them.

// isNameStartChar and isNameChar implement the XML Name production:
// Name ::= NameStartChar NameChar*.
func isNameStartChar(r rune) bool {
	switch {
	case r == ':' || r == '_',
		'A' <= r && r <= 'Z', 'a' <= r && r <= 'z',
		0xC0 <= r && r <= 0xD6, 0xD8 <= r && r <= 0xF6,
		0xF8 <= r && r <= 0x2FF, 0x370 <= r && r <= 0x37D,
		0x37F <= r && r <= 0x1FFF, 0x200C <= r && r <= 0x200D,
		0x2070 <= r && r <= 0x218F, 0x2C00 <= r && r <= 0x2FEF,
		0x3001 <= r && r <= 0xD7FF, 0xF900 <= r && r <= 0xFDCF,
		0xFDF0 <= r && r <= 0xFFFD, 0x10000 <= r && r <= 0xEFFFF:
		return true
	}
	return false
}

func isNameChar(r rune) bool {
	switch {
	case isNameStartChar(r),
		r == '-' || r == '.' || r == 0xB7,
		'0' <= r && r <= '9',
		0x300 <= r && r <= 0x36F, 0x203F <= r && r <= 0x2040:
		return true
	}
	return false
}

// isValidName returns whether name serializes verbatim and survives
// both the browser's parsing and its DOM APIs.
func isValidName(name string, foreign map[string]bool) bool {
	if !utf8.ValidString(name) {
		return false
	}
	hasUpper := false
	for i, r := range name {
		if i == 0 && !isNameStartChar(r) || !isNameChar(r) {
			return false
		}
		hasUpper = hasUpper || 'A' <= r && r <= 'Z'
	}
	return name != "" && (!hasUpper || foreign[name])
}

// isValidTagName returns whether name is a valid tag name.
func isValidTagName(name string) bool {
	if !isValidName(name, foreignTagNames) {
		return false
	}
	c := name[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// mustValidTagName panics unless name is a valid tag name.
func mustValidTagName(name string) {
	if !isValidTagName(name) {
		panic(fmt.Sprintf("domi: invalid tag name %q", name))
	}
}

// mustValidAttrName panics unless name is a valid attribute name.
func mustValidAttrName(name string) {
	if !isValidName(name, foreignAttrNames) {
		panic(fmt.Sprintf("domi: invalid attribute name %q", name))
	}
}

// foreignTagNames contains the mixed-case tag names the browser's
// parser produces in foreign content.
var foreignTagNames = map[string]bool{
	"altGlyph":            true,
	"altGlyphDef":         true,
	"altGlyphItem":        true,
	"animateColor":        true,
	"animateMotion":       true,
	"animateTransform":    true,
	"clipPath":            true,
	"feBlend":             true,
	"feColorMatrix":       true,
	"feComponentTransfer": true,
	"feComposite":         true,
	"feConvolveMatrix":    true,
	"feDiffuseLighting":   true,
	"feDisplacementMap":   true,
	"feDistantLight":      true,
	"feFlood":             true,
	"feFuncA":             true,
	"feFuncB":             true,
	"feFuncG":             true,
	"feFuncR":             true,
	"feGaussianBlur":      true,
	"feImage":             true,
	"feMerge":             true,
	"feMergeNode":         true,
	"feMorphology":        true,
	"feOffset":            true,
	"fePointLight":        true,
	"feSpecularLighting":  true,
	"feSpotLight":         true,
	"feTile":              true,
	"feTurbulence":        true,
	"foreignObject":       true,
	"glyphRef":            true,
	"linearGradient":      true,
	"radialGradient":      true,
	"textPath":            true,
}

// foreignAttrNames contains the mixed-case attribute names the
// browser's parser produces in foreign content.
var foreignAttrNames = map[string]bool{
	"attributeName":       true,
	"attributeType":       true,
	"baseFrequency":       true,
	"baseProfile":         true,
	"calcMode":            true,
	"clipPathUnits":       true,
	"definitionURL":       true,
	"diffuseConstant":     true,
	"edgeMode":            true,
	"filterUnits":         true,
	"glyphRef":            true,
	"gradientTransform":   true,
	"gradientUnits":       true,
	"kernelMatrix":        true,
	"kernelUnitLength":    true,
	"keyPoints":           true,
	"keySplines":          true,
	"keyTimes":            true,
	"lengthAdjust":        true,
	"limitingConeAngle":   true,
	"markerHeight":        true,
	"markerUnits":         true,
	"markerWidth":         true,
	"maskContentUnits":    true,
	"maskUnits":           true,
	"numOctaves":          true,
	"pathLength":          true,
	"patternContentUnits": true,
	"patternTransform":    true,
	"patternUnits":        true,
	"pointsAtX":           true,
	"pointsAtY":           true,
	"pointsAtZ":           true,
	"preserveAlpha":       true,
	"preserveAspectRatio": true,
	"primitiveUnits":      true,
	"refX":                true,
	"refY":                true,
	"repeatCount":         true,
	"repeatDur":           true,
	"requiredExtensions":  true,
	"requiredFeatures":    true,
	"specularConstant":    true,
	"specularExponent":    true,
	"spreadMethod":        true,
	"startOffset":         true,
	"stdDeviation":        true,
	"stitchTiles":         true,
	"surfaceScale":        true,
	"systemLanguage":      true,
	"tableValues":         true,
	"targetX":             true,
	"targetY":             true,
	"textLength":          true,
	"viewBox":             true,
	"viewTarget":          true,
	"xChannelSelector":    true,
	"yChannelSelector":    true,
	"zoomAndPan":          true,
}

// Package html provides constructors for common HTML elements.
package html

import "ily.dev/domi"

// An Element is an HTML element containing a tag name and attributes.
type Element = domi.Element

// A Node is text, an HTML element, or a fragment (a sequence of nodes).
//
// The constructors in this package return element nodes.
type Node = domi.Node

// A returns an "a" element with the given attributes.
func A(attr ...domi.Attr) Element { return domi.Tag("a", attr...) }

// Abbr returns an "abbr" element with the given attributes.
func Abbr(attr ...domi.Attr) Element { return domi.Tag("abbr", attr...) }

// Address returns an "address" element with the given attributes.
func Address(attr ...domi.Attr) Element { return domi.Tag("address", attr...) }

// Article returns an "article" element with the given attributes.
func Article(attr ...domi.Attr) Element { return domi.Tag("article", attr...) }

// Aside returns an "aside" element with the given attributes.
func Aside(attr ...domi.Attr) Element { return domi.Tag("aside", attr...) }

// Audio returns an "audio" element with the given attributes.
func Audio(attr ...domi.Attr) Element { return domi.Tag("audio", attr...) }

// B returns a "b" element with the given attributes.
func B(attr ...domi.Attr) Element { return domi.Tag("b", attr...) }

// Blockquote returns a "blockquote" element with the given attributes.
func Blockquote(attr ...domi.Attr) Element { return domi.Tag("blockquote", attr...) }

// Br returns a "br" element with the given attributes.
func Br(attr ...domi.Attr) Node { return domi.Tag("br", attr...)() }

// Button returns a "button" element with the given attributes.
func Button(attr ...domi.Attr) Element { return domi.Tag("button", attr...) }

// Canvas returns a "canvas" element with the given attributes.
func Canvas(attr ...domi.Attr) Element { return domi.Tag("canvas", attr...) }

// Caption returns a "caption" element with the given attributes.
func Caption(attr ...domi.Attr) Element { return domi.Tag("caption", attr...) }

// Cite returns a "cite" element with the given attributes.
func Cite(attr ...domi.Attr) Element { return domi.Tag("cite", attr...) }

// Code returns a "code" element with the given attributes.
func Code(attr ...domi.Attr) Element { return domi.Tag("code", attr...) }

// Col returns a "col" element with the given attributes.
func Col(attr ...domi.Attr) Node { return domi.Tag("col", attr...)() }

// ColGroup returns a "colgroup" element with the given attributes.
func ColGroup(attr ...domi.Attr) Element { return domi.Tag("colgroup", attr...) }

// Data returns a "data" element with the given attributes.
func Data(attr ...domi.Attr) Element { return domi.Tag("data", attr...) }

// DataList returns a "datalist" element with the given attributes.
func DataList(attr ...domi.Attr) Element { return domi.Tag("datalist", attr...) }

// DD returns a "dd" element with the given attributes.
func DD(attr ...domi.Attr) Element { return domi.Tag("dd", attr...) }

// Del returns a "del" element with the given attributes.
func Del(attr ...domi.Attr) Element { return domi.Tag("del", attr...) }

// Details returns a "details" element with the given attributes.
func Details(attr ...domi.Attr) Element { return domi.Tag("details", attr...) }

// Dfn returns a "dfn" element with the given attributes.
func Dfn(attr ...domi.Attr) Element { return domi.Tag("dfn", attr...) }

// Dialog returns a "dialog" element with the given attributes.
func Dialog(attr ...domi.Attr) Element { return domi.Tag("dialog", attr...) }

// Div returns a "div" element with the given attributes.
func Div(attr ...domi.Attr) Element { return domi.Tag("div", attr...) }

// DL returns a "dl" element with the given attributes.
func DL(attr ...domi.Attr) Element { return domi.Tag("dl", attr...) }

// DT returns a "dt" element with the given attributes.
func DT(attr ...domi.Attr) Element { return domi.Tag("dt", attr...) }

// Em returns an "em" element with the given attributes.
func Em(attr ...domi.Attr) Element { return domi.Tag("em", attr...) }

// Embed returns an "embed" element with the given attributes.
func Embed(attr ...domi.Attr) Node { return domi.Tag("embed", attr...)() }

// Fieldset returns a "fieldset" element with the given attributes.
func Fieldset(attr ...domi.Attr) Element { return domi.Tag("fieldset", attr...) }

// FigCaption returns a "figcaption" element with the given attributes.
func FigCaption(attr ...domi.Attr) Element { return domi.Tag("figcaption", attr...) }

// Figure returns a "figure" element with the given attributes.
func Figure(attr ...domi.Attr) Element { return domi.Tag("figure", attr...) }

// Footer returns a "footer" element with the given attributes.
func Footer(attr ...domi.Attr) Element { return domi.Tag("footer", attr...) }

// Form returns a "form" element with the given attributes.
func Form(attr ...domi.Attr) Element { return domi.Tag("form", attr...) }

// H1 returns an "h1" element with the given attributes.
func H1(attr ...domi.Attr) Element { return domi.Tag("h1", attr...) }

// H2 returns an "h2" element with the given attributes.
func H2(attr ...domi.Attr) Element { return domi.Tag("h2", attr...) }

// H3 returns an "h3" element with the given attributes.
func H3(attr ...domi.Attr) Element { return domi.Tag("h3", attr...) }

// H4 returns an "h4" element with the given attributes.
func H4(attr ...domi.Attr) Element { return domi.Tag("h4", attr...) }

// H5 returns an "h5" element with the given attributes.
func H5(attr ...domi.Attr) Element { return domi.Tag("h5", attr...) }

// H6 returns an "h6" element with the given attributes.
func H6(attr ...domi.Attr) Element { return domi.Tag("h6", attr...) }

// Head returns a "head" element with the given attributes.
func Head(attr ...domi.Attr) Element { return domi.Tag("head", attr...) }

// Header returns a "header" element with the given attributes.
func Header(attr ...domi.Attr) Element { return domi.Tag("header", attr...) }

// HGroup returns an "hgroup" element with the given attributes.
func HGroup(attr ...domi.Attr) Element { return domi.Tag("hgroup", attr...) }

// HR returns an "hr" element with the given attributes.
func HR(attr ...domi.Attr) Node { return domi.Tag("hr", attr...)() }

// HTML returns an "html" element with the given attributes.
func HTML(attr ...domi.Attr) Element { return domi.Tag("html", attr...) }

// I returns an "i" element with the given attributes.
func I(attr ...domi.Attr) Element { return domi.Tag("i", attr...) }

// IFrame returns an "iframe" element with the given attributes.
func IFrame(attr ...domi.Attr) Element { return domi.Tag("iframe", attr...) }

// Img returns an "img" element with the given attributes.
func Img(attr ...domi.Attr) Node { return domi.Tag("img", attr...)() }

// Input returns an "input" element with the given attributes.
func Input(attr ...domi.Attr) Node { return domi.Tag("input", attr...)() }

// Ins returns an "ins" element with the given attributes.
func Ins(attr ...domi.Attr) Element { return domi.Tag("ins", attr...) }

// Kbd returns a "kbd" element with the given attributes.
func Kbd(attr ...domi.Attr) Element { return domi.Tag("kbd", attr...) }

// Label returns a "label" element with the given attributes.
func Label(attr ...domi.Attr) Element { return domi.Tag("label", attr...) }

// Legend returns a "legend" element with the given attributes.
func Legend(attr ...domi.Attr) Element { return domi.Tag("legend", attr...) }

// LI returns an "li" element with the given attributes.
func LI(attr ...domi.Attr) Element { return domi.Tag("li", attr...) }

// Link returns a "link" element with the given attributes.
func Link(attr ...domi.Attr) Node { return domi.Tag("link", attr...)() }

// Main returns a "main" element with the given attributes.
func Main(attr ...domi.Attr) Element { return domi.Tag("main", attr...) }

// Mark returns a "mark" element with the given attributes.
func Mark(attr ...domi.Attr) Element { return domi.Tag("mark", attr...) }

// Menu returns a "menu" element with the given attributes.
func Menu(attr ...domi.Attr) Element { return domi.Tag("menu", attr...) }

// Meta returns a "meta" element with the given attributes.
func Meta(attr ...domi.Attr) Node { return domi.Tag("meta", attr...)() }

// Meter returns a "meter" element with the given attributes.
func Meter(attr ...domi.Attr) Element { return domi.Tag("meter", attr...) }

// Nav returns a "nav" element with the given attributes.
func Nav(attr ...domi.Attr) Element { return domi.Tag("nav", attr...) }

// OL returns an "ol" element with the given attributes.
func OL(attr ...domi.Attr) Element { return domi.Tag("ol", attr...) }

// OptGroup returns an "optgroup" element with the given attributes.
func OptGroup(attr ...domi.Attr) Element { return domi.Tag("optgroup", attr...) }

// Option returns an "option" element with the given attributes.
func Option(attr ...domi.Attr) Element { return domi.Tag("option", attr...) }

// Output returns an "output" element with the given attributes.
func Output(attr ...domi.Attr) Element { return domi.Tag("output", attr...) }

// P returns a "p" element with the given attributes.
func P(attr ...domi.Attr) Element { return domi.Tag("p", attr...) }

// Picture returns a "picture" element with the given attributes.
func Picture(attr ...domi.Attr) Element { return domi.Tag("picture", attr...) }

// Pre returns a "pre" element with the given attributes.
func Pre(attr ...domi.Attr) Element { return domi.Tag("pre", attr...) }

// Progress returns a "progress" element with the given attributes.
func Progress(attr ...domi.Attr) Element { return domi.Tag("progress", attr...) }

// Q returns a "q" element with the given attributes.
func Q(attr ...domi.Attr) Element { return domi.Tag("q", attr...) }

// RP returns an "rp" element with the given attributes.
func RP(attr ...domi.Attr) Element { return domi.Tag("rp", attr...) }

// RT returns an "rt" element with the given attributes.
func RT(attr ...domi.Attr) Element { return domi.Tag("rt", attr...) }

// Ruby returns a "ruby" element with the given attributes.
func Ruby(attr ...domi.Attr) Element { return domi.Tag("ruby", attr...) }

// S returns an "s" element with the given attributes.
func S(attr ...domi.Attr) Element { return domi.Tag("s", attr...) }

// Samp returns a "samp" element with the given attributes.
func Samp(attr ...domi.Attr) Element { return domi.Tag("samp", attr...) }

// Script returns a "script" element with the given attributes.
func Script(attr ...domi.Attr) Element { return domi.Tag("script", attr...) }

// Search returns a "search" element with the given attributes.
func Search(attr ...domi.Attr) Element { return domi.Tag("search", attr...) }

// Section returns a "section" element with the given attributes.
func Section(attr ...domi.Attr) Element { return domi.Tag("section", attr...) }

// Select returns a "select" element with the given attributes.
func Select(attr ...domi.Attr) Element { return domi.Tag("select", attr...) }

// Slot returns a "slot" element with the given attributes.
func Slot(attr ...domi.Attr) Element { return domi.Tag("slot", attr...) }

// Small returns a "small" element with the given attributes.
func Small(attr ...domi.Attr) Element { return domi.Tag("small", attr...) }

// Source returns a "source" element with the given attributes.
func Source(attr ...domi.Attr) Node { return domi.Tag("source", attr...)() }

// Span returns a "span" element with the given attributes.
func Span(attr ...domi.Attr) Element { return domi.Tag("span", attr...) }

// Strong returns a "strong" element with the given attributes.
func Strong(attr ...domi.Attr) Element { return domi.Tag("strong", attr...) }

// Sub returns a "sub" element with the given attributes.
func Sub(attr ...domi.Attr) Element { return domi.Tag("sub", attr...) }

// Summary returns a "summary" element with the given attributes.
func Summary(attr ...domi.Attr) Element { return domi.Tag("summary", attr...) }

// Sup returns a "sup" element with the given attributes.
func Sup(attr ...domi.Attr) Element { return domi.Tag("sup", attr...) }

// Table returns a "table" element with the given attributes.
func Table(attr ...domi.Attr) Element { return domi.Tag("table", attr...) }

// TBody returns a "tbody" element with the given attributes.
func TBody(attr ...domi.Attr) Element { return domi.Tag("tbody", attr...) }

// TD returns a "td" element with the given attributes.
func TD(attr ...domi.Attr) Element { return domi.Tag("td", attr...) }

// Template returns a "template" element with the given attributes.
func Template(attr ...domi.Attr) Element { return domi.Tag("template", attr...) }

// Textarea returns a "textarea" element with the given attributes.
func Textarea(attr ...domi.Attr) Element { return domi.Tag("textarea", attr...) }

// TFoot returns a "tfoot" element with the given attributes.
func TFoot(attr ...domi.Attr) Element { return domi.Tag("tfoot", attr...) }

// TH returns a "th" element with the given attributes.
func TH(attr ...domi.Attr) Element { return domi.Tag("th", attr...) }

// THead returns a "thead" element with the given attributes.
func THead(attr ...domi.Attr) Element { return domi.Tag("thead", attr...) }

// Time returns a "time" element with the given attributes.
func Time(attr ...domi.Attr) Element { return domi.Tag("time", attr...) }

// Title returns a "title" element with the given attributes.
func Title(attr ...domi.Attr) Element { return domi.Tag("title", attr...) }

// TR returns a "tr" element with the given attributes.
func TR(attr ...domi.Attr) Element { return domi.Tag("tr", attr...) }

// Track returns a "track" element with the given attributes.
func Track(attr ...domi.Attr) Node { return domi.Tag("track", attr...)() }

// U returns a "u" element with the given attributes.
func U(attr ...domi.Attr) Element { return domi.Tag("u", attr...) }

// UL returns a "ul" element with the given attributes.
func UL(attr ...domi.Attr) Element { return domi.Tag("ul", attr...) }

// Var returns a "var" element with the given attributes.
func Var(attr ...domi.Attr) Element { return domi.Tag("var", attr...) }

// Video returns a "video" element with the given attributes.
func Video(attr ...domi.Attr) Element { return domi.Tag("video", attr...) }

// Wbr returns a "wbr" element with the given attributes.
func Wbr(attr ...domi.Attr) Node { return domi.Tag("wbr", attr...)() }

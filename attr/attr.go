// Package attr provides constructors for common HTML attributes.
package attr

import (
	"fmt"

	"ily.dev/domi"
)

// An Attr is an HTML attribute.
type Attr = domi.Attr

// Accept returns an "accept" attribute with the given value.
func Accept(value string) Attr { return domi.Name("accept")(value) }

// AcceptCharset returns an "accept-charset" attribute with the given value.
func AcceptCharset(value string) Attr { return domi.Name("accept-charset")(value) }

// AccessKey returns an "accesskey" attribute with the given value.
func AccessKey(value string) Attr { return domi.Name("accesskey")(value) }

// Action returns an "action" attribute with the given value.
func Action(value string) Attr { return domi.Name("action")(value) }

// Alt returns an "alt" attribute with the given value.
func Alt(value string) Attr { return domi.Name("alt")(value) }

// Autocapitalize returns an "autocapitalize" attribute with the given value.
func Autocapitalize(value string) Attr { return domi.Name("autocapitalize")(value) }

// Autocomplete returns an "autocomplete" attribute with the given value.
func Autocomplete(value string) Attr { return domi.Name("autocomplete")(value) }

// Blocking returns a "blocking" attribute with the given value.
func Blocking(value string) Attr { return domi.Name("blocking")(value) }

// Charset returns a "charset" attribute with the given value.
func Charset(value string) Attr { return domi.Name("charset")(value) }

// Cite returns a "cite" attribute with the given value.
func Cite(value string) Attr { return domi.Name("cite")(value) }

// Class returns a "class" attribute with the given values
// joined with " " (space) between elements.
func Class(value ...string) Attr { return domi.Name("class")(value...) }

// Cols returns a "cols" attribute with the given value.
func Cols(value string) Attr { return domi.Name("cols")(value) }

// ColSpan returns a "colspan" attribute with the given value.
func ColSpan(value string) Attr { return domi.Name("colspan")(value) }

// Content returns a "content" attribute with the given value.
func Content(value string) Attr { return domi.Name("content")(value) }

// CrossOrigin returns a "crossorigin" attribute with the given value.
func CrossOrigin(value string) Attr { return domi.Name("crossorigin")(value) }

// DateTime returns a "datetime" attribute with the given value.
func DateTime(value string) Attr { return domi.Name("datetime")(value) }

// Decoding returns a "decoding" attribute with the given value.
func Decoding(value string) Attr { return domi.Name("decoding")(value) }

// Dir returns a "dir" attribute with the given value.
func Dir(value string) Attr { return domi.Name("dir")(value) }

// Download returns a "download" attribute with the given value.
func Download(value string) Attr { return domi.Name("download")(value) }

// Enctype returns an "enctype" attribute with the given value.
func Enctype(value string) Attr { return domi.Name("enctype")(value) }

// EnterKeyHint returns an "enterkeyhint" attribute with the given value.
func EnterKeyHint(value string) Attr { return domi.Name("enterkeyhint")(value) }

// FetchPriority returns a "fetchpriority" attribute with the given value.
func FetchPriority(value string) Attr { return domi.Name("fetchpriority")(value) }

// For returns a "for" attribute with the given value.
func For(value string) Attr { return domi.Name("for")(value) }

// Form returns a "form" attribute with the given value.
func Form(value string) Attr { return domi.Name("form")(value) }

// FormAction returns a "formaction" attribute with the given value.
func FormAction(value string) Attr { return domi.Name("formaction")(value) }

// FormEnctype returns a "formenctype" attribute with the given value.
func FormEnctype(value string) Attr { return domi.Name("formenctype")(value) }

// FormMethod returns a "formmethod" attribute with the given value.
func FormMethod(value string) Attr { return domi.Name("formmethod")(value) }

// FormTarget returns a "formtarget" attribute with the given value.
func FormTarget(value string) Attr { return domi.Name("formtarget")(value) }

// Headers returns a "headers" attribute with the given value.
func Headers(value string) Attr { return domi.Name("headers")(value) }

// Height returns a "height" attribute with the given value.
func Height(value string) Attr { return domi.Name("height")(value) }

// High returns a "high" attribute with the given value.
func High(value string) Attr { return domi.Name("high")(value) }

// Href returns an "href" attribute with the given value.
func Href(value string) Attr { return domi.Name("href")(value) }

// HrefLang returns an "hreflang" attribute with the given value.
func HrefLang(value string) Attr { return domi.Name("hreflang")(value) }

// ID returns an "id" attribute with the given value.
func ID(value string) Attr { return domi.Name("id")(value) }

// InputMode returns an "inputmode" attribute with the given value.
func InputMode(value string) Attr { return domi.Name("inputmode")(value) }

// Integrity returns an "integrity" attribute with the given value.
func Integrity(value string) Attr { return domi.Name("integrity")(value) }

// ItemProp returns an "itemprop" attribute with the given value.
func ItemProp(value string) Attr { return domi.Name("itemprop")(value) }

// Kind returns a "kind" attribute with the given value.
func Kind(value string) Attr { return domi.Name("kind")(value) }

// Label returns a "label" attribute with the given value.
func Label(value string) Attr { return domi.Name("label")(value) }

// Lang returns a "lang" attribute with the given value.
func Lang(value string) Attr { return domi.Name("lang")(value) }

// List returns a "list" attribute with the given value.
func List(value string) Attr { return domi.Name("list")(value) }

// Loading returns a "loading" attribute with the given value.
func Loading(value string) Attr { return domi.Name("loading")(value) }

// Low returns a "low" attribute with the given value.
func Low(value string) Attr { return domi.Name("low")(value) }

// Max returns a "max" attribute with the given value.
func Max(value string) Attr { return domi.Name("max")(value) }

// MaxLength returns a "maxlength" attribute with the given value.
func MaxLength(value string) Attr { return domi.Name("maxlength")(value) }

// Media returns a "media" attribute with the given value.
func Media(value string) Attr { return domi.Name("media")(value) }

// Method returns a "method" attribute with the given value.
func Method(value string) Attr { return domi.Name("method")(value) }

// Min returns a "min" attribute with the given value.
func Min(value string) Attr { return domi.Name("min")(value) }

// MinLength returns a "minlength" attribute with the given value.
func MinLength(value string) Attr { return domi.Name("minlength")(value) }

// Name returns a "name" attribute with the given value.
func Name(value string) Attr { return domi.Name("name")(value) }

// Nonce returns a "nonce" attribute with the given value.
func Nonce(value string) Attr { return domi.Name("nonce")(value) }

// Optimum returns an "optimum" attribute with the given value.
func Optimum(value string) Attr { return domi.Name("optimum")(value) }

// Pattern returns a "pattern" attribute with the given value.
func Pattern(value string) Attr { return domi.Name("pattern")(value) }

// Ping returns a "ping" attribute with the given value.
func Ping(value string) Attr { return domi.Name("ping")(value) }

// Placeholder returns a "placeholder" attribute with the given value.
func Placeholder(value string) Attr { return domi.Name("placeholder")(value) }

// Popover returns a "popover" attribute with the given value.
func Popover(value string) Attr { return domi.Name("popover")(value) }

// PopoverTarget returns a "popovertarget" attribute with the given value.
func PopoverTarget(value string) Attr { return domi.Name("popovertarget")(value) }

// PopoverTargetAction returns a "popovertargetaction" attribute with the given value.
func PopoverTargetAction(value string) Attr { return domi.Name("popovertargetaction")(value) }

// Poster returns a "poster" attribute with the given value.
func Poster(value string) Attr { return domi.Name("poster")(value) }

// Preload returns a "preload" attribute with the given value.
func Preload(value string) Attr { return domi.Name("preload")(value) }

// ReferrerPolicy returns a "referrerpolicy" attribute with the given value.
func ReferrerPolicy(value string) Attr { return domi.Name("referrerpolicy")(value) }

// Rel returns a "rel" attribute with the given value.
func Rel(value string) Attr { return domi.Name("rel")(value) }

// Role returns a "role" attribute with the given value.
func Role(value string) Attr { return domi.Name("role")(value) }

// Rows returns a "rows" attribute with the given value.
func Rows(value string) Attr { return domi.Name("rows")(value) }

// RowSpan returns a "rowspan" attribute with the given value.
func RowSpan(value string) Attr { return domi.Name("rowspan")(value) }

// Sandbox returns a "sandbox" attribute with the given value.
func Sandbox(value string) Attr { return domi.Name("sandbox")(value) }

// Scope returns a "scope" attribute with the given value.
func Scope(value string) Attr { return domi.Name("scope")(value) }

// Size returns a "size" attribute with the given value.
func Size(value string) Attr { return domi.Name("size")(value) }

// Sizes returns a "sizes" attribute with the given value.
func Sizes(value string) Attr { return domi.Name("sizes")(value) }

// Slot returns a "slot" attribute with the given value.
func Slot(value string) Attr { return domi.Name("slot")(value) }

// Span returns a "span" attribute with the given value.
func Span(value string) Attr { return domi.Name("span")(value) }

// Src returns a "src" attribute with the given value.
func Src(value string) Attr { return domi.Name("src")(value) }

// SrcDoc returns a "srcdoc" attribute with the given value.
func SrcDoc(value string) Attr { return domi.Name("srcdoc")(value) }

// SrcLang returns a "srclang" attribute with the given value.
func SrcLang(value string) Attr { return domi.Name("srclang")(value) }

// SrcSet returns a "srcset" attribute with the given value.
func SrcSet(value string) Attr { return domi.Name("srcset")(value) }

// Start returns a "start" attribute with the given value.
func Start(value string) Attr { return domi.Name("start")(value) }

// Step returns a "step" attribute with the given value.
func Step(value string) Attr { return domi.Name("step")(value) }

// Style returns a "style" attribute with the given values
// joined with ";" between elements.
func Style(value ...string) Attr { return domi.Name("style")(value...) }

// TabIndex returns a "tabindex" attribute with the given value.
func TabIndex(value string) Attr { return domi.Name("tabindex")(value) }

// Target returns a "target" attribute with the given value.
func Target(value string) Attr { return domi.Name("target")(value) }

// Title returns a "title" attribute with the given value.
func Title(value string) Attr { return domi.Name("title")(value) }

// Type returns a "type" attribute with the given value.
func Type(value string) Attr { return domi.Name("type")(value) }

// Value returns a "value" attribute with the given value.
func Value(value string) Attr { return domi.Name("value")(value) }

// Width returns a "width" attribute with the given value.
func Width(value string) Attr { return domi.Name("width")(value) }

// Wrap returns a "wrap" attribute with the given value.
func Wrap(value string) Attr { return domi.Name("wrap")(value) }

// Autofocus returns an "autofocus" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Autofocus(b bool) Attr { return domi.Bool("autofocus")(b) }

// Autoplay returns an "autoplay" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Autoplay(b bool) Attr { return domi.Bool("autoplay")(b) }

// Checked returns a "checked" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Checked(b bool) Attr { return domi.Bool("checked")(b) }

// Controls returns a "controls" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Controls(b bool) Attr { return domi.Bool("controls")(b) }

// Default returns a "default" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Default(b bool) Attr { return domi.Bool("default")(b) }

// Disabled returns a "disabled" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Disabled(b bool) Attr { return domi.Bool("disabled")(b) }

// Draggable returns a "draggable" attribute.
// It has the value "true" when b is true and "false" otherwise.
func Draggable(b bool) Attr { return domi.Bool("draggable")(b) }

// FormNoValidate returns a "formnovalidate" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func FormNoValidate(b bool) Attr { return domi.Bool("formnovalidate")(b) }

// Inert returns an "inert" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Inert(b bool) Attr { return domi.Bool("inert")(b) }

// Loop returns a "loop" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Loop(b bool) Attr { return domi.Bool("loop")(b) }

// Multiple returns a "multiple" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Multiple(b bool) Attr { return domi.Bool("multiple")(b) }

// Muted returns a "muted" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Muted(b bool) Attr { return domi.Bool("muted")(b) }

// NoValidate returns a "novalidate" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func NoValidate(b bool) Attr { return domi.Bool("novalidate")(b) }

// Open returns an "open" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Open(b bool) Attr { return domi.Bool("open")(b) }

// PlaysInline returns a "playsinline" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func PlaysInline(b bool) Attr { return domi.Bool("playsinline")(b) }

// ReadOnly returns a "readonly" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func ReadOnly(b bool) Attr { return domi.Bool("readonly")(b) }

// Required returns a "required" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Required(b bool) Attr { return domi.Bool("required")(b) }

// Reversed returns a "reversed" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Reversed(b bool) Attr { return domi.Bool("reversed")(b) }

// Selected returns a "selected" attribute.
// It is present as a name-only attribute when b is true and absent otherwise.
func Selected(b bool) Attr { return domi.Bool("selected")(b) }

// Spellcheck returns a "spellcheck" attribute.
// It has the value "true" when b is true and "false" otherwise.
func Spellcheck(b bool) Attr { return domi.Bool("spellcheck")(b) }

// Translate returns a "translate" attribute.
// It has the value "yes" when b is true and "no" otherwise.
func Translate(b bool) Attr { return domi.Bool("translate")(b) }

// Stylef returns a style attribute formatted with [fmt.Sprintf].
func Stylef(format string, a ...any) Attr {
	return Style(fmt.Sprintf(format, a...))
}

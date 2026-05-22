// Package domi is a server-rendered framework for building browser
// applications in Go. An application is a state machine: it implements
// [App], whose View method renders the current state as a [Node] tree
// and whose Update method transitions the state in response to events.
// The framework hosts the application behind an [http.Handler], keeps
// the browser's view in sync with whatever View returns, and dispatches
// user-generated events back through Update.
//
// The package exposes only the primitives needed to build any node or
// attribute ([Tag], [Keyed], [Fragment], [Text], [Attribute], [Group],
// [On]). Convenience wrappers for common HTML tags, attributes, and
// events live in [ily.dev/domi/html], [ily.dev/domi/attr], and
// [ily.dev/domi/event].
//
// A [Node] is anything that can appear in the tree: text, an element
// built via [Tag], or a keyed element built via [Keyed]. [Tag] and
// [Keyed] return curried builders — first attributes, then children.
// An element with no children is itself a [Node], so void elements
// (e.g. Br, Input) and other childless tags can appear in a parent's
// child list without a trailing empty children call.
//
// Attribute names beginning with "data-domi-" are reserved for use by
// this package and its subpackages. Application code and third-party
// packages should pick data attributes outside that prefix to avoid
// collisions with framework internals — present or future.
package domi

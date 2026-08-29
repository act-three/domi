// Package domi is a server-rendered framework
// for building browser applications in Go.
// Client packages implement interface [App].
// Method [App.View] renders the app's current state as a tree of [Node] values.
// Method [App.Update] transitions the state in response to events.
// Domi hosts the application as an [http.Handler].
// It keeps the browser's DOM in sync with the return value of View,
// and dispatches browser events to Update.
//
// Package domi exposes primitives for the app
// to build any node or attribute.
// Helpers for common HTML tags, attributes, and events
// can be found in [ily.dev/domi/html],
// [ily.dev/domi/attr],
// and [ily.dev/domi/event].
//
// # Rendered Output
//
// Domi renders the view inside a domi-root element just inside body,
// so that browser extensions placing their own elements inside body
// don't interfere with the app.
//
//	<body>
//	    <domi-root>
//	        <!-- output of View -->
//	    </domi-root>
//	</body>
//
// Element domi-root has the CSS property "display:contents",
// so the view's elements participate in layout
// as if they were direct children of the body element.
//
// # Form Controls
//
// Form controls display the value rendered by [App.View].
// This includes updates to the rendered value
// after the control has been edited by the user.
// Some programming environments call this "controlled components".
// The app is responsible for rendering the current value
// of each form control at all times.
// This behavior applies to the following cases:
//
//	input (except type=file)  => value attribute
//	textarea                  => text contents
//	input type=checkbox       => checked attribute
//	input type=radio          => checked attribute
//	option                    => selected attribute
//
// While the user is editing a control,
// domi avoids disturbing their in-progress work
// by suspending its updates to that control's value.
// When an "input" or "change" event handler is present,
// the event commits the user's changes
// and domi resumes applying server updates.
//
// Opaque form controls (see [WithKeyOpaque])
// are never updated by domi,
// like all opaque DOM content.
//
// # Bundling the Client
//
// The client-side runtime for domi lives in file client.js
// at the module root.
// Apps using [NewServer] without further customization
// don't need to access this file directly.
// Domi includes it in the default document head.
//
// Apps that provide their own document shell
// (see [Document])
// should use the following command in build automation
// to add client.js into their JavaScript bundle.
// Obtain the filesystem path of client.js by running:
//
//	go list -m -f '{{.Dir}}/client.js' ily.dev/domi
//
// Include this path in the app's JavaScript bundle.
// Then import the module, and call run:
//
//	import * as Domi from "path/to/bundle.js";
//	Domi.run();
package domi

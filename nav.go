package domi

import (
	"fmt"
	"net/url"
	"slices"
)

// PushURL returns a Cmd that changes the browser URL
// and adds an entry to the navigation history.
// The url must be an origin-relative URL (path, query, fragment)
// with no scheme or host.
//
// The resulting Cmd dispatches the onURLChange callback
// registered on [Handler] so the app can update its route state,
// and bundles the history.pushState instruction
// into the same SSE frame as the DOM patches from that update.
func PushURL[Msg any](url string) Cmd[Msg] {
	u := mustParseRelativeURL("domi.PushURL", url)
	href := u.String()
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *session[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{push: href}
		},
	}))
}

// ReplaceURL returns a Cmd that changes the browser URL
// without adding an entry to the navigation history.
// The url must be an origin-relative URL (path, query, fragment)
// with no scheme or host.
//
// The resulting Cmd dispatches the onURLChange callback
// registered on [Handler] so the app can update its route state,
// and bundles the history.replaceState instruction
// into the same SSE frame as the DOM patches from that update.
func ReplaceURL[Msg any](url string) Cmd[Msg] {
	u := mustParseRelativeURL("domi.ReplaceURL", url)
	href := u.String()
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *session[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{replace: href}
		},
	}))
}

// Load returns a Cmd that triggers a full-page browser navigation
// to url, leaving the current session behind. Unlike [PushURL] and
// [ReplaceURL], which update the history of the running application,
// Load performs a real navigation (window.location): the browser
// discards the current document and fetches a fresh one. The url may
// therefore be absolute and cross-origin.
//
// Load is the escape hatch for links the application does not route
// itself — logging out, leaving for an external site, or following a
// same-origin link served outside the domi app. The app returns it
// from Update in response to a [URLRequest] it decides not to handle
// internally. To opt a link out of interception ahead of time, without
// a server round trip, give the anchor the domi-bypass attribute
// instead.
func Load[Msg any](url string) Cmd[Msg] {
	mustParseURL("domi.Load", url)
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *session[Msg]) (Msg, *nav) {
			var zero Msg
			return zero, &nav{load: url}
		},
	}))
}

// mustParseURL parses url and panics if it is malformed. Unlike
// [mustParseRelativeURL] it permits a scheme and host: a [Load] target
// may be any absolute or relative URL the browser can navigate to.
func mustParseURL(caller, raw string) {
	if _, err := url.Parse(raw); err != nil {
		panic(fmt.Errorf("%s: %w", caller, err))
	}
}

// mustParseRelativeURL parses url and panics if it is malformed or
// contains a scheme or host. Navigation URLs must be relative to the
// application's origin.
func mustParseRelativeURL(caller, raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Errorf("%s: %w", caller, err))
	}
	if u.Scheme != "" || u.Host != "" {
		panic(fmt.Errorf("%s: url must be relative, got %q", caller, raw))
	}
	return u
}

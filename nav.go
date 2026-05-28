package domi

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"slices"
)

// A URLRequest represents a user clicking a link in the browser.
// The framework intercepts the click, prevents the default navigation,
// and dispatches the request through the onURLRequest callback
// registered on [Handler].
//
// Internal is true when the link target shares the current page's
// origin (same scheme, host, and port). For internal requests the app
// typically returns a [PushURL] command; for external requests it may
// ignore the event or navigate with a full page load.
type URLRequest struct {
	URL      *url.URL
	Internal bool
}

// PushURL returns a [Cmd] that changes the browser URL
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
	return Cmd[Msg]{slices.Values([]cmd[Msg]{
		func(s *session[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{push: href, outgoingID: rand.Text()}
		},
	})}
}

// ReplaceURL returns a [Cmd] that changes the browser URL
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
	return Cmd[Msg]{slices.Values([]cmd[Msg]{
		func(s *session[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{replace: href}
		},
	})}
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

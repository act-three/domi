package domi

import (
	"fmt"
	"net/url"
	"slices"
)

// PushURL changes the browser URL
// and adds an entry to the navigation history.
//
// The url must be an origin-relative URL (path, query, fragment)
// with no scheme or host.
func PushURL[Msg any](url string) Cmd[Msg] {
	u := mustParseRelativeURL("domi.PushURL", url)
	href := u.String()
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *instance[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{push: href}
		},
	}))
}

// ReplaceURL changes the browser URL
// without adding an entry to the navigation history.
//
// The url must be an origin-relative URL (path, query, fragment)
// with no scheme or host.
func ReplaceURL[Msg any](url string) Cmd[Msg] {
	u := mustParseRelativeURL("domi.ReplaceURL", url)
	href := u.String()
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *instance[Msg]) (Msg, *nav) {
			return s.sv.onURLChange(u), &nav{replace: href}
		},
	}))
}

// Load causes a full-page browser navigation to url,
// leaving the domi instance behind.
//
// The url can be any valid URL,
// including cross-scheme and cross-origin.
//
// To obtain this behavior from a link
// without a server round trip,
// use [Bypass] instead.
func Load[Msg any](url string) Cmd[Msg] {
	mustParseURL("domi.Load", url)
	return batch[Msg](slices.Values([]cmd[Msg]{
		func(s *instance[Msg]) (Msg, *nav) {
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

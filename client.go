package domi

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/http"
	"time"
)

//go:embed client.js
var rawClientJS []byte

var clientJS = append(rawClientJS, "\nrun();\n"...)

var clientJSDigest = func() string {
	h := sha256.Sum256(clientJS)
	return fmt.Sprintf("%x", h[:3])
}()

// ClientModule provides access
// to the JavaScript module used by domi.
//
// The returned handler serves the module
// at a URL path chosen by the app.
//
// The returned digest is a content hash of the module.
// Include it in the request path
// to ensure the browser never uses
// a module from the wrong version of domi.
//
//	digest, handler := domi.ClientModule()
//	path := "/domi." + digest + ".js"
//	mux := &http.ServeMux{}
//	mux.Handle(path, handler)
//
// The module can then be placed into the document head.
// See [Document].
//
//	html.Script(attr.Type("module"), attr.Src(path))
func ClientModule() (digest string, h http.Handler) {
	return clientJSDigest, http.HandlerFunc(clientJSHandler)
}

func clientJSHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=31536000, immutable")
	http.ServeContent(w, req, "domi.js", time.Time{}, bytes.NewReader(clientJS))
}

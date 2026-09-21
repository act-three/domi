package domi

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/http"
	"path"
	"time"
)

//go:embed domi.js
var rawClientJS []byte

var clientJS = append(rawClientJS, "\nrun();\n"...)

var clientJSDigest = func() string {
	h := sha256.Sum256(clientJS)
	return fmt.Sprintf("%x", h[:3])
}()

// ClientModule returns an HTML script element
// that loads the JavaScript module used by Domi.
// The returned Node can be placed into the head element
// of the application's custom document, if any.
// See [Document].
//
// The given prefix must match the value
// used in [InternalURLPrefix].
//
// Applications that do not provide a custom document
// do not need to use ClientModule.
// Applications that bundle the Domi module
// with additional JavaScript do not need to use ClientModule.
// See the “Serving the Client JavaScript Module” section
// in the package documentation for details.
func ClientModule(prefix string) Node {
	return Tag("script",
		Name("type", "module"),
		Name("src", clientJSPath(prefix)),
	)
}

// clientJSPath returns the URL path where a server
// with the given internal URL prefix serves the client module.
func clientJSPath(prefix string) string {
	return path.Join("/", prefix, "domi."+clientJSDigest+".js")
}

func clientJSHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=31536000, immutable")
	http.ServeContent(w, req, "domi.js", time.Time{}, bytes.NewReader(clientJS))
}

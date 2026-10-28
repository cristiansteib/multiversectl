package server

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// webapp is the Angular build. It is produced by `make ui` and is not in the
// repository: only a placeholder is, so that a plain `go build` still works.
//
//go:embed all:webapp
var webappFiles embed.FS

// signInCSS is the stylesheet of the page shown before anyone is signed in: the
// one file served without the credential.
//
//go:embed signin.css
var signInCSS []byte

// serveSignInCSS answers the one request a signed-out browser may make.
func serveSignInCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(signInCSS)
}

// toRoot sends a link to the app's old address to the same place at the root.
func toRoot(w http.ResponseWriter, r *http.Request) {
	to := "/" + strings.TrimPrefix(r.URL.Path, "/app/")
	if r.URL.RawQuery != "" {
		to += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, to, http.StatusPermanentRedirect)
}

// nonceMarker stands where index.html names the nonce Angular puts on the
// style elements it adds at run time.
const nonceMarker = "CSP_NONCE"

// appHandler serves the web app out of fsys and nothing else: a name is
// looked up in that tree and only there, so there is no path to the disk, no
// directory listing and no symlink to follow.
//
// A route of the app (no extension) is answered with index.html, which the
// router reads. A file that is not there is a 404, never index.html: an asset
// that comes back as a page is a script that fails with a syntax error.
func appHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(fsys, "index.html"); err != nil {
			// Not built: say so rather than answer 404 to every request.
			page, _ := fs.ReadFile(fsys, "placeholder.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(page)
			return
		}
		if info, err := fs.Stat(fsys, name); err != nil || info.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		if name == "index.html" {
			serveIndex(w, r, fsys)
			return
		}
		http.ServeFileFS(w, r, fsys, name)
	})
}

// serveIndex answers with index.html and a CSP that lets this response, and
// only this one, add style elements: Angular writes component styles into the
// page as it starts, which `default-src 'self'` would otherwise block.
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	page, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "the app is unavailable", http.StatusInternalServerError)
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "the app is unavailable", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(raw)
	page = bytes.ReplaceAll(page, []byte(nonceMarker), []byte(nonce))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'nonce-"+nonce+"'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}

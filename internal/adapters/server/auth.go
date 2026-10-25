package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

// The page can change things now, so reaching the port is not enough: a
// request has to carry a credential only the person who started the server has.
// The server makes up a token when it starts and prints it in the URL it
// serves; opening that URL trades it for a cookie, and the cookie is what every
// later request, the websocket and every action carries. The cookie is
// SameSite=Strict, so a page on another site cannot make the browser send it,
// and the token never appears in a path that is logged or cached, because the
// first response redirects it away.

const (
	cookieName  = "mvctl"
	tokenParam  = "t"
	csrfHeader  = "X-Mvctl"
	csrfValue   = "1"
	tokenLength = 24
)

// newToken is 192 random bits, URL-safe.
func newToken() string {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		panic("server: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sameToken(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// authorized reports whether the request carries the session cookie.
func authorized(r *http.Request, token string) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && sameToken(c.Value, token)
}

// exchange handles a visit with ?t=<token>: it sets the cookie and sends the
// browser to the same page without the token in its address. It reports
// whether it answered.
func exchange(w http.ResponseWriter, r *http.Request, token string) bool {
	given := r.URL.Query().Get(tokenParam)
	if given == "" {
		return false
	}
	if !sameToken(given, token) {
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	q := r.URL.Query()
	q.Del(tokenParam)
	target := r.URL.Path
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return true
}

// sameOrigin is true when a request that names an Origin names this server.
// Browsers send it on every cross-origin request and on websocket upgrades.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

const unauthorizedPage = `<!doctype html><meta charset="utf-8"><title>multiversectl</title>
<style>body{font:16px system-ui,sans-serif;max-width:34rem;margin:15vh auto;padding:0 1rem;color:#222}code{background:#eee;padding:.1em .3em;border-radius:4px}</style>
<h1>This needs the link</h1>
<p>The page changes things on this machine, so it only opens from the link the server prints when it starts. Get it again with:</p>
<p><code>multiversectl server status</code></p>`

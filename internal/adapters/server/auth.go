package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// bearer reports whether the request carries the token in an Authorization
// header, which is how a program (an agent's MCP client) presents it: it has no
// browser, so no cookie. Unlike the cookie, a header is never sent by the
// browser on its own, so it needs none of the cross-site protection the cookie does.
func bearer(r *http.Request, token string) bool {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return false
	}
	return sameToken(strings.TrimSpace(h[7:]), token)
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

// TokenPath is where the token is kept: beside the configuration, with the
// other files of the server.
func TokenPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "multiversectl", "server.token"), nil
}

// PersistentToken is the token kept at path, made and kept (readable by this
// user only) when there is none. A token that lived only as long as one start
// would end every link, and every tool configured with it, each time the
// server is restarted for an update. Deleting the file is how it is changed.
func PersistentToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t, nil
		}
	}
	token := newToken()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

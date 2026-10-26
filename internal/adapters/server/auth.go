package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
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
	setSession(w, token)
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

// pairPath is where the sign-in form posts: the one address a request without
// the cookie is allowed to reach.
const pairPath = "/api/pair/use"

// signIn is what anyone without the cookie is shown. It offers the two ways
// in: the link, for the machine the server runs on, and a code for one that
// cannot be sent a link — read off the first machine and typed into this one.
//
// The form is a form and not a script because this page is served before any
// of the server's own files have been allowed to load; its own policy allows
// it to post to this server and nothing else. The stylesheet is let through
// unsigned-in, so the page looks like the one it leads to.
func (s *Server) signIn(w http.ResponseWriter, status int, trouble string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	// The rest of the server says no-referrer, under which a browser sends the
	// form with `Origin: null` — and a request from nowhere is one the guard
	// refuses. This page is the server's own address and nothing else, so
	// naming it to itself gives away nothing.
	h.Set("Referrer-Policy", "same-origin")
	w.WriteHeader(status)
	page := signInPage
	if trouble != "" {
		page = strings.Replace(page, "<!--trouble-->", `<p class="error" role="alert">`+template.HTMLEscapeString(trouble)+`</p>`, 1)
	}
	_, _ = w.Write([]byte(page))
}

const signInPage = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in · multiversectl</title>
<link rel="stylesheet" href="/style.css"></head>
<body class="signin">
<main class="card">
<h1>multiversectl</h1>
<p class="lede">This page can start, stop and remove universes, so it opens for one machine at a time.</p>
<!--trouble-->
<form method="post" action="` + pairPath + `">
  <label for="code">Code from the other machine</label>
  <input id="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9 -]*"
         maxlength="9" placeholder="123 456" autofocus required>
  <button type="submit" class="btn primary">Open it</button>
</form>
<p class="muted">On the machine running the server, ask for a code:</p>
<p><code>multiversectl server pair</code></p>
<p class="muted">On that machine itself, the link does the same, and <code>multiversectl server status</code> prints it again.</p>
</main></body></html>`

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

// setSession is what both ways in end with: the link, and a pairing code.
func setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

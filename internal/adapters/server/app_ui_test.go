package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":        {Data: []byte(`<app-root ngCspNonce="CSP_NONCE"></app-root>`)},
		"main-ABC.js":       {Data: []byte("console.log(1)")},
		"assets/logo.svg":   {Data: []byte("<svg/>")},
		"placeholder.html":  {Data: []byte("not built")},
		"secret/inner.json": {Data: []byte("{}")},
	}
}

func serveApp(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestAppServesAssetsAndRoutesButNotMissingFiles(t *testing.T) {
	h := appHandler(built())
	if w := serveApp(h, "GET", "/main-ABC.js"); w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d %q", w.Code, w.Body)
	}
	if w := serveApp(h, "GET", "/universes/3"); w.Code != 200 || !strings.Contains(w.Body.String(), "app-root") {
		t.Fatalf("a route of the app should be index.html: %d %q", w.Code, w.Body)
	}
	if w := serveApp(h, "GET", "/missing.js"); w.Code != 404 {
		t.Fatalf("a missing asset must be a 404, not the page: %d", w.Code)
	}
}

func TestAppNeverListsADirectoryOrLeavesTheTree(t *testing.T) {
	h := appHandler(built())
	if w := serveApp(h, "GET", "/secret/"); strings.Contains(w.Body.String(), "inner.json") {
		t.Fatalf("a directory was listed: %q", w.Body)
	}
	for _, p := range []string{"/../server.go", "/%2e%2e/server.go", "/..%2f..%2fgo.mod", "/a\\..\\b.js"} {
		if w := serveApp(h, "GET", p); w.Code == 200 && !strings.Contains(w.Body.String(), "app-root") {
			t.Fatalf("%s served %q", p, w.Body)
		}
	}
}

func TestAppIsReadOnly(t *testing.T) {
	h := appHandler(built())
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if w := serveApp(h, m, "/"); w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: %d", m, w.Code)
		}
	}
}

func TestIndexGetsItsOwnNonceInThePageAndThePolicy(t *testing.T) {
	h := appHandler(built())
	a, b := serveApp(h, "GET", "/"), serveApp(h, "GET", "/")
	nonce := func(w *httptest.ResponseRecorder) string {
		csp := w.Header().Get("Content-Security-Policy")
		i := strings.Index(csp, "'nonce-")
		if i < 0 {
			t.Fatalf("no nonce in %q", csp)
		}
		n := strings.TrimSuffix(csp[i+len("'nonce-"):strings.Index(csp[i:], "';")+i], "")
		if !strings.Contains(w.Body.String(), `ngCspNonce="`+n+`"`) {
			t.Fatalf("the page does not carry the nonce %q: %q", n, w.Body)
		}
		return n
	}
	if nonce(a) == nonce(b) {
		t.Fatal("the nonce must differ per response")
	}
	if strings.Contains(a.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("the policy must not allow unsafe-inline")
	}
}

func TestAppNotBuiltSaysSo(t *testing.T) {
	h := appHandler(fstest.MapFS{"placeholder.html": {Data: []byte("not built")}})
	if w := serveApp(h, "GET", "/"); w.Code != http.StatusServiceUnavailable || w.Body.String() != "not built" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}
